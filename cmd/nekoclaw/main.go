package main

import (
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
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
	"github.com/doeshing/nekoclaw/internal/core"
	"github.com/doeshing/nekoclaw/internal/provider"
	"github.com/doeshing/nekoclaw/internal/tui"
)

type accountFile struct {
	Accounts []core.Account `json:"accounts"`
}

func main() {
	ensureGeminiOAuthEnvAliases()

	var (
		mode               = flag.String("mode", envOr("NEKOCLAW_MODE", "both"), "run mode: api | tui | both")
		addr               = flag.String("addr", envOr("NEKOCLAW_ADDR", "127.0.0.1:8085"), "api listen address")
		apiBaseURL         = flag.String("api-base-url", envOr("NEKOCLAW_API_BASE_URL", "http://127.0.0.1:8085"), "api base url for tui")
		defaultProvider    = flag.String("provider", envOr("NEKOCLAW_PROVIDER", "mock"), "default provider for TUI")
		defaultModel       = flag.String("model", envOr("NEKOCLAW_MODEL", "default"), "default model for TUI")
		sessionID          = flag.String("session", envOr("NEKOCLAW_SESSION", "main"), "default session id for TUI")
		accountsPath       = flag.String("accounts", envOr("NEKOCLAW_ACCOUNTS_FILE", "./accounts.json"), "account json path")
		geminiEndpoints    = flag.String("gemini-endpoints", envOr("GEMINI_INTERNAL_ENDPOINTS", defaultGeminiEndpoints()), "comma-separated gemini internal endpoints")
		geminiGeneratePath = flag.String("gemini-generate-path", envOr("GEMINI_INTERNAL_GENERATE_PATH", "/v1internal:streamGenerateContent?alt=sse"), "gemini internal generate path")
		authDir            = flag.String("auth-dir", envOr("NEKOCLAW_AUTH_DIR", ""), "auth state directory (defaults to ~/.nekoclaw/auth)")
		callbackHost       = flag.String("oauth-callback-host", envOr("OPENCLAW_GEMINI_OAUTH_CALLBACK_HOST", "localhost"), "gemini oauth callback host")
		callbackPort       = flag.Int("oauth-callback-port", envOrInt("OPENCLAW_GEMINI_OAUTH_CALLBACK_PORT", 8085), "gemini oauth callback port")
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
		OAuthCallbackHost:  *callbackHost,
		OAuthCallbackPort:  *callbackPort,
		APIAddr:            *addr,
	})
	if err != nil {
		fatal(err)
	}

	switch runMode {
	case "api":
		ctx := context.Background()
		server := api.NewServer(service)
		fmt.Printf("NekoClaw API listening on %s\n", *addr)
		fatal(server.Run(ctx, *addr))
	case "tui":
		fatal(tui.Run(*apiBaseURL, *defaultProvider, *defaultModel, *sessionID))
	case "both":
		ctx, cancel := context.WithCancel(context.Background())
		defer cancel()

		server := api.NewServer(service)
		var wg sync.WaitGroup
		var apiErr error
		wg.Add(1)
		go func() {
			defer wg.Done()
			apiErr = server.Run(ctx, *addr)
		}()

		// Give the API listener a short head start before opening the TUI.
		time.Sleep(250 * time.Millisecond)
		tuiErr := tui.Run(*apiBaseURL, *defaultProvider, *defaultModel, *sessionID)
		cancel()
		wg.Wait()

		if tuiErr != nil {
			fatal(tuiErr)
		}
		if apiErr != nil && !errors.Is(apiErr, context.Canceled) {
			fatal(apiErr)
		}
	default:
		fatal(fmt.Errorf("unsupported mode %q", *mode))
	}
}

