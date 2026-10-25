package chat

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/yourname/dada-router/internal/provider"
)

func TestEstimateTokens(t *testing.T) {
	assert.Equal(t, 0, EstimateTokens(""))

	// 100 characters: 100/4 = 25 * 1.10 = 27.5 -> 28
	text100 := strings.Repeat("a", 100)
	assert.Equal(t, 28, EstimateTokens(text100))
}

func TestBudgetConversation(t *testing.T) {
	messages := []provider.ChatMessage{
		{Role: "system", Content: "You are a helpful assistant."},
		{Role: "user", Content: "Turn 1: " + strings.Repeat("x", 200)},
		{Role: "assistant", Content: "Reply 1: " + strings.Repeat("y", 200)},
		{Role: "user", Content: "Turn 2: " + strings.Repeat("z", 200)},
		{Role: "assistant", Content: "Reply 2: " + strings.Repeat("w", 200)},
	}

	initialTokens := EstimateConversationTokens(messages)

	// Budget smaller than total tokens
	budget := initialTokens / 2
	trimmed, dropped := BudgetConversation(messages, budget)

	require.Greater(t, dropped, 0)
	assert.Equal(t, "system", trimmed[0].Role) // Preserved system prompt
	assert.LessOrEqual(t, EstimateConversationTokens(trimmed), budget)
}

func TestPrepareHandoff(t *testing.T) {
	messages := []provider.ChatMessage{
		{Role: "user", Content: "Hello world"},
	}

	out, notices := PrepareHandoff(messages, "gemini", 1000)
	assert.Equal(t, 1, len(out))
	assert.Empty(t, notices)

	// Overflow budget
	overflowMessages := []provider.ChatMessage{
		{Role: "user", Content: strings.Repeat("a", 1000)},
		{Role: "assistant", Content: strings.Repeat("b", 1000)},
	}
	_, notices = PrepareHandoff(overflowMessages, "anthropic", 50)
	assert.NotEmpty(t, notices)
	assert.Contains(t, notices[0], "Context trimmed")
}
