package app

import (
	"context"
	"encoding/json"
	"fmt"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"text/template"
	"time"

	"github.com/doeshing/nekoclaw/internal/core"
)

func (s *Service) GetPlaybookConfig() core.PlaybookConfig {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.playbookConfig
}

func (s *Service) SetPlaybookConfig(cfg core.PlaybookConfig) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.playbookConfig = cfg
}

func (s *Service) SavePlaybookConfig(cfg core.PlaybookConfig) error {
	s.mu.Lock()
	configDir := s.configDir
	s.playbookConfig = cfg
	s.mu.Unlock()

	appCfg, _ := core.LoadConfig(configDir)
	appCfg.Playbooks = cfg
	return core.SaveConfig(configDir, appCfg)
}

func (s *Service) GetAutomationConfig() core.AutomationConfig {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.automationConfig
}

func (s *Service) SetAutomationConfig(cfg core.AutomationConfig) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.automationConfig = cfg
}

func (s *Service) SaveAutomationConfig(cfg core.AutomationConfig) error {
	s.mu.Lock()
	configDir := s.configDir
	s.automationConfig = cfg
	s.mu.Unlock()

	appCfg, _ := core.LoadConfig(configDir)
	appCfg.Automations = cfg
	return core.SaveConfig(configDir, appCfg)
}

func (s *Service) GetPermissionConfig() core.PermissionConfig {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.permissionConfig
}

func (s *Service) SetPermissionConfig(cfg core.PermissionConfig) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.permissionConfig = cfg
}

func (s *Service) SavePermissionConfig(cfg core.PermissionConfig) error {
	s.mu.Lock()
	configDir := s.configDir
	s.permissionConfig = cfg
	s.mu.Unlock()

	appCfg, _ := core.LoadConfig(configDir)
	appCfg.Permissions = cfg
	return core.SaveConfig(configDir, appCfg)
}

func (s *Service) ListPlaybooks(statuses ...core.PlaybookStatus) []core.PlaybookEntry {
	if s.playbooks == nil {
		return nil
	}
	return s.playbooks.List(statuses...)
}

func (s *Service) GetPlaybook(id string) (core.PlaybookEntry, error) {
	if s.playbooks == nil {
		return core.PlaybookEntry{}, fmt.Errorf("playbook store unavailable")
	}
	entry, ok := s.playbooks.Get(id)
	if !ok {
		return core.PlaybookEntry{}, fmt.Errorf("playbook not found")
	}
	return entry, nil
}

func (s *Service) SavePlaybook(entry core.PlaybookEntry) (core.PlaybookEntry, error) {
	if s.playbooks == nil {
		return core.PlaybookEntry{}, fmt.Errorf("playbook store unavailable")
	}
	if strings.TrimSpace(entry.ID) == "" {
		return s.playbooks.Create(entry)
	}
	if err := s.playbooks.Update(entry); err != nil {
		return core.PlaybookEntry{}, err
	}
	updated, ok := s.playbooks.Get(entry.ID)
	if !ok {
		return core.PlaybookEntry{}, fmt.Errorf("playbook not found")
	}
	return updated, nil
}

func (s *Service) ApprovePlaybook(id string) (core.PlaybookEntry, error) {
	entry, err := s.GetPlaybook(id)
	if err != nil {
		return core.PlaybookEntry{}, err
	}
	now := time.Now().UTC()
	entry.Status = core.PlaybookStatusActive
	entry.ApprovedAt = &now
	if err := s.playbooks.Update(entry); err != nil {
		return core.PlaybookEntry{}, err
	}
	updated, ok := s.playbooks.Get(id)
	if !ok {
		return core.PlaybookEntry{}, fmt.Errorf("playbook not found")
	}
	return updated, nil
}

