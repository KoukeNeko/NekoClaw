package app

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/doeshing/nekoclaw/internal/core"
)

var (
	ErrPlanStoreUnavailable = errors.New("plan store unavailable")
	ErrPlanNotFound         = errors.New("plan not found")
	ErrPlanInvalidState     = errors.New("plan invalid state")
)

func (s *Service) ListPlans(sessionID string) []core.PlanRecord {
	if s.plans == nil {
		return nil
	}
	return s.plans.List(sessionID)
}

func (s *Service) GetPlan(planID string) (core.PlanRecord, error) {
	if s.plans == nil {
		return core.PlanRecord{}, ErrPlanStoreUnavailable
	}
	record, ok := s.plans.Get(planID)
	if !ok {
		return core.PlanRecord{}, ErrPlanNotFound
	}
	return record, nil
}

func (s *Service) CreatePlan(ctx context.Context, req core.CreatePlanRequest) (core.PlanRecord, error) {
	if s.plans == nil {
		return core.PlanRecord{}, ErrPlanStoreUnavailable
	}

	prompt := strings.TrimSpace(req.Prompt)
	if prompt == "" {
		return core.PlanRecord{}, fmt.Errorf("prompt is required")
	}

	sessionID := strings.TrimSpace(req.SessionID)
	if sessionID == "" {
		sessionID = "main"
	}
	surface := req.Surface
	if surface == "" {
		surface = core.SurfaceWeb
	}

	history := s.sessions.HistoryAsMessages(sessionID)
	ephemeral := make([]core.Message, 0, len(history)+1)
	ephemeral = append(ephemeral, core.Message{
		Role:    core.RoleSystem,
		Content: plannerSystemPrompt(),
	})
	ephemeral = append(ephemeral, history...)

	resp, err := s.HandleChat(ctx, core.ChatRequest{
		SessionID:         sessionID,
		DisableSession:    true,
		EphemeralMessages: ephemeral,
		Surface:           surface,
		Provider:          strings.TrimSpace(req.Provider),
		Model:             strings.TrimSpace(req.Model),
		Message:           prompt,
		EnableTools:       true,
		ToolMode:          core.ToolModePlanner,
	})
	if err != nil {
		return core.PlanRecord{}, err
	}

	planMarkdown := normalizePlanMarkdown(resp.Reply, prompt)
	plan := core.PlanRecord{
		SessionID:     sessionID,
		Surface:       surface,
		Provider:      resp.Provider,
		Model:         resp.Model,
		Status:        core.PlanStatusPendingApproval,
		RequestPrompt: prompt,
		PlanMarkdown:  planMarkdown,
		PlanSummary:   summarizePlan(planMarkdown, prompt),
	}
	return s.plans.Create(plan)
}

func (s *Service) ApprovePlan(planID string) (core.PlanRecord, error) {
	record, err := s.GetPlan(planID)
	if err != nil {
		return core.PlanRecord{}, err
	}
	if record.Status != core.PlanStatusPendingApproval {
		return core.PlanRecord{}, fmt.Errorf("%w: approve requires pending_approval", ErrPlanInvalidState)
	}
	now := time.Now().UTC()
	record.Status = core.PlanStatusApproved
	record.ApprovedAt = &now
	record.LastError = ""
	if err := s.plans.Update(record); err != nil {
		return core.PlanRecord{}, err
	}
	if tasks := taskListFromPlan(record); len(tasks.Items) > 0 {
		if _, taskErr := s.SaveTaskList(tasks); taskErr != nil {
			logService.Warnf("plan tasks: plan_id=%s error=%v", planID, taskErr)
		}
	}
	return record, nil
}

func (s *Service) RejectPlan(planID string) (core.PlanRecord, error) {
	record, err := s.GetPlan(planID)
	if err != nil {
		return core.PlanRecord{}, err
	}
	if record.Status != core.PlanStatusPendingApproval {
		return core.PlanRecord{}, fmt.Errorf("%w: reject requires pending_approval", ErrPlanInvalidState)
	}
	now := time.Now().UTC()
	record.Status = core.PlanStatusRejected
	record.RejectedAt = &now
	record.LastError = ""
	if err := s.plans.Update(record); err != nil {
		return core.PlanRecord{}, err
	}
	return record, nil
}

