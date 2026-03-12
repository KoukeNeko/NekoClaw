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

type PlaybookKind string
type PlaybookScope string
type PlaybookStatus string

const (
	PlaybookKindInstruction PlaybookKind = "instruction"
	PlaybookKindPreference  PlaybookKind = "preference"
	PlaybookKindWorkflow    PlaybookKind = "workflow"

	PlaybookScopeGlobal    PlaybookScope = "global"
	PlaybookScopeWorkspace PlaybookScope = "workspace"
	PlaybookScopePersona   PlaybookScope = "persona"
	PlaybookScopeSurface   PlaybookScope = "surface"
	PlaybookScopeSession   PlaybookScope = "session"

	PlaybookStatusCandidate PlaybookStatus = "candidate"
	PlaybookStatusActive    PlaybookStatus = "active"
	PlaybookStatusArchived  PlaybookStatus = "archived"
)

type PlaybookEntry struct {
	ID           string         `json:"id"`
	Kind         PlaybookKind   `json:"kind"`
	Scope        PlaybookScope  `json:"scope"`
	ScopeValue   string         `json:"scope_value,omitempty"`
	Status       PlaybookStatus `json:"status"`
	Content      string         `json:"content"`
	Source       string         `json:"source,omitempty"`
	HelpfulCount int            `json:"helpful_count"`
	HarmfulCount int            `json:"harmful_count"`
	CreatedAt    time.Time      `json:"created_at"`
	UpdatedAt    time.Time      `json:"updated_at"`
	ApprovedAt   *time.Time     `json:"approved_at,omitempty"`
}

type PlaybookQuery struct {
	WorkspaceRoot string
	Persona       string
	Surface       Surface
	SessionID     string
	IncludeStatus []PlaybookStatus
}

type playbookIndexFile struct {
	Entries map[string]PlaybookEntry `json:"entries"`
}

type PlaybookStore struct {
	mu      sync.Mutex
	dataDir string
	entries map[string]PlaybookEntry
}

func NewPlaybookStore(dataDir string) (*PlaybookStore, error) {
	store := &PlaybookStore{
		dataDir: strings.TrimSpace(dataDir),
		entries: map[string]PlaybookEntry{},
	}
	if store.dataDir == "" {
		return store, nil
	}
	if err := os.MkdirAll(store.dataDir, 0o755); err != nil {
		return nil, err
	}
	if err := store.load(); err != nil {
		return nil, err
	}
	return store, nil
}

func (s *PlaybookStore) Create(entry PlaybookEntry) (PlaybookEntry, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	now := time.Now().UTC()
	entry.ID = strings.TrimSpace(entry.ID)
	if entry.ID == "" {
		entry.ID = NewEntryID()
	}
	entry.Kind = normalizePlaybookKind(entry.Kind)
	entry.Scope = normalizePlaybookScope(entry.Scope)
	entry.ScopeValue = strings.TrimSpace(entry.ScopeValue)
	entry.Status = normalizePlaybookStatus(entry.Status)
	entry.Content = strings.TrimSpace(entry.Content)
	entry.Source = strings.TrimSpace(entry.Source)
	entry.CreatedAt = zeroTimeOr(entry.CreatedAt, now)
	entry.UpdatedAt = now
	s.entries[entry.ID] = entry
	return clonePlaybookEntry(entry), s.persistLocked()
}

func (s *PlaybookStore) Update(entry PlaybookEntry) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	entry.ID = strings.TrimSpace(entry.ID)
	if entry.ID == "" {
		return errors.New("playbook id is required")
	}
	entry.Kind = normalizePlaybookKind(entry.Kind)
	entry.Scope = normalizePlaybookScope(entry.Scope)
	entry.ScopeValue = strings.TrimSpace(entry.ScopeValue)
	entry.Status = normalizePlaybookStatus(entry.Status)
	entry.Content = strings.TrimSpace(entry.Content)
	entry.Source = strings.TrimSpace(entry.Source)
	entry.UpdatedAt = time.Now().UTC()
	s.entries[entry.ID] = entry
	return s.persistLocked()
}

func (s *PlaybookStore) Get(id string) (PlaybookEntry, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	entry, ok := s.entries[strings.TrimSpace(id)]
	if !ok {
		return PlaybookEntry{}, false
	}
	return clonePlaybookEntry(entry), true
}

func (s *PlaybookStore) List(statuses ...PlaybookStatus) []PlaybookEntry {
	s.mu.Lock()
	defer s.mu.Unlock()
	allow := map[PlaybookStatus]struct{}{}
	for _, status := range statuses {
		allow[normalizePlaybookStatus(status)] = struct{}{}
	}
	out := make([]PlaybookEntry, 0, len(s.entries))
	for _, entry := range s.entries {
		if len(allow) > 0 {
			if _, ok := allow[entry.Status]; !ok {
				continue
			}
		}
		out = append(out, clonePlaybookEntry(entry))
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].UpdatedAt.Equal(out[j].UpdatedAt) {
			return out[i].ID > out[j].ID
		}
		return out[i].UpdatedAt.After(out[j].UpdatedAt)
	})
	return out
}

