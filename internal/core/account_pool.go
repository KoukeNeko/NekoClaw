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
	case FailureBilling, FailureAuth, FailureAuthPermanent:
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
	if !contains(p.order, profileID) {
		p.order = append([]string{profileID}, p.order...)
	}
}

func (p *AccountPool) SetPreferred(accountID string) bool {
	p.mu.Lock()
	defer p.mu.Unlock()

	if _, ok := p.accounts[accountID]; !ok {
		return false
	}
	next := []string{accountID}
	for _, id := range p.order {
		if id == accountID {
			continue
		}
		if _, ok := p.accounts[id]; ok {
			next = append(next, id)
		}
	}
	for id := range p.accounts {
		if id == accountID || contains(next, id) {
			continue
		}
		next = append(next, id)
	}
	p.order = next
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

	available := make([]string, 0, len(base))
	inCooldown := make([]string, 0, len(base))
	for _, id := range base {
		if p.isInCooldownLocked(id, now) {
			inCooldown = append(inCooldown, id)
			continue
		}
		available = append(available, id)
	}

	if len(p.order) == 0 {
		sort.SliceStable(available, func(i, j int) bool {
			a := p.accounts[available[i]]
			b := p.accounts[available[j]]
			as := accountTypeScore(a.Type)
			bs := accountTypeScore(b.Type)
			if as != bs {
				return as < bs
			}
			return p.lastUsedOrZeroLocked(available[i]).Before(p.lastUsedOrZeroLocked(available[j]))
		})
	}

	sort.SliceStable(inCooldown, func(i, j int) bool {
		a := p.unusableUntilLocked(inCooldown[i])
		b := p.unusableUntilLocked(inCooldown[j])
		if a.IsZero() {
			return false
		}
		if b.IsZero() {
			return true
		}
		return a.Before(b)
	})

	ordered := append(append([]string{}, available...), inCooldown...)
	if preferredID == "" {
		return ordered
	}
	if !contains(ordered, preferredID) {
		return ordered
	}
	out := []string{preferredID}
	for _, id := range ordered {
		if id == preferredID {
			continue
		}
		out = append(out, id)
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
