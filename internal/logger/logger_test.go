package logger

import (
	"errors"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/ronishpaudel/dada-router/internal/config"
)

func TestLogger_DisabledByDefault(t *testing.T) {
	tempDir := t.TempDir()
	t.Setenv("DADA_ROUTER_CONFIG_DIR", tempDir)

	settings := config.NewDefaultSettings()
	log, err := NewLogger(settings)
	require.NoError(t, err)

	log.LogEvent("openai", "gpt-4o", "dada", "ok", 100, "hello", "hi", nil)

	content, err := log.ReadLogs()
	require.NoError(t, err)
	assert.Equal(t, "No logs found.", content)
}

func TestLogger_EnabledRedaction(t *testing.T) {
	tempDir := t.TempDir()
	t.Setenv("DADA_ROUTER_CONFIG_DIR", tempDir)

	settings := config.NewDefaultSettings()
	settings.Log.Enabled = true
	settings.Log.LogPrompts = true

	log, err := NewLogger(settings)
	require.NoError(t, err)

	secretPrompt := "my key is sk-1234567890abcdef1234567890"
	log.LogEvent("openai", "gpt-4o", "dada", "ok", 50, secretPrompt, "secret response", nil)

	content, err := log.ReadLogs()
	require.NoError(t, err)
	assert.Contains(t, content, "[REDACTED_KEY]")
	assert.NotContains(t, content, "sk-1234567890")

	// Verify file permissions
	logPath := filepath.Join(tempDir, "logs", "dada-router.log")
	assert.FileExists(t, logPath)

	// Reset
	err = log.Reset()
	require.NoError(t, err)
	content, err = log.ReadLogs()
	require.NoError(t, err)
	assert.Equal(t, "No logs found.", content)
}

func TestLogger_PanicLogging(t *testing.T) {
	tempDir := t.TempDir()
	t.Setenv("DADA_ROUTER_CONFIG_DIR", tempDir)

	settings := config.NewDefaultSettings()
	settings.Log.Enabled = true

	log, err := NewLogger(settings)
	require.NoError(t, err)

	log.LogPanic(errors.New("unexpected crash"), []byte("runtime error with sk-1234567890abcdef1234567890"))

	content, err := log.ReadLogs()
	require.NoError(t, err)
	assert.Contains(t, content, "PANIC")
	assert.Contains(t, content, "[REDACTED_KEY]")
}
