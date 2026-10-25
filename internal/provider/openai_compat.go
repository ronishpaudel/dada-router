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

// OpenAICompatProvider implements Provider for OpenAI and OpenAI-compatible APIs (xAI, DeepSeek)
type OpenAICompatProvider struct {
	name         string
	endpoint     string
	defaultModel string
	httpClient   *http.Client
}

// NewOpenAICompatProvider creates an adapter for OpenAI-compatible APIs
func NewOpenAICompatProvider(name string, endpoint string, defaultModel string) *OpenAICompatProvider {
	return &OpenAICompatProvider{
		name:         name,
		endpoint:     endpoint,
		defaultModel: defaultModel,
		httpClient: &http.Client{
			Timeout: config.DefaultTotalTimeout,
		},
	}
}

func (p *OpenAICompatProvider) Name() string {
	return p.name
}

func (p *OpenAICompatProvider) DefaultModel() string {
	return p.defaultModel
}

type openAIMessage struct {
	Role    string `json:"role"`
	Content string `json:"content"`
}

type openAIStreamOptions struct {
	IncludeUsage bool `json:"include_usage"`
}

type openAIRequest struct {
	Model         string               `json:"model"`
	Messages      []openAIMessage      `json:"messages"`
	Stream        bool                 `json:"stream"`
	StreamOptions *openAIStreamOptions `json:"stream_options,omitempty"`
	MaxTokens     int                  `json:"max_tokens,omitempty"`
	Temperature   float32              `json:"temperature,omitempty"`
}

type openAIStreamChunk struct {
	Choices []struct {
		Delta struct {
			Content string `json:"content"`
		} `json:"delta"`
		FinishReason *string `json:"finish_reason"`
	} `json:"choices"`
	Usage *struct {
		PromptTokens     int `json:"prompt_tokens"`
		CompletionTokens int `json:"completion_tokens"`
		TotalTokens      int `json:"total_tokens"`
	} `json:"usage"`
}

// ValidateKey performs a minimal 1-token test request to verify API credentials
func (p *OpenAICompatProvider) ValidateKey(ctx context.Context, apiKey string) error {
	if err := ValidateEndpoint(p.name, p.endpoint); err != nil {
		return err
	}

	reqBody := openAIRequest{
		Model: p.defaultModel,
		Messages: []openAIMessage{
			{Role: "user", Content: "hi"},
		},
		MaxTokens: 1,
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
	req.Header.Set("Authorization", "Bearer "+apiKey)

	resp, err := p.httpClient.Do(req)
	if err != nil {
		return fmt.Errorf("network error during key validation: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		respBody, _ := io.ReadAll(io.LimitReader(resp.Body, 512))
		return MapHTTPError(resp.StatusCode, string(respBody), resp.Header, p.name, p.defaultModel)
	}

	return nil
}

// Stream sends a chat request and emits chunks over a channel
func (p *OpenAICompatProvider) Stream(ctx context.Context, apiKey string, req StreamRequest) (<-chan StreamChunk, error) {
	if err := ValidateEndpoint(p.name, p.endpoint); err != nil {
		return nil, err
	}

	model := req.Model
	if model == "" {
		model = p.defaultModel
	}

	messages := make([]openAIMessage, len(req.Messages))
	for i, m := range req.Messages {
		messages[i] = openAIMessage{
			Role:    m.Role,
			Content: m.Content,
		}
	}

	reqBody := openAIRequest{
		Model:         model,
		Messages:      messages,
		Stream:        true,
		StreamOptions: &openAIStreamOptions{IncludeUsage: true},
		MaxTokens:     req.MaxTokens,
		Temperature:   req.Temperature,
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
	httpReq.Header.Set("Authorization", "Bearer "+apiKey)
	httpReq.Header.Set("Accept", "text/event-stream")

	resp, err := p.httpClient.Do(httpReq)
	if err != nil {
		return nil, fmt.Errorf("request failed: %w", err)
	}

	if resp.StatusCode != http.StatusOK {
		defer resp.Body.Close()
		respBody, _ := io.ReadAll(io.LimitReader(resp.Body, 512))
		return nil, MapHTTPError(resp.StatusCode, string(respBody), resp.Header, p.name, model)
	}

	ch := make(chan StreamChunk, 32)

	go func() {
		defer resp.Body.Close()
		defer close(ch)

		scanner := bufio.NewScanner(resp.Body)
		var emittedFirstToken bool

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
			if payload == "[DONE]" {
				ch <- StreamChunk{Done: true}
				return
			}

			var chunk openAIStreamChunk
			if err := json.Unmarshal([]byte(payload), &chunk); err != nil {
				continue
			}

			if chunk.Usage != nil {
				ch <- StreamChunk{
					Usage: &UsageMetrics{
						PromptTokens:     chunk.Usage.PromptTokens,
						CompletionTokens: chunk.Usage.CompletionTokens,
						TotalTokens:      chunk.Usage.TotalTokens,
					},
				}
			}

			if len(chunk.Choices) > 0 {
				content := chunk.Choices[0].Delta.Content
				if content != "" {
					emittedFirstToken = true
					ch <- StreamChunk{Content: content}
				}
			}
		}

		if err := scanner.Err(); err != nil {
			ch <- StreamChunk{
				Partial: emittedFirstToken,
				Err:     err,
			}
		}
	}()

	return ch, nil
}
