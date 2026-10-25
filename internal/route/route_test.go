package route

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/ronishpaudel/dada-router/internal/config"
)

func TestRouteAllowedProviders(t *testing.T) {
	r := Route{
		Name:             "test-route",
		OrderedProviders: []string{"gemini", "openai", "anthropic"},
		AllowedProviders: []string{"gemini", "anthropic"},
	}

	assert.True(t, r.IsProviderAllowed("gemini"))
	assert.True(t, r.IsProviderAllowed("anthropic"))
	assert.False(t, r.IsProviderAllowed("openai"))
}

func TestRoutePreferenceRules(t *testing.T) {
	r := Route{
		Name: "test-route",
		OrderedProviders: []string{
			config.ProviderOpenAI,
			config.ProviderGemini,
			config.ProviderAnthropic,
		},
		AllowedProviders: []string{
			config.ProviderOpenAI,
			config.ProviderGemini,
			config.ProviderAnthropic,
		},
	}

	// Short simple prompt -> Gemini preferred
	shortOrder := r.SelectCandidates("hi there")
	assert.Equal(t, config.ProviderGemini, shortOrder[0])

	// Code prompt -> Anthropic or OpenAI preferred
	codeOrder := r.SelectCandidates("```go\nfunc main() {}\n```")
	assert.Equal(t, config.ProviderAnthropic, codeOrder[0])
}

func TestNextCandidate(t *testing.T) {
	r := Route{
		Name:             "test-route",
		OrderedProviders: []string{"gemini", "openai"},
		AllowedProviders: []string{"gemini", "openai"},
		ModelPerProvider: map[string]string{
			"gemini": "gemini-1.5-flash",
			"openai": "gpt-4o",
		},
	}

	unavailable := make(map[string]bool)

	// First candidate
	p, m, err := r.NextCandidate("normal prompt", unavailable)
	require.NoError(t, err)
	assert.Equal(t, "gemini", p)
	assert.Equal(t, "gemini-1.5-flash", m)

	// When gemini is down
	unavailable["gemini"] = true
	p, m, err = r.NextCandidate("normal prompt", unavailable)
	require.NoError(t, err)
	assert.Equal(t, "openai", p)
	assert.Equal(t, "gpt-4o", m)

	// When both are down
	unavailable["openai"] = true
	_, _, err = r.NextCandidate("normal prompt", unavailable)
	require.ErrorIs(t, err, ErrNoAvailableProvider)
}
