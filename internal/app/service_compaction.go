package app

import (
	"context"
	"fmt"
	"strings"

	"github.com/doeshing/nekoclaw/internal/compaction"
	"github.com/doeshing/nekoclaw/internal/core"
	"github.com/doeshing/nekoclaw/internal/provider"
)

const (
	minCompactionTriggerRatio   = 0.50
	maxCompactionTriggerRatio   = 0.95
	minCompactionKeepRecent     = 1024
	defaultCompactionCtxWindow  = 128_000
	defaultCompactionFileFactor = 1.0
)

// GetCompactionConfig returns a copy of the current compaction configuration.
func (s *Service) GetCompactionConfig() core.CompactionConfig {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.compactionConfig
}

// SaveCompactionConfig persists compaction settings to config.json and updates in-memory state.
func (s *Service) SaveCompactionConfig(cfg core.CompactionConfig) error {
	cfg = normalizeCompactionConfig(cfg)
	if err := validateCompactionConfig(cfg); err != nil {
		return err
	}

	s.mu.Lock()
	configDir := s.configDir
	s.compactionConfig = cfg
	s.mu.Unlock()

	appCfg, _ := core.LoadConfig(configDir)
	appCfg.Compaction = cfg
	return core.SaveConfig(configDir, appCfg)
}

// SetCompactionConfig sets the in-memory compaction config (used during startup).
func (s *Service) SetCompactionConfig(cfg core.CompactionConfig) {
	s.mu.Lock()
	defer s.mu.Unlock()
	cfg = normalizeCompactionConfig(cfg)
	if err := validateCompactionConfig(cfg); err != nil {
		s.compactionConfig = core.DefaultCompactionConfig()
		return
	}
	s.compactionConfig = cfg
}

// StartBackgroundCompaction launches the single background compaction worker.
func (s *Service) StartBackgroundCompaction(ctx context.Context) {
	s.compactionStart.Do(func() {
		if s.sessions == nil || s.sessions.TranscriptsDir() == "" {
			return
		}
		s.compactionQueueMu.Lock()
		s.compactionActive = true
		s.compactionQueueMu.Unlock()
		go s.runCompactionWorker(ctx)
		go s.enqueueStartupCompactionCandidates(ctx)
	})
}

func normalizeCompactionConfig(cfg core.CompactionConfig) core.CompactionConfig {
	return core.DefaultCompactionConfig().ApplyDefaults(cfg)
}

func validateCompactionConfig(cfg core.CompactionConfig) error {
	switch {
	case cfg.TriggerThresholdRatio < minCompactionTriggerRatio || cfg.TriggerThresholdRatio > maxCompactionTriggerRatio:
		return fmt.Errorf("%w: trigger_threshold_ratio must be between %.2f and %.2f", ErrInvalidCompactionConfig, minCompactionTriggerRatio, maxCompactionTriggerRatio)
	case cfg.KeepRecentTokens < minCompactionKeepRecent:
		return fmt.Errorf("%w: keep_recent_tokens must be at least %d", ErrInvalidCompactionConfig, minCompactionKeepRecent)
	default:
		return nil
	}
}

func (s *Service) appendSessionEntries(sessionID string, contextWindow int, entries []core.SessionEntry) {
	if s.sessions == nil || len(entries) == 0 {
		return
	}
	s.sessions.Append(sessionID, entries...)
	if s.searchIndex != nil {
		indexEntries := append([]core.SessionEntry(nil), entries...)
		go func() {
			if idxErr := s.searchIndex.Index(sessionID, indexEntries); idxErr != nil {
				logService.Errorf("search index: session_id=%s error=%q", sessionID, idxErr)
			}
		}()
	}
	s.maybeEnqueueSessionCompaction(sessionID, contextWindow)
}

func (s *Service) maybeEnqueueSessionCompaction(sessionID string, contextWindow int) {
	cfg := s.GetCompactionConfig()
	if !cfg.Enabled || !cfg.BackgroundEnabled || strings.TrimSpace(sessionID) == "" {
		return
	}
	if s.sessions == nil || s.sessions.TranscriptsDir() == "" {
		return
	}
	s.compactionQueueMu.Lock()
	active := s.compactionActive
	s.compactionQueueMu.Unlock()
	if !active {
		return
	}
	if !s.shouldQueueSessionCompaction(sessionID, contextWindow, cfg) {
		return
	}
	s.enqueueSessionCompaction(sessionID)
}

