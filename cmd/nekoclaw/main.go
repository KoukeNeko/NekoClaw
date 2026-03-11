package main

import (
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"io/fs"
	"log"
	"net"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/doeshing/nekoclaw/internal/api"
	"github.com/doeshing/nekoclaw/internal/app"
	"github.com/doeshing/nekoclaw/internal/auth"
	"github.com/doeshing/nekoclaw/internal/backup"
	"github.com/doeshing/nekoclaw/internal/core"
	"github.com/doeshing/nekoclaw/internal/discord"
	"github.com/doeshing/nekoclaw/internal/logger"
	"github.com/doeshing/nekoclaw/internal/memory"
	"github.com/doeshing/nekoclaw/internal/provider"
	"github.com/doeshing/nekoclaw/internal/security"
	"github.com/doeshing/nekoclaw/internal/telegram"
	"github.com/doeshing/nekoclaw/web"
)

var logSystem = logger.New("system", logger.White)

type accountFile struct {
	Accounts []core.Account `json:"accounts"`
}

func main() {
	var (
		mode               = flag.String("mode", "web", "run mode: api | web")
		addr               = flag.String("addr", "127.0.0.1:8085", "api listen address")
		defaultProvider    = flag.String("provider", "google-gemini-cli", "default provider")
		defaultModel       = flag.String("model", "default", "default model")
		accountsPath       = flag.String("accounts", "./accounts.json", "account json path")
		geminiEndpoints    = flag.String("gemini-endpoints", defaultGeminiEndpoints(), "comma-separated gemini internal endpoints")
		geminiGeneratePath = flag.String("gemini-generate-path", "/v1internal:streamGenerateContent?alt=sse", "gemini internal generate path")
		authDir            = flag.String("auth-dir", "", "auth state directory (defaults to ~/.nekoclaw/auth)")
		sessionsDir        = flag.String("sessions-dir", "", "session persistence directory (defaults to ~/.nekoclaw/sessions)")
		memoryDir          = flag.String("memory-dir", "", "memory directory for MEMORY.md and daily logs (defaults to ~/.nekoclaw/memory)")
		callbackHost       = flag.String("oauth-callback-host", "127.0.0.1", "gemini oauth callback host")
		callbackPort       = flag.Int("oauth-callback-port", 8085, "gemini oauth callback port")
	)
	flag.Parse()
	runMode := strings.ToLower(strings.TrimSpace(*mode))

	closeLogs := configureRuntimeLogging(runMode, *authDir)
	defer closeLogs()

	service, err := buildService(buildServiceOptions{
		AccountsPath:       *accountsPath,
		GeminiEndpoints:    *geminiEndpoints,
		GeminiGeneratePath: *geminiGeneratePath,
		AuthDir:            *authDir,
		SessionsDir:        *sessionsDir,
		MemoryDir:          *memoryDir,
		OAuthCallbackHost:  *callbackHost,
		OAuthCallbackPort:  *callbackPort,
		APIAddr:            *addr,
	})
	if err != nil {
		fatal(err)
	}

	providerExplicit := flagWasProvided("provider")
	modelExplicit := flagWasProvided("model")
	resolvedProvider, resolvedModel := resolveRuntimeDefaultSelection(
		service.GetDefaultProvider(),
		service.GetDefaultModel(),
		*defaultProvider,
		*defaultModel,
		providerExplicit,
		modelExplicit,
	)

	// Start MCP server connections (non-fatal on failure).
	if err := service.StartMCP(context.Background()); err != nil {
		logSystem.Errorf("mcp start: %v", err)
	}

	// Load persona definitions (non-fatal on failure).
	if err := service.StartPersonas(); err != nil {
		logSystem.Errorf("personas start: %v", err)
	}
	defer func() {
		if err := service.StopMCP(); err != nil {
			logSystem.Errorf("mcp stop: %v", err)
		}
	}()

	// Apply startup defaults after config load so explicit flags/env can override
	// saved settings while preserving persisted web UI selections by default.
	service.SetDefaultProvider(resolvedProvider)
	service.SetDefaultModel(resolvedModel)

	// Start Discord bot if token is configured (runs in all modes).
	discordBot, discordErr := startDiscordBot(service)
	if discordErr != nil {
		logSystem.Errorf("discord bot init: %v", discordErr)
	}

	// Start Telegram bot if token is configured (runs in all modes).
	telegramBot, telegramErr := startTelegramBot(service)
	if telegramErr != nil {
		logSystem.Errorf("telegram bot init: %v", telegramErr)
	}

	switch runMode {
	case "api":
		ctx, cancel := context.WithCancel(context.Background())
		defer cancel()
		service.StartBackgroundCompaction(ctx)

		var wg sync.WaitGroup
		if discordBot != nil {
			wg.Add(1)
			go func() {
				defer wg.Done()
				if err := discordBot.Start(ctx); err != nil {
					logSystem.Errorf("discord bot: %v", err)
				}
			}()
		}
		if telegramBot != nil {
			wg.Add(1)
			go func() {
				defer wg.Done()
				if err := telegramBot.Start(ctx); err != nil {
					logSystem.Errorf("telegram bot: %v", err)
				}
			}()
		}

		server := api.NewServer(service)
		fmt.Printf("NekoClaw API listening on %s\n", *addr)
		apiErr := server.Run(ctx, *addr)
		cancel()
		wg.Wait()
		fatal(apiErr)
	case "web":
		ctx, cancel := context.WithCancel(context.Background())
		defer cancel()
		service.StartBackgroundCompaction(ctx)

		var wg sync.WaitGroup

		if discordBot != nil {
			wg.Add(1)
			go func() {
				defer wg.Done()
				if err := discordBot.Start(ctx); err != nil {
					logSystem.Errorf("discord bot: %v", err)
				}
			}()
		}
		if telegramBot != nil {
			wg.Add(1)
			go func() {
				defer wg.Done()
				if err := telegramBot.Start(ctx); err != nil {
					logSystem.Errorf("telegram bot: %v", err)
				}
			}()
		}

		server := api.NewServer(service)
		server.SetConsoleLogPath(resolveDefaultLogFilePath(*authDir))
		securityRuntime, err := security.NewRuntime(security.RuntimeOptions{
			BaseDir: *authDir,
			Config:  service.GetSecurityConfig(),
		})
		if err != nil {
			fatal(fmt.Errorf("init browser security: %w", err))
		}
		server.SetSecurityRuntime(securityRuntime)
		webFS, fsErr := fs.Sub(web.DistFS, "dist")
		if fsErr != nil {
			fatal(fmt.Errorf("embed web dist: %w", fsErr))
		}
		server.SetWebFS(webFS)
		if status := securityRuntime.Status("", "boot"); !status.Initialized {
			token, tokenErr := securityRuntime.EnsureSetupToken()
			if tokenErr != nil && !errors.Is(tokenErr, security.ErrAlreadyInitialized) {
				fatal(fmt.Errorf("issue setup token: %w", tokenErr))
			}
			if strings.TrimSpace(token) != "" {
				setupURL := buildSecuritySetupURL(*addr, token)
				fmt.Printf("NekoClaw admin setup URL: %s\n", setupURL)
				logSystem.Logf("admin setup url: %s", setupURL)
			}
		}

		fmt.Printf("NekoClaw Web UI listening on http://%s\n", *addr)
		logSystem.Logf("web ui listening: http://%s", *addr)
		apiErr := server.Run(ctx, *addr)
		cancel()
		wg.Wait()
		fatal(apiErr)
	default:
		fatal(fmt.Errorf("unsupported mode %q", *mode))
	}
}

