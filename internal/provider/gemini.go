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

	"github.com/ronishpaudel/dada-router/internal/config"
)

// GeminiProvider implements Provider for Google Gemini
type GeminiProvider struct {
	defaultModel string
	httpClient   *http.Client
}

// NewGeminiProvider creates a Google Gemini provider adapter
func NewGeminiProvider(defaultModel string) *GeminiProvider {
	return &GeminiProvider{
		defaultModel: defaultModel,
		httpClient: &http.Client{
			Timeout: config.DefaultTotalTimeout,
		},
	}
}

func (p *GeminiProvider) Name() string {
	return config.ProviderGemini
}

func (p *GeminiProvider) DefaultModel() string {
	return p.defaultModel
}

type geminiPart struct {
	Text string `json:"text"`
}

type geminiContent struct {
	Role  string       `json:"role,omitempty"` // "user" or "model"
	Parts []geminiPart `json:"parts"`
}

type geminiSystemInstruction struct {
	Parts []geminiPart `json:"parts"`
}

type geminiGenerationConfig struct {
	MaxOutputTokens int     `json:"maxOutputTokens,omitempty"`
	Temperature     float32 `json:"temperature,omitempty"`
}

type geminiRequest struct {
	Contents          []geminiContent          `json:"contents"`
	SystemInstruction *geminiSystemInstruction `json:"systemInstruction,omitempty"`
	GenerationConfig  *geminiGenerationConfig  `json:"generationConfig,omitempty"`
}

type geminiResponseChunk struct {
	Candidates []struct {
		Content struct {
			Parts []struct {
				Text string `json:"text"`
			} `json:"parts"`
		} `json:"content"`
	} `json:"candidates"`
	UsageMetadata *struct {
		PromptTokenCount     int `json:"promptTokenCount"`
		CandidatesTokenCount int `json:"candidatesTokenCount"`
		TotalTokenCount      int `json:"totalTokenCount"`
	} `json:"usageMetadata"`
}

// ValidateKey validates the Gemini API key using a minimal 1-token request
func (p *GeminiProvider) ValidateKey(ctx context.Context, apiKey string) error {
	endpoint := fmt.Sprintf("https://generativelanguage.googleapis.com/v1beta/models/%s:generateContent", p.defaultModel)
	if err := ValidateEndpoint(config.ProviderGemini, endpoint); err != nil {
		return err
	}

	reqBody := geminiRequest{
		Contents: []geminiContent{
			{
				Role:  "user",
				Parts: []geminiPart{{Text: "hi"}},
			},
		},
		GenerationConfig: &geminiGenerationConfig{
			MaxOutputTokens: 1,
		},
	}

	data, err := json.Marshal(reqBody)
	if err != nil {
		return fmt.Errorf("failed to marshal validation request: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, bytes.NewReader(data))
	if err != nil {
		return fmt.Errorf("failed to create validation request: %w", err)
	}

	req.Header.Set("Content-Type", "application/json")
	// Strict requirement: Gemini keys in header only, never in URL
	req.Header.Set("x-goog-api-key", apiKey)

	resp, err := p.httpClient.Do(req)
	if err != nil {
		return fmt.Errorf("network error during key validation: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		respBody, _ := io.ReadAll(io.LimitReader(resp.Body, 512))
		return MapHTTPError(resp.StatusCode, string(respBody), resp.Header, config.ProviderGemini, p.defaultModel)
	}

	return nil
}

// Stream streams tokens from Google Gemini via SSE
func (p *GeminiProvider) Stream(ctx context.Context, apiKey string, req StreamRequest) (<-chan StreamChunk, error) {
	model := req.Model
	if model == "" {
		model = p.defaultModel
	}

	endpoint := fmt.Sprintf("https://generativelanguage.googleapis.com/v1beta/models/%s:streamGenerateContent?alt=sse", model)
	if err := ValidateEndpoint(config.ProviderGemini, endpoint); err != nil {
		return nil, err
	}

	var contents []geminiContent
	var systemParts []geminiPart

	for _, m := range req.Messages {
		if strings.EqualFold(m.Role, "system") {
			systemParts = append(systemParts, geminiPart{Text: m.Content})
		} else {
			role := "user"
			if strings.EqualFold(m.Role, "assistant") || strings.EqualFold(m.Role, "model") {
				role = "model"
			}
			contents = append(contents, geminiContent{
				Role:  role,
				Parts: []geminiPart{{Text: m.Content}},
			})
		}
	}

	if len(contents) == 0 {
		contents = append(contents, geminiContent{
			Role:  "user",
			Parts: []geminiPart{{Text: "hello"}},
		})
	}

	geminiReq := geminiRequest{
		Contents: contents,
	}

	if len(systemParts) > 0 {
		geminiReq.SystemInstruction = &geminiSystemInstruction{Parts: systemParts}
	}

	genConfig := &geminiGenerationConfig{
		Temperature: req.Temperature,
	}
	if req.MaxTokens > 0 {
		genConfig.MaxOutputTokens = req.MaxTokens
	}
	geminiReq.GenerationConfig = genConfig

	data, err := json.Marshal(geminiReq)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal request: %w", err)
	}

	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, bytes.NewReader(data))
	if err != nil {
		return nil, fmt.Errorf("failed to create request: %w", err)
	}

	httpReq.Header.Set("Content-Type", "application/json")
	// Auth strictly in headers only!
	httpReq.Header.Set("x-goog-api-key", apiKey)
	httpReq.Header.Set("Accept", "text/event-stream")

	resp, err := p.httpClient.Do(httpReq)
	if err != nil {
		return nil, fmt.Errorf("request failed: %w", err)
	}

	if resp.StatusCode != http.StatusOK {
		defer resp.Body.Close()
		respBody, _ := io.ReadAll(io.LimitReader(resp.Body, 512))
		return nil, MapHTTPError(resp.StatusCode, string(respBody), resp.Header, config.ProviderGemini, model)
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

			var chunk geminiResponseChunk
			if err := json.Unmarshal([]byte(payload), &chunk); err != nil {
				continue
			}

			if chunk.UsageMetadata != nil {
				ch <- StreamChunk{
					Usage: &UsageMetrics{
						PromptTokens:     chunk.UsageMetadata.PromptTokenCount,
						CompletionTokens: chunk.UsageMetadata.CandidatesTokenCount,
						TotalTokens:      chunk.UsageMetadata.TotalTokenCount,
					},
				}
			}

			for _, candidate := range chunk.Candidates {
				for _, part := range candidate.Content.Parts {
					if part.Text != "" {
						emittedFirstToken = true
						ch <- StreamChunk{Content: part.Text}
					}
				}
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
