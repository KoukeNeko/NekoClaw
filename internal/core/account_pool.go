package core

import (
	"sort"
	"strings"
	"sync"
	"time"
)

type ProfileUsageStats struct {
	LastUsed       time.Time             `json:"last_used,omitempty"`
	CooldownUntil  time.Time             `json:"cooldown_until,omitempty"`
	DisabledUntil  time.Time             `json:"disabled_until,omitempty"`
	DisabledReason FailureReason         `json:"disabled_reason,omitempty"`
	ErrorCount     int                   `json:"error_count,omitempty"`
	FailureCounts  map[FailureReason]int `json:"failure_counts,omitempty"`
	LastFailureAt  time.Time             `json:"last_failure_at,omitempty"`
}

type CooldownConfig struct {
	BillingBackoff time.Duration
	BillingMax     time.Duration
	FailureWindow  time.Duration
}

func DefaultCooldownConfig() CooldownConfig {
	return CooldownConfig{
		BillingBackoff: 5 * time.Hour,
		BillingMax:     24 * time.Hour,
		FailureWindow:  24 * time.Hour,
	}
}

type AccountPool struct {
	mu       sync.Mutex
	provider string
	accounts map[string]Account
	order    []string
	usage    map[string]*ProfileUsageStats
	cooldown CooldownConfig
}

type AccountSnapshot struct {
	Account
	Usage *ProfileUsageStats `json:"usage,omitempty"`
}

func NewAccountPool(provider string, accounts []Account, explicitOrder []string, cfg CooldownConfig) *AccountPool {
	cooldown := cfg
	if cooldown.BillingBackoff <= 0 || cooldown.BillingMax <= 0 || cooldown.FailureWindow <= 0 {
		cooldown = DefaultCooldownConfig()
	}
	p := &AccountPool{
		provider: provider,
		accounts: make(map[string]Account, len(accounts)),
		order:    append([]string(nil), explicitOrder...),
		usage:    make(map[string]*ProfileUsageStats, len(accounts)),
		cooldown: cooldown,
	}
	for _, account := range accounts {
		if account.ID == "" || account.Token == "" {
			continue
		}
		p.accounts[account.ID] = account
	}
	return p
}

func (p *AccountPool) Provider() string {
	return p.provider
}

func (p *AccountPool) Acquire(preferredID string) (Account, bool) {
	p.mu.Lock()
	defer p.mu.Unlock()

	now := time.Now()
	p.clearExpiredCooldownsLocked(now)
	ordered := p.resolveOrderLocked(preferredID, now)
	for _, id := range ordered {
		if id == "" {
			continue
		}
		account, ok := p.accounts[id]
		if !ok {
			continue
		}
		if p.isInCooldownLocked(id, now) {
			continue
		}
		return account, true
	}
	return Account{}, false
}

func (p *AccountPool) MarkUsed(accountID string) {
	p.mu.Lock()
	defer p.mu.Unlock()

	if _, ok := p.accounts[accountID]; !ok {
		return
	}
	stats := p.ensureStatsLocked(accountID)
	stats.LastUsed = time.Now()
	stats.ErrorCount = 0
	stats.CooldownUntil = time.Time{}
	stats.DisabledUntil = time.Time{}
	stats.DisabledReason = ""
	stats.FailureCounts = nil
}

func (p *AccountPool) MarkFailure(accountID string, reason FailureReason) {
	p.mu.Lock()
	defer p.mu.Unlock()

	if _, ok := p.accounts[accountID]; !ok {
		return
	}
	if p.isCooldownBypassedLocked(accountID) {
		return
	}
	if reason == "" {
		reason = FailureUnknown
	}

	now := time.Now()
	stats := p.ensureStatsLocked(accountID)
	windowExpired := !stats.LastFailureAt.IsZero() && now.Sub(stats.LastFailureAt) > p.cooldown.FailureWindow

	baseErrorCount := stats.ErrorCount
	if windowExpired {
		baseErrorCount = 0
		stats.FailureCounts = nil
	}
	if stats.FailureCounts == nil {
		stats.FailureCounts = map[FailureReason]int{}
	}

	nextErrorCount := baseErrorCount + 1
	stats.ErrorCount = nextErrorCount
	stats.LastFailureAt = now
	stats.FailureCounts[reason] = stats.FailureCounts[reason] + 1

	switch reason {
	case FailureBilling, FailureAuthPermanent:
		count := stats.FailureCounts[reason]
		disableFor := calculateBillingDisableDuration(count, p.cooldown)
		stats.DisabledUntil = keepActiveWindowOrRecompute(stats.DisabledUntil, now, now.Add(disableFor))
		stats.DisabledReason = reason
	default:
		cooldownFor := calculateAuthCooldown(nextErrorCount)
		stats.CooldownUntil = keepActiveWindowOrRecompute(stats.CooldownUntil, now, now.Add(cooldownFor))
	}
}

