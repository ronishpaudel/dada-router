package provider

import (
	"fmt"

	"github.com/yourname/dada-router/internal/config"
)

// Registry manages instances of all available providers
type Registry struct {
	providers map[string]Provider
}

// NewRegistry instantiates the 5 supported providers
func NewRegistry(settings *config.Settings) *Registry {
	providers := make(map[string]Provider)

	getProviderModel := func(name string, fallback string) string {
		if settings != nil {
			if pc, ok := settings.Providers[name]; ok && pc.Model != "" {
				return pc.Model
			}
		}
		return fallback
	}

	providers[config.ProviderOpenAI] = NewOpenAICompatProvider(
		config.ProviderOpenAI,
		"https://api.openai.com/v1/chat/completions",
		getProviderModel(config.ProviderOpenAI, "gpt-4o"),
	)

	providers[config.ProviderAnthropic] = NewAnthropicProvider(
		getProviderModel(config.ProviderAnthropic, "claude-3-5-sonnet-20241022"),
	)

	providers[config.ProviderGemini] = NewGeminiProvider(
		getProviderModel(config.ProviderGemini, "gemini-1.5-flash"),
	)

	providers[config.ProviderXAI] = NewOpenAICompatProvider(
		config.ProviderXAI,
		"https://api.x.ai/v1/chat/completions",
		getProviderModel(config.ProviderXAI, "grok-beta"),
	)

	providers[config.ProviderDeepSeek] = NewOpenAICompatProvider(
		config.ProviderDeepSeek,
		"https://api.deepseek.com/chat/completions",
		getProviderModel(config.ProviderDeepSeek, "deepseek-chat"),
	)

	return &Registry{
		providers: providers,
	}
}

// Get retrieves a provider by name
func (r *Registry) Get(name string) (Provider, error) {
	p, ok := r.providers[name]
	if !ok {
		return nil, fmt.Errorf("unsupported provider %q", name)
	}
	return p, nil
}

// List returns all registered providers
func (r *Registry) List() map[string]Provider {
	return r.providers
}