// startDiscordBot creates a Discord bot if a token is available in persisted config.
// Returns nil bot (no error) when no token is configured.
func startDiscordBot(svc *app.Service) (*discord.Bot, error) {
	cfg := svc.GetDiscordConfig()
	token := strings.TrimSpace(cfg.BotToken)

	if token == "" {
		return nil, nil
	}

	bot, err := discord.New(svc, discord.Config{
		Token:    token,
		StateDir: resolveStateDir(),
	})
	if err != nil {
		return nil, err
	}
	logSystem.Logf("discord bot configured")
	return bot, nil
}

// startTelegramBot creates a Telegram bot if a token is available in persisted config.
// Returns nil bot (no error) when no token is configured.
func startTelegramBot(svc *app.Service) (*telegram.Bot, error) {
	cfg := svc.GetTelegramConfig()
	token := strings.TrimSpace(cfg.BotToken)

	if token == "" {
		return nil, nil
	}

	bot, err := telegram.New(svc, telegram.Config{
		Token:    token,
		StateDir: resolveStateDir(),
	})
	if err != nil {
		return nil, err
	}
	logSystem.Logf("telegram bot configured")
	return bot, nil
}

func configureRuntimeLogging(runMode string, authDir string) func() {
	switch runMode {
	case "web":
		logPath := resolveDefaultLogFilePath(authDir)
		if logPath == "" {
			log.SetOutput(io.Discard)
			logger.SetOutput(io.Discard)
			return func() {}
		}
		if err := os.MkdirAll(filepath.Dir(logPath), 0o755); err != nil {
			log.SetOutput(io.Discard)
			logger.SetOutput(io.Discard)
			return func() {}
		}
		file, err := os.OpenFile(logPath, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o644)
		if err != nil {
			log.SetOutput(io.Discard)
			logger.SetOutput(io.Discard)
			return func() {}
		}
		log.SetOutput(file)
		logger.SetOutput(file)
		logSystem.Logf("logging redirect: mode=%s path=%s", runMode, logPath)
		return func() {
			_ = file.Close()
		}
	default:
		return func() {}
	}
}