func (s *Service) shouldQueueSessionCompaction(sessionID string, contextWindow int, cfg core.CompactionConfig) bool {
	meta, ok := s.sessions.GetMetadata(sessionID)
	if !ok {
		return false
	}
	if threshold := compactionTokenThreshold(cfg, contextWindow); threshold > 0 && meta.ContextTokens >= threshold {
		return true
	}
	fileThreshold := s.compactionFileThreshold(cfg)
	if fileThreshold > 0 && s.sessions.TranscriptFileSize(sessionID) >= fileThreshold {
		return true
	}
	return false
}

func compactionTokenThreshold(cfg core.CompactionConfig, contextWindow int) int {
	if contextWindow <= 0 {
		contextWindow = defaultCompactionCtxWindow
	}
	return int(float64(contextWindow) * cfg.TriggerThresholdRatio)
}

func (s *Service) compactionFileThreshold(cfg core.CompactionConfig) int64 {
	maxFileSize := int64(core.DefaultLifecycleConfig().MaxFileSize)
	if s.lifecycle != nil {
		maxFileSize = s.lifecycle.Config().MaxFileSize
	}
	return int64(float64(maxFileSize) * cfg.TriggerThresholdRatio * defaultCompactionFileFactor)
}

func (s *Service) enqueueSessionCompaction(sessionID string) {
	sessionID = strings.TrimSpace(sessionID)
	if sessionID == "" {
		return
	}
	s.compactionQueueMu.Lock()
	if _, ok := s.compactionQueued[sessionID]; ok {
		s.compactionQueueMu.Unlock()
		return
	}
	if _, ok := s.compactionRunning[sessionID]; ok {
		s.compactionQueueMu.Unlock()
		return
	}
	s.compactionQueued[sessionID] = struct{}{}
	s.compactionQueueMu.Unlock()
	s.compactionQueue <- sessionID
}

func (s *Service) runCompactionWorker(ctx context.Context) {
	for {
		select {
		case <-ctx.Done():
			return
		case sessionID := <-s.compactionQueue:
			s.markCompactionRunning(sessionID)
			s.compactSessionInBackground(ctx, sessionID)
			s.markCompactionDone(sessionID)
		}
	}
}

func (s *Service) enqueueStartupCompactionCandidates(ctx context.Context) {
	contextWindow := s.backgroundCatchupContextWindow()
	cfg := s.GetCompactionConfig()
	for _, session := range s.ListSessions() {
		select {
		case <-ctx.Done():
			return
		default:
		}
		if s.shouldQueueSessionCompaction(session.SessionID, contextWindow, cfg) {
			s.enqueueSessionCompaction(session.SessionID)
		}
	}
}

func (s *Service) backgroundCatchupContextWindow() int {
	providerID := s.GetDefaultProvider()
	modelID := s.GetDefaultModel()
	if providerID == "" {
		return defaultCompactionCtxWindow
	}
	prov := s.Provider(providerID)
	if prov == nil {
		return defaultCompactionCtxWindow
	}
	if cw := prov.ContextWindow(modelID); cw > 0 {
		return cw
	}
	return defaultCompactionCtxWindow
}

func (s *Service) markCompactionRunning(sessionID string) {
	s.compactionQueueMu.Lock()
	delete(s.compactionQueued, sessionID)
	s.compactionRunning[sessionID] = struct{}{}
	s.compactionQueueMu.Unlock()
}

func (s *Service) markCompactionDone(sessionID string) {
	s.compactionQueueMu.Lock()
	delete(s.compactionRunning, sessionID)
	s.compactionQueueMu.Unlock()
}

