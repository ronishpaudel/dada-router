package redact

import (
	"strings"
	"testing"
)

func TestRedact(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		expected string
	}{
		{
			name:     "empty input",
			input:    "",
			expected: "",
		},
		{
			name:     "safe text",
			input:    "Hello world, normal conversation text.",
			expected: "Hello world, normal conversation text.",
		},
		{
			name:     "OpenAI key",
			input:    "Using key sk-1234567890abcdef1234567890 in header",
			expected: "Using key [REDACTED_KEY] in header",
		},
		{
			name:     "Anthropic key",
			input:    "Found key sk-ant-api03-abcdef1234567890abcdef in config",
			expected: "Found key [REDACTED_ANTHROPIC_KEY] in config",
		},
		{
			name:     "Google API key",
			input:    "Key is AIzaSyD9876543210abcdef1234567890ab ok",
			expected: "Key is [REDACTED_GOOGLE_KEY] ok",
		},
		{
			name:     "xAI key",
			input:    "xAI token xai-1234567890abcdef1234567890 ready",
			expected: "xAI token [REDACTED_XAI_KEY] ready",
		},
		{
			name:     "Bearer token",
			input:    "Authorization: Bearer abcd1234efgh5678ijkl9012mnop3456",
			expected: "Authorization: Bearer [REDACTED_TOKEN]",
		},
		{
			name:     "JWT token",
			input:    "Token: eyJhbGciOiJIUzI1NiIsInR5cCI6IkpXVCJ9.eyJzdWIiOiIxMjM0NTY3ODkwIn0.dozjgNryP4J3jVmNH",
			expected: "Token: [REDACTED_JWT]",
		},
		{
			name: "PEM private key block",
			input: "Certificate:\n-----BEGIN RSA PRIVATE KEY-----\nMIIEowIBAAKCAQEA0Y\nXYZ1234567890\n-----END RSA PRIVATE KEY-----\nEnd of cert",
			expected: "Certificate:\n[REDACTED_PRIVATE_KEY]\nEnd of cert",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := Redact(tt.input)
			if got != tt.expected {
				t.Errorf("Redact() = %q, want %q", got, tt.expected)
			}
		})
	}
}

func TestRedactErrorBody(t *testing.T) {
	longBody := strings.Repeat("A", 250) + " sk-1234567890abcdef1234567890"
	got := RedactErrorBody(longBody)
	if len(got) > 220 {
		t.Errorf("RedactErrorBody should truncate body, length is %d", len(got))
	}
	if !strings.HasSuffix(got, "...(truncated)") {
		t.Errorf("Expected truncated suffix, got %q", got)
	}

	shortWithSecret := "Error 401: Invalid key sk-abcdef1234567890abcdef"
	redacted := RedactErrorBody(shortWithSecret)
	if strings.Contains(redacted, "sk-abcdef") {
		t.Errorf("Error body still contains unredacted secret: %s", redacted)
	}
}
