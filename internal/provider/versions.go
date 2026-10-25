package provider

import (
	"time"

	"github.com/yourname/dada-router/internal/config"
)

// APIVersionInfo tracks the pinned API version, date verified, and changelog
type APIVersionInfo struct {
	Provider     string    `json:"provider"`
	Version      string    `json:"version"`
	HeaderName   string    `json:"header_name,omitempty"`
	HeaderValue  string    `json:"header_value,omitempty"`
	VerifiedOn   time.Time `json:"verified_on"`
	ChangelogURL string    `json:"changelog_url"`
	KnownModels  []string  `json:"known_models"`
}

// PinnedAPIVersions maps each provider to its pinned API version information
var PinnedAPIVersions = map[string]APIVersionInfo{
	config.ProviderOpenAI: {
		Provider:     config.ProviderOpenAI,
		Version:      "v1",
		VerifiedOn:   time.Date(2026, 1, 15, 0, 0, 0, 0, time.UTC),
		ChangelogURL: "https://platform.openai.com/docs/changelog",
		KnownModels:  []string{"gpt-4o", "gpt-4o-mini", "o1", "o3-mini"},
	},
	config.ProviderAnthropic: {
		Provider:     config.ProviderAnthropic,
		Version:      "2023-06-01",
		HeaderName:   "anthropic-version",
		HeaderValue:  "2023-06-01",
		VerifiedOn:   time.Date(2026, 1, 15, 0, 0, 0, 0, time.UTC),
		ChangelogURL: "https://docs.anthropic.com/en/release-notes/api",
		KnownModels:  []string{"claude-3-5-sonnet-20241022", "claude-3-5-haiku-20241022", "claude-3-opus-20240229"},
	},
	config.ProviderGemini: {
		Provider:     config.ProviderGemini,
		Version:      "v1beta",
		VerifiedOn:   time.Date(2026, 1, 15, 0, 0, 0, 0, time.UTC),
		ChangelogURL: "https://ai.google.dev/gemini-api/docs/changelog",
		KnownModels:  []string{"gemini-1.5-flash", "gemini-1.5-pro", "gemini-2.0-flash-exp"},
	},
	config.ProviderXAI: {
		Provider:     config.ProviderXAI,
		Version:      "v1",
		VerifiedOn:   time.Date(2026, 1, 15, 0, 0, 0, 0, time.UTC),
		ChangelogURL: "https://docs.x.ai/docs/overview",
		KnownModels:  []string{"grok-beta", "grok-2-1212"},
	},
	config.ProviderDeepSeek: {
		Provider:     config.ProviderDeepSeek,
		Version:      "v1",
		VerifiedOn:   time.Date(2026, 1, 15, 0, 0, 0, 0, time.UTC),
		ChangelogURL: "https://api-docs.deepseek.com/news/news241226",
		KnownModels:  []string{"deepseek-chat", "deepseek-reasoner"},
	},
}

// IsOlderThanDays checks if a date verified is older than the specified days
func IsOlderThanDays(t time.Time, days int) bool {
	return time.Since(t) > time.Duration(days)*24*time.Hour
}
