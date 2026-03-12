package tooling

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/doeshing/nekoclaw/internal/core"
	"github.com/doeshing/nekoclaw/internal/provider"
)

const maxToolRounds = 8

// maxToolResultBytes is the upper bound on tool output stored in context
// messages. Outputs exceeding this are head+tail truncated to preserve
// both the initial context and the final result/error.
const maxToolResultBytes = 30 * 1024 // 30 KB ≈ ~7500 tokens
const doomLoopWindow = 20

type Runtime struct {
	executor  Executor
	approvals *ApprovalStore
}

func NewRuntime(executor Executor, approvals *ApprovalStore) *Runtime {
	return &Runtime{
		executor:  executor,
		approvals: approvals,
	}
}

func (r *Runtime) ReadOnlyToolNames() []string {
	defs := r.executor.Definitions()
	names := make([]string, 0, len(defs))
	for _, def := range defs {
		if r.executor.IsMutating(def.Name) {
			continue
		}
		names = append(names, def.Name)
	}
	sort.Strings(names)
	return names
}

func (r *Runtime) DefaultActionToolNames() []string {
	return []string{
		"file_list",
		"file_read",
		"file_search",
		"git_status",
		"git_diff",
		"sessions_list",
		"memory_search",
		"providers_list",
		"accounts_list",
		"datetime",
		"task_list",
		"task_update",
		"tool_catalog_search",
		"tool_catalog_describe",
		"spawn_subagent",
	}
}

func (r *Runtime) GetPendingRun(runID string) (PendingRun, error) {
	return r.approvals.Get(runID)
}

// emitToolEvent invokes the OnToolEvent callback if configured.
func emitToolEvent(req *RunRequest, evt core.ToolEvent) {
	if req.OnToolEvent != nil {
		req.OnToolEvent(evt)
	}
}