func (s *PlaybookStore) Active(query PlaybookQuery, limit int) []PlaybookEntry {
	s.mu.Lock()
	defer s.mu.Unlock()

	allowed := map[PlaybookStatus]struct{}{PlaybookStatusActive: {}}
	if len(query.IncludeStatus) > 0 {
		allowed = map[PlaybookStatus]struct{}{}
		for _, status := range query.IncludeStatus {
			allowed[normalizePlaybookStatus(status)] = struct{}{}
		}
	}

	type scoredEntry struct {
		entry PlaybookEntry
		score int
	}
	candidates := make([]scoredEntry, 0, len(s.entries))
	for _, entry := range s.entries {
		if _, ok := allowed[entry.Status]; !ok {
			continue
		}
		if !playbookMatchesQuery(entry, query) {
			continue
		}
		score := playbookSpecificity(entry.Scope)
		score = score*1000 + entry.HelpfulCount - entry.HarmfulCount
		score = score*1000 + int(entry.UpdatedAt.Unix())
		candidates = append(candidates, scoredEntry{
			entry: clonePlaybookEntry(entry),
			score: score,
		})
	}
	sort.Slice(candidates, func(i, j int) bool {
		if candidates[i].score == candidates[j].score {
			return candidates[i].entry.ID > candidates[j].entry.ID
		}
		return candidates[i].score > candidates[j].score
	})
	if limit <= 0 || limit > len(candidates) {
		limit = len(candidates)
	}
	out := make([]PlaybookEntry, 0, limit)
	for _, candidate := range candidates[:limit] {
		out = append(out, candidate.entry)
	}
	return out
}

func (s *PlaybookStore) load() error {
	content, err := os.ReadFile(filepath.Join(s.dataDir, "index.json"))
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return nil
		}
		return err
	}
	if len(strings.TrimSpace(string(content))) == 0 {
		return nil
	}
	var idx playbookIndexFile
	if err := json.Unmarshal(content, &idx); err != nil {
		return err
	}
	for id, entry := range idx.Entries {
		if entry.ID == "" {
			entry.ID = id
		}
		s.entries[entry.ID] = entry
	}
	return nil
}

func (s *PlaybookStore) persistLocked() error {
	if s.dataDir == "" {
		return nil
	}
	idx := playbookIndexFile{Entries: s.entries}
	data, err := json.MarshalIndent(idx, "", "  ")
	if err != nil {
		return err
	}
	return writeFileAtomically(filepath.Join(s.dataDir, "index.json"), append(data, '\n'), 0o644)
}

func clonePlaybookEntry(entry PlaybookEntry) PlaybookEntry {
	clone := entry
	if entry.ApprovedAt != nil {
		ts := *entry.ApprovedAt
		clone.ApprovedAt = &ts
	}
	return clone
}

func playbookMatchesQuery(entry PlaybookEntry, query PlaybookQuery) bool {
	switch entry.Scope {
	case PlaybookScopeGlobal:
		return true
	case PlaybookScopeWorkspace:
		return strings.TrimSpace(entry.ScopeValue) != "" && strings.TrimSpace(entry.ScopeValue) == strings.TrimSpace(query.WorkspaceRoot)
	case PlaybookScopePersona:
		return strings.TrimSpace(entry.ScopeValue) != "" && strings.TrimSpace(entry.ScopeValue) == strings.TrimSpace(query.Persona)
	case PlaybookScopeSurface:
		return strings.TrimSpace(entry.ScopeValue) != "" && strings.TrimSpace(entry.ScopeValue) == strings.TrimSpace(string(query.Surface))
	case PlaybookScopeSession:
		return strings.TrimSpace(entry.ScopeValue) != "" && strings.TrimSpace(entry.ScopeValue) == strings.TrimSpace(query.SessionID)
	default:
		return false
	}
}

func playbookSpecificity(scope PlaybookScope) int {
	switch scope {
	case PlaybookScopeSession:
		return 5
	case PlaybookScopePersona:
		return 4
	case PlaybookScopeWorkspace:
		return 3
	case PlaybookScopeSurface:
		return 2
	default:
		return 1
	}
}

func normalizePlaybookKind(kind PlaybookKind) PlaybookKind {
	switch kind {
	case PlaybookKindInstruction, PlaybookKindPreference, PlaybookKindWorkflow:
		return kind
	default:
		return PlaybookKindInstruction
	}
}

func normalizePlaybookScope(scope PlaybookScope) PlaybookScope {
	switch scope {
	case PlaybookScopeWorkspace, PlaybookScopePersona, PlaybookScopeSurface, PlaybookScopeSession:
		return scope
	default:
		return PlaybookScopeGlobal
	}
}

func normalizePlaybookStatus(status PlaybookStatus) PlaybookStatus {
	switch status {
	case PlaybookStatusActive, PlaybookStatusArchived:
		return status
	default:
		return PlaybookStatusCandidate
	}
}

type TaskStatus string

const (
	TaskStatusPending    TaskStatus = "pending"
	TaskStatusInProgress TaskStatus = "in_progress"
	TaskStatusCompleted  TaskStatus = "completed"
	TaskStatusBlocked    TaskStatus = "blocked"
)

