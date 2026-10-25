package provider

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"

	"github.com/yourname/dada-router/internal/config"
)

// AnthropicProvider implements the Provider interface for Anthropic
type AnthropicProvider struct {
	endpoint     string
	defaultModel string
	httpClient   *http.Client
}

// NewAnthropicProvider creates an Anthropic provider adapter
func NewAnthropicProvider(defaultModel string) *AnthropicProvider {
	return &AnthropicProvider{
		endpoint:     "https://api.anthropic.com/v1/messages",
		defaultModel: defaultModel,
		httpClient: &http.Client{
			Timeout: config.DefaultTotalTimeout,
		},
	}
}

func (p *AnthropicProvider) Name() string {
	return config.ProviderAnthropic
}

func (p *AnthropicProvider) DefaultModel() string {
	return p.defaultModel
}

type anthropicMessage struct {
	Role    string `json:"role"`
	Content string `json:"content"`
}

type anthropicRequest struct {
	Model     string             `json:"model"`
	System    string             `json:"system,omitempty"`
	Messages  []anthropicMessage `json:"messages"`
	MaxTokens int                `json:"max_tokens"`
	Stream    bool               `json:"stream"`
}

type anthropicDeltaEvent struct {
	Type  string `json:"type"`
	Delta struct {
		Type string `json:"type"`
		Text string `json:"text"`
	} `json:"delta"`
	Usage *struct {
		OutputTokens int `json:"output_tokens"`
	} `json:"usage"`
}

type anthropicMessageStartEvent struct {
	Type    string `json:"type"`
	Message struct {
		Usage struct {
			InputTokens int `json:"input_tokens"`
		} `json:"usage"`
	} `json:"message"`
}

// ValidateKey validates the Anthropic API key with a 1-token request
func (p *AnthropicProvider) ValidateKey(ctx context.Context, apiKey string) error {
	if err := ValidateEndpoint(config.ProviderAnthropic, p.endpoint); err != nil {
		return err
	}

	reqBody := anthropicRequest{
		Model: p.defaultModel,
		Messages: []anthropicMessage{
			{Role: "user", Content: "hi"},
		},
		MaxTokens: 1,
		Stream:    false,
	}

	data, err := json.Marshal(reqBody)
	if err != nil {
		return fmt.Errorf("failed to marshal validation request: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, p.endpoint, bytes.NewReader(data))
	if err != nil {
		return fmt.Errorf("failed to create validation request: %w", err)
	}

	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("x-api-key", apiKey)
	req.Header.Set("anthropic-version", PinnedAPIVersions[config.ProviderAnthropic].Version)

	resp, err := p.httpClient.Do(req)
	if err != nil {
		return fmt.Errorf("network error during key validation: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		respBody, _ := io.ReadAll(io.LimitReader(resp.Body, 512))
		return MapHTTPError(resp.StatusCode, string(respBody), resp.Header, config.ProviderAnthropic, p.defaultModel)
	}

	return nil
}

// Stream streams tokens from Anthropic API
func (p *AnthropicProvider) Stream(ctx context.Context, apiKey string, req StreamRequest) (<-chan StreamChunk, error) {
	if err := ValidateEndpoint(config.ProviderAnthropic, p.endpoint); err != nil {
		return nil, err
	}

	model := req.Model
	if model == "" {
		model = p.defaultModel
	}

	maxTokens := req.MaxTokens
	if maxTokens <= 0 {
		maxTokens = 2048
	}

	var systemPrompt string
	var messages []anthropicMessage

	for _, m := range req.Messages {
		if strings.EqualFold(m.Role, "system") {
			if systemPrompt != "" {
				systemPrompt += "\n"
			}
			systemPrompt += m.Content
		} else {
			role := m.Role
			if role != "user" && role != "assistant" {
				role = "user"
			}
			messages = append(messages, anthropicMessage{
				Role:    role,
				Content: m.Content,
			})
		}
	}

	if len(messages) == 0 {
		messages = append(messages, anthropicMessage{
			Role:    "user",
			Content: "hello",
		})
	}

	reqBody := anthropicRequest{
		Model:     model,
		System:    systemPrompt,
		Messages:  messages,
		MaxTokens: maxTokens,
		Stream:    true,
	}

	data, err := json.Marshal(reqBody)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal request: %w", err)
	}

	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, p.endpoint, bytes.NewReader(data))
	if err != nil {
		return nil, fmt.Errorf("failed to create request: %w", err)
	}

	httpReq.Header.Set("Content-Type", "application/json")
	httpReq.Header.Set("x-api-key", apiKey)
	httpReq.Header.Set("anthropic-version", PinnedAPIVersions[config.ProviderAnthropic].Version)
	httpReq.Header.Set("Accept", "text/event-stream")

	resp, err := p.httpClient.Do(httpReq)
	if err != nil {
		return nil, fmt.Errorf("request failed: %w", err)
	}

	if resp.StatusCode != http.StatusOK {
		defer resp.Body.Close()
		respBody, _ := io.ReadAll(io.LimitReader(resp.Body, 512))
		return nil, MapHTTPError(resp.StatusCode, string(respBody), resp.Header, config.ProviderAnthropic, model)
	}

	ch := make(chan StreamChunk, 32)

	go func() {
		defer resp.Body.Close()
		defer close(ch)

		scanner := bufio.NewScanner(resp.Body)
		var emittedFirstToken bool
		var inputTokens, outputTokens int

		for scanner.Scan() {
			select {
			case <-ctx.Done():
				ch <- StreamChunk{
					Partial: emittedFirstToken,
					Err:     ctx.Err(),
				}
				return
			default:
			}

			line := scanner.Text()
			if !strings.HasPrefix(line, "data: ") {
				continue
			}

			payload := strings.TrimPrefix(line, "data: ")
			payload = strings.TrimSpace(payload)

			var startEvt anthropicMessageStartEvent
			if err := json.Unmarshal([]byte(payload), &startEvt); err == nil && startEvt.Type == "message_start" {
				inputTokens = startEvt.Message.Usage.InputTokens
			}

			var deltaEvt anthropicDeltaEvent
			if err := json.Unmarshal([]byte(payload), &deltaEvt); err != nil {
				continue
			}

			if deltaEvt.Usage != nil {
				outputTokens = deltaEvt.Usage.OutputTokens
				ch <- StreamChunk{
					Usage: &UsageMetrics{
						PromptTokens:     inputTokens,
						CompletionTokens: outputTokens,
						TotalTokens:      inputTokens + outputTokens,
					},
				}
			}

			if deltaEvt.Delta.Text != "" {
				emittedFirstToken = true
				ch <- StreamChunk{Content: deltaEvt.Delta.Text}
			}
		}

		if err := scanner.Err(); err != nil {
			ch <- StreamChunk{
				Partial: emittedFirstToken,
				Err:     err,
			}
		} else {
			ch <- StreamChunk{Done: true}
		}
	}()

	return ch, nil
}