func resolveDefaultLogFilePath(authDir string) string {
	base := strings.TrimSpace(authDir)
	if base == "" {
		home, err := os.UserHomeDir()
		if err != nil || strings.TrimSpace(home) == "" {
			return ""
		}
		base = filepath.Join(home, ".nekoclaw", "auth")
	}
	root := base
	if strings.EqualFold(filepath.Base(base), "auth") {
		root = filepath.Dir(base)
	}
	return filepath.Join(root, "logs", "nekoclaw.log")
}

type buildServiceOptions struct {
	AccountsPath       string
	GeminiEndpoints    string
	GeminiGeneratePath string
	AuthDir            string
	SessionsDir        string
	MemoryDir          string
	OAuthCallbackHost  string
	OAuthCallbackPort  int
	APIAddr            string
}

func buildService(opts buildServiceOptions) (*app.Service, error) {
	sessionsDir := strings.TrimSpace(opts.SessionsDir)
	if sessionsDir == "" {
		if home, err := os.UserHomeDir(); err == nil && strings.TrimSpace(home) != "" {
			sessionsDir = filepath.Join(home, ".nekoclaw", "sessions")
		}
	}
	var sessionStore *core.SessionStore
	if sessionsDir != "" {
		var err error
		sessionStore, err = core.NewPersistentSessionStore(sessionsDir)
		if err != nil {
			return nil, fmt.Errorf("init session store: %w", err)
		}
	}

	memoryDir := strings.TrimSpace(opts.MemoryDir)
	if memoryDir == "" {
		if home, err := os.UserHomeDir(); err == nil && strings.TrimSpace(home) != "" {
			memoryDir = filepath.Join(home, ".nekoclaw", "memory")
		}
	}

	var searchIndex *memory.SearchIndex
	if memoryDir != "" {
		dbPath := filepath.Join(memoryDir, "search.db")
		var idxErr error
		searchIndex, idxErr = memory.NewSearchIndex(dbPath)
		if idxErr != nil {
			logSystem.Errorf("search index init: %v", idxErr)
		}
	}

	var lifecycle *core.SessionLifecycle
	if sessionStore != nil {
		lifecycle = core.NewSessionLifecycle(sessionStore, core.DefaultLifecycleConfig())
		// Run periodic housekeeping (retention cleanup, session rotation) in the background.
		go runHousekeepingLoop(lifecycle)
	}
	workspaceRoot, _ := os.Getwd()
	stateDir := resolveStateDir()

	// Resolve MCP config directory (default: ~/.nekoclaw/mcp/).
	mcpDir := ""
	if mcpDir == "" {
		if home, err := os.UserHomeDir(); err == nil && strings.TrimSpace(home) != "" {
			mcpDir = filepath.Join(home, ".nekoclaw", "mcp")
		}
	}

	// Resolve personas directory (default: ~/.nekoclaw/personas/).
	personasDir := ""
	if personasDir == "" {
		if home, err := os.UserHomeDir(); err == nil && strings.TrimSpace(home) != "" {
			personasDir = filepath.Join(home, ".nekoclaw", "personas")
		}
	}

	// Resolve config directory early — needed for tool settings and later for
	// fallback chain / Discord / Telegram config.
	configDir := ""
	if home, homeErr := os.UserHomeDir(); homeErr == nil && strings.TrimSpace(home) != "" {
		configDir = filepath.Join(home, ".nekoclaw")
	}

	appConfig, configErr := core.LoadConfig(configDir)
	if configErr != nil {
		logSystem.Errorf("config load: %v", configErr)
	}

	svc := app.NewService(app.ServiceOptions{
		SessionStore:     sessionStore,
		Lifecycle:        lifecycle,
		MemoryDir:        memoryDir,
		SearchIndex:      searchIndex,
		WorkspaceRoot:    workspaceRoot,
		ToolRunTTL:       10 * time.Minute,
		MCPConfigDir:     mcpDir,
		PersonasDir:      personasDir,
		ToolsConfig:      appConfig.Tools,
		CompactionConfig: appConfig.Compaction,
	})

	mockProvider := provider.NewMockProvider()
	svc.RegisterProvider(mockProvider)
	svc.RegisterPool(core.NewAccountPool("mock", []core.Account{
		{ID: "mock-default", Provider: "mock", Type: core.AccountAPIKey, Token: "mock"},
	}, nil, core.DefaultCooldownConfig()))

	geminiProvider := provider.NewGeminiInternalProvider(provider.GeminiInternalOptions{
		Endpoints:    splitCSV(opts.GeminiEndpoints),
		GeneratePath: opts.GeminiGeneratePath,
	})
	svc.RegisterProvider(geminiProvider)
	aiStudioProvider := provider.NewGoogleAIStudioProvider(provider.GoogleAIStudioOptions{})
	svc.RegisterProvider(aiStudioProvider)
	anthropicProvider := provider.NewAnthropicProvider(provider.AnthropicOptions{})
	svc.RegisterProvider(anthropicProvider)
	openAIProvider := provider.NewOpenAIProvider(provider.OpenAIOptions{
		ProviderID:   "openai",
		DefaultModel: "gpt-5.1-codex",
	})
	svc.RegisterProvider(openAIProvider)
	openAICodexProvider := provider.NewOpenAIProvider(provider.OpenAIOptions{
		ProviderID:   "openai-codex",
		DefaultModel: "gpt-5.3-codex",
	})
	svc.RegisterProvider(openAICodexProvider)

	accounts, err := loadAccounts(opts.AccountsPath)
	if err != nil {
		return nil, err
	}

	byProvider := map[string][]core.Account{}
	for _, account := range accounts {
		if strings.TrimSpace(account.Provider) == "" || strings.TrimSpace(account.ID) == "" {
			continue
		}
		if strings.TrimSpace(account.Token) == "" {
			continue
		}
		byProvider[account.Provider] = append(byProvider[account.Provider], account)
	}

	for providerID, providerAccounts := range byProvider {
		svc.RegisterPool(core.NewAccountPool(providerID, providerAccounts, nil, core.DefaultCooldownConfig()))
	}

	if svc.Pool("google-gemini-cli") == nil {
		// Keep provider registered even if no tokens are configured.
		svc.RegisterPool(core.NewAccountPool("google-gemini-cli", nil, nil, core.DefaultCooldownConfig()))
	}
	if svc.Pool("google-ai-studio") == nil {
		svc.RegisterPool(core.NewAccountPool("google-ai-studio", nil, nil, core.DefaultCooldownConfig()))
	}
	if svc.Pool("anthropic") == nil {
		svc.RegisterPool(core.NewAccountPool("anthropic", nil, nil, core.DefaultCooldownConfig()))
	}
	if svc.Pool("openai") == nil {
		svc.RegisterPool(core.NewAccountPool("openai", nil, nil, core.DefaultCooldownConfig()))
	}
	if svc.Pool("openai-codex") == nil {
		svc.RegisterPool(core.NewAccountPool("openai-codex", nil, nil, core.DefaultCooldownConfig()))
	}

	// Apply remaining config (fallbacks, general settings, Discord, Telegram) from the earlier load.
	svc.SetConfigDir(configDir)
	svc.SetGeneralConfig(appConfig.General)
	svc.SetCompactionConfig(appConfig.Compaction)
	svc.SetSecurityConfig(appConfig.Security)
	svc.SetDiscordConfig(appConfig.Discord)
	svc.SetTelegramConfig(appConfig.Telegram)
	svc.SetToolsConfig(appConfig.Tools)
	svc.SetDefaultThinkingMode(appConfig.DefaultThinkingMode)
	if appConfig.DefaultProvider != "" {
		svc.SetDefaultProvider(appConfig.DefaultProvider)
		svc.SetDefaultModel(appConfig.DefaultModel)
	}

	if len(appConfig.Fallbacks) > 0 {
		svc.SetFallbacks(appConfig.Fallbacks)
	} else {
		// Default fallback: when primary accounts are exhausted, try google-ai-studio.
		svc.SetFallbacks([]core.FallbackEntry{
			{Provider: "google-ai-studio", Model: "default", ThinkingMode: core.ThinkingModeAuto},
		})
	}

	authStore, err := auth.NewStore(auth.StoreOptions{BaseDir: strings.TrimSpace(opts.AuthDir)})
	if err != nil {
		return nil, fmt.Errorf("init auth store: %w", err)
	}
	svc.SetBackupManager(backup.NewManager(backup.ManagerOptions{
		ConfigRoot:  configDir,
		SessionsDir: sessionsDir,
		MemoryDir:   memoryDir,
		MCPDir:      mcpDir,
		PersonasDir: personasDir,
		StateDir:    stateDir,
		AuthStore:   authStore,
	}))

	oauthManager := auth.NewGeminiOAuthManager(auth.ManagerOptions{
		Host: strings.TrimSpace(opts.OAuthCallbackHost),
		Port: resolveOAuthCallbackPort(opts.OAuthCallbackPort, opts.APIAddr),
	})
	svc.SetAuthIntegration(oauthManager, authStore)
	svc.SetAnthropicLoginManager(auth.NewAnthropicLoginManager(auth.AnthropicLoginManagerOptions{
		JobTTL: 10 * time.Minute,
	}))
	svc.SetOpenAICodexLoginManager(auth.NewOpenAICodexLoginManager(auth.OpenAICodexLoginManagerOptions{
		JobTTL: 10 * time.Minute,
	}))

	if err := hydrateGeminiProfiles(svc, authStore); err != nil {
		return nil, fmt.Errorf("hydrate gemini profiles: %w", err)
	}
	if err := hydrateAIStudioProfiles(svc, authStore); err != nil {
		return nil, fmt.Errorf("hydrate ai studio profiles: %w", err)
	}
	if err := hydrateAnthropicProfiles(svc, authStore); err != nil {
		return nil, fmt.Errorf("hydrate anthropic profiles: %w", err)
	}
	if err := hydrateOpenAIProfiles(svc, authStore); err != nil {
		return nil, fmt.Errorf("hydrate openai profiles: %w", err)
	}
	if err := hydrateOpenAICodexProfiles(svc, authStore); err != nil {
		return nil, fmt.Errorf("hydrate openai-codex profiles: %w", err)
	}

	return svc, nil
}