type TaskItem struct {
	ID        string     `json:"id"`
	Content   string     `json:"content"`
	Status    TaskStatus `json:"status"`
	Order     int        `json:"order"`
	UpdatedAt time.Time  `json:"updated_at"`
}

type TaskList struct {
	SessionID string     `json:"session_id"`
	PlanID    string     `json:"plan_id,omitempty"`
	Items     []TaskItem `json:"items"`
	UpdatedAt time.Time  `json:"updated_at"`
}

type taskIndexFile struct {
	Tasks map[string]TaskList `json:"tasks"`
}

type TaskStore struct {
	mu      sync.Mutex
	dataDir string
	lists   map[string]TaskList
}

func NewTaskStore(dataDir string) (*TaskStore, error) {
	store := &TaskStore{
		dataDir: strings.TrimSpace(dataDir),
		lists:   map[string]TaskList{},
	}
	if store.dataDir == "" {
		return store, nil
	}
	if err := os.MkdirAll(store.dataDir, 0o755); err != nil {
		return nil, err
	}
	if err := store.load(); err != nil {
		return nil, err
	}
	return store, nil
}

func (s *TaskStore) Get(sessionID string) TaskList {
	s.mu.Lock()
	defer s.mu.Unlock()
	list := s.lists[strings.TrimSpace(sessionID)]
	return cloneTaskList(list)
}

func (s *TaskStore) Put(list TaskList) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	sessionID := strings.TrimSpace(list.SessionID)
	if sessionID == "" {
		return errors.New("task session_id is required")
	}
	now := time.Now().UTC()
	list.SessionID = sessionID
	list.PlanID = strings.TrimSpace(list.PlanID)
	list.UpdatedAt = now
	items := make([]TaskItem, 0, len(list.Items))
	for i, item := range list.Items {
		item.ID = strings.TrimSpace(item.ID)
		if item.ID == "" {
			item.ID = NewEntryID()
		}
		item.Content = strings.TrimSpace(item.Content)
		if item.Content == "" {
			continue
		}
		item.Status = normalizeTaskStatus(item.Status)
		item.Order = i
		item.UpdatedAt = now
		items = append(items, item)
	}
	list.Items = items
	s.lists[sessionID] = list
	return s.persistLocked()
}

func (s *TaskStore) load() error {
	content, err := os.ReadFile(filepath.Join(s.dataDir, "index.json"))
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return nil
		}
		return err
	}
	if len(strings.TrimSpace(string(content))) == 0 {
		return nil
	}
	var idx taskIndexFile
	if err := json.Unmarshal(content, &idx); err != nil {
		return err
	}
	for sessionID, list := range idx.Tasks {
		if list.SessionID == "" {
			list.SessionID = sessionID
		}
		s.lists[list.SessionID] = list
	}
	return nil
}

func (s *TaskStore) persistLocked() error {
	if s.dataDir == "" {
		return nil
	}
	idx := taskIndexFile{Tasks: s.lists}
	data, err := json.MarshalIndent(idx, "", "  ")
	if err != nil {
		return err
	}
	return writeFileAtomically(filepath.Join(s.dataDir, "index.json"), append(data, '\n'), 0o644)
}

func cloneTaskList(list TaskList) TaskList {
	clone := list
	if len(list.Items) > 0 {
		clone.Items = append([]TaskItem(nil), list.Items...)
	}
	return clone
}

func normalizeTaskStatus(status TaskStatus) TaskStatus {
	switch status {
	case TaskStatusInProgress, TaskStatusCompleted, TaskStatusBlocked:
		return status
	default:
		return TaskStatusPending
	}
}

type PermissionGrant struct {
	ID            string     `json:"id"`
	WorkspaceRoot string     `json:"workspace_root"`
	ToolName      string     `json:"tool_name"`
	Selector      string     `json:"selector,omitempty"`
	Decision      string     `json:"decision"`
	CreatedAt     time.Time  `json:"created_at"`
	ExpiresAt     *time.Time `json:"expires_at,omitempty"`
}

type permissionIndexFile struct {
	Grants map[string]PermissionGrant `json:"grants"`
}

type PermissionStore struct {
	mu      sync.Mutex
	dataDir string
	grants  map[string]PermissionGrant
}

func NewPermissionStore(dataDir string) (*PermissionStore, error) {
	store := &PermissionStore{
		dataDir: strings.TrimSpace(dataDir),
		grants:  map[string]PermissionGrant{},
	}
	if store.dataDir == "" {
		return store, nil
	}
	if err := os.MkdirAll(store.dataDir, 0o755); err != nil {
		return nil, err
	}
	if err := store.load(); err != nil {
		return nil, err
	}
	return store, nil
}

func (s *PermissionStore) List() []PermissionGrant {
	s.mu.Lock()
	defer s.mu.Unlock()
	out := make([]PermissionGrant, 0, len(s.grants))
	now := time.Now().UTC()
	for _, grant := range s.grants {
		if grant.ExpiresAt != nil && now.After(*grant.ExpiresAt) {
			continue
		}
		out = append(out, clonePermissionGrant(grant))
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].CreatedAt.Equal(out[j].CreatedAt) {
			return out[i].ID > out[j].ID
		}
		return out[i].CreatedAt.After(out[j].CreatedAt)
	})
	return out
}