func configureRuntimeLogging(runMode string, authDir string) func() {
	if strings.TrimSpace(os.Getenv("NEKOCLAW_LOG_STDERR")) == "1" {
		return func() {}
	}
	switch runMode {
	case "tui", "both":
		logPath := strings.TrimSpace(os.Getenv("NEKOCLAW_LOG_FILE"))
		if logPath == "" {
			logPath = resolveDefaultLogFilePath(authDir)
		}
		if logPath == "" {
			log.SetOutput(io.Discard)
			return func() {}
		}
		if err := os.MkdirAll(filepath.Dir(logPath), 0o755); err != nil {
			log.SetOutput(io.Discard)
			return func() {}
		}
		file, err := os.OpenFile(logPath, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o644)
		if err != nil {
			log.SetOutput(io.Discard)
			return func() {}
		}
		log.SetOutput(file)
		log.Printf("event=logging_redirect mode=%s path=%s", runMode, logPath)
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
	OAuthCallbackHost  string
	OAuthCallbackPort  int
	APIAddr            string
}

func buildService(opts buildServiceOptions) (*app.Service, error) {
	svc := app.NewService()

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

	accounts, err := loadAccounts(opts.AccountsPath)
	if err != nil {
		return nil, err
	}
	accounts = append(accounts, loadGeminiAccountsFromEnv()...)

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

	authStore, err := auth.NewStore(auth.StoreOptions{BaseDir: strings.TrimSpace(opts.AuthDir)})
	if err != nil {
		return nil, fmt.Errorf("init auth store: %w", err)
	}

	oauthManager := auth.NewGeminiOAuthManager(auth.ManagerOptions{
		Host: strings.TrimSpace(opts.OAuthCallbackHost),
		Port: resolveOAuthCallbackPort(opts.OAuthCallbackPort, opts.APIAddr),
	})
	svc.SetAuthIntegration(oauthManager, authStore)

	if err := hydrateGeminiProfiles(svc, authStore); err != nil {
		return nil, fmt.Errorf("hydrate gemini profiles: %w", err)
	}

	return svc, nil
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
		pool.SetCredential(profile.ProfileID, core.Account{
			ID:       profile.ProfileID,
			Provider: "google-gemini-cli",
			Type:     core.AccountOAuth,
			Token:    credential.AccessToken,
			Email:    profile.Email,
			Metadata: core.Metadata{
				"profile_id": profile.ProfileID,
				"project_id": strings.TrimSpace(profile.ProjectID),
				"endpoint":   strings.TrimSpace(profile.Endpoint),
			},
		})
		log.Printf(
			"event=profile_hydrated provider=google-gemini-cli profile_id=%s endpoint=%s",
			profile.ProfileID,
			strings.TrimSpace(profile.Endpoint),
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

func loadGeminiAccountsFromEnv() []core.Account {
	multi := splitCSV(envOr("GEMINI_INTERNAL_TOKENS", ""))
	single := strings.TrimSpace(os.Getenv("GEMINI_INTERNAL_TOKEN"))
	if single != "" {
		multi = append(multi, single)
	}
	if len(multi) == 0 {
		return nil
	}

	projectID := strings.TrimSpace(os.Getenv("GEMINI_INTERNAL_PROJECT_ID"))
	endpoint := strings.TrimSpace(os.Getenv("GEMINI_INTERNAL_ENDPOINT"))
	accounts := make([]core.Account, 0, len(multi))
	for i, token := range multi {
		metadata := core.Metadata{}
		if projectID != "" {
			metadata["project_id"] = projectID
		}
		if endpoint != "" {
			metadata["endpoint"] = strings.TrimRight(endpoint, "/")
		}
		accounts = append(accounts, core.Account{
			ID:       fmt.Sprintf("gemini-env-%d", i+1),
			Provider: "google-gemini-cli",
			Type:     core.AccountOAuth,
			Token:    token,
			Metadata: metadata,
		})
	}
	return accounts
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

func envOr(key string, fallback string) string {
	value := strings.TrimSpace(os.Getenv(key))
	if value == "" {
		return fallback
	}
	return value
}

func envOrInt(key string, fallback int) int {
	raw := strings.TrimSpace(os.Getenv(key))
	if raw == "" {
		return fallback
	}
	n, err := strconv.Atoi(raw)
	if err != nil || n <= 0 {
		return fallback
	}
	return n
}

func ensureGeminiOAuthEnvAliases() {
	aliasEnv("OPENCLAW_GEMINI_OAUTH_CLIENT_ID", "GEMINI_CLI_OAUTH_CLIENT_ID")
	aliasEnv("OPENCLAW_GEMINI_OAUTH_CLIENT_SECRET", "GEMINI_CLI_OAUTH_CLIENT_SECRET")
}

func aliasEnv(primary, secondary string) {
	if strings.TrimSpace(os.Getenv(primary)) != "" {
		return
	}
	value := strings.TrimSpace(os.Getenv(secondary))
	if value == "" {
		return
	}
	_ = os.Setenv(primary, value)
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

func defaultGeminiEndpoints() string {
	return "https://cloudcode-pa.googleapis.com,https://daily-cloudcode-pa.sandbox.googleapis.com,https://autopush-cloudcode-pa.sandbox.googleapis.com"
}

func fatal(err error) {
	if err == nil {
		return
	}
	fmt.Fprintln(os.Stderr, "error:", err)
	os.Exit(1)
}