func (s *Service) ArchivePlaybook(id string) (core.PlaybookEntry, error) {
	entry, err := s.GetPlaybook(id)
	if err != nil {
		return core.PlaybookEntry{}, err
	}
	entry.Status = core.PlaybookStatusArchived
	if err := s.playbooks.Update(entry); err != nil {
		return core.PlaybookEntry{}, err
	}
	updated, ok := s.playbooks.Get(id)
	if !ok {
		return core.PlaybookEntry{}, fmt.Errorf("playbook not found")
	}
	return updated, nil
}

func (s *Service) ActivePlaybooks(sessionID string, surface core.Surface, persona string, limit int) []core.PlaybookEntry {
	if s.playbooks == nil {
		return nil
	}
	return s.playbooks.Active(core.PlaybookQuery{
		WorkspaceRoot: s.workspaceRoot,
		Persona:       persona,
		Surface:       surface,
		SessionID:     sessionID,
		IncludeStatus: []core.PlaybookStatus{core.PlaybookStatusActive},
	}, limit)
}

func (s *Service) TaskList(sessionID string) core.TaskList {
	if s.tasks == nil {
		return core.TaskList{SessionID: sessionID}
	}
	return s.tasks.Get(sessionID)
}

func (s *Service) SaveTaskList(list core.TaskList) (core.TaskList, error) {
	if s.tasks == nil {
		return core.TaskList{}, fmt.Errorf("task store unavailable")
	}
	if err := s.tasks.Put(list); err != nil {
		return core.TaskList{}, err
	}
	return s.tasks.Get(list.SessionID), nil
}

func (s *Service) ListPermissions() []core.PermissionGrant {
	if s.permissions == nil {
		return nil
	}
	return s.permissions.List()
}

func (s *Service) SavePermissions(grants []core.PermissionGrant) error {
	if s.permissions == nil {
		return fmt.Errorf("permission store unavailable")
	}
	return s.permissions.Replace(grants)
}

func (s *Service) ListWorkflows() []core.WorkflowRecord {
	if s.workflows == nil {
		return nil
	}
	return s.workflows.List()
}

func (s *Service) GetWorkflow(id string) (core.WorkflowRecord, error) {
	if s.workflows == nil {
		return core.WorkflowRecord{}, fmt.Errorf("workflow store unavailable")
	}
	record, ok := s.workflows.Get(id)
	if !ok {
		return core.WorkflowRecord{}, fmt.Errorf("workflow not found")
	}
	return record, nil
}

func (s *Service) SaveWorkflow(record core.WorkflowRecord) (core.WorkflowRecord, error) {
	if s.workflows == nil {
		return core.WorkflowRecord{}, fmt.Errorf("workflow store unavailable")
	}
	return s.workflows.Put(record)
}

func (s *Service) DeleteWorkflow(id string) error {
	if s.workflows == nil {
		return fmt.Errorf("workflow store unavailable")
	}
	return s.workflows.Delete(id)
}

func (s *Service) ListWorkflowRuns(workflowID string) []core.WorkflowRun {
	if s.workflowRuns == nil {
		return nil
	}
	return s.workflowRuns.List(workflowID)
}

func (s *Service) ListSnapshots(sessionID string) []core.SnapshotRecord {
	if s.snapshots == nil {
		return nil
	}
	return s.snapshots.List(sessionID)
}

func (s *Service) ListTraces(sessionID string) []core.TraceRecord {
	if s.traces == nil {
		return nil
	}
	return s.traces.List(sessionID)
}

func (s *Service) recordTrace(record core.TraceRecord) {
	if s.traces == nil {
		return
	}
	if _, err := s.traces.Put(record); err != nil {
		logService.Warnf("trace store: %v", err)
	}
}

func (s *Service) maybeCreatePlaybookCandidate(source, content string, scope core.PlaybookScope, scopeValue string) {
	if s.playbooks == nil || !s.playbookConfig.Enabled {
		return
	}
	content = strings.TrimSpace(content)
	if content == "" {
		return
	}
	candidate := extractPlaybookCandidate(content)
	if candidate == "" {
		return
	}
	if _, err := s.playbooks.Create(core.PlaybookEntry{
		Kind:       core.PlaybookKindWorkflow,
		Scope:      scope,
		ScopeValue: scopeValue,
		Status:     core.PlaybookStatusCandidate,
		Content:    candidate,
		Source:     source,
	}); err != nil {
		logService.Warnf("playbook candidate: %v", err)
	}
}

