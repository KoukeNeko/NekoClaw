package tui

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"os/exec"
	"runtime"
	"sort"
	"strings"
	"time"

	"github.com/charmbracelet/lipgloss"
	"github.com/doeshing/nekoclaw/internal/client"
	"github.com/doeshing/nekoclaw/internal/termclean"
)

// ---------------------------------------------------------------------------
// String utilities
// ---------------------------------------------------------------------------

func fallback(value, fallbackValue string) string {
	if strings.TrimSpace(value) == "" {
		return fallbackValue
	}
	return strings.TrimSpace(value)
}

func clampLine(text string, max int) string {
	text = sanitizeDisplayText(text)
	if max <= 0 {
		return text
	}
	runes := []rune(text)
	if len(runes) <= max {
		return text
	}
	if max <= 1 {
		return string(runes[:max])
	}
	return string(runes[:max-1]) + "…"
}

func wrapToWidth(text string, max int) []string {
	if max <= 0 {
		return []string{text}
	}
	runes := []rune(text)
	if len(runes) == 0 {
		return []string{""}
	}
	out := make([]string, 0, (len(runes)/max)+1)
	for len(runes) > 0 {
		n := max
		if n > len(runes) {
			n = len(runes)
		}
		out = append(out, string(runes[:n]))
		runes = runes[n:]
	}
	return out
}

func appendUnique(items []string, value string) []string {
	value = strings.TrimSpace(value)
	if value == "" {
		return items
	}
	for _, item := range items {
		if strings.TrimSpace(item) == value {
			return items
		}
	}
	return append(items, value)
}

func removeFromSlice(items []string, target string) []string {
	out := make([]string, 0, len(items))
	for _, item := range items {
		if strings.TrimSpace(item) != target {
			out = append(out, item)
		}
	}
	return out
}

// ---------------------------------------------------------------------------
// Time formatting
// ---------------------------------------------------------------------------

func formatTimeAgo(t time.Time) string {
	if t.IsZero() {
		return ""
	}
	dur := time.Since(t)
	switch {
	case dur < time.Minute:
		return "剛剛"
	case dur < time.Hour:
		return fmt.Sprintf("%d 分鐘前", int(dur.Minutes()))
	case dur < 24*time.Hour:
		return fmt.Sprintf("%d 小時前", int(dur.Hours()))
	default:
		return fmt.Sprintf("%d 天前", int(dur.Hours()/24))
	}
}

// ---------------------------------------------------------------------------
// Terminal utilities
// ---------------------------------------------------------------------------

func fitToTerminalWidth(rendered string, width int) string {
	if width <= 0 || strings.TrimSpace(rendered) == "" {
		return rendered
	}
	lines := strings.Split(rendered, "\n")
	maxWidthStyle := lipgloss.NewStyle().MaxWidth(width)
	for idx, line := range lines {
		if lipgloss.Width(line) <= width {
			continue
		}
		lines[idx] = maxWidthStyle.Render(line)
	}
	return strings.Join(lines, "\n")
}

func openExternalURL(rawURL string) error {
	rawURL = strings.TrimSpace(rawURL)
	if rawURL == "" {
		return fmt.Errorf("empty url")
	}
	var cmd *exec.Cmd
	switch runtime.GOOS {
	case "darwin":
		cmd = exec.Command("open", rawURL)
	case "windows":
		cmd = exec.Command("rundll32", "url.dll,FileProtocolHandler", rawURL)
	default:
		cmd = exec.Command("xdg-open", rawURL)
	}
	return cmd.Start()
}

// ---------------------------------------------------------------------------
// Error formatting (Traditional Chinese)
// ---------------------------------------------------------------------------