func (r *Runtime) Run(ctx context.Context, req RunRequest) (RunResult, error) {
	if !req.EnableTools {
		return RunResult{}, fmt.Errorf("%w: tools disabled", ErrRunInvalid)
	}
	if req.ToolProvider == nil {
		return RunResult{}, fmt.Errorf("%w: tool provider is required", ErrRunInvalid)
	}
	caps := req.ToolProvider.ToolCapabilities()
	if !caps.SupportsTools {
		return RunResult{}, &provider.FailureError{
			Reason:   core.FailureFormat,
			Message:  "tools_not_supported",
			Endpoint: req.ProviderID,
		}
	}

	decisions := map[string]string{}
	for _, decision := range req.Approvals {
		id := strings.TrimSpace(decision.ApprovalID)
		if id == "" {
			continue
		}
		decisions[id] = strings.ToLower(strings.TrimSpace(decision.Decision))
	}

	var (
		modelID         = strings.TrimSpace(req.ModelID)
		account         = req.Account
		messages        = append([]core.Message(nil), req.Messages...)
		pending         []provider.ToolCall
		events          []core.ToolEvent
		usage           core.UsageInfo
		sessionMsgs     []core.Message
		rawModelContent json.RawMessage // raw model content from current tool turn (e.g. Gemini thought_signature)
	)
	if modelID == "" {
		modelID = "default"
	}
	if len(req.AllowedTools) == 0 {
		if req.ToolMode == core.ToolModePlanner {
			req.AllowedTools = r.ReadOnlyToolNames()
		} else {
			req.AllowedTools = append([]string(nil), r.DefaultActionToolNames()...)
		}
	}
	if strings.TrimSpace(req.RunID) != "" {
		run, err := r.approvals.Get(req.RunID)
		if err != nil {
			return RunResult{}, err
		}
		req.RunID = run.RunID
		req.SessionID = run.SessionID
		req.Surface = run.Surface
		req.ProviderID = run.ProviderID
		modelID = run.ModelID
		account = run.Account
		messages = append([]core.Message(nil), run.Messages...)
		req.AllowedTools = append([]string(nil), run.AllowedTools...)
		pending = append([]provider.ToolCall(nil), run.PendingCalls...)
		events = append([]core.ToolEvent(nil), run.PendingEvents...)
		sessionMsgs = append([]core.Message(nil), run.PendingMessage...)
		usage = run.Usage
		r.approvals.Delete(run.RunID)
	} else if strings.TrimSpace(req.UserMessage.Content) != "" {
		sessionMsgs = append(sessionMsgs, req.UserMessage)
	}

	lastReply := ""
	recentFingerprints := make([]string, 0, doomLoopWindow)
	warnedFingerprints := map[string]struct{}{}
	for round := 0; round < maxToolRounds; round++ {
		if len(pending) == 0 {
			toolDefs := r.filterDefinitions(req.AllowedTools)
			turnResp, err := req.ToolProvider.GenerateToolTurn(ctx, provider.ToolTurnRequest{
				Model:      modelID,
				Messages:   messages,
				Account:    account,
				Tools:      toolDefs,
				Generation: req.Generation,
			})
			if err != nil {
				return RunResult{}, err
			}
			// Each turn's InputTokens includes the full context (history + tool
			// results), so we take the latest value instead of summing.
			// OutputTokens are cumulative across all turns.
			usage.InputTokens = turnResp.Usage.InputTokens
			usage.OutputTokens += turnResp.Usage.OutputTokens
			usage.TotalTokens = usage.InputTokens + usage.OutputTokens
			if txt := strings.TrimSpace(turnResp.Text); txt != "" {
				lastReply = txt
				assistant := core.Message{
					Role:      core.RoleAssistant,
					Content:   txt,
					CreatedAt: time.Now(),
				}
				messages = append(messages, assistant)
				sessionMsgs = append(sessionMsgs, assistant)
			}
			if len(turnResp.ToolCalls) == 0 {
				return RunResult{
					Response: core.ChatResponse{
						SessionID:   req.SessionID,
						Provider:    req.ProviderID,
						Model:       modelID,
						Reply:       lastReply,
						AccountID:   account.ID,
						Usage:       usage,
						Status:      core.ChatStatusCompleted,
						Compressed:  req.Compressed,
						Compression: req.Compression,
						ToolEvents:  events,
					},
					SessionMessages: sessionMsgs,
				}, nil
			}
			pending = append([]provider.ToolCall(nil), turnResp.ToolCalls...)
			// Preserve the raw model content block (e.g. Gemini thought_signature)
			// to be stored on the first assistant message for this tool turn.
			rawModelContent = turnResp.RawModelContent
		}

		needsApproval := false
		pendingApprovals := make([]core.PendingToolApproval, 0, len(pending))
		for _, call := range pending {
			if !r.executor.HasTool(call.Name) {
				continue
			}
			if !r.toolAllowed(req.ToolMode, req.AllowedTools, call.Name) {
				continue
			}
			if !r.executor.IsCallMutating(call) {
				continue
			}
			if _, ok := decisions[call.ID]; !ok {
				needsApproval = true
				pendingApprovals = append(pendingApprovals, core.PendingToolApproval{
					ApprovalID:       call.ID,
					ToolCallID:       call.ID,
					ToolName:         call.Name,
					ArgumentsPreview: r.executor.ArgumentPreview(call),
					RiskLevel:        "mutating",
				})
			}
		}
		if needsApproval {
			run := r.approvals.Put(PendingRun{
				RunID:          req.RunID,
				SessionID:      req.SessionID,
				Surface:        req.Surface,
				ProviderID:     req.ProviderID,
				ModelID:        modelID,
				Account:        account,
				Messages:       messages,
				AllowedTools:   append([]string(nil), req.AllowedTools...),
				Compressed:     req.Compressed,
				Compression:    req.Compression,
				Usage:          usage,
				PendingCalls:   pending,
				PendingEvents:  events,
				PendingMessage: sessionMsgs,
			})
			return RunResult{
				Response: core.ChatResponse{
					SessionID:        req.SessionID,
					Provider:         req.ProviderID,
					Model:            modelID,
					Reply:            "",
					AccountID:        account.ID,
					Usage:            usage,
					Status:           core.ChatStatusApprovalRequired,
					RunID:            run.RunID,
					PendingApprovals: pendingApprovals,
					Compressed:       req.Compressed,
					Compression:      req.Compression,
					ToolEvents:       events,
				},
				SessionMessages: sessionMsgs,
				Pending:         true,
			}, nil
		}

		// consumeRawModel returns the rawModelContent on first call, nil thereafter.
		// This attaches the raw model content block (with thought_signature etc.)
		// to the first assistant message in this tool turn only.
		consumeRawModel := func() json.RawMessage {
			v := rawModelContent
			rawModelContent = nil
			return v
		}

		for _, call := range pending {
			if !r.executor.HasTool(call.Name) {
				errMsg := fmt.Sprintf("unknown tool: %s", call.Name)
				events = append(events, core.ToolEvent{
					At:         time.Now(),
					ToolCallID: call.ID,
					ToolName:   call.Name,
					Phase:      "failed",
					Error:      errMsg,
				})
				assistantTool := core.Message{
					Role:         core.RoleAssistant,
					ToolName:     call.Name,
					ToolCallID:   call.ID,
					Content:      string(call.Arguments),
					ProviderMeta: consumeRawModel(),
					CreatedAt:    time.Now(),
				}
				toolMsg := core.Message{
					Role:       core.RoleTool,
					ToolName:   call.Name,
					ToolCallID: call.ID,
					Content:    errMsg,
					CreatedAt:  time.Now(),
				}
				messages = append(messages, assistantTool, toolMsg)
				sessionMsgs = append(sessionMsgs, assistantTool, toolMsg)
				continue
			}
			if !r.toolAllowed(req.ToolMode, req.AllowedTools, call.Name) {
				errMsg := fmt.Sprintf("tool not allowed for current run: %s", call.Name)
				events = append(events, core.ToolEvent{
					At:         time.Now(),
					ToolCallID: call.ID,
					ToolName:   call.Name,
					Phase:      "failed",
					Error:      errMsg,
				})
				assistantTool := core.Message{
					Role:         core.RoleAssistant,
					ToolName:     call.Name,
					ToolCallID:   call.ID,
					Content:      string(call.Arguments),
					ProviderMeta: consumeRawModel(),
					CreatedAt:    time.Now(),
				}
				toolMsg := core.Message{
					Role:       core.RoleTool,
					ToolName:   call.Name,
					ToolCallID: call.ID,
					Content:    errMsg,
					CreatedAt:  time.Now(),
				}
				messages = append(messages, assistantTool, toolMsg)
				sessionMsgs = append(sessionMsgs, assistantTool, toolMsg)
				continue
			}

			mutating := r.executor.IsCallMutating(call)
			reqEvt := newToolEvent(call.ID, call.Name, "requested", mutating)
			events = append(events, reqEvt)
			emitToolEvent(&req, reqEvt)

			if mutating {
				switch decisions[call.ID] {
				case "allow":
					events = append(events, core.ToolEvent{
						At:         time.Now(),
						ToolCallID: call.ID,
						ToolName:   call.Name,
						Phase:      "approved",
						Mutating:   true,
						Decision:   "allow",
					})
				case "deny":
					events = append(events, core.ToolEvent{
						At:         time.Now(),
						ToolCallID: call.ID,
						ToolName:   call.Name,
						Phase:      "denied",
						Mutating:   true,
						Decision:   "deny",
					})
					assistantTool := core.Message{
						Role:         core.RoleAssistant,
						ToolName:     call.Name,
						ToolCallID:   call.ID,
						Content:      string(call.Arguments),
						ProviderMeta: consumeRawModel(),
						CreatedAt:    time.Now(),
					}
					toolMsg := core.Message{
						Role:       core.RoleTool,
						ToolName:   call.Name,
						ToolCallID: call.ID,
						Content:    "tool execution denied by user",
						CreatedAt:  time.Now(),
					}
					messages = append(messages, assistantTool, toolMsg)
					sessionMsgs = append(sessionMsgs, assistantTool, toolMsg)
					continue
				default:
					return RunResult{}, fmt.Errorf("%w: missing approval for %s", ErrRunInvalid, call.ID)
				}
			}

			assistantTool := core.Message{
				Role:         core.RoleAssistant,
				ToolName:     call.Name,
				ToolCallID:   call.ID,
				Content:      string(call.Arguments),
				ProviderMeta: consumeRawModel(),
				CreatedAt:    time.Now(),
			}
			fingerprint := toolFingerprint(call)
			count := fingerprintCount(recentFingerprints, fingerprint)
			if count >= 3 {
				if _, ok := warnedFingerprints[fingerprint]; ok {
					return RunResult{}, fmt.Errorf("doom loop detected for tool %s", call.Name)
				}
				warnedFingerprints[fingerprint] = struct{}{}
				warnMsg := fmt.Sprintf("[SYSTEM WARNING] repeated tool call detected for %s; choose a different approach before retrying.", call.Name)
				messages = append(messages, core.Message{
					Role:      core.RoleSystem,
					Content:   warnMsg,
					CreatedAt: time.Now(),
				})
				events = append(events, core.ToolEvent{
					At:         time.Now(),
					ToolCallID: call.ID,
					ToolName:   call.Name,
					Phase:      "failed",
					Mutating:   mutating,
					Error:      "doom_loop_warning",
				})
				continue
			}
			content, err := r.executor.Run(ctx, call)
			if err != nil {
				content = "tool_error: " + err.Error()
				failEvt := core.ToolEvent{
					At:         time.Now(),
					ToolCallID: call.ID,
					ToolName:   call.Name,
					Phase:      "failed",
					Mutating:   mutating,
					Error:      trimPreview(err.Error(), 200),
				}
				events = append(events, failEvt)
				emitToolEvent(&req, failEvt)
			} else {
				execEvt := core.ToolEvent{
					At:            time.Now(),
					ToolCallID:    call.ID,
					ToolName:      call.Name,
					Phase:         "executed",
					Mutating:      mutating,
					OutputPreview: trimPreview(content, 200),
				}
				events = append(events, execEvt)
				emitToolEvent(&req, execEvt)
			}
			// Head+tail truncate tool output to prevent oversized context.
			// Preserves both initial context and final results/errors.
			content = truncateHeadTail(content, maxToolResultBytes)

			toolMsg := core.Message{
				Role:       core.RoleTool,
				ToolName:   call.Name,
				ToolCallID: call.ID,
				Content:    content,
				CreatedAt:  time.Now(),
			}
			messages = append(messages, assistantTool, toolMsg)
			sessionMsgs = append(sessionMsgs, assistantTool, toolMsg)
			recentFingerprints = append(recentFingerprints, fingerprint)
			if len(recentFingerprints) > doomLoopWindow {
				recentFingerprints = recentFingerprints[len(recentFingerprints)-doomLoopWindow:]
			}
			if call.Name == "tool_catalog_describe" {
				req.AllowedTools = expandAllowedTools(req.AllowedTools, describedToolNames(call.Arguments))
			}
		}
		pending = nil
	}

	return RunResult{
		Response: core.ChatResponse{
			SessionID:   req.SessionID,
			Provider:    req.ProviderID,
			Model:       modelID,
			Reply:       lastReply,
			AccountID:   account.ID,
			Usage:       usage,
			Status:      core.ChatStatusCompleted,
			Compressed:  req.Compressed,
			Compression: req.Compression,
			ToolEvents:  events,
		},
		SessionMessages: sessionMsgs,
	}, nil
}

