package main

import (
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"testing"

	"github.com/doeshing/nekoclaw/internal/app"
	"github.com/doeshing/nekoclaw/internal/auth"
	"github.com/doeshing/nekoclaw/internal/core"
)

func TestResolveDefaultLogFilePath_UsesHomeDefaultWhenAuthDirEmpty(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)

	got := resolveDefaultLogFilePath("")
	want := filepath.Join(home, ".nekoclaw", "logs", "nekoclaw.log")
	if got != want {
		t.Fatalf("resolveDefaultLogFilePath(empty) = %q, want %q", got, want)
	}
}

func TestLoadAIStudioAccountsFromEnv_MergesAndDedupes(t *testing.T) {
	t.Setenv("GEMINI_API_KEY", "key-a")
	t.Setenv("GOOGLE_API_KEY", "key-a")
	t.Setenv("GEMINI_API_KEYS", "key-b,key-c")
	t.Setenv("GOOGLE_API_KEYS", "key-c,key-d")
	t.Setenv("GEMINI_API_KEY_TEAM", "key-e")
	t.Setenv("GOOGLE_API_KEY_X", "key-f")

	accounts := loadAIStudioAccountsFromEnv()
	if len(accounts) != 6 {
		t.Fatalf("unexpected accounts length: %d", len(accounts))
	}
	seenTokens := map[string]struct{}{}
	for _, account := range accounts {
		if account.Provider != "google-ai-studio" {
			t.Fatalf("unexpected provider: %s", account.Provider)
		}
		if account.Type != core.AccountAPIKey {
			t.Fatalf("unexpected account type: %s", account.Type)
		}
		if strings.TrimSpace(account.Token) == "" {
			t.Fatalf("expected token")
		}
		seenTokens[account.Token] = struct{}{}
	}
	keys := make([]string, 0, len(seenTokens))
	for key := range seenTokens {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	want := []string{"key-a", "key-b", "key-c", "key-d", "key-e", "key-f"}
	if strings.Join(keys, ",") != strings.Join(want, ",") {
		t.Fatalf("unexpected key set: got=%v want=%v", keys, want)
	}
}

func TestHydrateAIStudioProfilesLoadsCredentials(t *testing.T) {
	svc := app.NewService(app.ServiceOptions{})
	svc.RegisterPool(core.NewAccountPool("google-ai-studio", nil, nil, core.DefaultCooldownConfig()))
	store := mustNewAuthStore(t)

	if err := store.SaveCredential("google-ai-studio", "ai-1", auth.Credential{
		AccessToken: "api-key-1",
	}); err != nil {
		t.Fatalf("save credential: %v", err)
	}
	if err := store.UpsertProfile(auth.ProfileMetadata{
		ProfileID:   "ai-1",
		Provider:    "google-ai-studio",
		Type:        string(core.AccountAPIKey),
		DisplayName: "main",
		KeyHint:     "****-1",
	}); err != nil {
		t.Fatalf("upsert profile: %v", err)
	}

	if err := hydrateAIStudioProfiles(svc, store); err != nil {
		t.Fatalf("hydrate ai studio profiles: %v", err)
	}

	pool := svc.Pool("google-ai-studio")
	if pool == nil {
		t.Fatalf("missing pool")
	}
	account, ok := pool.GetAccount("ai-1")
	if !ok {
		t.Fatalf("expected ai-1 loaded")
	}
	if account.Token != "api-key-1" {
		t.Fatalf("unexpected token: %q", account.Token)
	}
	if account.Metadata["display_name"] != "main" {
		t.Fatalf("unexpected display name: %q", account.Metadata["display_name"])
	}
}

func TestLoadAnthropicAccountsFromEnv_MergesAndDedupes(t *testing.T) {
	token1 := "sk-ant-oat01-" + strings.Repeat("a", 80)
	token2 := "sk-ant-oat01-" + strings.Repeat("b", 80)
	key1 := "sk-ant-api-1"
	key2 := "sk-ant-api-2"
	key3 := "sk-ant-api-3"

	t.Setenv("ANTHROPIC_OAUTH_TOKEN", token1)
	t.Setenv("ANTHROPIC_SETUP_TOKEN", token1)
	t.Setenv("ANTHROPIC_OAUTH_TOKENS", token2+","+token1)
	t.Setenv("ANTHROPIC_OAUTH_TOKEN_TEAM", token2)
	t.Setenv("ANTHROPIC_API_KEY", key1)
	t.Setenv("ANTHROPIC_API_KEYS", key2+","+key1)
	t.Setenv("ANTHROPIC_API_KEY_TEAM", key3)

	accounts := loadAnthropicAccountsFromEnv()
	if len(accounts) != 5 {
		t.Fatalf("unexpected accounts length: %d", len(accounts))
	}

	type keyed struct {
		secret string
		typ    core.AccountType
	}
	seen := map[keyed]struct{}{}
	for _, account := range accounts {
		if account.Provider != "anthropic" {
			t.Fatalf("unexpected provider: %s", account.Provider)
		}
		if strings.TrimSpace(account.Token) == "" {
			t.Fatalf("missing token")
		}
		seen[keyed{secret: account.Token, typ: account.Type}] = struct{}{}
	}
	want := []keyed{
		{secret: token1, typ: core.AccountToken},
		{secret: token2, typ: core.AccountToken},
		{secret: key1, typ: core.AccountAPIKey},
		{secret: key2, typ: core.AccountAPIKey},
		{secret: key3, typ: core.AccountAPIKey},
	}
	for _, item := range want {
		if _, ok := seen[item]; !ok {
			t.Fatalf("missing expected account: %+v", item)
		}
	}
}

func TestHydrateAnthropicProfilesLoadsCredentials(t *testing.T) {
	svc := app.NewService(app.ServiceOptions{})
	svc.RegisterPool(core.NewAccountPool("anthropic", nil, nil, core.DefaultCooldownConfig()))
	store := mustNewAuthStore(t)

	if err := store.SaveCredential("anthropic", "anthropic:token_main", auth.Credential{
		AccessToken: "sk-ant-oat01-" + strings.Repeat("a", 80),
	}); err != nil {
		t.Fatalf("save token credential: %v", err)
	}
	if err := store.UpsertProfile(auth.ProfileMetadata{
		ProfileID:   "anthropic:token_main",
		Provider:    "anthropic",
		Type:        string(core.AccountToken),
		DisplayName: "token-main",
		KeyHint:     "****aaaaaa",
	}); err != nil {
		t.Fatalf("upsert token profile: %v", err)
	}

	if err := store.SaveCredential("anthropic", "anthropic:key_main", auth.Credential{
		AccessToken: "sk-ant-api-1",
	}); err != nil {
		t.Fatalf("save key credential: %v", err)
	}
	if err := store.UpsertProfile(auth.ProfileMetadata{
		ProfileID:   "anthropic:key_main",
		Provider:    "anthropic",
		Type:        string(core.AccountAPIKey),
		DisplayName: "key-main",
		KeyHint:     "****pi-1",
	}); err != nil {
		t.Fatalf("upsert key profile: %v", err)
	}

	if err := hydrateAnthropicProfiles(svc, store); err != nil {
		t.Fatalf("hydrate anthropic profiles: %v", err)
	}

	pool := svc.Pool("anthropic")
	if pool == nil {
		t.Fatalf("missing anthropic pool")
	}

	tokenAccount, ok := pool.GetAccount("anthropic:token_main")
	if !ok {
		t.Fatalf("expected token profile loaded")
	}
	if tokenAccount.Type != core.AccountToken {
		t.Fatalf("unexpected token account type: %s", tokenAccount.Type)
	}

	keyAccount, ok := pool.GetAccount("anthropic:key_main")
	if !ok {
		t.Fatalf("expected api key profile loaded")
	}
	if keyAccount.Type != core.AccountAPIKey {
		t.Fatalf("unexpected key account type: %s", keyAccount.Type)
	}
}

func TestLoadOpenAIAccountsFromEnv_MergesAndDedupes(t *testing.T) {
	t.Setenv("OPENAI_API_KEY", "sk-openai-a")
	t.Setenv("OPENAI_API_KEYS", "sk-openai-b,sk-openai-c,sk-openai-a")
	t.Setenv("OPENAI_API_KEY_TEAM", "sk-openai-d")

	accounts := loadOpenAIAccountsFromEnv()
	if len(accounts) != 4 {
		t.Fatalf("unexpected accounts length: %d", len(accounts))
	}
	seenTokens := map[string]struct{}{}
	for _, account := range accounts {
		if account.Provider != "openai" {
			t.Fatalf("unexpected provider: %s", account.Provider)
		}
		if account.Type != core.AccountAPIKey {
			t.Fatalf("unexpected account type: %s", account.Type)
		}
		seenTokens[account.Token] = struct{}{}
	}
	keys := make([]string, 0, len(seenTokens))
	for key := range seenTokens {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	want := []string{"sk-openai-a", "sk-openai-b", "sk-openai-c", "sk-openai-d"}
	if strings.Join(keys, ",") != strings.Join(want, ",") {
		t.Fatalf("unexpected key set: got=%v want=%v", keys, want)
	}
}

func TestLoadOpenAICodexAccountsFromEnv_MergesAndDedupes(t *testing.T) {
	t.Setenv("OPENAI_OAUTH_TOKEN", "oauth-a")
	t.Setenv("OPENAI_CODEX_OAUTH_TOKEN", "oauth-b")
	t.Setenv("OPENAI_OAUTH_TOKENS", "oauth-c,oauth-a")
	t.Setenv("OPENAI_CODEX_OAUTH_TOKENS", "oauth-d,oauth-b")
	t.Setenv("OPENAI_OAUTH_TOKEN_TEAM", "oauth-e")

	accounts := loadOpenAICodexAccountsFromEnv()
	if len(accounts) != 5 {
		t.Fatalf("unexpected accounts length: %d", len(accounts))
	}
	seenTokens := map[string]struct{}{}
	for _, account := range accounts {
		if account.Provider != "openai-codex" {
			t.Fatalf("unexpected provider: %s", account.Provider)
		}
		if account.Type != core.AccountOAuth {
			t.Fatalf("unexpected account type: %s", account.Type)
		}
		seenTokens[account.Token] = struct{}{}
	}
	keys := make([]string, 0, len(seenTokens))
	for key := range seenTokens {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	want := []string{"oauth-a", "oauth-b", "oauth-c", "oauth-d", "oauth-e"}
	if strings.Join(keys, ",") != strings.Join(want, ",") {
		t.Fatalf("unexpected key set: got=%v want=%v", keys, want)
	}
}

func TestHydrateOpenAIProfilesLoadsCredentials(t *testing.T) {
	svc := app.NewService(app.ServiceOptions{})
	svc.RegisterPool(core.NewAccountPool("openai", nil, nil, core.DefaultCooldownConfig()))
	store := mustNewAuthStore(t)

	if err := store.SaveCredential("openai", "openai:default", auth.Credential{
		AccessToken: "sk-openai-main",
	}); err != nil {
		t.Fatalf("save credential: %v", err)
	}
	if err := store.UpsertProfile(auth.ProfileMetadata{
		ProfileID:   "openai:default",
		Provider:    "openai",
		Type:        string(core.AccountAPIKey),
		DisplayName: "openai-main",
		KeyHint:     "****-main",
	}); err != nil {
		t.Fatalf("upsert profile: %v", err)
	}

	if err := hydrateOpenAIProfiles(svc, store); err != nil {
		t.Fatalf("hydrate openai profiles: %v", err)
	}
	pool := svc.Pool("openai")
	if pool == nil {
		t.Fatalf("missing openai pool")
	}
	account, ok := pool.GetAccount("openai:default")
	if !ok {
		t.Fatalf("expected openai profile loaded")
	}
	if account.Type != core.AccountAPIKey {
		t.Fatalf("unexpected account type: %s", account.Type)
	}
	if account.Token != "sk-openai-main" {
		t.Fatalf("unexpected token: %q", account.Token)
	}
}

func TestHydrateOpenAICodexProfilesLoadsCredentials(t *testing.T) {
	svc := app.NewService(app.ServiceOptions{})
	svc.RegisterPool(core.NewAccountPool("openai-codex", nil, nil, core.DefaultCooldownConfig()))
	store := mustNewAuthStore(t)

	if err := store.SaveCredential("openai-codex", "openai-codex:user_example_com", auth.Credential{
		AccessToken: "oauth-token-main",
	}); err != nil {
		t.Fatalf("save credential: %v", err)
	}
	if err := store.UpsertProfile(auth.ProfileMetadata{
		ProfileID:   "openai-codex:user_example_com",
		Provider:    "openai-codex",
		Type:        string(core.AccountOAuth),
		DisplayName: "codex-main",
		KeyHint:     "****-main",
		Email:       "user@example.com",
	}); err != nil {
		t.Fatalf("upsert profile: %v", err)
	}

	if err := hydrateOpenAICodexProfiles(svc, store); err != nil {
		t.Fatalf("hydrate openai-codex profiles: %v", err)
	}
	pool := svc.Pool("openai-codex")
	if pool == nil {
		t.Fatalf("missing openai-codex pool")
	}
	account, ok := pool.GetAccount("openai-codex:user_example_com")
	if !ok {
		t.Fatalf("expected openai-codex profile loaded")
	}
	if account.Type != core.AccountOAuth {
		t.Fatalf("unexpected account type: %s", account.Type)
	}
	if account.Token != "oauth-token-main" {
		t.Fatalf("unexpected token: %q", account.Token)
	}
}

func TestResolveDefaultLogFilePath_AuthDirParentRoot(t *testing.T) {
	got := resolveDefaultLogFilePath("/tmp/custom/auth")
	want := filepath.Join("/tmp/custom", "logs", "nekoclaw.log")
	if got != want {
		t.Fatalf("resolveDefaultLogFilePath(auth) = %q, want %q", got, want)
	}
}

func TestResolveDefaultLogFilePath_NonAuthDirRoot(t *testing.T) {
	got := resolveDefaultLogFilePath("/tmp/custom-state")
	want := filepath.Join("/tmp/custom-state", "logs", "nekoclaw.log")
	if got != want {
		t.Fatalf("resolveDefaultLogFilePath(non-auth) = %q, want %q", got, want)
	}
}

func TestHydrateGeminiProfilesSkipsMissingProjectWithoutEnv(t *testing.T) {
	svc := app.NewService(app.ServiceOptions{})
	svc.RegisterPool(core.NewAccountPool("google-gemini-cli", nil, nil, core.DefaultCooldownConfig()))
	store := mustNewAuthStore(t)

	if err := store.SaveCredential("google-gemini-cli", "p1", auth.Credential{
		AccessToken:  "token-p1",
		RefreshToken: "refresh-p1",
	}); err != nil {
		t.Fatalf("save credential: %v", err)
	}
	if err := store.UpsertProfile(auth.ProfileMetadata{
		ProfileID: "p1",
		Provider:  "google-gemini-cli",
		Type:      string(core.AccountOAuth),
		Email:     "p1@example.com",
	}); err != nil {
		t.Fatalf("upsert profile: %v", err)
	}

	if err := hydrateGeminiProfiles(svc, store); err != nil {
		t.Fatalf("hydrate profiles: %v", err)
	}

	pool := svc.Pool("google-gemini-cli")
	if pool == nil {
		t.Fatalf("missing pool")
	}
	if _, ok := pool.GetAccount("p1"); ok {
		t.Fatalf("expected profile p1 not loaded without project")
	}
}

func TestHydrateGeminiProfilesUsesEnvFallbackProject(t *testing.T) {
	t.Setenv("GOOGLE_CLOUD_PROJECT", "env-project-1")

	svc := app.NewService(app.ServiceOptions{})
	svc.RegisterPool(core.NewAccountPool("google-gemini-cli", nil, nil, core.DefaultCooldownConfig()))
	store := mustNewAuthStore(t)

	if err := store.SaveCredential("google-gemini-cli", "p2", auth.Credential{
		AccessToken:  "token-p2",
		RefreshToken: "refresh-p2",
	}); err != nil {
		t.Fatalf("save credential: %v", err)
	}
	if err := store.UpsertProfile(auth.ProfileMetadata{
		ProfileID: "p2",
		Provider:  "google-gemini-cli",
		Type:      string(core.AccountOAuth),
		Email:     "p2@example.com",
	}); err != nil {
		t.Fatalf("upsert profile: %v", err)
	}

	if err := hydrateGeminiProfiles(svc, store); err != nil {
		t.Fatalf("hydrate profiles: %v", err)
	}

	pool := svc.Pool("google-gemini-cli")
	if pool == nil {
		t.Fatalf("missing pool")
	}
	account, ok := pool.GetAccount("p2")
	if !ok {
		t.Fatalf("expected profile p2 loaded with env fallback")
	}
	if account.Metadata["project_id"] != "env-project-1" {
		t.Fatalf("unexpected project_id: %q", account.Metadata["project_id"])
	}
	if account.Metadata["project_source"] != "env_fallback" {
		t.Fatalf("unexpected project_source: %q", account.Metadata["project_source"])
	}
}

func mustNewAuthStore(t *testing.T) *auth.Store {
	t.Helper()
	store, err := auth.NewStore(auth.StoreOptions{
		BaseDir: t.TempDir(),
		Keyring: newMainTestKeyring(),
	})
	if err != nil {
		t.Fatalf("new auth store: %v", err)
	}
	return store
}

type mainTestKeyring struct {
	mu   sync.Mutex
	data map[string]string
}

func newMainTestKeyring() *mainTestKeyring {
	return &mainTestKeyring{data: map[string]string{}}
}

func (k *mainTestKeyring) Available() bool {
	return true
}

func (k *mainTestKeyring) Set(key, value string) error {
	k.mu.Lock()
	defer k.mu.Unlock()
	k.data[key] = value
	return nil
}

func (k *mainTestKeyring) Get(key string) (string, error) {
	k.mu.Lock()
	defer k.mu.Unlock()
	value, ok := k.data[key]
	if !ok {
		return "", auth.ErrCredentialNotFound
	}
	return value, nil
}

func (k *mainTestKeyring) Delete(key string) error {
	k.mu.Lock()
	defer k.mu.Unlock()
	delete(k.data, key)
	return nil
}
