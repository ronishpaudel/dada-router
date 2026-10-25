package quota

import (
	"errors"
	"fmt"
	"sync"
	"time"

	"github.com/yourname/dada-router/internal/config"
)

var (
	ErrSpendGuardExceeded = errors.New("daily spend guard ceiling reached: request refused before dialing network")
	ErrQuotaExceeded      = errors.New("provider quota exceeded for current window")
)

// ProviderUsage records windowed usage metrics for a provider
type ProviderUsage struct {
	RequestsInWindow int
	TokensInWindow   int
	WindowStart      time.Time
	RateLimitReset   time.Time
	LimitVerifiedOn  string
}

// Tracker manages quota tracking, rate limit resets, and daily spend guard limits
type Tracker struct {
	mu              sync.Mutex
	providers       map[string]*ProviderUsage
	dailyTokensUsed int
	dayStartLocal   time.Time
	dailyTokenLimit int
	settings        *config.Settings
}

// NewTracker creates a new Quota Tracker
func NewTracker(settings *config.Settings) *Tracker {
	dailyLimit := config.DefaultDailyTokenLimit
	if settings != nil && settings.SpendGuard.DailyTokenLimit > 0 {
		dailyLimit = settings.SpendGuard.DailyTokenLimit
	}

	now := time.Now()
	midnight := time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, now.Location())

	tracker := &Tracker{
		providers:       make(map[string]*ProviderUsage),
		dailyTokensUsed: 0,
		dayStartLocal:   midnight,
		dailyTokenLimit: dailyLimit,
		settings:        settings,
	}

	for _, p := range config.AllSupportedProviders() {
		verifiedOn := "2026-01-01"
		if settings != nil {
			if pc, ok := settings.Providers[p]; ok && pc.LimitVerifiedOn != "" {
				verifiedOn = pc.LimitVerifiedOn
			}
		}
		tracker.providers[p] = &ProviderUsage{
			WindowStart:     now,
			LimitVerifiedOn: verifiedOn,
		}
	}

	return tracker
}

// checkMidnightReset resets the daily spend guard counter if local midnight has passed
func (t *Tracker) checkMidnightReset() {
	now := time.Now()
	midnight := time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, now.Location())
	if now.After(t.dayStartLocal.Add(24 * time.Hour)) || midnight.After(t.dayStartLocal) {
		t.dailyTokensUsed = 0
		t.dayStartLocal = midnight
	}
}

// CheckSpendGuard verifies that the daily token ceiling has not been reached
func (t *Tracker) CheckSpendGuard(estimatedTokens int) error {
	t.mu.Lock()
	defer t.mu.Unlock()

	t.checkMidnightReset()

	if t.dailyTokenLimit > 0 && (t.dailyTokensUsed+estimatedTokens) > t.dailyTokenLimit {
		return fmt.Errorf("%w: used %d / %d tokens today", ErrSpendGuardExceeded, t.dailyTokensUsed, t.dailyTokenLimit)
	}

	return nil
}

// CanCallProvider checks whether a provider is currently available (not rate-limited)
func (t *Tracker) CanCallProvider(providerName string) bool {
	t.mu.Lock()
	defer t.mu.Unlock()

	usage, ok := t.providers[providerName]
	if !ok {
		return true
	}

	now := time.Now()
	if !usage.RateLimitReset.IsZero() && now.Before(usage.RateLimitReset) {
		return false
	}

	return true
}

// RecordRateLimit updates the provider's reset time from a 429 Retry-After header
func (t *Tracker) RecordRateLimit(providerName string, retryAfter time.Duration) {
	t.mu.Lock()
	defer t.mu.Unlock()

	usage, ok := t.providers[providerName]
	if !ok {
		usage = &ProviderUsage{WindowStart: time.Now(), LimitVerifiedOn: "2026-01-01"}
		t.providers[providerName] = usage
	}

	if retryAfter <= 0 {
		retryAfter = 60 * time.Second
	}

	usage.RateLimitReset = time.Now().Add(retryAfter)
}

// RecordUsage logs tokens and requests consumed by a successful call
func (t *Tracker) RecordUsage(providerName string, tokens int) {
	t.mu.Lock()
	defer t.mu.Unlock()

	t.checkMidnightReset()
	t.dailyTokensUsed += tokens

	usage, ok := t.providers[providerName]
	if !ok {
		usage = &ProviderUsage{WindowStart: time.Now(), LimitVerifiedOn: "2026-01-01"}
		t.providers[providerName] = usage
	}

	now := time.Now()
	if now.Sub(usage.WindowStart) > time.Minute {
		usage.WindowStart = now
		usage.RequestsInWindow = 0
		usage.TokensInWindow = 0
	}

	usage.RequestsInWindow++
	usage.TokensInWindow += tokens
}

// ProviderReport holds snapshot metrics for rendering
type ProviderReport struct {
	Provider        string
	RequestsUsed    int
	RequestsLimit   int
	TokensUsed      int
	TokensLimit     int
	LimitVerifiedOn string
	ResetTime       time.Time
	IsRateLimited   bool
}

// Report generates usage reports for all providers
func (t *Tracker) Report() []ProviderReport {
	t.mu.Lock()
	defer t.mu.Unlock()

	t.checkMidnightReset()
	now := time.Now()

	defaults := config.GetProviderDefaults()
	var reports []ProviderReport

	for _, p := range config.AllSupportedProviders() {
		usage, ok := t.providers[p]
		if !ok {
			usage = &ProviderUsage{WindowStart: now, LimitVerifiedOn: "2026-01-01"}
		}

		reqLimit := defaults[p].RequestsPerMin
		tokenLimit := defaults[p].TokensPerMin
		verifiedOn := usage.LimitVerifiedOn

		if t.settings != nil {
			if pc, exists := t.settings.Providers[p]; exists {
				if pc.RequestsPerMin > 0 {
					reqLimit = pc.RequestsPerMin
				}
				if pc.TokensPerMin > 0 {
					tokenLimit = pc.TokensPerMin
				}
				if pc.LimitVerifiedOn != "" {
					verifiedOn = pc.LimitVerifiedOn
				}
			}
		}

		isLimited := !usage.RateLimitReset.IsZero() && now.Before(usage.RateLimitReset)
		reset := usage.RateLimitReset
		if reset.IsZero() {
			reset = usage.WindowStart.Add(time.Minute)
		}

		reports = append(reports, ProviderReport{
			Provider:        p,
			RequestsUsed:    usage.RequestsInWindow,
			RequestsLimit:   reqLimit,
			TokensUsed:      usage.TokensInWindow,
			TokensLimit:     tokenLimit,
			LimitVerifiedOn: verifiedOn,
			ResetTime:       reset,
			IsRateLimited:   isLimited,
		})
	}

	return reports
}

// GetDailySpendStatus returns daily spend guard status
func (t *Tracker) GetDailySpendStatus() (used int, limit int) {
	t.mu.Lock()
	defer t.mu.Unlock()
	t.checkMidnightReset()
	return t.dailyTokensUsed, t.dailyTokenLimit
}