func buildSecuritySetupURL(addr string, token string) string {
	return fmt.Sprintf("http://%s/#/setup?token=%s", normalizeBrowserAddr(addr), token)
}

func normalizeBrowserAddr(addr string) string {
	host, port, err := net.SplitHostPort(strings.TrimSpace(addr))
	if err != nil {
		return strings.TrimSpace(addr)
	}
	switch host {
	case "", "0.0.0.0", "::":
		host = "127.0.0.1"
	}
	return net.JoinHostPort(host, port)
}

func hydrateGeminiProfiles(svc *app.Service, store *auth.Store) error {
	if svc == nil || store == nil {
		return nil
	}
	pool := svc.Pool("google-gemini-cli")
	if pool == nil {
		return nil
	}

	profiles, err := store.ListProfiles("google-gemini-cli")
	if err != nil {
		return err
	}
	for _, profile := range profiles {
		if strings.TrimSpace(profile.ProfileID) == "" {
			continue
		}
		credential, err := store.LoadCredential(profile.Provider, profile.ProfileID)
		if err != nil {
			continue
		}
		projectID := strings.TrimSpace(profile.ProjectID)
		if projectID == "" {
			logSystem.Warnf("profile skipped: provider=google-gemini-cli profile_id=%s reason=missing_project",
				profile.ProfileID,
			)
			continue
		}
		pool.SetCredential(profile.ProfileID, core.Account{
			ID:       profile.ProfileID,
			Provider: "google-gemini-cli",
			Type:     core.AccountOAuth,
			Token:    credential.AccessToken,
			Email:    profile.Email,
			Metadata: core.Metadata{
				"profile_id":     profile.ProfileID,
				"project_id":     projectID,
				"endpoint":       strings.TrimSpace(profile.Endpoint),
				"project_source": "stored",
			},
		})
		logSystem.Logf("profile hydrated: provider=google-gemini-cli profile_id=%s endpoint=%s project_source=%s",
			profile.ProfileID,
			strings.TrimSpace(profile.Endpoint),
			"stored",
		)
	}
	return nil
}