func (s *Service) compactSessionInBackground(ctx context.Context, sessionID string) {
	cfg := s.GetCompactionConfig()
	if !cfg.Enabled || !cfg.BackgroundEnabled || s.sessions == nil {
		return
	}

	entries := s.sessions.History(sessionID)
	if len(entries) == 0 {
		return
	}
	header, compactable := splitCompactionHeader(entries)
	if len(compactable) == 0 {
		return
	}

	compactionProv, modelID, account, contextWindow, allowLLM := resolveCompactionRuntime(s, compactable, cfg)
	if !s.shouldCompactLoadedSession(sessionID, compactable, contextWindow, cfg) {
		return
	}

	compactor := compaction.NewCompactor(compactionProv, modelID, account)
	result, err := compactor.Compact(ctx, compaction.CompactionRequest{
		Entries:          compactable,
		ContextWindow:    contextWindow,
		ReserveTokens:    compaction.DefaultReserveTokens,
		KeepRecentTokens: cfg.KeepRecentTokens,
		AllowLLMSummary:  allowLLM,
	})
	if err != nil {
		logService.Warnf("background compaction failed: session_id=%s error=%q", sessionID, err)
		return
	}
	if result.DroppedCount == 0 {
		return
	}

	rewritten := make([]core.SessionEntry, 0, len(result.KeptEntries)+2)
	if header != nil {
		rewritten = append(rewritten, *header)
	}
	rewritten = append(rewritten, result.CompactionEntry)
	rewritten = append(rewritten, result.KeptEntries...)

	if err := s.sessions.RewriteSession(sessionID, rewritten); err != nil {
		logService.Errorf("background compaction rewrite: session_id=%s error=%q", sessionID, err)
		return
	}
	if s.searchIndex != nil {
		if err := s.searchIndex.DeleteSession(sessionID); err != nil {
			logService.Warnf("background compaction delete index: session_id=%s error=%q", sessionID, err)
		}
		if err := s.searchIndex.Index(sessionID, rewritten); err != nil {
			logService.Warnf("background compaction reindex: session_id=%s error=%q", sessionID, err)
		}
	}
	logService.Logf("background compaction complete: session_id=%s dropped=%d", sessionID, result.DroppedCount)
}

func (s *Service) shouldCompactLoadedSession(sessionID string, entries []core.SessionEntry, contextWindow int, cfg core.CompactionConfig) bool {
	if threshold := compactionTokenThreshold(cfg, contextWindow); threshold > 0 && core.EstimateSessionEntriesTokens(entries) >= threshold {
		return true
	}
	return s.sessions.TranscriptFileSize(sessionID) >= s.compactionFileThreshold(cfg)
}

func resolveCompactionRuntime(s *Service, entries []core.SessionEntry, cfg core.CompactionConfig) (provider.Provider, string, core.Account, int, bool) {
	providerID := strings.TrimSpace(s.GetDefaultProvider())
	modelID := strings.TrimSpace(s.GetDefaultModel())
	for i := len(entries) - 1; i >= 0; i-- {
		entry := entries[i]
		if entry.Type != core.EntryMessage || entry.Role != core.RoleAssistant {
			continue
		}
		if providerID == "" && strings.TrimSpace(entry.MsgProvider) != "" {
			providerID = strings.TrimSpace(entry.MsgProvider)
		}
		if modelID == "" && strings.TrimSpace(entry.MsgModel) != "" {
			modelID = strings.TrimSpace(entry.MsgModel)
		}
		if providerID != "" && modelID != "" {
			break
		}
	}
	if providerID == "" {
		providerID = "google-gemini-cli"
	}
	if modelID == "" {
		modelID = "default"
	}

	prov := s.Provider(providerID)
	contextWindow := defaultCompactionCtxWindow
	if prov != nil {
		if cw := prov.ContextWindow(modelID); cw > 0 {
			contextWindow = cw
		}
	}

	if !cfg.LLMSummaryEnabled || prov == nil {
		return nil, modelID, core.Account{}, contextWindow, false
	}
	pool := s.Pool(providerID)
	if pool == nil {
		return nil, modelID, core.Account{}, contextWindow, false
	}
	account, ok := pool.Acquire(s.preferredProfile(providerID))
	if !ok {
		return nil, modelID, core.Account{}, contextWindow, false
	}
	return prov, modelID, account, contextWindow, true
}

func splitCompactionHeader(entries []core.SessionEntry) (*core.SessionEntry, []core.SessionEntry) {
	if len(entries) == 0 {
		return nil, nil
	}
	if entries[0].Type != core.EntrySession {
		return nil, append([]core.SessionEntry(nil), entries...)
	}
	header := entries[0]
	return &header, append([]core.SessionEntry(nil), entries[1:]...)
}