func describedToolNames(raw json.RawMessage) []string {
	var args struct {
		Names []string `json:"names"`
	}
	if err := json.Unmarshal(raw, &args); err != nil {
		return nil
	}
	out := make([]string, 0, len(args.Names))
	for _, name := range args.Names {
		name = strings.TrimSpace(name)
		if name == "" {
			continue
		}
		out = append(out, name)
	}
	return out
}

func expandAllowedTools(existing []string, names []string) []string {
	if len(names) == 0 {
		return existing
	}
	set := make(map[string]struct{}, len(existing)+len(names))
	out := make([]string, 0, len(existing)+len(names))
	for _, name := range existing {
		name = strings.TrimSpace(name)
		if name == "" {
			continue
		}
		if _, ok := set[name]; ok {
			continue
		}
		set[name] = struct{}{}
		out = append(out, name)
	}
	for _, name := range names {
		name = strings.TrimSpace(name)
		if name == "" {
			continue
		}
		if _, ok := set[name]; ok {
			continue
		}
		set[name] = struct{}{}
		out = append(out, name)
	}
	return out
}

func toolFingerprint(call provider.ToolCall) string {
	sum := sha256.Sum256(append([]byte(strings.TrimSpace(call.Name)+"|"), []byte(strings.TrimSpace(string(call.Arguments)))...))
	return fmt.Sprintf("%x", sum[:8])
}

func fingerprintCount(window []string, fingerprint string) int {
	count := 0
	for _, item := range window {
		if item == fingerprint {
			count++
		}
	}
	return count
}

func (r *Runtime) filterDefinitions(allowed []string) []provider.ToolDefinition {
	defs := r.executor.Definitions()
	if len(allowed) == 0 {
		return defs
	}
	allow := make(map[string]struct{}, len(allowed))
	for _, name := range allowed {
		name = strings.TrimSpace(name)
		if name == "" {
			continue
		}
		allow[name] = struct{}{}
	}
	filtered := make([]provider.ToolDefinition, 0, len(defs))
	for _, def := range defs {
		if _, ok := allow[def.Name]; ok {
			filtered = append(filtered, def)
		}
	}
	return filtered
}

func (r *Runtime) toolAllowed(mode core.ToolMode, allowed []string, toolName string) bool {
	if mode != core.ToolModePlanner && r.executor.HasTool(toolName) {
		return true
	}
	if len(allowed) == 0 {
		return true
	}
	toolName = strings.TrimSpace(toolName)
	for _, name := range allowed {
		if strings.TrimSpace(name) == toolName {
			return true
		}
	}
	return false
}
