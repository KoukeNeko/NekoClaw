package usage

import (
	"sort"
	"strings"
	"time"

	"github.com/doeshing/nekoclaw/internal/core"
)

const trendWindowDays = 14
const maxNamedModels = 6

type Summary struct {
	GeneratedAt time.Time           `json:"generated_at"`
	Totals      Totals              `json:"totals"`
	Performance Performance         `json:"performance"`
	Trend       []TrendPoint        `json:"trend"`
	Providers   []ProviderBreakdown `json:"providers"`
	Models      []ModelBreakdown    `json:"models"`
	Sessions    []SessionSummary    `json:"sessions"`
}

type Totals struct {
	SessionCount     int     `json:"session_count"`
	MessageCount     int     `json:"message_count"`
	RequestCount     int     `json:"request_count"`
	InputTokens      int     `json:"input_tokens"`
	OutputTokens     int     `json:"output_tokens"`
	TotalTokens      int     `json:"total_tokens"`
	EstimatedCostUSD float64 `json:"estimated_cost_usd"`
	CompactionCount  int     `json:"compaction_count"`
}

type TrendPoint struct {
	Date             string  `json:"date"`
	InputTokens      int     `json:"input_tokens"`
	OutputTokens     int     `json:"output_tokens"`
	TotalTokens      int     `json:"total_tokens"`
	EstimatedCostUSD float64 `json:"estimated_cost_usd"`
	RequestCount     int     `json:"request_count"`
}

type Performance struct {
	ProcessedPromptTokens *int     `json:"processed_prompt_tokens,omitempty"`
	CachedTokens          *int     `json:"cached_tokens,omitempty"`
	CacheEfficiency       *float64 `json:"cache_efficiency,omitempty"`
	PromptTokensPerSecond *float64 `json:"prompt_tokens_per_second,omitempty"`
	OutputTokensPerSecond *float64 `json:"output_tokens_per_second,omitempty"`
}

type ProviderBreakdown struct {
	Provider         string  `json:"provider"`
	RequestCount     int     `json:"request_count"`
	InputTokens      int     `json:"input_tokens"`
	OutputTokens     int     `json:"output_tokens"`
	TotalTokens      int     `json:"total_tokens"`
	EstimatedCostUSD float64 `json:"estimated_cost_usd"`
	SessionCount     int     `json:"session_count"`
}

type ModelBreakdown struct {
	Model            string  `json:"model"`
	RequestCount     int     `json:"request_count"`
	InputTokens      int     `json:"input_tokens"`
	OutputTokens     int     `json:"output_tokens"`
	TotalTokens      int     `json:"total_tokens"`
	EstimatedCostUSD float64 `json:"estimated_cost_usd"`
	SessionCount     int     `json:"session_count"`
}

type SessionSummary struct {
	SessionID        string    `json:"session_id"`
	Title            string    `json:"title,omitempty"`
	CreatedAt        time.Time `json:"created_at"`
	UpdatedAt        time.Time `json:"updated_at"`
	MessageCount     int       `json:"message_count"`
	RequestCount     int       `json:"request_count"`
	InputTokens      int       `json:"input_tokens"`
	OutputTokens     int       `json:"output_tokens"`
	TotalTokens      int       `json:"total_tokens"`
	EstimatedCostUSD float64   `json:"estimated_cost_usd"`
	CompactionCount  int       `json:"compaction_count"`
	TopProvider      string    `json:"top_provider,omitempty"`
	TopModel         string    `json:"top_model,omitempty"`
}

type aggregate struct {
	Key           string
	RequestCount  int
	InputTokens   int
	OutputTokens  int
	TotalTokens   int
	EstimatedCost float64
	SessionIDs    map[string]struct{}
}

type performanceAggregate struct {
	processedPromptTokens       int
	hasProcessedPromptTokens    bool
	cachedTokens                int
	hasCachedTokens             bool
	cacheEligiblePromptTokens   int
	hasCacheEligiblePromptTotal bool
	promptUncachedTokens        int
	promptElapsed               time.Duration
	hasPromptRate               bool
	outputTokens                int
	outputElapsed               time.Duration
	hasOutputRate               bool
}

