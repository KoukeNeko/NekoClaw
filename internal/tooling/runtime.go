package tooling

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/doeshing/nekoclaw/internal/core"
	"github.com/doeshing/nekoclaw/internal/provider"
)

const maxToolRounds = 8

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
		modelID     = strings.TrimSpace(req.ModelID)
		account     = req.Account
		messages    = append([]core.Message(nil), req.Messages...)
		pending     []provider.ToolCall
		events      []core.ToolEvent
		usage       core.UsageInfo
		sessionMsgs []core.Message
	)
	if modelID == "" {
		modelID = "default"
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
		pending = append([]provider.ToolCall(nil), run.PendingCalls...)
		events = append([]core.ToolEvent(nil), run.PendingEvents...)
		sessionMsgs = append([]core.Message(nil), run.PendingMessage...)
		usage = run.Usage
		r.approvals.Delete(run.RunID)
	} else if strings.TrimSpace(req.UserMessage.Content) != "" {
		sessionMsgs = append(sessionMsgs, req.UserMessage)
	}

	lastReply := ""
	for round := 0; round < maxToolRounds; round++ {
		if len(pending) == 0 {
			turnResp, err := req.ToolProvider.GenerateToolTurn(ctx, provider.ToolTurnRequest{
				Model:    modelID,
				Messages: messages,
				Account:  account,
				Tools:    r.executor.Definitions(),
			})
			if err != nil {
				return RunResult{}, err
			}
			usage.InputTokens += turnResp.Usage.InputTokens
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
		}

		needsApproval := false
		pendingApprovals := make([]core.PendingToolApproval, 0, len(pending))
		for _, call := range pending {
			if !r.executor.HasTool(call.Name) {
				continue
			}
			if !r.executor.IsCallMutating(call) {
				continue
			}
			if req.Surface != core.SurfaceTUI {
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
				messages = append(messages, core.Message{
					Role:       core.RoleTool,
					ToolName:   call.Name,
					ToolCallID: call.ID,
					Content:    errMsg,
					CreatedAt:  time.Now(),
				})
				sessionMsgs = append(sessionMsgs, messages[len(messages)-1])
				continue
			}

			mutating := r.executor.IsCallMutating(call)
			events = append(events, newToolEvent(call.ID, call.Name, "requested", mutating))

			if mutating && req.Surface != core.SurfaceTUI {
				errMsg := "approval_not_supported_for_surface"
				events = append(events, core.ToolEvent{
					At:         time.Now(),
					ToolCallID: call.ID,
					ToolName:   call.Name,
					Phase:      "denied",
					Mutating:   true,
					Decision:   "deny",
					Error:      errMsg,
				})
				assistantTool := core.Message{
					Role:       core.RoleAssistant,
					ToolName:   call.Name,
					ToolCallID: call.ID,
					Content:    string(call.Arguments),
					CreatedAt:  time.Now(),
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
						Role:       core.RoleAssistant,
						ToolName:   call.Name,
						ToolCallID: call.ID,
						Content:    string(call.Arguments),
						CreatedAt:  time.Now(),
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
				Role:       core.RoleAssistant,
				ToolName:   call.Name,
				ToolCallID: call.ID,
				Content:    string(call.Arguments),
				CreatedAt:  time.Now(),
			}
			content, err := r.executor.Run(ctx, call)
			if err != nil {
				content = "tool_error: " + err.Error()
				events = append(events, core.ToolEvent{
					At:         time.Now(),
					ToolCallID: call.ID,
					ToolName:   call.Name,
					Phase:      "failed",
					Mutating:   mutating,
					Error:      trimPreview(err.Error(), 200),
				})
			} else {
				events = append(events, core.ToolEvent{
					At:            time.Now(),
					ToolCallID:    call.ID,
					ToolName:      call.Name,
					Phase:         "executed",
					Mutating:      mutating,
					OutputPreview: trimPreview(content, 200),
				})
			}
			toolMsg := core.Message{
				Role:       core.RoleTool,
				ToolName:   call.Name,
				ToolCallID: call.ID,
				Content:    content,
				CreatedAt:  time.Now(),
			}
			messages = append(messages, assistantTool, toolMsg)
			sessionMsgs = append(sessionMsgs, assistantTool, toolMsg)
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
