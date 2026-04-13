package tooling

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"os"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/doeshing/nekoclaw/internal/core"
	"github.com/doeshing/nekoclaw/internal/provider"
)

const maxToolRounds = 8
const maxParallelToolWorkers = 4

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
		if !r.executor.IsReadOnlySafe(def.Name) {
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
		"memory_get",
		"memory_save",
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
		decisions[id] = normalizeApprovalDecision(decision.Decision)
	}

	var (
		modelID         = strings.TrimSpace(req.ModelID)
		account         = req.Account
		messages        = append([]core.Message(nil), req.Messages...)
		pending         []provider.ToolCall
		events          []core.ToolEvent
		usage           core.UsageInfo
		sessionMsgs     []core.Message
		observedFiles   = map[string]string{}
		mutatedFiles    = map[string]struct{}{}
		snapshotID      string
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
		observedFiles = cloneObservedFiles(run.ObservedFiles)
		mutatedFiles = cloneMutatedFiles(run.MutatedFiles)
		snapshotID = strings.TrimSpace(run.SnapshotID)
		r.approvals.Delete(run.RunID)
	} else if strings.TrimSpace(req.UserMessage.Content) != "" {
		sessionMsgs = append(sessionMsgs, req.UserMessage)
	}

	// Build schema map for argument type coercion (open-source models
	// frequently send wrong types, e.g. string "5" instead of int 5).
	schemaMap := map[string]json.RawMessage{}
	for _, def := range r.executor.Definitions() {
		if len(def.InputSchema) > 0 {
			schemaMap[def.Name] = def.InputSchema
		}
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
			if !r.executor.RequiresApproval(call) {
				continue
			}
			if _, ok := decisions[call.ID]; !ok {
				if req.LookupGrant != nil {
					if decision, ok := req.LookupGrant(req.SessionID, call.Name, selectorForCall(req, call)); ok {
						decisions[call.ID] = normalizeApprovalDecision(decision)
					}
				}
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
				ObservedFiles:  cloneObservedFiles(observedFiles),
				MutatedFiles:   cloneMutatedFiles(mutatedFiles),
				SnapshotID:     snapshotID,
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

		// Pre-execute eligible read-only tools in parallel for performance.
		prefetched := r.prefetchReadOnlyTools(ctx, req, pending)

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
			requiresApproval := r.executor.RequiresApproval(call)
			reqEvt := newToolEvent(call.ID, call.Name, "requested", mutating)
			events = append(events, reqEvt)
			emitToolEvent(&req, reqEvt)

			if requiresApproval {
				decision := normalizeApprovalDecision(decisions[call.ID])
				switch decision {
				case "allow_once", "allow_for_session", "allow_for_workspace":
					if decision != "allow_once" && req.PersistGrant != nil {
						if err := req.PersistGrant(req.SessionID, call.Name, selectorForCall(req, call), decision); err != nil {
							return RunResult{}, err
						}
					}
					events = append(events, core.ToolEvent{
						At:         time.Now(),
						ToolCallID: call.ID,
						ToolName:   call.Name,
						Phase:      "approved",
						Mutating:   mutating,
						Decision:   decision,
					})
				case "deny":
					events = append(events, core.ToolEvent{
						At:         time.Now(),
						ToolCallID: call.ID,
						ToolName:   call.Name,
						Phase:      "denied",
						Mutating:   mutating,
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
			if requiresApproval && req.PrepareMutation != nil {
				record, err := req.PrepareMutation(req.SessionID, call, snapshotID)
				if err != nil {
					return RunResult{}, err
				}
				if record != nil {
					snapshotID = strings.TrimSpace(record.ID)
					if req.OnSnapshotCreated != nil {
						req.OnSnapshotCreated(*record)
					}
				}
			}
			if requiresApproval {
				if staleErr := validateStaleRead(call, selectorForCall(req, call), observedFiles, mutatedFiles); staleErr != nil {
					content := "tool_error: " + staleErr.Error()
					failEvt := core.ToolEvent{
						At:         time.Now(),
						ToolCallID: call.ID,
						ToolName:   call.Name,
						Phase:      "failed",
						Mutating:   true,
						Error:      trimPreview(staleErr.Error(), 200),
					}
					events = append(events, failEvt)
					emitToolEvent(&req, failEvt)
					toolMsg := core.Message{
						Role:       core.RoleTool,
						ToolName:   call.Name,
						ToolCallID: call.ID,
						Content:    truncateHeadTail(content, maxToolResultBytes),
						CreatedAt:  time.Now(),
					}
					messages = append(messages, assistantTool, toolMsg)
					sessionMsgs = append(sessionMsgs, assistantTool, toolMsg)
					continue
				}
			}
			// Coerce argument types to match JSON Schema declarations.
			if schema, ok := schemaMap[call.Name]; ok {
				call.Arguments = coerceToolArguments(call.Arguments, schema)
			}
			// Use prefetched result if available (read-only tools executed in parallel).
			var content string
			var err error
			if pf, ok := prefetched[call.ID]; ok {
				content, err = pf.content, pf.err
			} else {
				content, err = r.executor.Run(ctx, call)
			}
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
			updateObservedState(call, selectorForCall(req, call), err == nil, observedFiles, mutatedFiles)
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

func normalizeApprovalDecision(raw string) string {
	switch strings.ToLower(strings.TrimSpace(raw)) {
	case "", "allow", "allow_once":
		return "allow_once"
	case "allow_for_session":
		return "allow_for_session"
	case "allow_for_workspace":
		return "allow_for_workspace"
	case "deny":
		return "deny"
	default:
		return strings.ToLower(strings.TrimSpace(raw))
	}
}

func selectorForCall(req RunRequest, call provider.ToolCall) string {
	if req.SelectorForCall == nil {
		return ""
	}
	return strings.TrimSpace(req.SelectorForCall(call))
}

func cloneObservedFiles(src map[string]string) map[string]string {
	if len(src) == 0 {
		return map[string]string{}
	}
	out := make(map[string]string, len(src))
	for k, v := range src {
		out[k] = v
	}
	return out
}

func cloneMutatedFiles(src map[string]struct{}) map[string]struct{} {
	if len(src) == 0 {
		return map[string]struct{}{}
	}
	out := make(map[string]struct{}, len(src))
	for k := range src {
		out[k] = struct{}{}
	}
	return out
}

func validateStaleRead(call provider.ToolCall, selector string, observedFiles map[string]string, mutatedFiles map[string]struct{}) error {
	if !isStaleReadProtectedCall(call.Name) {
		return nil
	}
	path := strings.TrimSpace(selector)
	if path == "" {
		return nil
	}
	if _, ok := mutatedFiles[path]; ok {
		return nil
	}
	observedHash, ok := observedFiles[path]
	if !ok {
		return nil
	}
	currentHash, err := hashFileState(path)
	if err != nil {
		return err
	}
	if currentHash == observedHash {
		return nil
	}
	return fmt.Errorf("stale read detected for %s; reread the file before modifying it", path)
}

func isStaleReadProtectedCall(toolName string) bool {
	switch strings.TrimSpace(toolName) {
	case "file_write", "file_replace":
		return true
	default:
		return false
	}
}

func updateObservedState(call provider.ToolCall, selector string, succeeded bool, observedFiles map[string]string, mutatedFiles map[string]struct{}) {
	if !succeeded {
		return
	}
	path := strings.TrimSpace(selector)
	if path == "" {
		return
	}
	switch strings.TrimSpace(call.Name) {
	case "file_read":
		if hash, err := hashFileState(path); err == nil {
			observedFiles[path] = hash
		}
	case "file_write", "file_replace":
		if hash, err := hashFileState(path); err == nil {
			observedFiles[path] = hash
		}
		mutatedFiles[path] = struct{}{}
	}
}

func hashFileState(path string) (string, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return "__missing__", nil
		}
		return "", err
	}
	sum := sha256.Sum256(data)
	return fmt.Sprintf("%x", sum[:]), nil
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

// prefetchResult holds the output of a prefetched read-only tool execution.
type prefetchResult struct {
	content string
	err     error
}

// prefetchReadOnlyTools identifies eligible read-only tool calls from the
// pending list and executes them concurrently. Returns a map from call ID
// to result. Returns nil if fewer than 2 calls are eligible (no parallelism
// benefit). The existing sequential loop uses prefetch results when available
// instead of calling executor.Run again.
func (r *Runtime) prefetchReadOnlyTools(
	ctx context.Context,
	req RunRequest,
	pending []provider.ToolCall,
) map[string]prefetchResult {
	var eligible []provider.ToolCall
	for _, call := range pending {
		if !r.executor.HasTool(call.Name) {
			continue
		}
		if !r.toolAllowed(req.ToolMode, req.AllowedTools, call.Name) {
			continue
		}
		if !r.executor.IsReadOnlySafe(call.Name) {
			continue
		}
		if r.executor.RequiresApproval(call) {
			continue
		}
		eligible = append(eligible, call)
	}
	if len(eligible) < 2 {
		return nil
	}

	results := make(map[string]prefetchResult, len(eligible))
	var mu sync.Mutex
	var wg sync.WaitGroup
	sem := make(chan struct{}, maxParallelToolWorkers)

	for _, call := range eligible {
		wg.Add(1)
		go func(c provider.ToolCall) {
			defer wg.Done()
			sem <- struct{}{}
			defer func() { <-sem }()
			content, err := r.executor.Run(ctx, c)
			mu.Lock()
			results[c.ID] = prefetchResult{content: content, err: err}
			mu.Unlock()
		}(call)
	}
	wg.Wait()

	return results
}