func (p *AccountPool) Snapshot() []AccountSnapshot {
	p.mu.Lock()
	defer p.mu.Unlock()

	now := time.Now()
	p.clearExpiredCooldownsLocked(now)
	ordered := p.resolveOrderLocked("", now)
	seen := map[string]struct{}{}
	snapshots := make([]AccountSnapshot, 0, len(p.accounts))

	appendOne := func(id string) {
		if id == "" {
			return
		}
		if _, ok := seen[id]; ok {
			return
		}
		account, ok := p.accounts[id]
		if !ok {
			return
		}
		account.Token = ""
		seen[id] = struct{}{}
		var usage *ProfileUsageStats
		if stats, ok := p.usage[id]; ok {
			cloned := *stats
			if stats.FailureCounts != nil {
				cloned.FailureCounts = make(map[FailureReason]int, len(stats.FailureCounts))
				for k, v := range stats.FailureCounts {
					cloned.FailureCounts[k] = v
				}
			}
			usage = &cloned
		}
		snapshots = append(snapshots, AccountSnapshot{Account: account, Usage: usage})
	}

	for _, id := range ordered {
		appendOne(id)
	}
	for id := range p.accounts {
		appendOne(id)
	}

	return snapshots
}

func (p *AccountPool) GetAccount(accountID string) (Account, bool) {
	p.mu.Lock()
	defer p.mu.Unlock()
	account, ok := p.accounts[accountID]
	return account, ok
}

func (p *AccountPool) SetCredential(profileID string, account Account) {
	p.mu.Lock()
	defer p.mu.Unlock()

	profileID = strings.TrimSpace(profileID)
	if profileID == "" {
		return
	}
	account.ID = profileID
	if strings.TrimSpace(account.Provider) == "" {
		account.Provider = p.provider
	}
	p.accounts[profileID] = account
}

func (p *AccountPool) SetPreferred(accountID string) bool {
	p.mu.Lock()
	defer p.mu.Unlock()

	if _, ok := p.accounts[accountID]; !ok {
		return false
	}
	// OpenClaw-style selection keeps preferred as a caller input (Acquire(preferredID))
	// and does not mutate explicit order storage.
	return true
}

func (p *AccountPool) SoonestAvailableAt() time.Time {
	p.mu.Lock()
	defer p.mu.Unlock()

	now := time.Now()
	p.clearExpiredCooldownsLocked(now)
	var soonest time.Time
	for id := range p.accounts {
		until := p.unusableUntilLocked(id)
		if until.IsZero() || !now.Before(until) {
			return time.Time{}
		}
		if soonest.IsZero() || until.Before(soonest) {
			soonest = until
		}
	}
	return soonest
}

func (p *AccountPool) ResolveUnavailableReason() FailureReason {
	p.mu.Lock()
	defer p.mu.Unlock()

	now := time.Now()
	scores := map[FailureReason]int{}
	ids := p.resolveOrderLocked("", now)
	for _, id := range ids {
		stats := p.usage[id]
		if stats == nil {
			continue
		}
		disabledActive := !stats.DisabledUntil.IsZero() && now.Before(stats.DisabledUntil)
		if disabledActive && stats.DisabledReason != "" {
			scores[stats.DisabledReason] += 1000
			continue
		}
		cooldownActive := !stats.CooldownUntil.IsZero() && now.Before(stats.CooldownUntil)
		if !cooldownActive {
			continue
		}
		if len(stats.FailureCounts) == 0 {
			scores[FailureRateLimit]++
			continue
		}
		for reason, count := range stats.FailureCounts {
			if count > 0 {
				scores[reason] += count
			}
		}
	}

	order := []FailureReason{
		FailureAuthPermanent,
		FailureAuth,
		FailureBilling,
		FailureFormat,
		FailureModelNotFound,
		FailureTimeout,
		FailureRateLimit,
		FailureUnknown,
	}
	best := FailureUnknown
	bestScore := 0
	for _, reason := range order {
		score := scores[reason]
		if score > bestScore {
			best = reason
			bestScore = score
		}
	}
	if bestScore == 0 {
		return FailureUnknown
	}
	return best
}