func (s *PermissionStore) Grant(grant PermissionGrant) (PermissionGrant, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	now := time.Now().UTC()
	grant.ID = strings.TrimSpace(grant.ID)
	if grant.ID == "" {
		grant.ID = NewEntryID()
	}
	grant.WorkspaceRoot = strings.TrimSpace(grant.WorkspaceRoot)
	grant.ToolName = strings.TrimSpace(grant.ToolName)
	grant.Selector = strings.TrimSpace(grant.Selector)
	grant.Decision = strings.TrimSpace(grant.Decision)
	if grant.Decision == "" {
		grant.Decision = "allow_for_workspace"
	}
	grant.CreatedAt = zeroTimeOr(grant.CreatedAt, now)
	s.grants[grant.ID] = grant
	return clonePermissionGrant(grant), s.persistLocked()
}

func (s *PermissionStore) Replace(grants []PermissionGrant) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	next := make(map[string]PermissionGrant, len(grants))
	for _, grant := range grants {
		grant.ID = strings.TrimSpace(grant.ID)
		if grant.ID == "" {
			grant.ID = NewEntryID()
		}
		grant.WorkspaceRoot = strings.TrimSpace(grant.WorkspaceRoot)
		grant.ToolName = strings.TrimSpace(grant.ToolName)
		grant.Selector = strings.TrimSpace(grant.Selector)
		grant.Decision = strings.TrimSpace(grant.Decision)
		grant.CreatedAt = zeroTimeOr(grant.CreatedAt, time.Now().UTC())
		next[grant.ID] = grant
	}
	s.grants = next
	return s.persistLocked()
}

func (s *PermissionStore) Match(workspaceRoot, toolName, selector string) []PermissionGrant {
	s.mu.Lock()
	defer s.mu.Unlock()
	workspaceRoot = strings.TrimSpace(workspaceRoot)
	toolName = strings.TrimSpace(toolName)
	selector = strings.TrimSpace(selector)
	now := time.Now().UTC()
	out := make([]PermissionGrant, 0, len(s.grants))
	for _, grant := range s.grants {
		if grant.ExpiresAt != nil && now.After(*grant.ExpiresAt) {
			continue
		}
		if strings.TrimSpace(grant.WorkspaceRoot) != workspaceRoot || strings.TrimSpace(grant.ToolName) != toolName {
			continue
		}
		if grant.Selector != "" && selector != "" && grant.Selector != selector {
			continue
		}
		out = append(out, clonePermissionGrant(grant))
	}
	return out
}

func (s *PermissionStore) load() error {
	content, err := os.ReadFile(filepath.Join(s.dataDir, "index.json"))
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return nil
		}
		return err
	}
	if len(strings.TrimSpace(string(content))) == 0 {
		return nil
	}
	var idx permissionIndexFile
	if err := json.Unmarshal(content, &idx); err != nil {
		return err
	}
	for id, grant := range idx.Grants {
		if grant.ID == "" {
			grant.ID = id
		}
		s.grants[grant.ID] = grant
	}
	return nil
}

func (s *PermissionStore) persistLocked() error {
	if s.dataDir == "" {
		return nil
	}
	idx := permissionIndexFile{Grants: s.grants}
	data, err := json.MarshalIndent(idx, "", "  ")
	if err != nil {
		return err
	}
	return writeFileAtomically(filepath.Join(s.dataDir, "index.json"), append(data, '\n'), 0o644)
}

func clonePermissionGrant(grant PermissionGrant) PermissionGrant {
	clone := grant
	if grant.ExpiresAt != nil {
		ts := *grant.ExpiresAt
		clone.ExpiresAt = &ts
	}
	return clone
}

type WorkflowTriggerKind string
type WorkflowStepKind string
type WorkflowRunStatus string

const (
	WorkflowTriggerSchedule WorkflowTriggerKind = "schedule"
	WorkflowTriggerWebhook  WorkflowTriggerKind = "webhook"
	WorkflowTriggerEvent    WorkflowTriggerKind = "event"

	WorkflowStepAgentRun     WorkflowStepKind = "agent_run"
	WorkflowStepSubagentRun  WorkflowStepKind = "subagent_run"
	WorkflowStepToolCall     WorkflowStepKind = "tool_call"
	WorkflowStepApprovalGate WorkflowStepKind = "approval_gate"
	WorkflowStepEmitMessage  WorkflowStepKind = "emit_message"

	WorkflowRunPending   WorkflowRunStatus = "pending"
	WorkflowRunRunning   WorkflowRunStatus = "running"
	WorkflowRunCompleted WorkflowRunStatus = "completed"
	WorkflowRunFailed    WorkflowRunStatus = "failed"
)

