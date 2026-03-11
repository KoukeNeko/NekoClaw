package core

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"
)

type PlanStatus string

const (
	PlanStatusPendingApproval PlanStatus = "pending_approval"
	PlanStatusApproved        PlanStatus = "approved"
	PlanStatusRejected        PlanStatus = "rejected"
	PlanStatusExecuting       PlanStatus = "executing"
	PlanStatusCompleted       PlanStatus = "completed"
	PlanStatusFailed          PlanStatus = "failed"
	PlanStatusSuperseded      PlanStatus = "superseded"
)

type PlanRecord struct {
	ID                  string     `json:"id"`
	SessionID           string     `json:"session_id"`
	Surface             Surface    `json:"surface"`
	Provider            string     `json:"provider"`
	Model               string     `json:"model"`
	Status              PlanStatus `json:"status"`
	RequestPrompt       string     `json:"request_prompt"`
	PlanMarkdown        string     `json:"plan_markdown"`
	PlanSummary         string     `json:"plan_summary"`
	CreatedAt           time.Time  `json:"created_at"`
	UpdatedAt           time.Time  `json:"updated_at"`
	ApprovedAt          *time.Time `json:"approved_at,omitempty"`
	RejectedAt          *time.Time `json:"rejected_at,omitempty"`
	ExecutionStartedAt  *time.Time `json:"execution_started_at,omitempty"`
	ExecutionFinishedAt *time.Time `json:"execution_finished_at,omitempty"`
	LastError           string     `json:"last_error,omitempty"`
}

type CreatePlanRequest struct {
	SessionID string  `json:"session_id"`
	Surface   Surface `json:"surface"`
	Provider  string  `json:"provider"`
	Model     string  `json:"model"`
	Prompt    string  `json:"prompt"`
}

func IsTerminalPlanStatus(status PlanStatus) bool {
	switch status {
	case PlanStatusRejected, PlanStatusCompleted, PlanStatusFailed, PlanStatusSuperseded:
		return true
	default:
		return false
	}
}

type planIndexFile struct {
	Plans map[string]PlanRecord `json:"plans"`
}

type PlanStore struct {
	mu      sync.Mutex
	dataDir string
	plans   map[string]PlanRecord
}

func NewPlanStore(dataDir string) (*PlanStore, error) {
	store := &PlanStore{
		dataDir: strings.TrimSpace(dataDir),
		plans:   map[string]PlanRecord{},
	}
	if store.dataDir == "" {
		return store, nil
	}
	if err := os.MkdirAll(store.artifactsDir(), 0o755); err != nil {
		return nil, err
	}
	if err := store.loadIndex(); err != nil {
		return nil, err
	}
	return store, nil
}

func (s *PlanStore) Create(record PlanRecord) (PlanRecord, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	now := time.Now().UTC()
	record.ID = strings.TrimSpace(record.ID)
	if record.ID == "" {
		record.ID = NewEntryID()
	}
	record.SessionID = strings.TrimSpace(record.SessionID)
	if record.SessionID == "" {
		record.SessionID = "main"
	}
	record.Provider = strings.TrimSpace(record.Provider)
	if record.Provider == "" {
		record.Provider = "mock"
	}
	record.Model = strings.TrimSpace(record.Model)
	if record.Model == "" {
		record.Model = "default"
	}
	record.RequestPrompt = strings.TrimSpace(record.RequestPrompt)
	record.PlanMarkdown = strings.TrimSpace(record.PlanMarkdown)
	record.PlanSummary = strings.TrimSpace(record.PlanSummary)
	if record.Status == "" {
		record.Status = PlanStatusPendingApproval
	}
	record.CreatedAt = zeroTimeOr(record.CreatedAt, now)
	record.UpdatedAt = now

	for id, existing := range s.plans {
		if existing.SessionID != record.SessionID || IsTerminalPlanStatus(existing.Status) {
			continue
		}
		existing.Status = PlanStatusSuperseded
		existing.UpdatedAt = now
		existing.ExecutionFinishedAt = nil
		existing.LastError = ""
		s.plans[id] = existing
	}

	s.plans[record.ID] = record
	if err := s.persistLocked(record); err != nil {
		return PlanRecord{}, err
	}
	return clonePlanRecord(record), nil
}

