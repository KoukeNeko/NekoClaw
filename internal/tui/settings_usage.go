package tui

import (
	"fmt"
	"strings"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/doeshing/nekoclaw/internal/core"
	"github.com/doeshing/nekoclaw/internal/usage"
)

// UsageSection displays cumulative token usage and estimated cost.
type UsageSection struct {
	totalInput  int
	totalOutput int
	totalTokens int
	totalCost   float64
	modelID     string
	provider    string

	// Per-message history for breakdown
	messages []usageEntry
}

type usageEntry struct {
	Input  int
	Output int
	Total  int
	Cost   float64
	Model  string
}

func NewUsageSection(provider, modelID string) UsageSection {
	return UsageSection{
		provider: provider,
		modelID:  modelID,
	}
}

func (us *UsageSection) SetProvider(p string) { us.provider = p }
func (us *UsageSection) SetModel(m string)    { us.modelID = m }

// RecordUsage accumulates token counts from a single API response.
func (us *UsageSection) RecordUsage(usage core.UsageInfo, model string) {
	if usage.TotalTokens == 0 && usage.InputTokens == 0 && usage.OutputTokens == 0 {
		return
	}
	cost := calculateCost(usage, model)
	us.totalInput += usage.InputTokens
	us.totalOutput += usage.OutputTokens
	us.totalTokens += usage.TotalTokens
	us.totalCost += cost
	us.messages = append(us.messages, usageEntry{
		Input:  usage.InputTokens,
		Output: usage.OutputTokens,
		Total:  usage.TotalTokens,
		Cost:   cost,
		Model:  model,
	})
}

// TotalCost returns accumulated cost for status bar display.
func (us *UsageSection) TotalCost() float64 { return us.totalCost }

// Reset clears all accumulated usage (e.g. on session change).
func (us *UsageSection) Reset() {
	us.totalInput = 0
	us.totalOutput = 0
	us.totalTokens = 0
	us.totalCost = 0
	us.messages = nil
}

func (us *UsageSection) Update(_ tea.KeyMsg) tea.Cmd {
	return nil
}

func (us *UsageSection) View(width, height int) string {
	var lines []string

	lines = append(lines, theme.HeaderStyle.Render("Usage"))
	lines = append(lines, "")

	// Summary
	lines = append(lines, theme.SectionStyle.Render("Session Totals"))
	lines = append(lines, "")
	lines = append(lines, fmt.Sprintf("  Input tokens:   %s", formatTokenCount(us.totalInput)))
	lines = append(lines, fmt.Sprintf("  Output tokens:  %s", formatTokenCount(us.totalOutput)))
	lines = append(lines, fmt.Sprintf("  Total tokens:   %s", formatTokenCount(us.totalTokens)))
	lines = append(lines, fmt.Sprintf("  Estimated cost: %s", formatCost(us.totalCost)))
	lines = append(lines, "")

	// Model pricing info
	pricing, found := usage.LookupPricing(us.modelID)
	if found {
		lines = append(lines, theme.SectionStyle.Render("Current Model Rates"))
		lines = append(lines, "")
		lines = append(lines, fmt.Sprintf("  Model:  %s", us.modelID))
		lines = append(lines, fmt.Sprintf("  Input:  $%.2f / 1M tokens", pricing.InputPerMillion))
		lines = append(lines, fmt.Sprintf("  Output: $%.2f / 1M tokens", pricing.OutputPerMillion))
		lines = append(lines, "")
	}

	// Recent messages breakdown (last 10)
	if len(us.messages) > 0 {
		lines = append(lines, theme.SectionStyle.Render("Recent Requests"))
		lines = append(lines, "")
		start := len(us.messages) - 10
		if start < 0 {
			start = 0
		}
		for i := start; i < len(us.messages); i++ {
			entry := us.messages[i]
			idx := i + 1
			line := fmt.Sprintf("  #%d  in:%s  out:%s  %s",
				idx,
				formatTokenCount(entry.Input),
				formatTokenCount(entry.Output),
				formatCost(entry.Cost),
			)
			lines = append(lines, clampLine(line, width))
		}
		lines = append(lines, "")
	} else {
		lines = append(lines, theme.HintStyle.Render("  尚無使用紀錄。發送訊息後將在此顯示 token 用量。"))
		lines = append(lines, "")
	}

	return strings.Join(lines, "\n")
}

// calculateCost computes estimated cost for a single API call.
// Formula: (input × inputRate / 1M) + (output × outputRate / 1M)
func calculateCost(info core.UsageInfo, model string) float64 {
	return usage.CalculateCost(info.InputTokens, info.OutputTokens, model)
}

func formatTokenCount(n int) string {
	if n == 0 {
		return "0"
	}
	if n >= 1_000_000 {
		return fmt.Sprintf("%.1fM", float64(n)/1_000_000)
	}
	if n >= 1_000 {
		return fmt.Sprintf("%.1fK", float64(n)/1_000)
	}
	return fmt.Sprintf("%d", n)
}

func formatCost(cost float64) string {
	if cost == 0 {
		return "$0.00"
	}
	if cost < 0.01 {
		return fmt.Sprintf("$%.4f", cost)
	}
	return fmt.Sprintf("$%.2f", cost)
}