type WorkflowStep struct {
	ID          string          `json:"id"`
	Kind        WorkflowStepKind`json:"kind"`
	Name        string          `json:"name,omitempty"`
	Template    string          `json:"template,omitempty"`
	Subagent    string          `json:"subagent,omitempty"`
	ToolName    string          `json:"tool_name,omitempty"`
	ToolArgs    json.RawMessage `json:"tool_args,omitempty"`
	Review      bool            `json:"review,omitempty"`
	MessageRole MessageRole     `json:"message_role,omitempty"`
}

type WorkflowRecord struct {
	ID                   string              `json:"id"`
	Name                 string              `json:"name"`
	SessionID            string              `json:"session_id,omitempty"`
	Surface              Surface             `json:"surface,omitempty"`
	Enabled              bool                `json:"enabled"`
	TriggerKind          WorkflowTriggerKind `json:"trigger_kind"`
	ScheduleIntervalMins int                 `json:"schedule_interval_mins,omitempty"`
	WebhookToken         string              `json:"webhook_token,omitempty"`
	EventName            string              `json:"event_name,omitempty"`
	Steps                []WorkflowStep      `json:"steps"`
	CreatedAt            time.Time           `json:"created_at"`
	UpdatedAt            time.Time           `json:"updated_at"`
	LastRunAt            *time.Time          `json:"last_run_at,omitempty"`
	NextRunAt            *time.Time          `json:"next_run_at,omitempty"`
}

type WorkflowRun struct {
	ID             string            `json:"id"`
	WorkflowID     string            `json:"workflow_id"`
	SessionID      string            `json:"session_id,omitempty"`
	TriggerKind    WorkflowTriggerKind `json:"trigger_kind"`
	Status         WorkflowRunStatus `json:"status"`
	TriggerPayload json.RawMessage   `json:"trigger_payload,omitempty"`
	StepOutputs    map[string]string `json:"step_outputs,omitempty"`
	StartedAt      time.Time         `json:"started_at"`
	FinishedAt     *time.Time        `json:"finished_at,omitempty"`
	LastError      string            `json:"last_error,omitempty"`
}

type workflowIndexFile struct {
	Workflows map[string]WorkflowRecord `json:"workflows"`
}

type workflowRunIndexFile struct {
	Runs map[string]WorkflowRun `json:"runs"`
}

type WorkflowStore struct {
	mu        sync.Mutex
	dataDir   string
	workflows map[string]WorkflowRecord
}

type WorkflowRunStore struct {
	mu      sync.Mutex
	dataDir string
	runs    map[string]WorkflowRun
}

func NewWorkflowStore(dataDir string) (*WorkflowStore, error) {
	store := &WorkflowStore{
		dataDir:   strings.TrimSpace(dataDir),
		workflows: map[string]WorkflowRecord{},
	}
	if store.dataDir == "" {
		return store, nil
	}
	if err := os.MkdirAll(store.dataDir, 0o755); err != nil {
		return nil, err
	}
	if err := store.load(); err != nil {
		return nil, err
	}
	return store, nil
}

func (s *WorkflowStore) List() []WorkflowRecord {
	s.mu.Lock()
	defer s.mu.Unlock()
	out := make([]WorkflowRecord, 0, len(s.workflows))
	for _, wf := range s.workflows {
		out = append(out, cloneWorkflowRecord(wf))
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].UpdatedAt.Equal(out[j].UpdatedAt) {
			return out[i].ID > out[j].ID
		}
		return out[i].UpdatedAt.After(out[j].UpdatedAt)
	})
	return out
}

func (s *WorkflowStore) Get(id string) (WorkflowRecord, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	record, ok := s.workflows[strings.TrimSpace(id)]
	if !ok {
		return WorkflowRecord{}, false
	}
	return cloneWorkflowRecord(record), true
}

func (s *WorkflowStore) Put(record WorkflowRecord) (WorkflowRecord, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	now := time.Now().UTC()
	record.ID = strings.TrimSpace(record.ID)
	if record.ID == "" {
		record.ID = NewEntryID()
	}
	record.Name = strings.TrimSpace(record.Name)
	record.SessionID = strings.TrimSpace(record.SessionID)
		record.Surface = normalizeWorkflowSurface(record.Surface)
	record.TriggerKind = normalizeWorkflowTrigger(record.TriggerKind)
	record.WebhookToken = strings.TrimSpace(record.WebhookToken)
	record.EventName = strings.TrimSpace(record.EventName)
	record.CreatedAt = zeroTimeOr(record.CreatedAt, now)
	record.UpdatedAt = now
	record.Steps = normalizeWorkflowSteps(record.Steps)
	if record.TriggerKind == WorkflowTriggerWebhook && record.WebhookToken == "" {
		record.WebhookToken = NewEntryID()
	}
	if record.TriggerKind == WorkflowTriggerSchedule && record.ScheduleIntervalMins <= 0 {
		record.ScheduleIntervalMins = 60
	}
	if record.TriggerKind == WorkflowTriggerSchedule {
		next := now.Add(time.Duration(record.ScheduleIntervalMins) * time.Minute)
		record.NextRunAt = &next
	}
	s.workflows[record.ID] = record
	return cloneWorkflowRecord(record), s.persistLocked()
}

func (s *WorkflowStore) Delete(id string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	delete(s.workflows, strings.TrimSpace(id))
	return s.persistLocked()
}

