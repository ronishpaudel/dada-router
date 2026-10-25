package provider

import (
	"context"
	"errors"
	"fmt"
	"net/url"
	"os"
	"strings"

	"github.com/ronishpaudel/dada-router/internal/config"
)

var (
	ErrForbiddenEndpoint = errors.New("custom endpoint forbidden: set DADA_ROUTER_ALLOW_CUSTOM_ENDPOINT=1 to allow overrides")
)

// Official hardcoded hostnames
var officialHosts = map[string]string{
	config.ProviderOpenAI:    "api.openai.com",
	config.ProviderAnthropic: "api.anthropic.com",
	config.ProviderGemini:    "generativelanguage.googleapis.com",
	config.ProviderXAI:       "api.x.ai",
	config.ProviderDeepSeek:  "api.deepseek.com",
}

// ValidateEndpoint checks whether the URL hostname matches the official provider host
// or is permitted via DADA_ROUTER_ALLOW_CUSTOM_ENDPOINT=1
func ValidateEndpoint(providerName string, rawURL string) error {
	u, err := url.Parse(rawURL)
	if err != nil {
		return fmt.Errorf("invalid endpoint URL: %w", err)
	}

	expectedHost, ok := officialHosts[providerName]
	if !ok {
		return fmt.Errorf("unknown provider %q", providerName)
	}

	if strings.EqualFold(u.Hostname(), expectedHost) {
		return nil
	}

	// Custom endpoint detected
	if os.Getenv("DADA_ROUTER_ALLOW_CUSTOM_ENDPOINT") != "1" {
		return fmt.Errorf("%w: requested host %q, expected %q", ErrForbiddenEndpoint, u.Hostname(), expectedHost)
	}

	fmt.Fprintf(os.Stderr, "WARNING: using custom endpoint %s for provider %s\n", rawURL, providerName)
	return nil
}

// ChatMessage represents a single message turn
type ChatMessage struct {
	Role    string `json:"role"` // "system", "user", "assistant"
	Content string `json:"content"`
}

// StreamRequest encapsulates a chat completion request
type StreamRequest struct {
	Model       string        `json:"model"`
	Messages    []ChatMessage `json:"messages"`
	MaxTokens   int           `json:"max_tokens,omitempty"`
	Temperature float32       `json:"temperature,omitempty"`
}

// UsageMetrics reports token counts from a completion
type UsageMetrics struct {
	PromptTokens     int `json:"prompt_tokens"`
	CompletionTokens int `json:"completion_tokens"`
	TotalTokens      int `json:"total_tokens"`
}

// StreamChunk represents a chunk in a streamed response
type StreamChunk struct {
	Content string
	Usage   *UsageMetrics
	Done    bool
	Partial bool
	Err     error
}

// Provider represents a common interface for all supported LLM providers
type Provider interface {
	Name() string
	DefaultModel() string
	ValidateKey(ctx context.Context, apiKey string) error
	Stream(ctx context.Context, apiKey string, req StreamRequest) (<-chan StreamChunk, error)
}