func (p *AccountPool) resolveOrderLocked(preferredID string, now time.Time) []string {
	base := p.baseOrderLocked()
	if len(base) == 0 {
		return nil
	}

	// If user specified explicit order (store override or config), respect it
	// exactly, but still apply cooldown sorting to avoid repeatedly selecting
	// known-bad/rate-limited accounts as the first candidate.
	if len(p.order) > 0 {
		available := make([]string, 0, len(base))
		type cooldownEntry struct {
			id    string
			until time.Time
		}
		inCooldown := make([]cooldownEntry, 0, len(base))
		for _, id := range base {
			if p.isInCooldownLocked(id, now) {
				inCooldown = append(inCooldown, cooldownEntry{id: id, until: p.unusableUntilLocked(id)})
			} else {
				available = append(available, id)
			}
		}
		sort.SliceStable(inCooldown, func(i, j int) bool {
			a, b := inCooldown[i].until, inCooldown[j].until
			if a.IsZero() {
				return false
			}
			if b.IsZero() {
				return true
			}
			return a.Before(b)
		})
		cooldownIDs := make([]string, len(inCooldown))
		for i, e := range inCooldown {
			cooldownIDs[i] = e.id
		}
		ordered := append(append([]string{}, available...), cooldownIDs...)
		return p.putPreferredFirst(ordered, preferredID)
	}

	// Otherwise, use round-robin: sort by type preference, then by lastUsed
	// (oldest first for round-robin within type).
	// preferredProfile goes first if specified (for explicit user choice).
	ordered := p.orderProfilesByModeLocked(base, now)
	return p.putPreferredFirst(ordered, preferredID)
}

// orderProfilesByModeLocked mirrors OpenClaw's orderProfilesByMode:
//   - Partition accounts into available and in-cooldown.
//   - Sort available by type (OAuth > Token > APIKey), then by lastUsed (oldest first = round-robin).
//   - Append in-cooldown accounts sorted by soonest-available.
func (p *AccountPool) orderProfilesByModeLocked(ids []string, now time.Time) []string {
	type scoredEntry struct {
		id        string
		typeScore int
		lastUsed  time.Time
	}
	available := make([]scoredEntry, 0, len(ids))
	type cooldownEntry struct {
		id    string
		until time.Time
	}
	inCooldown := make([]cooldownEntry, 0, len(ids))

	for _, id := range ids {
		if p.isInCooldownLocked(id, now) {
			inCooldown = append(inCooldown, cooldownEntry{id: id, until: p.unusableUntilLocked(id)})
		} else {
			account := p.accounts[id]
			available = append(available, scoredEntry{
				id:        id,
				typeScore: accountTypeScore(account.Type),
				lastUsed:  p.lastUsedOrZeroLocked(id),
			})
		}
	}

	// Primary sort: type preference (oauth > token > api_key).
	// Secondary sort: lastUsed (oldest first for round-robin within type).
	sort.SliceStable(available, func(i, j int) bool {
		if available[i].typeScore != available[j].typeScore {
			return available[i].typeScore < available[j].typeScore
		}
		return available[i].lastUsed.Before(available[j].lastUsed)
	})

	// Append cooldown accounts at the end, sorted by soonest available.
	sort.SliceStable(inCooldown, func(i, j int) bool {
		a, b := inCooldown[i].until, inCooldown[j].until
		if a.IsZero() {
			return false
		}
		if b.IsZero() {
			return true
		}
		return a.Before(b)
	})

	out := make([]string, 0, len(ids))
	for _, e := range available {
		out = append(out, e.id)
	}
	for _, e := range inCooldown {
		out = append(out, e.id)
	}
	return out
}

// putPreferredFirst moves the preferredID to the front of the ordered slice
// if it is present, without otherwise disturbing the order.
func (p *AccountPool) putPreferredFirst(ordered []string, preferredID string) []string {
	if preferredID == "" || !contains(ordered, preferredID) {
		return ordered
	}
	out := make([]string, 0, len(ordered))
	out = append(out, preferredID)
	for _, id := range ordered {
		if id != preferredID {
			out = append(out, id)
		}
	}
	return out
}

func (p *AccountPool) baseOrderLocked() []string {
	if len(p.order) > 0 {
		ids := make([]string, 0, len(p.order))
		for _, id := range p.order {
			if _, ok := p.accounts[id]; ok {
				ids = append(ids, id)
			}
		}
		return dedupe(ids)
	}
	ids := make([]string, 0, len(p.accounts))
	for id := range p.accounts {
		ids = append(ids, id)
	}
	sort.Strings(ids)
	return ids
}