func extractPlaybookCandidate(content string) string {
	lines := strings.Split(strings.TrimSpace(content), "\n")
	candidates := make([]string, 0, 3)
	for _, line := range lines {
		trimmed := strings.TrimSpace(strings.TrimPrefix(strings.TrimPrefix(line, "- "), "* "))
		if trimmed == "" {
			continue
		}
		if len(trimmed) < 24 {
			continue
		}
		if strings.HasPrefix(trimmed, "##") {
			continue
		}
		candidates = append(candidates, trimmed)
		if len(candidates) >= 3 {
			break
		}
	}
	if len(candidates) == 0 {
		return ""
	}
	return strings.Join(candidates, "\n- ")
}

var implementationStepPattern = regexp.MustCompile(`(?m)^\s*(?:[-*]|\d+\.)\s+(.+)$`)

func taskListFromPlan(plan core.PlanRecord) core.TaskList {
	steps := extractImplementationSteps(plan.PlanMarkdown)
	items := make([]core.TaskItem, 0, len(steps))
	for i, step := range steps {
		items = append(items, core.TaskItem{
			ID:      core.NewEntryID(),
			Content: step,
			Status:  core.TaskStatusPending,
			Order:   i,
		})
	}
	return core.TaskList{
		SessionID: plan.SessionID,
		PlanID:    plan.ID,
		Items:     items,
	}
}

func extractImplementationSteps(markdown string) []string {
	markdown = strings.TrimSpace(markdown)
	if markdown == "" {
		return nil
	}
	section := markdown
	if idx := strings.Index(strings.ToLower(markdown), "## implementation steps"); idx >= 0 {
		section = markdown[idx:]
	}
	matches := implementationStepPattern.FindAllStringSubmatch(section, -1)
	out := make([]string, 0, len(matches))
	for _, match := range matches {
		step := strings.TrimSpace(match[1])
		if step == "" {
			continue
		}
		out = append(out, step)
	}
	return out
}

func renderPlaybookPrompt(entries []core.PlaybookEntry) string {
	if len(entries) == 0 {
		return ""
	}
	sort.Slice(entries, func(i, j int) bool {
		return entries[i].UpdatedAt.After(entries[j].UpdatedAt)
	})
	lines := make([]string, 0, len(entries)+1)
	lines = append(lines, "# Playbook")
	for _, entry := range entries {
		lines = append(lines, "- "+strings.TrimSpace(entry.Content))
	}
	return strings.Join(lines, "\n")
}

func (s *Service) renderWorkflowTemplate(tmpl string, data map[string]any) (string, error) {
	tmpl = strings.TrimSpace(tmpl)
	if tmpl == "" {
		return "", nil
	}
	parsed, err := template.New("workflow").Option("missingkey=zero").Parse(tmpl)
	if err != nil {
		return "", err
	}
	var b strings.Builder
	if err := parsed.Execute(&b, data); err != nil {
		return "", err
	}
	return strings.TrimSpace(b.String()), nil
}

