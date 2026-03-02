package provider

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
	"sync"
	"time"
)

const (
	googleAuthURL     = "https://accounts.google.com/o/oauth2/v2/auth"
	googleTokenURL    = "https://oauth2.googleapis.com/token"
	googleUserInfoURL = "https://www.googleapis.com/oauth2/v1/userinfo?alt=json"
)

var (
	googleClientIDEnvKeys     = []string{"OPENCLAW_GEMINI_OAUTH_CLIENT_ID", "GEMINI_CLI_OAUTH_CLIENT_ID"}
	googleClientSecretEnvKeys = []string{"OPENCLAW_GEMINI_OAUTH_CLIENT_SECRET", "GEMINI_CLI_OAUTH_CLIENT_SECRET"}
	googleScopes              = []string{
		"https://www.googleapis.com/auth/cloud-platform",
		"https://www.googleapis.com/auth/userinfo.email",
		"https://www.googleapis.com/auth/userinfo.profile",
	}
)

type oauthClientConfig struct {
	ClientID     string
	ClientSecret string
}

var (
	cachedOAuthClientConfig *oauthClientConfig
	oauthClientMu           sync.Mutex
)

func (p *GeminiInternalProvider) StartOAuth(_ context.Context, req OAuthStartRequest) (OAuthStartResponse, error) {
	cfg, err := resolveOAuthClientConfig()
	if err != nil {
		return OAuthStartResponse{}, err
	}
	if strings.TrimSpace(req.State) == "" || strings.TrimSpace(req.Challenge) == "" || strings.TrimSpace(req.RedirectURI) == "" {
		return OAuthStartResponse{}, fmt.Errorf("state/challenge/redirect_uri are required")
	}
	q := url.Values{}
	q.Set("client_id", cfg.ClientID)
	q.Set("response_type", "code")
	q.Set("redirect_uri", req.RedirectURI)
	q.Set("scope", strings.Join(googleScopes, " "))
	q.Set("code_challenge", req.Challenge)
	q.Set("code_challenge_method", "S256")
	q.Set("state", req.State)
	q.Set("access_type", "offline")
	q.Set("prompt", "consent")
	return OAuthStartResponse{AuthURL: googleAuthURL + "?" + q.Encode()}, nil
}

func (p *GeminiInternalProvider) CompleteOAuth(ctx context.Context, req OAuthCompleteRequest) (OAuthCredential, error) {
	cfg, err := resolveOAuthClientConfig()
	if err != nil {
		return OAuthCredential{}, err
	}
	code := strings.TrimSpace(req.Code)
	verifier := strings.TrimSpace(req.Verifier)
	redirectURI := strings.TrimSpace(req.RedirectURI)
	if code == "" || verifier == "" || redirectURI == "" {
		return OAuthCredential{}, fmt.Errorf("code/verifier/redirect_uri are required")
	}

	form := url.Values{}
	form.Set("client_id", cfg.ClientID)
	form.Set("grant_type", "authorization_code")
	form.Set("code", code)
	form.Set("redirect_uri", redirectURI)
	form.Set("code_verifier", verifier)
	if strings.TrimSpace(cfg.ClientSecret) != "" {
		form.Set("client_secret", cfg.ClientSecret)
	}

	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, googleTokenURL, strings.NewReader(form.Encode()))
	if err != nil {
		return OAuthCredential{}, err
	}
	httpReq.Header.Set("Content-Type", "application/x-www-form-urlencoded;charset=UTF-8")
	httpReq.Header.Set("Accept", "*/*")
	httpReq.Header.Set("User-Agent", "google-api-nodejs-client/9.15.1")

	resp, err := p.client.Do(httpReq)
	if err != nil {
		return OAuthCredential{}, err
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(resp.Body)
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return OAuthCredential{}, fmt.Errorf("token exchange failed: status=%d body=%s", resp.StatusCode, strings.TrimSpace(string(body)))
	}

	var tokenResp struct {
		AccessToken  string `json:"access_token"`
		RefreshToken string `json:"refresh_token"`
		ExpiresIn    int64  `json:"expires_in"`
	}
	if err := json.Unmarshal(body, &tokenResp); err != nil {
		return OAuthCredential{}, err
	}
	if strings.TrimSpace(tokenResp.AccessToken) == "" {
		return OAuthCredential{}, fmt.Errorf("token exchange returned empty access token")
	}
	if strings.TrimSpace(tokenResp.RefreshToken) == "" {
		return OAuthCredential{}, fmt.Errorf("token exchange returned empty refresh token")
	}

	email := ""
	if e, err := p.getUserEmail(ctx, tokenResp.AccessToken); err == nil {
		email = e
	}

	discovered, err := p.DiscoverProject(ctx, DiscoverProjectRequest{
		Token: tokenResp.AccessToken,
	})
	if err != nil {
		fallbackEndpoint := resolveEndpointFallback(p.endpoints)
		projectID := resolveGoogleCloudProject()
		// Keep OAuth usable even if project discovery endpoints are temporarily unavailable.
		if strings.Contains(strings.ToLower(err.Error()), "loadcodeassist failed on all configured endpoints") {
			discovered = DiscoverProjectResult{
				ProjectID:      projectID,
				ActiveEndpoint: fallbackEndpoint,
			}
		} else {
			return OAuthCredential{}, err
		}
	}

	expiresAt := time.Now().Add(time.Duration(tokenResp.ExpiresIn) * time.Second).Add(-5 * time.Minute)
	if tokenResp.ExpiresIn <= 0 {
		expiresAt = time.Now().Add(55 * time.Minute)
	}

	return OAuthCredential{
		AccessToken:    tokenResp.AccessToken,
		RefreshToken:   tokenResp.RefreshToken,
		ExpiresAt:      expiresAt,
		Email:          email,
		ProjectID:      discovered.ProjectID,
		ActiveEndpoint: discovered.ActiveEndpoint,
	}, nil
}