func (s *PlanStore) Update(record PlanRecord) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	record.ID = strings.TrimSpace(record.ID)
	if record.ID == "" {
		return errors.New("plan id is required")
	}
	record.PlanMarkdown = strings.TrimSpace(record.PlanMarkdown)
	record.PlanSummary = strings.TrimSpace(record.PlanSummary)
	record.UpdatedAt = time.Now().UTC()
	s.plans[record.ID] = record
	return s.persistLocked(record)
}

func (s *PlanStore) Get(planID string) (PlanRecord, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()

	record, ok := s.plans[strings.TrimSpace(planID)]
	if !ok {
		return PlanRecord{}, false
	}
	return clonePlanRecord(record), true
}

func (s *PlanStore) List(sessionID string) []PlanRecord {
	s.mu.Lock()
	defer s.mu.Unlock()

	sessionID = strings.TrimSpace(sessionID)
	plans := make([]PlanRecord, 0, len(s.plans))
	for _, plan := range s.plans {
		if sessionID != "" && plan.SessionID != sessionID {
			continue
		}
		plans = append(plans, clonePlanRecord(plan))
	}
	sort.Slice(plans, func(i, j int) bool {
		if plans[i].CreatedAt.Equal(plans[j].CreatedAt) {
			return plans[i].ID > plans[j].ID
		}
		return plans[i].CreatedAt.After(plans[j].CreatedAt)
	})
	return plans
}

func (s *PlanStore) loadIndex() error {
	path := s.indexPath()
	content, err := os.ReadFile(path)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return nil
		}
		return err
	}
	if len(strings.TrimSpace(string(content))) == 0 {
		return nil
	}

	var idx planIndexFile
	if err := json.Unmarshal(content, &idx); err != nil {
		return err
	}
	if idx.Plans == nil {
		return nil
	}

	for id, record := range idx.Plans {
		if record.ID == "" {
			record.ID = id
		}
		if record.PlanMarkdown == "" && s.dataDir != "" {
			if artifact, err := os.ReadFile(s.artifactPath(record.ID)); err == nil {
				record.PlanMarkdown = string(artifact)
			}
		}
		s.plans[record.ID] = record
	}
	return nil
}

func (s *PlanStore) persistLocked(record PlanRecord) error {
	if s.dataDir == "" {
		return nil
	}
	if err := os.MkdirAll(s.artifactsDir(), 0o755); err != nil {
		return err
	}
	if err := writeFileAtomically(s.artifactPath(record.ID), []byte(record.PlanMarkdown), 0o644); err != nil {
		return err
	}
	idx := planIndexFile{Plans: s.plans}
	data, err := json.MarshalIndent(idx, "", "  ")
	if err != nil {
		return err
	}
	return writeFileAtomically(s.indexPath(), append(data, '\n'), 0o644)
}

func (s *PlanStore) indexPath() string {
	return filepath.Join(s.dataDir, "index.json")
}

func (s *PlanStore) artifactsDir() string {
	return filepath.Join(s.dataDir, "artifacts")
}

func (s *PlanStore) artifactPath(planID string) string {
	return filepath.Join(s.artifactsDir(), planID+".md")
}

func clonePlanRecord(record PlanRecord) PlanRecord {
	clone := record
	if record.ApprovedAt != nil {
		ts := *record.ApprovedAt
		clone.ApprovedAt = &ts
	}
	if record.RejectedAt != nil {
		ts := *record.RejectedAt
		clone.RejectedAt = &ts
	}
	if record.ExecutionStartedAt != nil {
		ts := *record.ExecutionStartedAt
		clone.ExecutionStartedAt = &ts
	}
	if record.ExecutionFinishedAt != nil {
		ts := *record.ExecutionFinishedAt
		clone.ExecutionFinishedAt = &ts
	}
	return clone
}

func zeroTimeOr(ts time.Time, fallback time.Time) time.Time {
	if ts.IsZero() {
		return fallback
	}
	return ts
}

func writeFileAtomically(path string, content []byte, mode os.FileMode) error {
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return err
	}
	tmp, err := os.CreateTemp(dir, ".tmp-*")
	if err != nil {
		return err
	}
	tmpPath := tmp.Name()
	defer os.Remove(tmpPath)
	if _, err := tmp.Write(content); err != nil {
		_ = tmp.Close()
		return err
	}
	if err := tmp.Chmod(mode); err != nil {
		_ = tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	return os.Rename(tmpPath, path)
}