func hydrateAIStudioProfiles(svc *app.Service, store *auth.Store) error {
	if svc == nil || store == nil {
		return nil
	}
	pool := svc.Pool("google-ai-studio")
	if pool == nil {
		return nil
	}

	profiles, err := store.ListProfiles("google-ai-studio")
	if err != nil {
		return err
	}
	for _, profile := range profiles {
		if strings.TrimSpace(profile.ProfileID) == "" {
			continue
		}
		credential, err := store.LoadCredential(profile.Provider, profile.ProfileID)
		if err != nil {
			continue
		}
		account := core.Account{
			ID:       profile.ProfileID,
			Provider: "google-ai-studio",
			Type:     core.AccountAPIKey,
			Token:    credential.AccessToken,
			Metadata: core.Metadata{
				"display_name": strings.TrimSpace(profile.DisplayName),
				"key_hint":     strings.TrimSpace(profile.KeyHint),
				"endpoint":     strings.TrimSpace(profile.Endpoint),
			},
		}
		pool.SetCredential(profile.ProfileID, account)
		logSystem.Logf("profile hydrated: provider=google-ai-studio profile_id=%s key_hint=%s",
			profile.ProfileID,
			strings.TrimSpace(profile.KeyHint),
		)
	}
	return nil
}

func hydrateAnthropicProfiles(svc *app.Service, store *auth.Store) error {
	if svc == nil || store == nil {
		return nil
	}
	pool := svc.Pool("anthropic")
	if pool == nil {
		return nil
	}

	profiles, err := store.ListProfiles("anthropic")
	if err != nil {
		return err
	}
	for _, profile := range profiles {
		if strings.TrimSpace(profile.ProfileID) == "" {
			continue
		}
		credential, err := store.LoadCredential(profile.Provider, profile.ProfileID)
		if err != nil {
			continue
		}
		accountType := core.AccountAPIKey
		if strings.TrimSpace(profile.Type) == string(core.AccountToken) {
			accountType = core.AccountToken
		}
		account := core.Account{
			ID:       profile.ProfileID,
			Provider: "anthropic",
			Type:     accountType,
			Token:    credential.AccessToken,
			Metadata: core.Metadata{
				"display_name": strings.TrimSpace(profile.DisplayName),
				"key_hint":     strings.TrimSpace(profile.KeyHint),
				"endpoint":     strings.TrimSpace(profile.Endpoint),
			},
		}
		pool.SetCredential(profile.ProfileID, account)
		logSystem.Logf("profile hydrated: provider=anthropic profile_id=%s type=%s key_hint=%s",
			profile.ProfileID,
			accountType,
			strings.TrimSpace(profile.KeyHint),
		)
	}
	return nil
}