func (s *Service) RunWorkflow(ctx context.Context, workflowID string, payload json.RawMessage) (core.WorkflowRun, error) {
	record, err := s.GetWorkflow(workflowID)
	if err != nil {
		return core.WorkflowRun{}, err
	}
	if !record.Enabled {
		return core.WorkflowRun{}, fmt.Errorf("workflow disabled")
	}

	run, err := s.workflowRuns.Put(core.WorkflowRun{
		WorkflowID:     record.ID,
		SessionID:      record.SessionID,
		TriggerKind:    record.TriggerKind,
		Status:         core.WorkflowRunRunning,
		TriggerPayload: payload,
		StepOutputs:    map[string]string{},
	})
	if err != nil {
		return core.WorkflowRun{}, err
	}

	ctxData := map[string]any{
		"trigger": map[string]any{
			"payload": string(payload),
		},
		"session_id": record.SessionID,
	}
	for _, step := range record.Steps {
		switch step.Kind {
		case core.WorkflowStepAgentRun:
			msg, renderErr := s.renderWorkflowTemplate(step.Template, ctxData)
			if renderErr != nil {
				return s.failWorkflowRun(run, renderErr)
			}
			resp, chatErr := s.HandleChat(ctx, core.ChatRequest{
				SessionID:   record.SessionID,
				Surface:     record.Surface,
				Message:     msg,
				EnableTools: true,
			})
			if chatErr != nil {
				return s.failWorkflowRun(run, chatErr)
			}
			run.StepOutputs[step.ID] = resp.Reply
			ctxData[step.ID] = map[string]any{"reply": resp.Reply}
			s.maybeCreatePlaybookCandidate("workflow:"+record.ID, resp.Reply, core.PlaybookScopeWorkspace, s.workspaceRoot)
		case core.WorkflowStepSubagentRun:
			goal, renderErr := s.renderWorkflowTemplate(step.Template, ctxData)
			if renderErr != nil {
				return s.failWorkflowRun(run, renderErr)
			}
			artifact, spawnErr := s.SpawnSubagent(ctx, record.SessionID, record.Surface, step.Subagent, goal, "")
			if spawnErr != nil {
				return s.failWorkflowRun(run, spawnErr)
			}
			run.StepOutputs[step.ID] = artifact.Markdown
			ctxData[step.ID] = map[string]any{"markdown": artifact.Markdown}
		case core.WorkflowStepEmitMessage:
			msg, renderErr := s.renderWorkflowTemplate(step.Template, ctxData)
			if renderErr != nil {
				return s.failWorkflowRun(run, renderErr)
			}
			role := step.MessageRole
			if role == "" {
				role = core.RoleAssistant
			}
			if record.SessionID != "" && s.sessions != nil {
				s.sessions.AppendMessage(record.SessionID, core.Message{
					Role:      role,
					Content:   msg,
					CreatedAt: time.Now().UTC(),
				})
			}
			run.StepOutputs[step.ID] = msg
			ctxData[step.ID] = map[string]any{"message": msg}
		default:
			run.StepOutputs[step.ID] = ""
		}
	}

	now := time.Now().UTC()
	run.Status = core.WorkflowRunCompleted
	run.FinishedAt = &now
	s.workflowRuns.Put(run)

	record.LastRunAt = &now
	if record.TriggerKind == core.WorkflowTriggerSchedule && record.ScheduleIntervalMins > 0 {
		next := now.Add(time.Duration(record.ScheduleIntervalMins) * time.Minute)
		record.NextRunAt = &next
	}
	s.workflows.Put(record)
	return run, nil
}

func (s *Service) failWorkflowRun(run core.WorkflowRun, err error) (core.WorkflowRun, error) {
	now := time.Now().UTC()
	run.Status = core.WorkflowRunFailed
	run.LastError = strings.TrimSpace(err.Error())
	run.FinishedAt = &now
	if s.workflowRuns != nil {
		_, _ = s.workflowRuns.Put(run)
	}
	return run, err
}