func SummarizeSessionStore(store *core.SessionStore, now time.Time) Summary {
	summary := Summary{
		GeneratedAt: now,
		Trend:       buildTrend(now),
	}
	if store == nil {
		return summary
	}

	metadata := store.ListSessions()
	summary.Totals.SessionCount = len(metadata)

	trendIndex := make(map[string]int, len(summary.Trend))
	for idx, point := range summary.Trend {
		trendIndex[point.Date] = idx
	}

	providerAggs := map[string]*aggregate{}
	modelAggs := map[string]*aggregate{}
	performance := performanceAggregate{}
	sessionSummaries := make([]SessionSummary, 0, len(metadata))
	location := now.Location()

	for _, meta := range metadata {
		sessionSummary := SessionSummary{
			SessionID:    meta.SessionID,
			Title:        meta.Title,
			CreatedAt:    meta.CreatedAt,
			UpdatedAt:    meta.UpdatedAt,
			MessageCount: 0,
		}

		sessionProviderAggs := map[string]*aggregate{}
		sessionModelAggs := map[string]*aggregate{}

		entries := store.History(meta.SessionID)
		for _, entry := range entries {
			switch entry.Type {
			case core.EntryMessage:
				sessionSummary.MessageCount++
				summary.Totals.MessageCount++

				if entry.Role != core.RoleAssistant {
					continue
				}

				sessionSummary.RequestCount++
				summary.Totals.RequestCount++

				inputTokens, outputTokens, totalTokens := usageParts(entry.MsgUsage)
				cost := CalculateCost(inputTokens, outputTokens, entry.MsgModel)

				sessionSummary.InputTokens += inputTokens
				sessionSummary.OutputTokens += outputTokens
				sessionSummary.TotalTokens += totalTokens
				sessionSummary.EstimatedCostUSD += cost

				summary.Totals.InputTokens += inputTokens
				summary.Totals.OutputTokens += outputTokens
				summary.Totals.TotalTokens += totalTokens
				summary.Totals.EstimatedCostUSD += cost
				performance.observe(entry.MsgUsage, entry.MsgElapsedMs)

				if providerKey := strings.TrimSpace(entry.MsgProvider); providerKey != "" {
					accumulate(providerAggs, providerKey, meta.SessionID, inputTokens, outputTokens, totalTokens, cost)
					incrementRequests(providerAggs[providerKey])
					accumulate(sessionProviderAggs, providerKey, meta.SessionID, inputTokens, outputTokens, totalTokens, cost)
					incrementRequests(sessionProviderAggs[providerKey])
				}

				if modelKey := strings.TrimSpace(entry.MsgModel); modelKey != "" {
					accumulate(modelAggs, modelKey, meta.SessionID, inputTokens, outputTokens, totalTokens, cost)
					incrementRequests(modelAggs[modelKey])
					accumulate(sessionModelAggs, modelKey, meta.SessionID, inputTokens, outputTokens, totalTokens, cost)
					incrementRequests(sessionModelAggs[modelKey])
				}

				dateKey := entry.Timestamp.In(location).Format("2006-01-02")
				if idx, ok := trendIndex[dateKey]; ok {
					summary.Trend[idx].InputTokens += inputTokens
					summary.Trend[idx].OutputTokens += outputTokens
					summary.Trend[idx].TotalTokens += totalTokens
					summary.Trend[idx].EstimatedCostUSD += cost
					summary.Trend[idx].RequestCount++
				}

			case core.EntryCompaction:
				sessionSummary.CompactionCount++
				summary.Totals.CompactionCount++
			}
		}

		sessionSummary.TopProvider = topAggregateKey(sessionProviderAggs)
		sessionSummary.TopModel = topAggregateKey(sessionModelAggs)
		sessionSummaries = append(sessionSummaries, sessionSummary)
	}

	sort.Slice(sessionSummaries, func(i, j int) bool {
		if sessionSummaries[i].UpdatedAt.Equal(sessionSummaries[j].UpdatedAt) {
			return sessionSummaries[i].SessionID < sessionSummaries[j].SessionID
		}
		return sessionSummaries[i].UpdatedAt.After(sessionSummaries[j].UpdatedAt)
	})
	summary.Sessions = sessionSummaries
	summary.Providers = flattenProviderAggregates(providerAggs)
	summary.Models = flattenModelAggregates(modelAggs)
	summary.Performance = performance.finalize()

	return summary
}

func buildTrend(now time.Time) []TrendPoint {
	points := make([]TrendPoint, 0, trendWindowDays)
	base := time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, now.Location())
	for daysAgo := trendWindowDays - 1; daysAgo >= 0; daysAgo-- {
		day := base.AddDate(0, 0, -daysAgo)
		points = append(points, TrendPoint{Date: day.Format("2006-01-02")})
	}
	return points
}

func usageParts(info *core.UsageInfo) (int, int, int) {
	if info == nil {
		return 0, 0, 0
	}
	total := info.TotalTokens
	if total == 0 && (info.InputTokens > 0 || info.OutputTokens > 0) {
		total = info.InputTokens + info.OutputTokens
	}
	return info.InputTokens, info.OutputTokens, total
}

func (agg *performanceAggregate) observe(info *core.UsageInfo, elapsedMs int64) {
	if info == nil {
		return
	}
	if info.PromptTokensTotal != nil {
		agg.processedPromptTokens += *info.PromptTokensTotal
		agg.hasProcessedPromptTokens = true
	}
	if info.CachedTokens != nil {
		agg.cachedTokens += *info.CachedTokens
		agg.hasCachedTokens = true
		if info.PromptTokensTotal != nil {
			agg.cacheEligiblePromptTokens += *info.PromptTokensTotal
			agg.hasCacheEligiblePromptTotal = true
		}
	}
	if info.PromptTokensUncached != nil && elapsedMs > 0 {
		agg.promptUncachedTokens += *info.PromptTokensUncached
		agg.promptElapsed += time.Duration(elapsedMs) * time.Millisecond
		agg.hasPromptRate = true
	}
	if elapsedMs > 0 {
		agg.outputTokens += info.OutputTokens
		agg.outputElapsed += time.Duration(elapsedMs) * time.Millisecond
		agg.hasOutputRate = true
	}
}