func hydrateOpenAIProfiles(svc *app.Service, store *auth.Store) error {
	if svc == nil || store == nil {
		return nil
	}
	pool := svc.Pool("openai")
	if pool == nil {
		return nil
	}

	profiles, err := store.ListProfiles("openai")
	if err != nil {
		return err
	}
	for _, profile := range profiles {
		if strings.TrimSpace(profile.ProfileID) == "" {
			continue
		}
		credential, err := store.LoadCredential(profile.Provider, profile.ProfileID)
		if err != nil {
			continue
		}
		account := core.Account{
			ID:       profile.ProfileID,
			Provider: "openai",
			Type:     core.AccountAPIKey,
			Token:    credential.AccessToken,
			Metadata: core.Metadata{
				"display_name": strings.TrimSpace(profile.DisplayName),
				"key_hint":     strings.TrimSpace(profile.KeyHint),
				"endpoint":     strings.TrimSpace(profile.Endpoint),
			},
		}
		pool.SetCredential(profile.ProfileID, account)
		logSystem.Logf("profile hydrated: provider=openai profile_id=%s key_hint=%s",
			profile.ProfileID,
			strings.TrimSpace(profile.KeyHint),
		)
	}
	return nil
}

func hydrateOpenAICodexProfiles(svc *app.Service, store *auth.Store) error {
	if svc == nil || store == nil {
		return nil
	}
	pool := svc.Pool("openai-codex")
	if pool == nil {
		return nil
	}

	profiles, err := store.ListProfiles("openai-codex")
	if err != nil {
		return err
	}
	for _, profile := range profiles {
		if strings.TrimSpace(profile.ProfileID) == "" {
			continue
		}
		credential, err := store.LoadCredential(profile.Provider, profile.ProfileID)
		if err != nil {
			continue
		}
		accountType := core.AccountOAuth
		if strings.TrimSpace(profile.Type) == string(core.AccountToken) {
			accountType = core.AccountToken
		}
		account := core.Account{
			ID:       profile.ProfileID,
			Provider: "openai-codex",
			Type:     accountType,
			Token:    credential.AccessToken,
			Metadata: core.Metadata{
				"display_name": strings.TrimSpace(profile.DisplayName),
				"key_hint":     strings.TrimSpace(profile.KeyHint),
				"endpoint":     strings.TrimSpace(profile.Endpoint),
				"email":        strings.TrimSpace(profile.Email),
			},
		}
		pool.SetCredential(profile.ProfileID, account)
		logSystem.Logf("profile hydrated: provider=openai-codex profile_id=%s type=%s key_hint=%s",
			profile.ProfileID,
			accountType,
			strings.TrimSpace(profile.KeyHint),
		)
	}
	return nil
}

