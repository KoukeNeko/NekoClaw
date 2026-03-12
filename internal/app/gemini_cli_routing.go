package app

import (
	"context"
	"strings"

	"github.com/doeshing/nekoclaw/internal/core"
	"github.com/doeshing/nekoclaw/internal/provider"
)

const (
	geminiCLI31ProModelID               = "gemini-3.1-pro-preview"
	geminiCLIRouteSkipImagesUnsupported = "images_not_supported"
	geminiCLIRouteSkipModelNot31Pro     = "model_not_3_1_pro"
	geminiCLIRouteSkipRunnerUnavailable = "runner_unavailable"
	geminiCLIRouteSkipToolsEnabled      = "tools_enabled"
	geminiCLIRouteSkipWorkspaceMissing  = "workspace_root_missing"
)

type geminiCLIExecutionRequest struct {
	Surface     string
	ProviderID  string
	ModelID     string
	Account     core.Account
	Messages    []core.Message
	EnableTools bool
	ToolMode    core.ToolMode
	OnText      func(string)
}

type geminiCLIProviderAdapter struct {
	service *Service
	base    provider.Provider
	surface string
}

func (s *Service) prepareGeminiRuntimeTarget(
	ctx context.Context,
	prov provider.Provider,
	pool *core.AccountPool,
	account core.Account,
	requestedModelID string,
) (core.Account, string, bool, error) {
	providerID := ""
	if prov != nil {
		providerID = strings.TrimSpace(prov.ID())
	}
	requestedModelID = strings.TrimSpace(requestedModelID)
	if providerID != geminiCLIProviderID {
		return account, requestedModelID, true, nil
	}

	updatedAccount, err := s.ensureGeminiProject(ctx, prov, pool, account)
	if err != nil {
		return account, requestedModelID, false, err
	}

	resolvedModel, source, usable := s.resolveRuntimeModel(ctx, prov, providerID, updatedAccount, requestedModelID)
	if !usable {
		return updatedAccount, requestedModelID, false, nil
	}
	if resolvedModel != requestedModelID {
		logService.Logf(
			"runtime model resolved: provider=%s profile_id=%s source=%s requested_model=%s resolved_model=%s",
			providerID,
			updatedAccount.ID,
			source,
			requestedModelID,
			resolvedModel,
		)
	}
	return updatedAccount, strings.TrimSpace(resolvedModel), true, nil
}

func (s *Service) shouldUseGeminiCLIHeadless(
	providerID string,
	modelID string,
	account core.Account,
	messages []core.Message,
	enableTools bool,
	toolMode core.ToolMode,
) (bool, string) {
	if strings.TrimSpace(providerID) != geminiCLIProviderID {
		return false, ""
	}
	if s.geminiExecutionModeForAccount(account) != geminiExecutionModeCLIHeadless {
		return false, ""
	}
	if s.geminiCLIRunner == nil {
		return false, geminiCLIRouteSkipRunnerUnavailable
	}
	if !matchesModelID(modelID, geminiCLI31ProModelID) {
		return false, geminiCLIRouteSkipModelNot31Pro
	}
	// Planner requests intentionally remain CLI-eligible for 3.1 Pro mode.
	// The CLI path is plain generation only and does not hand off NekoClaw tools.
	if enableTools && toolMode != core.ToolModePlanner {
		return false, geminiCLIRouteSkipToolsEnabled
	}
	if !canUseGeminiCLIHeadless(messages) {
		return false, geminiCLIRouteSkipImagesUnsupported
	}
	return true, ""
}

func (s *Service) maybeGenerateWithGeminiCLIHeadless(
	ctx context.Context,
	req geminiCLIExecutionRequest,
) (geminiCLIHeadlessResponse, bool, error) {
	shouldUse, reason := s.shouldUseGeminiCLIHeadless(
		req.ProviderID,
		req.ModelID,
		req.Account,
		req.Messages,
		req.EnableTools,
		req.ToolMode,
	)
	if !shouldUse {
		s.logGeminiCLIRouteSkipped(req, reason)
		return geminiCLIHeadlessResponse{}, false, nil
	}
	if strings.TrimSpace(s.workspaceRoot) == "" {
		s.logGeminiCLIRouteSkipped(req, geminiCLIRouteSkipWorkspaceMissing)
		return geminiCLIHeadlessResponse{}, false, nil
	}

	store := s.authStoreSafe()
	if store == nil {
		return geminiCLIHeadlessResponse{}, true, &provider.FailureError{
			Reason:   core.FailureAuthPermanent,
			Message:  "auth store not configured",
			Endpoint: geminiCLIProviderID,
		}
	}

	credential, err := s.loadGeminiCLICredential(req.Account.ID, req.Account.Token)
	if err != nil {
		return geminiCLIHeadlessResponse{}, true, &provider.FailureError{
			Reason:   core.FailureAuthPermanent,
			Message:  err.Error(),
			Endpoint: geminiCLIProviderID,
		}
	}

	cliReq := geminiCLIHeadlessRequest{
		ProfileID:     req.Account.ID,
		Model:         req.ModelID,
		Prompt:        renderGeminiCLIHeadlessPrompt(req.Messages),
		Credential:    credential,
		WorkspaceRoot: s.workspaceRoot,
		AuthBaseDir:   store.BaseDir(),
	}

	var resp geminiCLIHeadlessResponse
	if req.OnText != nil {
		resp, err = s.geminiCLIRunner.GenerateStream(ctx, cliReq, req.OnText)
	} else {
		resp, err = s.geminiCLIRunner.Generate(ctx, cliReq)
	}
	if routeErr, ok := asGeminiCLIRouteUnavailable(err); ok {
		s.logGeminiCLIRouteSkipped(req, routeErr.Reason)
		return geminiCLIHeadlessResponse{}, false, nil
	}

	logService.Logf(
		"cli route selected: surface=%s provider=%s profile_id=%s effective_model=%s",
		strings.TrimSpace(req.Surface),
		req.ProviderID,
		req.Account.ID,
		req.ModelID,
	)
	return resp, true, err
}

func (s *Service) logGeminiCLIRouteSkipped(req geminiCLIExecutionRequest, reason string) {
	reason = strings.TrimSpace(reason)
	if reason == "" {
		return
	}
	logService.Logf(
		"cli route skipped: surface=%s provider=%s profile_id=%s effective_model=%s reason=%s",
		strings.TrimSpace(req.Surface),
		req.ProviderID,
		req.Account.ID,
		req.ModelID,
		reason,
	)
}

func (p *geminiCLIProviderAdapter) ID() string {
	if p.base == nil {
		return ""
	}
	return p.base.ID()
}

func (p *geminiCLIProviderAdapter) ContextWindow(model string) int {
	if p.base == nil {
		return 0
	}
	return p.base.ContextWindow(model)
}

func (p *geminiCLIProviderAdapter) Generate(
	ctx context.Context,
	req provider.GenerateRequest,
) (provider.GenerateResponse, error) {
	if p.base == nil || p.service == nil {
		return provider.GenerateResponse{}, nil
	}

	cliResp, routed, err := p.service.maybeGenerateWithGeminiCLIHeadless(ctx, geminiCLIExecutionRequest{
		Surface:    p.surface,
		ProviderID: p.base.ID(),
		ModelID:    strings.TrimSpace(req.Model),
		Account:    req.Account,
		Messages:   req.Messages,
	})
	if err != nil {
		return provider.GenerateResponse{}, err
	}
	if routed {
		return provider.GenerateResponse{
			Text:  cliResp.Text,
			Usage: cliResp.Usage,
		}, nil
	}
	return p.base.Generate(ctx, req)
}
