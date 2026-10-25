package provider

import (
	"net/http"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/ronishpaudel/dada-router/internal/config"
)

func TestValidateEndpoint(t *testing.T) {
	// Official OpenAI host
	err := ValidateEndpoint(config.ProviderOpenAI, "https://api.openai.com/v1/chat/completions")
	assert.NoError(t, err)

	// Official Gemini host
	err = ValidateEndpoint(config.ProviderGemini, "https://generativelanguage.googleapis.com/v1beta/models/gemini-1.5-flash")
	assert.NoError(t, err)

	// Custom endpoint without env var should fail
	t.Setenv("DADA_ROUTER_ALLOW_CUSTOM_ENDPOINT", "0")
	err = ValidateEndpoint(config.ProviderOpenAI, "https://my-proxy.com/v1/chat/completions")
	assert.ErrorIs(t, err, ErrForbiddenEndpoint)

	// Custom endpoint with env var allowed
	t.Setenv("DADA_ROUTER_ALLOW_CUSTOM_ENDPOINT", "1")
	err = ValidateEndpoint(config.ProviderOpenAI, "https://my-proxy.com/v1/chat/completions")
	assert.NoError(t, err)
}

func TestMapHTTPError(t *testing.T) {
	header := make(http.Header)
	header.Set("Retry-After", "30")

	// 401 Auth error
	err := MapHTTPError(401, "invalid api key sk-1234567890abcdef1234567890", header, "openai", "gpt-4o")
	require.ErrorIs(t, err, ErrAuth)
	assert.NotContains(t, err.Error(), "sk-1234567890")

	// 404 Model not found
	err = MapHTTPError(404, "model not found", header, "anthropic", "unknown-model")
	require.ErrorIs(t, err, ErrModelNotFound)
	assert.Contains(t, err.Error(), "dada doctor")

	// 400 Context too large
	err = MapHTTPError(400, "This model's maximum context length is 8192 tokens", header, "openai", "gpt-4o")
	require.ErrorIs(t, err, ErrContextTooLarge)

	// 429 Rate limited
	err = MapHTTPError(429, "rate limit exceeded", header, "openai", "gpt-4o")
	require.ErrorIs(t, err, ErrRateLimited)
	var provErr *ProviderError
	require.ErrorAs(t, err, &provErr)
	assert.Equal(t, 30*time.Second, provErr.RetryAfter)

	// 503 Server error
	err = MapHTTPError(503, "service unavailable", header, "deepseek", "deepseek-chat")
	require.ErrorIs(t, err, ErrServer)
}

func TestRegistry(t *testing.T) {
	settings := config.NewDefaultSettings()
	reg := NewRegistry(settings)

	assert.Equal(t, 5, len(reg.List()))

	p, err := reg.Get(config.ProviderGemini)
	require.NoError(t, err)
	assert.Equal(t, config.ProviderGemini, p.Name())

	_, err = reg.Get("nonexistent")
	assert.Error(t, err)
}