func (s *Service) RunApprovedPlan(ctx context.Context, planID string) (<-chan core.StreamChunk, error) {
	record, err := s.GetPlan(planID)
	if err != nil {
		return nil, err
	}
	if record.Status == core.PlanStatusPendingApproval {
		if record, err = s.ApprovePlan(planID); err != nil {
			return nil, err
		}
	}
	if record.Status != core.PlanStatusApproved && record.Status != core.PlanStatusFailed {
		return nil, fmt.Errorf("%w: execute requires approved plan", ErrPlanInvalidState)
	}

	now := time.Now().UTC()
	record.Status = core.PlanStatusExecuting
	record.ExecutionStartedAt = &now
	record.ExecutionFinishedAt = nil
	record.LastError = ""
	if err := s.plans.Update(record); err != nil {
		return nil, err
	}

	execReq := core.ChatRequest{
		SessionID:   record.SessionID,
		Surface:     record.Surface,
		Provider:    record.Provider,
		Model:       record.Model,
		Message:     executionPrompt(record),
		EnableTools: true,
	}

	src := s.HandleChatStream(ctx, execReq)
	out := make(chan core.StreamChunk, 16)
	go func() {
		defer close(out)

		for chunk := range src {
			if chunk.Type == core.ChunkDone && chunk.Response != nil {
				record, resp, criticNote, updateErr := s.finalizePlanExecutionResponse(planID, *chunk.Response)
				if updateErr != nil {
					logService.Errorf("plan update after execution: plan_id=%s error=%v", planID, updateErr)
				} else {
					_ = record
					chunk.Response = &resp
					if strings.TrimSpace(criticNote) != "" {
						select {
						case out <- core.StreamChunk{Type: core.ChunkText, Content: criticNote}:
						case <-ctx.Done():
							return
						}
					}
				}
			}
			if chunk.Type == core.ChunkError {
				if _, updateErr := s.failPlanExecution(planID, chunk.Error); updateErr != nil {
					logService.Errorf("plan update after error: plan_id=%s error=%v", planID, updateErr)
				}
			}
			select {
			case out <- chunk:
			case <-ctx.Done():
				return
			}
		}
	}()
	return out, nil
}

func (s *Service) ContinuePendingToolRunStream(ctx context.Context, runID string, decisions []core.ToolApprovalDecision) (<-chan core.StreamChunk, error) {
	pending, err := s.toolRuntime.GetPendingRun(runID)
	if err != nil {
		return nil, err
	}
	src := s.HandleChatStream(ctx, core.ChatRequest{
		SessionID:     pending.SessionID,
		Surface:       pending.Surface,
		Provider:      pending.ProviderID,
		Model:         pending.ModelID,
		EnableTools:   true,
		RunID:         runID,
		ToolApprovals: decisions,
	})
	out := make(chan core.StreamChunk, 16)
	go func() {
		defer close(out)
		for chunk := range src {
			if chunk.Type == core.ChunkDone && chunk.Response != nil {
				record, resp, criticNote, err := s.finalizePendingPlanRunResponse(runID, *chunk.Response)
				if err != nil && !errors.Is(err, ErrPlanNotFound) {
					logService.Errorf("finalize pending plan run: run_id=%s error=%v", runID, err)
				} else {
					_ = record
					chunk.Response = &resp
					if strings.TrimSpace(criticNote) != "" {
						select {
						case out <- core.StreamChunk{Type: core.ChunkText, Content: criticNote}:
						case <-ctx.Done():
							return
						}
					}
				}
			}
			if chunk.Type == core.ChunkError {
				if _, err := s.FailPendingPlanRun(runID, chunk.Error); err != nil && !errors.Is(err, ErrPlanNotFound) {
					logService.Errorf("fail pending plan run: run_id=%s error=%v", runID, err)
				}
			}
			select {
			case out <- chunk:
			case <-ctx.Done():
				return
			}
		}
	}()
	return out, nil
}

func plannerSystemPrompt() string {
	return strings.TrimSpace(`
You are the Planner for NekoClaw.
Create an implementation plan only. Do not execute changes.
Use available read-only tools when useful to inspect files or repository state.
Return markdown only with exactly these sections in order:
## Goal
## Context
## Files to Modify
## New Files
## Implementation Steps
## Verification
## Risks
Keep the plan concrete and execution-ready.
`)
}

func executionPrompt(plan core.PlanRecord) string {
	return strings.TrimSpace(fmt.Sprintf(`
Execute the approved implementation plan below in this session.
Follow the steps pragmatically, use tools as needed, and report concrete outcomes.

%s
`, plan.PlanMarkdown))
}