func formatChatError(err error) string {
	var apiErr *client.APIError
	if !errors.As(err, &apiErr) {
		if errors.Is(err, context.DeadlineExceeded) || strings.Contains(err.Error(), "deadline exceeded") {
			return "請求逾時，請稍後再試。"
		}
		if strings.Contains(err.Error(), "connection refused") {
			return "無法連線至 API 伺服器，請確認伺服器是否正在執行。"
		}
		return err.Error()
	}

	switch {
	case apiErr.StatusCode == http.StatusConflict:
		msg := "所有帳號暫時不可用。"
		if reason := parseFieldFromMessage(apiErr.Message, "reason"); reason != "" {
			msg += "可能原因：" + translateFailureReason(reason) + "。"
		}
		if retryAt := parseFieldFromMessage(apiErr.Message, "retry_at"); retryAt != "" {
			if t, parseErr := time.Parse(time.RFC3339, retryAt); parseErr == nil {
				remaining := time.Until(t).Round(time.Second)
				if remaining > 0 {
					msg += fmt.Sprintf(" 預計 %s 後可恢復。", remaining)
				}
			}
		}
		msg += " 請稍後再試或新增帳號。"
		return msg
	case apiErr.Code == "missing_project":
		return "Gemini 缺少 Project ID，請重新 OAuth 或設定環境變數 GOOGLE_CLOUD_PROJECT。"
	case apiErr.Code == "model_not_found":
		return "模型不可用或不存在，請改選其他模型。"
	case apiErr.Code == "auth", apiErr.Code == "auth_permanent":
		detail := strings.TrimSpace(apiErr.Message)
		if detail == "" {
			return "驗證失敗，請重新登入或更換憑證。"
		}
		return "驗證失敗，請重新登入或更換憑證。(" + detail + ")"
	case apiErr.Code == "billing":
		return "帳號配額或帳單狀態異常，請確認帳戶設定。"
	case apiErr.Code == "rate_limit":
		return "請求過於頻繁，請稍後再試。"
	case apiErr.Code == "format":
		return "請求格式被 provider 拒絕，請檢查模型與輸入內容。"
	case apiErr.StatusCode == http.StatusBadGateway:
		msg := strings.TrimSpace(apiErr.Message)
		if msg == "" {
			return "API 連線失敗，請確認網路或 provider 狀態。"
		}
		return "Provider 回應異常：" + msg
	case apiErr.StatusCode == http.StatusServiceUnavailable:
		return "服務暫時不可用，請稍後再試。"
	default:
		return apiErr.Error()
	}
}

func parseFieldFromMessage(message, key string) string {
	prefix := key + "="
	idx := strings.Index(message, prefix)
	if idx < 0 {
		return ""
	}
	rest := message[idx+len(prefix):]
	if spaceIdx := strings.IndexByte(rest, ' '); spaceIdx >= 0 {
		return rest[:spaceIdx]
	}
	return rest
}

func translateFailureReason(reason string) string {
	switch reason {
	case "rate_limit":
		return "API 請求頻率限制"
	case "billing":
		return "帳單/配額問題"
	case "auth", "auth_permanent":
		return "驗證失敗"
	case "timeout":
		return "請求逾時"
	case "model_not_found":
		return "找不到指定模型"
	case "format":
		return "請求格式錯誤"
	default:
		return "未知原因"
	}
}

// ---------------------------------------------------------------------------
// Profile / option builders
// ---------------------------------------------------------------------------

func sortedProfiles(in []client.GeminiAuthProfile) []client.GeminiAuthProfile {
	profiles := append([]client.GeminiAuthProfile(nil), in...)
	sort.SliceStable(profiles, func(i, j int) bool {
		if profiles[i].Preferred != profiles[j].Preferred {
			return profiles[i].Preferred
		}
		return profiles[i].ProfileID < profiles[j].ProfileID
	})
	return profiles
}

func sortedAIStudioProfiles(in []client.AIStudioProfile) []client.AIStudioProfile {
	profiles := append([]client.AIStudioProfile(nil), in...)
	sort.SliceStable(profiles, func(i, j int) bool {
		if profiles[i].Preferred != profiles[j].Preferred {
			return profiles[i].Preferred
		}
		return profiles[i].ProfileID < profiles[j].ProfileID
	})
	return profiles
}

func sortedAnthropicProfiles(in []client.AnthropicProfile) []client.AnthropicProfile {
	profiles := append([]client.AnthropicProfile(nil), in...)
	sort.SliceStable(profiles, func(i, j int) bool {
		if profiles[i].Preferred != profiles[j].Preferred {
			return profiles[i].Preferred
		}
		return profiles[i].ProfileID < profiles[j].ProfileID
	})
	return profiles
}

func sortedOpenAIProfiles(in []client.OpenAIProfile) []client.OpenAIProfile {
	profiles := append([]client.OpenAIProfile(nil), in...)
	sort.SliceStable(profiles, func(i, j int) bool {
		if profiles[i].Preferred != profiles[j].Preferred {
			return profiles[i].Preferred
		}
		if profiles[i].Provider != profiles[j].Provider {
			return profiles[i].Provider < profiles[j].Provider
		}
		return profiles[i].ProfileID < profiles[j].ProfileID
	})
	return profiles
}

