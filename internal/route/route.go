package route

import (
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/ronishpaudel/dada-router/internal/config"
)

var (
	ErrNoAvailableProvider = errors.New("all providers in route unavailable or exhausted")
	ErrProviderNotAllowed  = errors.New("provider not in allowed_providers for this route")
)

// Route encapsulates a multi-provider execution route
type Route struct {
	Name             string            `json:"name"`
	OrderedProviders []string          `json:"ordered_providers"`
	ModelPerProvider map[string]string `json:"model_per_provider"`
	AllowedProviders []string          `json:"allowed_providers"`
	FallbackPolicy   string            `json:"fallback_policy"`
	Timeout          time.Duration     `json:"timeout"`
	Cache            bool              `json:"cache"`
}

// IsProviderAllowed checks if a provider is permitted on this route
func (r *Route) IsProviderAllowed(providerName string) bool {
	for _, p := range r.AllowedProviders {
		if strings.EqualFold(p, providerName) {
			return true
		}
	}
	return false
}

// ResolveModel returns the configured model for a provider on this route
func (r *Route) ResolveModel(providerName string, defaultModel string) string {
	if m, ok := r.ModelPerProvider[providerName]; ok && m != "" {
		return m
	}
	return defaultModel
}

// SelectCandidates orders the providers on the route based on explicit preference rules:
// - Short prompts (< 60 chars) prioritize faster/cheaper models (e.g., gemini)
// - Code-heavy or long prompts (> 500 chars) prioritize high-reasoning models (e.g., anthropic, openai)
func (r *Route) SelectCandidates(prompt string) []string {
	var candidates []string

	// Filter down to allowed providers only
	for _, p := range r.OrderedProviders {
		if r.IsProviderAllowed(p) {
			candidates = append(candidates, p)
		}
	}

	if len(candidates) <= 1 {
		return candidates
	}

	hasCode := strings.Contains(prompt, "```") || strings.Contains(prompt, "def ") || strings.Contains(prompt, "func ")
	isLong := len(prompt) > 500
	isShort := len(prompt) < 60 && !hasCode

	reordered := make([]string, 0, len(candidates))

	if isShort {
		// Prefer fast/cheap provider (gemini) if present in route
		for _, p := range candidates {
			if p == config.ProviderGemini {
				reordered = append(reordered, p)
				break
			}
		}
	} else if hasCode || isLong {
		// Prefer strong reasoning models (anthropic, openai) if present in route
		for _, p := range []string{config.ProviderAnthropic, config.ProviderOpenAI} {
			for _, c := range candidates {
				if c == p {
					reordered = append(reordered, p)
					break
				}
			}
		}
	}

	// Append remaining candidates preserving defined route order
	for _, p := range candidates {
		found := false
		for _, r := range reordered {
			if r == p {
				found = true
				break
			}
		}
		if !found {
			reordered = append(reordered, p)
		}
	}

	return reordered
}

// NextCandidate finds the next runnable candidate provider skipping blocked/failed providers
func (r *Route) NextCandidate(prompt string, unavailable map[string]bool) (string, string, error) {
	candidates := r.SelectCandidates(prompt)

	for _, p := range candidates {
		if unavailable[p] {
			continue
		}
		model := r.ResolveModel(p, "")
		return p, model, nil
	}

	return "", "", fmt.Errorf("%w for route %q", ErrNoAvailableProvider, r.Name)
}

// FromConfig converts a config.RouteConfig into a Route
func FromConfig(name string, cfg config.RouteConfig) Route {
	timeout := config.DefaultTotalTimeout
	if cfg.TimeoutSeconds > 0 {
		timeout = time.Duration(cfg.TimeoutSeconds) * time.Second
	}

	return Route{
		Name:             name,
		OrderedProviders: cfg.OrderedProviders,
		ModelPerProvider: cfg.ModelPerProvider,
		AllowedProviders: cfg.AllowedProviders,
		FallbackPolicy:   cfg.FallbackPolicy,
		Timeout:          timeout,
		Cache:            cfg.Cache,
	}
}