func (p *AccountPool) clearExpiredCooldownsLocked(now time.Time) {
	for _, stats := range p.usage {
		if stats == nil {
			continue
		}
		changed := false
		if !stats.CooldownUntil.IsZero() && !now.Before(stats.CooldownUntil) {
			stats.CooldownUntil = time.Time{}
			changed = true
		}
		if !stats.DisabledUntil.IsZero() && !now.Before(stats.DisabledUntil) {
			stats.DisabledUntil = time.Time{}
			stats.DisabledReason = ""
			changed = true
		}
		if changed && stats.CooldownUntil.IsZero() && stats.DisabledUntil.IsZero() {
			stats.ErrorCount = 0
			stats.FailureCounts = nil
		}
	}
}

func (p *AccountPool) isInCooldownLocked(accountID string, now time.Time) bool {
	if p.isCooldownBypassedLocked(accountID) {
		return false
	}
	stats := p.usage[accountID]
	if stats == nil {
		return false
	}
	until := p.unusableUntilLocked(accountID)
	if until.IsZero() {
		return false
	}
	return now.Before(until)
}

func (p *AccountPool) isCooldownBypassedLocked(accountID string) bool {
	account, ok := p.accounts[accountID]
	if !ok {
		return false
	}
	return isOpenRouterProvider(account.Provider) || isOpenRouterProvider(p.provider)
}

func (p *AccountPool) unusableUntilLocked(accountID string) time.Time {
	stats := p.usage[accountID]
	if stats == nil {
		return time.Time{}
	}
	if stats.CooldownUntil.After(stats.DisabledUntil) {
		return stats.CooldownUntil
	}
	return stats.DisabledUntil
}

func (p *AccountPool) lastUsedOrZeroLocked(accountID string) time.Time {
	stats := p.usage[accountID]
	if stats == nil {
		return time.Time{}
	}
	return stats.LastUsed
}

func (p *AccountPool) ensureStatsLocked(accountID string) *ProfileUsageStats {
	stats, ok := p.usage[accountID]
	if ok {
		return stats
	}
	stats = &ProfileUsageStats{}
	p.usage[accountID] = stats
	return stats
}

func calculateAuthCooldown(errorCount int) time.Duration {
	n := errorCount
	if n < 1 {
		n = 1
	}
	steps := n - 1
	if steps > 3 {
		steps = 3
	}
	base := time.Minute
	scale := 1
	for i := 0; i < steps; i++ {
		scale *= 5
	}
	duration := time.Duration(scale) * base
	max := time.Hour
	if duration > max {
		return max
	}
	return duration
}

func calculateBillingDisableDuration(errorCount int, cfg CooldownConfig) time.Duration {
	n := errorCount
	if n < 1 {
		n = 1
	}
	base := cfg.BillingBackoff
	if base < time.Minute {
		base = time.Minute
	}
	max := cfg.BillingMax
	if max < base {
		max = base
	}
	exp := n - 1
	if exp > 10 {
		exp = 10
	}
	factor := 1 << exp
	d := time.Duration(factor) * base
	if d > max {
		return max
	}
	return d
}

func keepActiveWindowOrRecompute(existingUntil, now, recomputed time.Time) time.Time {
	if !existingUntil.IsZero() && now.Before(existingUntil) {
		return existingUntil
	}
	return recomputed
}

func accountTypeScore(accountType AccountType) int {
	switch accountType {
	case AccountOAuth:
		return 0
	case AccountToken:
		return 1
	case AccountAPIKey:
		return 2
	default:
		return 3
	}
}

func isOpenRouterProvider(provider string) bool {
	normalized := strings.ToLower(strings.TrimSpace(provider))
	if normalized == "" {
		return false
	}
	normalized = strings.ReplaceAll(normalized, "_", "")
	normalized = strings.ReplaceAll(normalized, "-", "")
	return normalized == "openrouter"
}

func contains(items []string, target string) bool {
	for _, item := range items {
		if item == target {
			return true
		}
	}
	return false
}

func dedupe(items []string) []string {
	seen := map[string]struct{}{}
	result := make([]string, 0, len(items))
	for _, item := range items {
		if item == "" {
			continue
		}
		if _, ok := seen[item]; ok {
			continue
		}
		seen[item] = struct{}{}
		result = append(result, item)
	}
	return result
}