func (agg performanceAggregate) finalize() Performance {
	performance := Performance{}
	if agg.hasProcessedPromptTokens {
		performance.ProcessedPromptTokens = usageIntPtr(agg.processedPromptTokens)
	}
	if agg.hasCachedTokens {
		performance.CachedTokens = usageIntPtr(agg.cachedTokens)
	}
	if agg.hasCacheEligiblePromptTotal && agg.cacheEligiblePromptTokens > 0 {
		performance.CacheEfficiency = usageFloatPtr(float64(agg.cachedTokens) / float64(agg.cacheEligiblePromptTokens))
	}
	if agg.hasPromptRate && agg.promptElapsed > 0 {
		performance.PromptTokensPerSecond = usageFloatPtr(float64(agg.promptUncachedTokens) / agg.promptElapsed.Seconds())
	}
	if agg.hasOutputRate && agg.outputElapsed > 0 {
		performance.OutputTokensPerSecond = usageFloatPtr(float64(agg.outputTokens) / agg.outputElapsed.Seconds())
	}
	return performance
}

func usageIntPtr(v int) *int {
	value := v
	return &value
}

func usageFloatPtr(v float64) *float64 {
	value := v
	return &value
}

func accumulate(target map[string]*aggregate, key, sessionID string, inputTokens, outputTokens, totalTokens int, cost float64) {
	agg, ok := target[key]
	if !ok {
		agg = &aggregate{
			Key:        key,
			SessionIDs: map[string]struct{}{},
		}
		target[key] = agg
	}
	agg.InputTokens += inputTokens
	agg.OutputTokens += outputTokens
	agg.TotalTokens += totalTokens
	agg.EstimatedCost += cost
	agg.SessionIDs[sessionID] = struct{}{}
}

func incrementRequests(agg *aggregate) {
	if agg != nil {
		agg.RequestCount++
	}
}

func topAggregateKey(values map[string]*aggregate) string {
	sorted := sortAggregates(values)
	if len(sorted) == 0 {
		return ""
	}
	return sorted[0].Key
}

func flattenProviderAggregates(values map[string]*aggregate) []ProviderBreakdown {
	sorted := sortAggregates(values)
	out := make([]ProviderBreakdown, 0, len(sorted))
	for _, agg := range sorted {
		out = append(out, ProviderBreakdown{
			Provider:         agg.Key,
			RequestCount:     agg.RequestCount,
			InputTokens:      agg.InputTokens,
			OutputTokens:     agg.OutputTokens,
			TotalTokens:      agg.TotalTokens,
			EstimatedCostUSD: agg.EstimatedCost,
			SessionCount:     len(agg.SessionIDs),
		})
	}
	return out
}

func flattenModelAggregates(values map[string]*aggregate) []ModelBreakdown {
	sorted := sortAggregates(values)
	if len(sorted) <= maxNamedModels {
		return mapModels(sorted)
	}

	named := sorted[:maxNamedModels]
	other := &aggregate{
		Key:        "other",
		SessionIDs: map[string]struct{}{},
	}
	for _, agg := range sorted[maxNamedModels:] {
		other.RequestCount += agg.RequestCount
		other.InputTokens += agg.InputTokens
		other.OutputTokens += agg.OutputTokens
		other.TotalTokens += agg.TotalTokens
		other.EstimatedCost += agg.EstimatedCost
		for sessionID := range agg.SessionIDs {
			other.SessionIDs[sessionID] = struct{}{}
		}
	}

	out := mapModels(named)
	if other.RequestCount > 0 || other.TotalTokens > 0 {
		out = append(out, ModelBreakdown{
			Model:            other.Key,
			RequestCount:     other.RequestCount,
			InputTokens:      other.InputTokens,
			OutputTokens:     other.OutputTokens,
			TotalTokens:      other.TotalTokens,
			EstimatedCostUSD: other.EstimatedCost,
			SessionCount:     len(other.SessionIDs),
		})
	}
	return out
}

func mapModels(sorted []*aggregate) []ModelBreakdown {
	out := make([]ModelBreakdown, 0, len(sorted))
	for _, agg := range sorted {
		out = append(out, ModelBreakdown{
			Model:            agg.Key,
			RequestCount:     agg.RequestCount,
			InputTokens:      agg.InputTokens,
			OutputTokens:     agg.OutputTokens,
			TotalTokens:      agg.TotalTokens,
			EstimatedCostUSD: agg.EstimatedCost,
			SessionCount:     len(agg.SessionIDs),
		})
	}
	return out
}

func sortAggregates(values map[string]*aggregate) []*aggregate {
	out := make([]*aggregate, 0, len(values))
	for _, agg := range values {
		out = append(out, agg)
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].TotalTokens != out[j].TotalTokens {
			return out[i].TotalTokens > out[j].TotalTokens
		}
		if out[i].RequestCount != out[j].RequestCount {
			return out[i].RequestCount > out[j].RequestCount
		}
		return out[i].Key < out[j].Key
	})
	return out
}
