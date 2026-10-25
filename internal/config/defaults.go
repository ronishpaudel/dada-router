package config

import "time"

// Default timeouts
const (
	DefaultConnectTimeout   = 5 * time.Second
	DefaultFirstByteTimeout = 30 * time.Second
	DefaultTotalTimeout     = 120 * time.Second

	// Default daily spend guard token limit (0 = unlimited)
	DefaultDailyTokenLimit = 500000

	// Default route name
	DefaultRouteName = "dada"

	// Keyring service name
	KeyringService = "dada-router"
)

// Supported providers
const (
	ProviderOpenAI    = "openai"
	ProviderAnthropic = "anthropic"
	ProviderGemini    = "gemini"
	ProviderXAI       = "xai"
	ProviderDeepSeek  = "deepseek"
)

// AllSupportedProviders returns the canonical list of supported providers.
func AllSupportedProviders() []string {
	return []string{
		ProviderOpenAI,
		ProviderAnthropic,
		ProviderGemini,
		ProviderXAI,
		ProviderDeepSeek,
	}
}

// ProviderDefaults holds default settings for a provider
type ProviderDefaults struct {
	DefaultModel    string `json:"default_model"`
	RequestsPerMin  int    `json:"requests_per_min"`
	TokensPerMin    int    `json:"tokens_per_min"`
	DailyTokenLimit int    `json:"daily_token_limit"`
}

// GetProviderDefaults returns baseline defaults when settings.json does not specify them.
func GetProviderDefaults() map[string]ProviderDefaults {
	return map[string]ProviderDefaults{
		ProviderOpenAI: {
			DefaultModel:    "gpt-4o",
			RequestsPerMin:  500,
			TokensPerMin:    30000,
			DailyTokenLimit: 250000,
		},
		ProviderAnthropic: {
			DefaultModel:    "claude-3-5-sonnet-20241022",
			RequestsPerMin:  50,
			TokensPerMin:    40000,
			DailyTokenLimit: 250000,
		},
		ProviderGemini: {
			DefaultModel:    "gemini-1.5-flash",
			RequestsPerMin:  15,
			TokensPerMin:    1000000,
			DailyTokenLimit: 500000,
		},
		ProviderXAI: {
			DefaultModel:    "grok-beta",
			RequestsPerMin:  60,
			TokensPerMin:    100000,
			DailyTokenLimit: 200000,
		},
		ProviderDeepSeek: {
			DefaultModel:    "deepseek-chat",
			RequestsPerMin:  60,
			TokensPerMin:    100000,
			DailyTokenLimit: 200000,
		},
	}
}