func normalizePlanMarkdown(raw string, fallbackGoal string) string {
	type sectionDef struct {
		title string
		key   string
	}
	order := []sectionDef{
		{title: "Goal", key: "goal"},
		{title: "Context", key: "context"},
		{title: "Files to Modify", key: "files to modify"},
		{title: "New Files", key: "new files"},
		{title: "Implementation Steps", key: "implementation steps"},
		{title: "Verification", key: "verification"},
		{title: "Risks", key: "risks"},
	}
	sections := map[string][]string{}
	current := ""
	for _, line := range strings.Split(strings.ReplaceAll(raw, "\r\n", "\n"), "\n") {
		trimmed := strings.TrimSpace(line)
		heading := normalizePlanHeading(trimmed)
		if heading != "" {
			current = heading
			continue
		}
		if current == "" {
			current = "implementation steps"
		}
		sections[current] = append(sections[current], line)
	}

	if strings.TrimSpace(strings.Join(sections["goal"], "\n")) == "" {
		sections["goal"] = []string{strings.TrimSpace(fallbackGoal)}
	}
	if strings.TrimSpace(strings.Join(sections["implementation steps"], "\n")) == "" {
		raw = strings.TrimSpace(raw)
		if raw != "" {
			sections["implementation steps"] = []string{raw}
		}
	}

	var builder strings.Builder
	for _, item := range order {
		builder.WriteString("## ")
		builder.WriteString(item.title)
		builder.WriteString("\n")
		body := strings.TrimSpace(strings.Join(sections[item.key], "\n"))
		if body == "" {
			body = "- None."
		}
		builder.WriteString(body)
		builder.WriteString("\n\n")
	}
	return strings.TrimSpace(builder.String())
}

func normalizePlanHeading(line string) string {
	line = strings.TrimSpace(line)
	line = strings.TrimLeft(line, "#")
	line = strings.TrimSpace(strings.TrimSuffix(line, ":"))
	switch strings.ToLower(line) {
	case "goal":
		return "goal"
	case "context":
		return "context"
	case "files to modify", "files":
		return "files to modify"
	case "new files":
		return "new files"
	case "implementation steps", "steps":
		return "implementation steps"
	case "verification":
		return "verification"
	case "risks", "risk":
		return "risks"
	default:
		return ""
	}
}

func summarizePlan(markdown string, fallback string) string {
	lines := strings.Split(markdown, "\n")
	start := false
	for _, line := range lines {
		trimmed := strings.TrimSpace(strings.TrimPrefix(line, "-"))
		if strings.EqualFold(strings.TrimSpace(line), "## Goal") {
			start = true
			continue
		}
		if start {
			if strings.HasPrefix(strings.TrimSpace(line), "## ") {
				break
			}
			if trimmed != "" {
				return trimmed
			}
		}
	}
	return strings.TrimSpace(fallback)
}

func finalizePlanExecution(record core.PlanRecord, resp core.ChatResponse) core.PlanRecord {
	now := time.Now().UTC()
	record.ExecutionFinishedAt = &now
	record.LastError = ""
	for _, evt := range resp.ToolEvents {
		if evt.Phase == "denied" && strings.EqualFold(evt.Decision, "deny") {
			record.Status = core.PlanStatusFailed
			record.LastError = "tool_denied"
			return record
		}
	}
	record.Status = core.PlanStatusCompleted
	return record
}

func (s *Service) FinalizePendingPlanRun(runID string, resp core.ChatResponse) (core.PlanRecord, error) {
	record, _, _, err := s.finalizePendingPlanRunResponse(runID, resp)
	return record, err
}

func (s *Service) finalizePendingPlanRunResponse(runID string, resp core.ChatResponse) (core.PlanRecord, core.ChatResponse, string, error) {
	planID, ok := s.planIDForRun(runID)
	if !ok {
		return core.PlanRecord{}, resp, "", ErrPlanNotFound
	}
	return s.finalizePlanExecutionResponse(planID, resp)
}

func (s *Service) FailPendingPlanRun(runID string, reason string) (core.PlanRecord, error) {
	planID, ok := s.planIDForRun(runID)
	if !ok {
		return core.PlanRecord{}, ErrPlanNotFound
	}
	return s.failPlanExecution(planID, reason)
}

func (s *Service) updatePlanExecutionResult(planID string, resp core.ChatResponse) (core.PlanRecord, error) {
	record, _, _, err := s.finalizePlanExecutionResponse(planID, resp)
	return record, err
}