func loadAccounts(path string) ([]core.Account, error) {
	trimmed := strings.TrimSpace(path)
	if trimmed == "" {
		return nil, nil
	}
	resolved, err := filepath.Abs(trimmed)
	if err != nil {
		return nil, err
	}
	content, err := os.ReadFile(resolved)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return nil, nil
		}
		return nil, err
	}
	var payload accountFile
	if err := json.Unmarshal(content, &payload); err != nil {
		return nil, fmt.Errorf("parse accounts file: %w", err)
	}
	return payload.Accounts, nil
}

func splitCSV(value string) []string {
	parts := strings.Split(value, ",")
	out := make([]string, 0, len(parts))
	for _, part := range parts {
		trimmed := strings.TrimSpace(part)
		if trimmed != "" {
			out = append(out, trimmed)
		}
	}
	return out
}

func resolveRuntimeDefaultSelection(
	savedProvider string,
	savedModel string,
	fallbackProvider string,
	fallbackModel string,
	providerExplicit bool,
	modelExplicit bool,
) (string, string) {
	providerID := strings.TrimSpace(fallbackProvider)
	modelID := strings.TrimSpace(fallbackModel)
	savedProvider = strings.TrimSpace(savedProvider)
	savedModel = strings.TrimSpace(savedModel)

	if !providerExplicit && savedProvider != "" {
		providerID = savedProvider
		if !modelExplicit {
			modelID = savedModel
		}
	}

	if providerID == "" {
		providerID = "google-gemini-cli"
	}
	if modelID == "" {
		modelID = "default"
	}
	return providerID, modelID
}

func flagWasProvided(name string) bool {
	provided := false
	flag.Visit(func(f *flag.Flag) {
		if f.Name == name {
			provided = true
		}
	})
	return provided
}

func resolveOAuthCallbackPort(explicit int, apiAddr string) int {
	if explicit > 0 {
		return explicit
	}
	host, port, err := net.SplitHostPort(strings.TrimSpace(apiAddr))
	if err == nil && host != "" {
		if parsed, convErr := strconv.Atoi(port); convErr == nil && parsed > 0 {
			return parsed
		}
	}
	return 8085
}

// resolveStateDir returns the persistent state directory (~/.nekoclaw/state).
// Returns empty string if the home directory cannot be determined.
func resolveStateDir() string {
	home, err := os.UserHomeDir()
	if err != nil || strings.TrimSpace(home) == "" {
		return ""
	}
	return filepath.Join(home, ".nekoclaw", "state")
}

func defaultGeminiEndpoints() string {
	return "https://cloudcode-pa.googleapis.com,https://daily-cloudcode-pa.sandbox.googleapis.com,https://autopush-cloudcode-pa.sandbox.googleapis.com"
}

func runHousekeepingLoop(lifecycle *core.SessionLifecycle) {
	ticker := time.NewTicker(1 * time.Hour)
	defer ticker.Stop()
	for range ticker.C {
		if err := lifecycle.RunHousekeeping(); err != nil {
			logSystem.Errorf("housekeeping: %v", err)
		}
	}
}

func fatal(err error) {
	if err == nil {
		return
	}
	fmt.Fprintln(os.Stderr, "error:", err)
	os.Exit(1)
}
