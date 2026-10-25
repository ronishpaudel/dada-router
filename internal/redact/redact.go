package redact

import (
	"regexp"
	"strings"
)

var (
	// Anthropic API keys (sk-ant-...)
	anthropicKeyPattern = regexp.MustCompile(`sk-ant-[a-zA-Z0-9_\-\.]{15,}`)

	// OpenAI / DeepSeek / generic secret keys (sk-...)
	genericSKPattern = regexp.MustCompile(`sk-[a-zA-Z0-9_\-\.]{15,}`)

	// Google Gemini / Cloud API keys (AIza...)
	googleKeyPattern = regexp.MustCompile(`AIza[0-9A-Za-z_\-]{30,}`)

	// xAI API keys (xai-...)
	xaiKeyPattern = regexp.MustCompile(`xai-[a-zA-Z0-9_\-\.]{15,}`)

	// Bearer tokens in Authorization headers or text
	bearerPattern = regexp.MustCompile(`(?i)(bearer\s+)[a-zA-Z0-9_\-\.\~+/]{15,}={0,2}`)

	// JWT shaped strings (ey... . ey... . ...)
	jwtPattern = regexp.MustCompile(`\bey[a-zA-Z0-9_-]{10,}\.ey[a-zA-Z0-9_-]{10,}\.[a-zA-Z0-9_-]+\b`)

	// PEM private key blocks
	pemPattern = regexp.MustCompile(`-----BEGIN [A-Z\s]*PRIVATE KEY-----[\s\S]*?-----END [A-Z\s]*PRIVATE KEY-----`)
)

// Redact scans the input string and replaces sensitive tokens (API keys, JWTs, PEM blocks, bearer tokens)
// with redacted placeholders.
func Redact(input string) string {
	if input == "" {
		return ""
	}

	result := pemPattern.ReplaceAllString(input, "[REDACTED_PRIVATE_KEY]")
	result = anthropicKeyPattern.ReplaceAllString(result, "[REDACTED_ANTHROPIC_KEY]")
	result = genericSKPattern.ReplaceAllString(result, "[REDACTED_KEY]")
	result = googleKeyPattern.ReplaceAllString(result, "[REDACTED_GOOGLE_KEY]")
	result = xaiKeyPattern.ReplaceAllString(result, "[REDACTED_XAI_KEY]")
	result = bearerPattern.ReplaceAllString(result, "${1}[REDACTED_TOKEN]")
	result = jwtPattern.ReplaceAllString(result, "[REDACTED_JWT]")

	return result
}

// RedactErrorBody truncates an error response body to 200 characters and filters out secrets.
func RedactErrorBody(body string) string {
	cleaned := strings.TrimSpace(body)
	if len(cleaned) > 200 {
		cleaned = cleaned[:200] + "...(truncated)"
	}
	return Redact(cleaned)
}
