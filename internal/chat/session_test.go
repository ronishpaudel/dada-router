package chat

import (
	"bytes"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/ronishpaudel/dada-router/internal/cache"
	"github.com/ronishpaudel/dada-router/internal/config"
	"github.com/ronishpaudel/dada-router/internal/keyring"
	"github.com/ronishpaudel/dada-router/internal/logger"
	"github.com/ronishpaudel/dada-router/internal/provider"
	"github.com/ronishpaudel/dada-router/internal/quota"
)

func setupTestSession(t *testing.T) (*Session, *bytes.Buffer) {
	tempDir := t.TempDir()
	t.Setenv("DADA_ROUTER_CONFIG_DIR", tempDir)

	settings := config.NewDefaultSettings()
	reg := provider.NewRegistry(settings)
	store := keyring.NewFileStoreAdapter(tempDir+"/test_creds.enc", []byte("pass"))
	q := quota.NewTracker(settings)
	c, _ := cache.NewResponseCache(0)
	l, _ := logger.NewLogger(settings)

	buf := new(bytes.Buffer)
	session, err := NewSession(settings, reg, store, q, c, l, "dada")
	require.NoError(t, err)
	session.OutputWriter = buf

	return session, buf
}

func TestSession_PromptAndSwitch(t *testing.T) {
	session, buf := setupTestSession(t)

	// Initial prompt
	prompt := session.Prompt()
	assert.Contains(t, prompt, "gemini")

	// Slash command: /help
	cont, err := session.HandleSlashCommand("/help")
	require.NoError(t, err)
	assert.True(t, cont)
	assert.Contains(t, buf.String(), "/switch")

	// Slash command: /switch openai
	buf.Reset()
	cont, err = session.HandleSlashCommand("/switch openai")
	require.NoError(t, err)
	assert.True(t, cont)
	assert.Equal(t, "openai", session.ActiveProvider)
	assert.Contains(t, buf.String(), "Switched to openai")

	// Slash command: /model openai gpt-4o-mini
	buf.Reset()
	cont, err = session.HandleSlashCommand("/model openai gpt-4o-mini")
	require.NoError(t, err)
	assert.True(t, cont)
	assert.Equal(t, "gpt-4o-mini", session.ActiveModel)

	// Slash command: /clear
	session.History = append(session.History, provider.ChatMessage{Role: "user", Content: "test"})
	cont, err = session.HandleSlashCommand("/clear")
	require.NoError(t, err)
	assert.True(t, cont)
	assert.Empty(t, session.History)

	// Slash command: /quit
	cont, err = session.HandleSlashCommand("/quit")
	require.NoError(t, err)
	assert.False(t, cont)
}