func (s *Service) SpawnSubagent(
	ctx context.Context,
	sessionID string,
	surface core.Surface,
	subagentType string,
	goal string,
	contextHint string,
) (core.SubagentArtifact, error) {
	subagentType = strings.TrimSpace(strings.ToLower(subagentType))
	goal = strings.TrimSpace(goal)
	if goal == "" {
		return core.SubagentArtifact{}, fmt.Errorf("goal is required")
	}
	if sessionID == "" {
		sessionID = "main"
	}
	if surface == "" {
		surface = core.SurfaceWeb
	}

	role := core.ModelRoleExplorer
	systemPrompt := strings.TrimSpace(`
You are the Code Explorer for NekoClaw.
Inspect the repository and return concise markdown with:
## Goal
## Relevant Files
## Findings
## Suggested Change Sites
Do not modify files.
`)
	switch subagentType {
	case "planner":
		role = core.ModelRolePlanner
		systemPrompt = plannerSystemPrompt()
	case "critic":
		role = core.ModelRoleCritic
		systemPrompt = strings.TrimSpace(`
You are the Critic for NekoClaw.
Return markdown with:
## Findings
## Missing Verification
## Risks
Keep findings first and focus on regressions or weak assumptions.
Do not modify files.
`)
	default:
		subagentType = "code_explorer"
	}

	resolved := s.ResolveModelRole(role)
	history := s.sessions.HistoryAsMessages(sessionID)
	ephemeral := append([]core.Message{{
		Role:    core.RoleSystem,
		Content: systemPrompt,
	}}, history...)
	if trimmed := strings.TrimSpace(contextHint); trimmed != "" {
		ephemeral = append(ephemeral, core.Message{
			Role:    core.RoleSystem,
			Content: "Context hint:\n" + trimmed,
		})
	}

	resp, err := s.HandleChat(ctx, core.ChatRequest{
		SessionID:         sessionID,
		DisableSession:    true,
		EphemeralMessages: ephemeral,
		Surface:           surface,
		Provider:          resolved.Provider,
		Model:             resolved.Model,
		Message:           goal,
		EnableTools:       true,
		ToolMode:          core.ToolModePlanner,
	})
	if err != nil {
		return core.SubagentArtifact{}, err
	}
	artifact := core.SubagentArtifact{
		Type:      subagentType,
		Goal:      goal,
		Markdown:  strings.TrimSpace(resp.Reply),
		CreatedAt: time.Now().UTC(),
	}
	s.recordTrace(core.TraceRecord{
		SessionID: sessionID,
		RunKind:   "subagent:" + subagentType,
		Provider:  resp.Provider,
		Model:     resp.Model,
		Prompt:    goal,
		Reply:     resp.Reply,
		SubagentArtifacts: []core.SubagentArtifact{
			artifact,
		},
	})
	return artifact, nil
}

func (s *Service) StartAutomationLoop(ctx context.Context) {
	if s.workflows == nil || !s.automationConfig.Enabled {
		return
	}
	ticker := time.NewTicker(time.Minute)
	go func() {
		defer ticker.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				now := time.Now().UTC()
				for _, workflow := range s.workflows.List() {
					if !workflow.Enabled || workflow.TriggerKind != core.WorkflowTriggerSchedule || workflow.NextRunAt == nil || workflow.NextRunAt.After(now) {
						continue
					}
					if _, err := s.RunWorkflow(ctx, workflow.ID, nil); err != nil {
						logService.Warnf("scheduled workflow: id=%s error=%v", workflow.ID, err)
					}
				}
			}
		}
	}()
}

func (s *Service) emitWorkflowEvent(ctx context.Context, eventName string, payload map[string]any) {
	if s.workflows == nil || !s.automationConfig.Enabled {
		return
	}
	eventName = strings.TrimSpace(eventName)
	if eventName == "" {
		return
	}
	raw, _ := json.Marshal(payload)
	for _, workflow := range s.workflows.List() {
		if !workflow.Enabled || workflow.TriggerKind != core.WorkflowTriggerEvent || workflow.EventName != eventName {
			continue
		}
		if _, err := s.RunWorkflow(ctx, workflow.ID, raw); err != nil {
			logService.Warnf("workflow event: id=%s event=%s error=%v", workflow.ID, eventName, err)
		}
	}
}

func normalizeSurface(surface core.Surface) core.Surface {
	switch surface {
	case core.SurfaceDiscord, core.SurfaceTelegram:
		return surface
	default:
		return core.SurfaceWeb
	}
}

func workspaceScopeValue(root string) string {
	root = strings.TrimSpace(root)
	if root == "" {
		return ""
	}
	clean := filepath.Clean(root)
	if clean == "." {
		return root
	}
	return clean
}