func (s *WorkflowStore) load() error {
	content, err := os.ReadFile(filepath.Join(s.dataDir, "index.json"))
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return nil
		}
		return err
	}
	if len(strings.TrimSpace(string(content))) == 0 {
		return nil
	}
	var idx workflowIndexFile
	if err := json.Unmarshal(content, &idx); err != nil {
		return err
	}
	for id, record := range idx.Workflows {
		if record.ID == "" {
			record.ID = id
		}
		s.workflows[record.ID] = record
	}
	return nil
}

func (s *WorkflowStore) persistLocked() error {
	if s.dataDir == "" {
		return nil
	}
	idx := workflowIndexFile{Workflows: s.workflows}
	data, err := json.MarshalIndent(idx, "", "  ")
	if err != nil {
		return err
	}
	return writeFileAtomically(filepath.Join(s.dataDir, "index.json"), append(data, '\n'), 0o644)
}

func NewWorkflowRunStore(dataDir string) (*WorkflowRunStore, error) {
	store := &WorkflowRunStore{
		dataDir: strings.TrimSpace(dataDir),
		runs:    map[string]WorkflowRun{},
	}
	if store.dataDir == "" {
		return store, nil
	}
	if err := os.MkdirAll(store.dataDir, 0o755); err != nil {
		return nil, err
	}
	if err := store.load(); err != nil {
		return nil, err
	}
	return store, nil
}

func (s *WorkflowRunStore) List(workflowID string) []WorkflowRun {
	s.mu.Lock()
	defer s.mu.Unlock()
	workflowID = strings.TrimSpace(workflowID)
	out := make([]WorkflowRun, 0, len(s.runs))
	for _, run := range s.runs {
		if workflowID != "" && run.WorkflowID != workflowID {
			continue
		}
		out = append(out, cloneWorkflowRun(run))
	}
	sort.Slice(out, func(i, j int) bool {
		return out[i].StartedAt.After(out[j].StartedAt)
	})
	return out
}

func (s *WorkflowRunStore) Put(run WorkflowRun) (WorkflowRun, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	now := time.Now().UTC()
	run.ID = strings.TrimSpace(run.ID)
	if run.ID == "" {
		run.ID = NewEntryID()
	}
	run.WorkflowID = strings.TrimSpace(run.WorkflowID)
	run.SessionID = strings.TrimSpace(run.SessionID)
	run.TriggerKind = normalizeWorkflowTrigger(run.TriggerKind)
	run.Status = normalizeWorkflowRunStatus(run.Status)
	run.StartedAt = zeroTimeOr(run.StartedAt, now)
	if run.StepOutputs == nil {
		run.StepOutputs = map[string]string{}
	}
	s.runs[run.ID] = run
	return cloneWorkflowRun(run), s.persistLocked()
}

func (s *WorkflowRunStore) Get(id string) (WorkflowRun, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	run, ok := s.runs[strings.TrimSpace(id)]
	if !ok {
		return WorkflowRun{}, false
	}
	return cloneWorkflowRun(run), true
}

func (s *WorkflowRunStore) load() error {
	content, err := os.ReadFile(filepath.Join(s.dataDir, "index.json"))
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return nil
		}
		return err
	}
	if len(strings.TrimSpace(string(content))) == 0 {
		return nil
	}
	var idx workflowRunIndexFile
	if err := json.Unmarshal(content, &idx); err != nil {
		return err
	}
	for id, run := range idx.Runs {
		if run.ID == "" {
			run.ID = id
		}
		s.runs[run.ID] = run
	}
	return nil
}

func (s *WorkflowRunStore) persistLocked() error {
	if s.dataDir == "" {
		return nil
	}
	idx := workflowRunIndexFile{Runs: s.runs}
	data, err := json.MarshalIndent(idx, "", "  ")
	if err != nil {
		return err
	}
	return writeFileAtomically(filepath.Join(s.dataDir, "index.json"), append(data, '\n'), 0o644)
}

func normalizeWorkflowTrigger(kind WorkflowTriggerKind) WorkflowTriggerKind {
	switch kind {
	case WorkflowTriggerWebhook, WorkflowTriggerEvent:
		return kind
	default:
		return WorkflowTriggerSchedule
	}
}

func normalizeWorkflowSurface(surface Surface) Surface {
	switch surface {
	case SurfaceDiscord, SurfaceTelegram:
		return surface
	default:
		return SurfaceWeb
	}
}

func normalizeWorkflowRunStatus(status WorkflowRunStatus) WorkflowRunStatus {
	switch status {
	case WorkflowRunRunning, WorkflowRunCompleted, WorkflowRunFailed:
		return status
	default:
		return WorkflowRunPending
	}
}

func normalizeWorkflowSteps(steps []WorkflowStep) []WorkflowStep {
	out := make([]WorkflowStep, 0, len(steps))
	for _, step := range steps {
		step.ID = strings.TrimSpace(step.ID)
		if step.ID == "" {
			step.ID = NewEntryID()
		}
		step.Kind = normalizeWorkflowStepKind(step.Kind)
		step.Name = strings.TrimSpace(step.Name)
		step.Template = strings.TrimSpace(step.Template)
		step.Subagent = strings.TrimSpace(step.Subagent)
		step.ToolName = strings.TrimSpace(step.ToolName)
		out = append(out, step)
	}
	return out
}