func formatProfileLine(profile client.GeminiAuthProfile) string {
	state := "available"
	if !profile.Available {
		if !profile.DisabledUntil.IsZero() {
			state = "disabled_until=" + profile.DisabledUntil.Format(time.RFC3339)
		} else if !profile.CooldownUntil.IsZero() {
			state = "cooldown_until=" + profile.CooldownUntil.Format(time.RFC3339)
		} else {
			state = "unavailable"
		}
	}
	if profile.DisabledReason != "" {
		state += " reason=" + profile.DisabledReason
	}
	if !profile.ProjectReady {
		state += " project=missing"
	}
	if reason := strings.TrimSpace(profile.UnavailableReason); reason != "" {
		state += " unavailable_reason=" + reason
	}
	return fmt.Sprintf(
		"%s · email=%s · project=%s · %s",
		profile.ProfileID,
		fallback(profile.Email, "-"),
		fallback(profile.ProjectID, "-"),
		state,
	)
}

func modelOptionsForProvider(providerID, current string) []string {
	providerID = strings.TrimSpace(providerID)
	models := []string{"default"}
	switch providerID {
	case "google-gemini-cli":
		models = []string{"default", "gemini-3-pro-preview", "gemini-2.5-pro", "gemini-3-flash-preview", "gemini-2.5-flash"}
	case "google-ai-studio":
		models = []string{"default", "gemini-2.5-pro", "gemini-2.5-flash"}
	case "anthropic":
		models = []string{"default", "claude-opus-4-6", "claude-sonnet-4-6", "claude-sonnet-4-5", "claude-haiku-3-5"}
	case "openai":
		models = []string{"default", "gpt-5.1-codex", "gpt-5", "gpt-4.1"}
	case "openai-codex":
		models = []string{"default", "gpt-5.3-codex", "gpt-5.1-codex"}
	case "mock":
		models = []string{"default"}
	}
	if current = strings.TrimSpace(current); current != "" {
		models = appendUnique(models, current)
	}
	return models
}

// ---------------------------------------------------------------------------
// Overlay utilities
// ---------------------------------------------------------------------------

// dimLines dims all lines in the given content string for use as overlay background.
func dimLines(content string) string {
	lines := strings.Split(content, "\n")
	dimStyle := theme.DimStyle
	for i, line := range lines {
		plain := stripTerminalControlSequences(line)
		lines[i] = dimStyle.Render(plain)
	}
	return strings.Join(lines, "\n")
}

// centerOverlay places an overlay box on top of background content, centered.
func centerOverlay(bg, overlay string, bgW, bgH int) string {
	bgLines := strings.Split(bg, "\n")
	overlayLines := strings.Split(overlay, "\n")

	// Ensure bg has enough lines
	for len(bgLines) < bgH {
		bgLines = append(bgLines, "")
	}

	overlayW := 0
	for _, line := range overlayLines {
		if w := lipgloss.Width(line); w > overlayW {
			overlayW = w
		}
	}
	overlayH := len(overlayLines)

	// Calculate position (centered)
	startRow := (bgH - overlayH) / 2
	if startRow < 0 {
		startRow = 0
	}
	startCol := (bgW - overlayW) / 2
	if startCol < 0 {
		startCol = 0
	}

	// Overlay line by line
	for i, overlayLine := range overlayLines {
		row := startRow + i
		if row >= len(bgLines) {
			break
		}
		bgLine := bgLines[row]
		bgLineW := lipgloss.Width(bgLine)

		var sb strings.Builder
		// Left padding from bg
		if startCol > 0 {
			if bgLineW >= startCol {
				sb.WriteString(strings.Repeat(" ", startCol))
			} else {
				sb.WriteString(strings.Repeat(" ", startCol))
			}
		}
		sb.WriteString(overlayLine)

		// Right padding
		used := startCol + lipgloss.Width(overlayLine)
		if used < bgW {
			remaining := bgW - used
			sb.WriteString(strings.Repeat(" ", remaining))
		}

		bgLines[row] = sb.String()
	}

	return strings.Join(bgLines[:bgH], "\n")
}

// stripAnsi removes ANSI escape codes from a string.
func stripAnsi(s string) string {
	return stripTerminalControlSequences(s)
}

func stripTerminalControlSequences(s string) string {
	return termclean.StripTerminalControlSequences(s)
}

func sanitizeDisplayText(s string) string {
	return termclean.SanitizeDisplayText(s)
}
