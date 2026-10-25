package provider

import (
	"errors"
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/yourname/dada-router/internal/redact"
)

var (
	ErrAuth            = errors.New("authentication failure (invalid API key or unauthorized)")
	ErrModelNotFound   = errors.New("model not found")
	ErrContextTooLarge = errors.New("context length exceeded budget")
	ErrRateLimited     = errors.New("rate limited")
	ErrServer          = errors.New("server error or timeout")
)

// ProviderError encapsulates rich error details from an LLM provider
type ProviderError struct {
	StatusCode int
	Provider   string
	Model      string
	Message    string
	RetryAfter time.Duration
	RawError   error
}

func (e *ProviderError) Error() string {
	redactedMsg := redact.RedactErrorBody(e.Message)
	if e.RetryAfter > 0 {
		return fmt.Sprintf("[%s] HTTP %d: %s (retry-after: %v)", e.Provider, e.StatusCode, redactedMsg, e.RetryAfter)
	}
	return fmt.Sprintf("[%s] HTTP %d: %s", e.Provider, e.StatusCode, redactedMsg)
}

func (e *ProviderError) Unwrap() error {
	return e.RawError
}

// ParseRetryAfter attempts to parse the Retry-After header as seconds or HTTP date
func ParseRetryAfter(header string) time.Duration {
	if header == "" {
		return 0
	}
	// Try seconds integer first
	if secs, err := strconv.Atoi(header); err == nil && secs > 0 {
		return time.Duration(secs) * time.Second
	}
	// Try HTTP-date format (RFC1123)
	if parsedTime, err := http.ParseTime(header); err == nil {
		diff := time.Until(parsedTime)
		if diff > 0 {
			return diff
		}
	}
	return 0
}

// MapHTTPError maps an HTTP status code and response body to the exact error specification
func MapHTTPError(statusCode int, body string, header http.Header, providerName string, model string) error {
	cleanBody := redact.RedactErrorBody(body)
	retryAfter := ParseRetryAfter(header.Get("Retry-After"))

	switch {
	case statusCode == 401 || statusCode == 403:
		return &ProviderError{
			StatusCode: statusCode,
			Provider:   providerName,
			Model:      model,
			Message:    cleanBody,
			RawError:   ErrAuth,
		}

	case statusCode == 404:
		// Model not found
		return &ProviderError{
			StatusCode: statusCode,
			Provider:   providerName,
			Model:      model,
			Message:    fmt.Sprintf("model %q not found or endpoint unavailable. Run 'dada doctor' to verify: %s", model, cleanBody),
			RawError:   ErrModelNotFound,
		}

	case statusCode == 400 && (strings.Contains(strings.ToLower(cleanBody), "context_length") ||
		strings.Contains(strings.ToLower(cleanBody), "maximum context") ||
		strings.Contains(strings.ToLower(cleanBody), "token limit") ||
		strings.Contains(strings.ToLower(cleanBody), "prompt is too long")):
		return &ProviderError{
			StatusCode: statusCode,
			Provider:   providerName,
			Model:      model,
			Message:    cleanBody,
			RawError:   ErrContextTooLarge,
		}

	case statusCode == 429:
		return &ProviderError{
			StatusCode: statusCode,
			Provider:   providerName,
			Model:      model,
			Message:    cleanBody,
			RetryAfter: retryAfter,
			RawError:   ErrRateLimited,
		}

	case statusCode >= 500 && statusCode <= 599:
		return &ProviderError{
			StatusCode: statusCode,
			Provider:   providerName,
			Model:      model,
			Message:    cleanBody,
			RawError:   ErrServer,
		}

	default:
		// Other 4xx errors
		return &ProviderError{
			StatusCode: statusCode,
			Provider:   providerName,
			Model:      model,
			Message:    cleanBody,
			RawError:   fmt.Errorf("client error HTTP %d: %s", statusCode, cleanBody),
		}
	}
}