func normalizeWorkflowStepKind(kind WorkflowStepKind) WorkflowStepKind {
	switch kind {
	case WorkflowStepSubagentRun, WorkflowStepToolCall, WorkflowStepApprovalGate, WorkflowStepEmitMessage:
		return kind
	default:
		return WorkflowStepAgentRun
	}
}

func cloneWorkflowRecord(record WorkflowRecord) WorkflowRecord {
	clone := record
	if len(record.Steps) > 0 {
		clone.Steps = append([]WorkflowStep(nil), record.Steps...)
	}
	if record.LastRunAt != nil {
		ts := *record.LastRunAt
		clone.LastRunAt = &ts
	}
	if record.NextRunAt != nil {
		ts := *record.NextRunAt
		clone.NextRunAt = &ts
	}
	return clone
}

func cloneWorkflowRun(run WorkflowRun) WorkflowRun {
	clone := run
	if run.StepOutputs != nil {
		clone.StepOutputs = map[string]string{}
		for k, v := range run.StepOutputs {
			clone.StepOutputs[k] = v
		}
	}
	if run.FinishedAt != nil {
		ts := *run.FinishedAt
		clone.FinishedAt = &ts
	}
	return clone
}

type SnapshotRecord struct {
	ID            string     `json:"id"`
	SessionID     string     `json:"session_id"`
	WorkspaceRoot string     `json:"workspace_root,omitempty"`
	ToolName      string     `json:"tool_name,omitempty"`
	Artifact      string     `json:"artifact,omitempty"`
	CreatedAt     time.Time  `json:"created_at"`
	RestoredAt    *time.Time `json:"restored_at,omitempty"`
}

type snapshotIndexFile struct {
	Snapshots map[string]SnapshotRecord `json:"snapshots"`
}

type SnapshotStore struct {
	mu        sync.Mutex
	dataDir   string
	snapshots map[string]SnapshotRecord
}

func NewSnapshotStore(dataDir string) (*SnapshotStore, error) {
	store := &SnapshotStore{
		dataDir:   strings.TrimSpace(dataDir),
		snapshots: map[string]SnapshotRecord{},
	}
	if store.dataDir == "" {
		return store, nil
	}
	if err := os.MkdirAll(store.dataDir, 0o755); err != nil {
		return nil, err
	}
	if err := store.load(); err != nil {
		return nil, err
	}
	return store, nil
}

func (s *SnapshotStore) Put(record SnapshotRecord) (SnapshotRecord, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	now := time.Now().UTC()
	record.ID = strings.TrimSpace(record.ID)
	if record.ID == "" {
		record.ID = NewEntryID()
	}
	record.SessionID = strings.TrimSpace(record.SessionID)
	record.ToolName = strings.TrimSpace(record.ToolName)
	record.WorkspaceRoot = strings.TrimSpace(record.WorkspaceRoot)
	record.CreatedAt = zeroTimeOr(record.CreatedAt, now)
	s.snapshots[record.ID] = record
	return cloneSnapshotRecord(record), s.persistLocked()
}

func (s *SnapshotStore) List(sessionID string) []SnapshotRecord {
	s.mu.Lock()
	defer s.mu.Unlock()
	sessionID = strings.TrimSpace(sessionID)
	out := make([]SnapshotRecord, 0, len(s.snapshots))
	for _, record := range s.snapshots {
		if sessionID != "" && record.SessionID != sessionID {
			continue
		}
		out = append(out, cloneSnapshotRecord(record))
	}
	sort.Slice(out, func(i, j int) bool {
		return out[i].CreatedAt.After(out[j].CreatedAt)
	})
	return out
}

func (s *SnapshotStore) Update(record SnapshotRecord) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	record.ID = strings.TrimSpace(record.ID)
	if record.ID == "" {
		return errors.New("snapshot id is required")
	}
	s.snapshots[record.ID] = record
	return s.persistLocked()
}

func (s *SnapshotStore) Get(id string) (SnapshotRecord, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	record, ok := s.snapshots[strings.TrimSpace(id)]
	if !ok {
		return SnapshotRecord{}, false
	}
	return cloneSnapshotRecord(record), true
}

func (s *SnapshotStore) load() error {
	content, err := os.ReadFile(filepath.Join(s.dataDir, "index.json"))
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return nil
		}
		return err
	}
	if len(strings.TrimSpace(string(content))) == 0 {
		return nil
	}
	var idx snapshotIndexFile
	if err := json.Unmarshal(content, &idx); err != nil {
		return err
	}
	for id, record := range idx.Snapshots {
		if record.ID == "" {
			record.ID = id
		}
		s.snapshots[record.ID] = record
	}
	return nil
}