func resolveEndpointFallback(endpoints []string) string {
	if len(endpoints) > 0 {
		if endpoint := strings.TrimSpace(endpoints[0]); endpoint != "" {
			return endpoint
		}
	}
	return defaultGeminiProdEndpoint
}

func (p *GeminiInternalProvider) RefreshOAuthIfNeeded(
	ctx context.Context,
	credential OAuthCredential,
) (OAuthCredential, bool, error) {
	if strings.TrimSpace(credential.AccessToken) == "" {
		return credential, false, fmt.Errorf("missing access token")
	}
	if strings.TrimSpace(credential.RefreshToken) == "" {
		return credential, false, fmt.Errorf("missing refresh token")
	}
	if !credential.ExpiresAt.IsZero() && time.Now().Before(credential.ExpiresAt.Add(-2*time.Minute)) {
		return credential, false, nil
	}

	cfg, err := resolveOAuthClientConfig()
	if err != nil {
		return credential, false, err
	}
	form := url.Values{}
	form.Set("client_id", cfg.ClientID)
	form.Set("grant_type", "refresh_token")
	form.Set("refresh_token", credential.RefreshToken)
	if strings.TrimSpace(cfg.ClientSecret) != "" {
		form.Set("client_secret", cfg.ClientSecret)
	}

	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, googleTokenURL, bytes.NewBufferString(form.Encode()))
	if err != nil {
		return credential, false, err
	}
	httpReq.Header.Set("Content-Type", "application/x-www-form-urlencoded;charset=UTF-8")
	resp, err := p.client.Do(httpReq)
	if err != nil {
		return credential, false, err
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(resp.Body)
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return credential, false, fmt.Errorf("refresh token failed: status=%d body=%s", resp.StatusCode, strings.TrimSpace(string(body)))
	}

	var refreshResp struct {
		AccessToken  string `json:"access_token"`
		RefreshToken string `json:"refresh_token"`
		ExpiresIn    int64  `json:"expires_in"`
	}
	if err := json.Unmarshal(body, &refreshResp); err != nil {
		return credential, false, err
	}
	if strings.TrimSpace(refreshResp.AccessToken) == "" {
		return credential, false, fmt.Errorf("refresh token returned empty access token")
	}
	credential.AccessToken = strings.TrimSpace(refreshResp.AccessToken)
	if strings.TrimSpace(refreshResp.RefreshToken) != "" {
		credential.RefreshToken = strings.TrimSpace(refreshResp.RefreshToken)
	}
	if refreshResp.ExpiresIn > 0 {
		credential.ExpiresAt = time.Now().Add(time.Duration(refreshResp.ExpiresIn) * time.Second).Add(-5 * time.Minute)
	}
	if email, err := p.getUserEmail(ctx, credential.AccessToken); err == nil && strings.TrimSpace(email) != "" {
		credential.Email = email
	}
	return credential, true, nil
}