func (s *Service) finalizePlanExecutionResponse(planID string, resp core.ChatResponse) (core.PlanRecord, core.ChatResponse, string, error) {
	record, err := s.GetPlan(planID)
	if err != nil {
		return core.PlanRecord{}, resp, "", err
	}
	if resp.Status == core.ChatStatusApprovalRequired && strings.TrimSpace(resp.RunID) != "" {
		record.Status = core.PlanStatusExecuting
		record.ExecutionFinishedAt = nil
		record.LastError = ""
		s.bindPlanRun(resp.RunID, planID)
		if err := s.plans.Update(record); err != nil {
			return core.PlanRecord{}, resp, "", err
		}
		return record, resp, "", nil
	}
	record = finalizePlanExecution(record, resp)
	var criticNote string
	if record.Status == core.PlanStatusCompleted {
		artifact, criticErr := s.runPlanCritic(record, resp)
		if criticErr != nil {
			logService.Warnf("plan critic: plan_id=%s error=%v", planID, criticErr)
			s.recordTrace(core.TraceRecord{
				SessionID: record.SessionID,
				RunID:     planID,
				RunKind:   "plan_critic_error",
				Prompt:    record.RequestPrompt,
				Reply:     criticErr.Error(),
			})
		} else {
			criticNote = formatCriticNote(artifact)
			resp.SubagentArtifacts = append(resp.SubagentArtifacts, artifact)
			s.attachSubagentArtifactsToLastAssistant(record.SessionID, []core.SubagentArtifact{artifact})
		}
	}
	s.unbindPlanRunByPlan(planID)
	if err := s.plans.Update(record); err != nil {
		return core.PlanRecord{}, resp, criticNote, err
	}
	s.recordTrace(core.TraceRecord{
		SessionID:         record.SessionID,
		RunID:             planID,
		RunKind:           "plan_execution",
		Provider:          resp.Provider,
		Model:             resp.Model,
		Prompt:            record.RequestPrompt,
		Reply:             resp.Reply,
		ToolEvents:        append([]core.ToolEvent(nil), resp.ToolEvents...),
		Reminders:         append([]core.ReminderEvent(nil), resp.Reminders...),
		SubagentArtifacts: append([]core.SubagentArtifact(nil), resp.SubagentArtifacts...),
	})
	s.maybeCreatePlaybookCandidate("plan:"+planID, record.PlanMarkdown, core.PlaybookScopeWorkspace, workspaceScopeValue(s.workspaceRoot))
	return record, resp, criticNote, nil
}

func (s *Service) runPlanCritic(record core.PlanRecord, resp core.ChatResponse) (core.SubagentArtifact, error) {
	return s.SpawnSubagent(
		context.Background(),
		record.SessionID,
		record.Surface,
		"critic",
		fmt.Sprintf("Review the completed approved plan `%s` for regressions, missing verification, or unsafe assumptions.", chooseFirstNonEmpty(record.PlanSummary, record.RequestPrompt)),
		resp.Reply,
	)
}

func formatCriticNote(artifact core.SubagentArtifact) string {
	markdown := strings.TrimSpace(artifact.Markdown)
	if markdown == "" {
		return ""
	}
	return "\n\n" + markdown
}

func (s *Service) failPlanExecution(planID string, reason string) (core.PlanRecord, error) {
	record, err := s.GetPlan(planID)
	if err != nil {
		return core.PlanRecord{}, err
	}
	record.Status = core.PlanStatusFailed
	now := time.Now().UTC()
	record.ExecutionFinishedAt = &now
	record.LastError = strings.TrimSpace(reason)
	if record.LastError == "" {
		record.LastError = "execution_error"
	}
	s.unbindPlanRunByPlan(planID)
	if err := s.plans.Update(record); err != nil {
		return core.PlanRecord{}, err
	}
	return record, nil
}

func (s *Service) bindPlanRun(runID, planID string) {
	runID = strings.TrimSpace(runID)
	planID = strings.TrimSpace(planID)
	if runID == "" || planID == "" {
		return
	}
	s.planRunBindings.Store(runID, planID)
}

func (s *Service) planIDForRun(runID string) (string, bool) {
	value, ok := s.planRunBindings.Load(strings.TrimSpace(runID))
	if !ok {
		return "", false
	}
	planID, ok := value.(string)
	return planID, ok && strings.TrimSpace(planID) != ""
}

func (s *Service) unbindPlanRunByPlan(planID string) {
	planID = strings.TrimSpace(planID)
	if planID == "" {
		return
	}
	s.planRunBindings.Range(func(key, value any) bool {
		if current, ok := value.(string); ok && current == planID {
			s.planRunBindings.Delete(key)
		}
		return true
	})
}