func (s *SnapshotStore) persistLocked() error {
	if s.dataDir == "" {
		return nil
	}
	idx := snapshotIndexFile{Snapshots: s.snapshots}
	data, err := json.MarshalIndent(idx, "", "  ")
	if err != nil {
		return err
	}
	return writeFileAtomically(filepath.Join(s.dataDir, "index.json"), append(data, '\n'), 0o644)
}

func cloneSnapshotRecord(record SnapshotRecord) SnapshotRecord {
	clone := record
	if record.RestoredAt != nil {
		ts := *record.RestoredAt
		clone.RestoredAt = &ts
	}
	return clone
}

type SubagentArtifact struct {
	Type      string    `json:"type"`
	Goal      string    `json:"goal"`
	Markdown  string    `json:"markdown"`
	CreatedAt time.Time `json:"created_at"`
}

type TraceRecord struct {
	ID               string             `json:"id"`
	SessionID        string             `json:"session_id,omitempty"`
	RunID            string             `json:"run_id,omitempty"`
	RunKind          string             `json:"run_kind,omitempty"`
	Provider         string             `json:"provider,omitempty"`
	Model            string             `json:"model,omitempty"`
	Prompt           string             `json:"prompt,omitempty"`
	Reply            string             `json:"reply,omitempty"`
	ToolEvents       []ToolEvent        `json:"tool_events,omitempty"`
	Reminders        []ReminderEvent    `json:"reminders,omitempty"`
	SubagentArtifacts []SubagentArtifact `json:"subagent_artifacts,omitempty"`
	CreatedAt        time.Time          `json:"created_at"`
}

type traceIndexFile struct {
	Traces map[string]TraceRecord `json:"traces"`
}

type TraceStore struct {
	mu      sync.Mutex
	dataDir string
	traces  map[string]TraceRecord
}

func NewTraceStore(dataDir string) (*TraceStore, error) {
	store := &TraceStore{
		dataDir: strings.TrimSpace(dataDir),
		traces:  map[string]TraceRecord{},
	}
	if store.dataDir == "" {
		return store, nil
	}
	if err := os.MkdirAll(store.dataDir, 0o755); err != nil {
		return nil, err
	}
	if err := store.load(); err != nil {
		return nil, err
	}
	return store, nil
}

func (s *TraceStore) Put(record TraceRecord) (TraceRecord, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	record.ID = strings.TrimSpace(record.ID)
	if record.ID == "" {
		record.ID = NewEntryID()
	}
	record.SessionID = strings.TrimSpace(record.SessionID)
	record.RunID = strings.TrimSpace(record.RunID)
	record.RunKind = strings.TrimSpace(record.RunKind)
	record.Provider = strings.TrimSpace(record.Provider)
	record.Model = strings.TrimSpace(record.Model)
	record.Prompt = strings.TrimSpace(record.Prompt)
	record.Reply = strings.TrimSpace(record.Reply)
	record.CreatedAt = zeroTimeOr(record.CreatedAt, time.Now().UTC())
	s.traces[record.ID] = record
	return cloneTraceRecord(record), s.persistLocked()
}

func (s *TraceStore) List(sessionID string) []TraceRecord {
	s.mu.Lock()
	defer s.mu.Unlock()
	sessionID = strings.TrimSpace(sessionID)
	out := make([]TraceRecord, 0, len(s.traces))
	for _, record := range s.traces {
		if sessionID != "" && record.SessionID != sessionID {
			continue
		}
		out = append(out, cloneTraceRecord(record))
	}
	sort.Slice(out, func(i, j int) bool {
		return out[i].CreatedAt.After(out[j].CreatedAt)
	})
	return out
}

func (s *TraceStore) load() error {
	content, err := os.ReadFile(filepath.Join(s.dataDir, "index.json"))
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return nil
		}
		return err
	}
	if len(strings.TrimSpace(string(content))) == 0 {
		return nil
	}
	var idx traceIndexFile
	if err := json.Unmarshal(content, &idx); err != nil {
		return err
	}
	for id, record := range idx.Traces {
		if record.ID == "" {
			record.ID = id
		}
		s.traces[record.ID] = record
	}
	return nil
}

func (s *TraceStore) persistLocked() error {
	if s.dataDir == "" {
		return nil
	}
	idx := traceIndexFile{Traces: s.traces}
	data, err := json.MarshalIndent(idx, "", "  ")
	if err != nil {
		return err
	}
	return writeFileAtomically(filepath.Join(s.dataDir, "index.json"), append(data, '\n'), 0o644)
}

func cloneTraceRecord(record TraceRecord) TraceRecord {
	clone := record
	if len(record.ToolEvents) > 0 {
		clone.ToolEvents = append([]ToolEvent(nil), record.ToolEvents...)
	}
	if len(record.Reminders) > 0 {
		clone.Reminders = append([]ReminderEvent(nil), record.Reminders...)
	}
	if len(record.SubagentArtifacts) > 0 {
		clone.SubagentArtifacts = append([]SubagentArtifact(nil), record.SubagentArtifacts...)
	}
	return clone
}

type PlaybookConfig struct {
	Enabled bool `json:"enabled"`
}

type AutomationConfig struct {
	Enabled bool `json:"enabled"`
}

type PermissionConfig struct {
	Enabled bool `json:"enabled"`
}