func (p *GeminiInternalProvider) getUserEmail(ctx context.Context, accessToken string) (string, error) {
	httpReq, err := http.NewRequestWithContext(ctx, http.MethodGet, googleUserInfoURL, nil)
	if err != nil {
		return "", err
	}
	httpReq.Header.Set("Authorization", "Bearer "+accessToken)
	resp, err := p.client.Do(httpReq)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return "", fmt.Errorf("userinfo failed: status=%d", resp.StatusCode)
	}
	var payload struct {
		Email string `json:"email"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&payload); err != nil {
		return "", err
	}
	return strings.TrimSpace(payload.Email), nil
}

func resolveOAuthClientConfig() (oauthClientConfig, error) {
	oauthClientMu.Lock()
	defer oauthClientMu.Unlock()
	if cachedOAuthClientConfig != nil {
		return *cachedOAuthClientConfig, nil
	}

	clientID := resolveFirstNonEmptyEnv(googleClientIDEnvKeys)
	clientSecret := resolveFirstNonEmptyEnv(googleClientSecretEnvKeys)
	if clientID != "" {
		cachedOAuthClientConfig = &oauthClientConfig{ClientID: clientID, ClientSecret: clientSecret}
		return *cachedOAuthClientConfig, nil
	}

	config, err := extractOAuthClientConfigFromGeminiCLI()
	if err == nil {
		cachedOAuthClientConfig = &config
		return *cachedOAuthClientConfig, nil
	}

	return oauthClientConfig{}, fmt.Errorf(
		"gemini oauth client not found; install gemini-cli or set OPENCLAW_GEMINI_OAUTH_CLIENT_ID",
	)
}

func resolveFirstNonEmptyEnv(keys []string) string {
	for _, key := range keys {
		if value := strings.TrimSpace(os.Getenv(key)); value != "" {
			return value
		}
	}
	return ""
}

func extractOAuthClientConfigFromGeminiCLI() (oauthClientConfig, error) {
	binPath, err := exec.LookPath("gemini")
	if err != nil {
		return oauthClientConfig{}, err
	}
	resolvedPath, err := filepath.EvalSymlinks(binPath)
	if err != nil {
		resolvedPath = binPath
	}
	dirs := resolveGeminiCLIDirs(binPath, resolvedPath)
	for _, dir := range dirs {
		for _, candidate := range []string{
			filepath.Join(dir, "node_modules", "@google", "gemini-cli-core", "dist", "src", "code_assist", "oauth2.js"),
			filepath.Join(dir, "node_modules", "@google", "gemini-cli-core", "dist", "code_assist", "oauth2.js"),
		} {
			if cfg, ok := parseOAuthClientFile(candidate); ok {
				return cfg, nil
			}
		}
		if found := findFileByName(dir, "oauth2.js", 8); found != "" {
			if cfg, ok := parseOAuthClientFile(found); ok {
				return cfg, nil
			}
		}
	}
	return oauthClientConfig{}, fmt.Errorf("oauth2.js not found")
}

func resolveGeminiCLIDirs(binPath, resolvedPath string) []string {
	binDir := filepath.Dir(binPath)
	candidates := []string{
		filepath.Dir(filepath.Dir(resolvedPath)),
		filepath.Join(filepath.Dir(resolvedPath), "node_modules", "@google", "gemini-cli"),
		filepath.Join(binDir, "node_modules", "@google", "gemini-cli"),
		filepath.Join(filepath.Dir(binDir), "node_modules", "@google", "gemini-cli"),
		filepath.Join(filepath.Dir(binDir), "lib", "node_modules", "@google", "gemini-cli"),
	}
	seen := map[string]struct{}{}
	out := make([]string, 0, len(candidates))
	for _, candidate := range candidates {
		normalized := strings.TrimSpace(candidate)
		if normalized == "" {
			continue
		}
		if _, ok := seen[normalized]; ok {
			continue
		}
		seen[normalized] = struct{}{}
		out = append(out, normalized)
	}
	return out
}

func findFileByName(root, name string, maxDepth int) string {
	if maxDepth <= 0 {
		return ""
	}
	entries, err := os.ReadDir(root)
	if err != nil {
		return ""
	}
	for _, entry := range entries {
		candidate := filepath.Join(root, entry.Name())
		if entry.IsDir() {
			if strings.HasPrefix(entry.Name(), ".") {
				continue
			}
			if nested := findFileByName(candidate, name, maxDepth-1); nested != "" {
				return nested
			}
			continue
		}
		if entry.Name() == name {
			return candidate
		}
	}
	return ""
}

var (
	oauthClientIDPattern     = regexp.MustCompile(`\d+-[a-z0-9]+\.apps\.googleusercontent\.com`)
	oauthClientSecretPattern = regexp.MustCompile(`GOCSPX-[A-Za-z0-9_-]+`)
)

func parseOAuthClientFile(path string) (oauthClientConfig, bool) {
	content, err := os.ReadFile(path)
	if err != nil {
		return oauthClientConfig{}, false
	}
	id := oauthClientIDPattern.Find(content)
	secret := oauthClientSecretPattern.Find(content)
	if len(id) == 0 || len(secret) == 0 {
		return oauthClientConfig{}, false
	}
	return oauthClientConfig{ClientID: string(id), ClientSecret: string(secret)}, true
}
