package tooling

import (
	"errors"
	"fmt"
	"strings"
	"sync"
	"time"

	"github.com/doeshing/nekoclaw/internal/core"
	"github.com/doeshing/nekoclaw/internal/provider"
)

var ErrRunNotFound = errors.New("tool run not found")
var ErrRunExpired = errors.New("tool run expired")
var ErrRunInvalid = errors.New("tool run invalid")

type PendingRun struct {
	RunID          string
	CreatedAt      time.Time
	ExpiresAt      time.Time
	SessionID      string
	Surface        core.Surface
	ProviderID     string
	ModelID        string
	Account        core.Account
	Messages       []core.Message
	Compressed     bool
	Compression    core.CompressionMeta
	Usage          core.UsageInfo
	PendingCalls   []provider.ToolCall
	PendingEvents  []core.ToolEvent
	PendingMessage []core.Message
}

type ApprovalStore struct {
	mu   sync.Mutex
	ttl  time.Duration
	runs map[string]PendingRun
}

func NewApprovalStore(ttl time.Duration) *ApprovalStore {
	if ttl <= 0 {
		ttl = 10 * time.Minute
	}
	return &ApprovalStore{
		ttl:  ttl,
		runs: map[string]PendingRun{},
	}
}

func (s *ApprovalStore) Put(run PendingRun) PendingRun {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.gcLocked(time.Now())
	if strings.TrimSpace(run.RunID) == "" {
		run.RunID = fmt.Sprintf("run-%d", time.Now().UnixNano())
	}
	now := time.Now()
	run.CreatedAt = now
	run.ExpiresAt = now.Add(s.ttl)
	run.Messages = append([]core.Message(nil), run.Messages...)
	run.PendingCalls = append([]provider.ToolCall(nil), run.PendingCalls...)
	run.PendingEvents = append([]core.ToolEvent(nil), run.PendingEvents...)
	run.PendingMessage = append([]core.Message(nil), run.PendingMessage...)
	s.runs[run.RunID] = run
	return run
}

func (s *ApprovalStore) Get(runID string) (PendingRun, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.gcLocked(time.Now())
	runID = strings.TrimSpace(runID)
	if runID == "" {
		return PendingRun{}, fmt.Errorf("%w: missing run_id", ErrRunInvalid)
	}
	run, ok := s.runs[runID]
	if !ok {
		return PendingRun{}, ErrRunNotFound
	}
	if time.Now().After(run.ExpiresAt) {
		delete(s.runs, runID)
		return PendingRun{}, ErrRunExpired
	}
	return run, nil
}

func (s *ApprovalStore) Delete(runID string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	delete(s.runs, strings.TrimSpace(runID))
}

func (s *ApprovalStore) gcLocked(now time.Time) {
	for id, run := range s.runs {
		if now.After(run.ExpiresAt) {
			delete(s.runs, id)
		}
	}
}
