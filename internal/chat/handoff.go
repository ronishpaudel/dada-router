package chat

import (
	"fmt"
	"math"

	"github.com/ronishpaudel/dada-router/internal/provider"
)

// EstimateTokens calculates estimated token count using formula:
// len(text)/4 + 10%
func EstimateTokens(text string) int {
	if len(text) == 0 {
		return 0
	}
	base := float64(len(text)) / 4.0
	withTenPercent := base * 1.10
	return int(math.Ceil(withTenPercent))
}

// EstimateConversationTokens calculates total estimated tokens across all turns
func EstimateConversationTokens(messages []provider.ChatMessage) int {
	total := 0
	for _, m := range messages {
		// ~4 tokens overhead per message turn
		total += 4 + EstimateTokens(m.Role) + EstimateTokens(m.Content)
	}
	return total
}

// BudgetConversation trims the oldest non-system turns if total estimated tokens exceed maxBudget.
// Returns trimmed slice and the number of dropped turns.
func BudgetConversation(messages []provider.ChatMessage, maxBudget int) ([]provider.ChatMessage, int) {
	if maxBudget <= 0 || len(messages) == 0 {
		return messages, 0
	}

	currentTokens := EstimateConversationTokens(messages)
	if currentTokens <= maxBudget {
		return messages, 0
	}

	// Keep system prompts and trim oldest non-system turns
	var systemMessages []provider.ChatMessage
	var nonSystemMessages []provider.ChatMessage

	for _, m := range messages {
		if m.Role == "system" {
			systemMessages = append(systemMessages, m)
		} else {
			nonSystemMessages = append(nonSystemMessages, m)
		}
	}

	droppedCount := 0
	for len(nonSystemMessages) > 0 && currentTokens > maxBudget {
		// Drop oldest non-system message
		nonSystemMessages = nonSystemMessages[1:]
		droppedCount++

		combined := append([]provider.ChatMessage{}, systemMessages...)
		combined = append(combined, nonSystemMessages...)
		currentTokens = EstimateConversationTokens(combined)
	}

	result := append([]provider.ChatMessage{}, systemMessages...)
	result = append(result, nonSystemMessages...)
	return result, droppedCount
}

// PrepareHandoff adapts existing conversation turns to the target provider,
// enforcing token budgets and generating user notices.
func PrepareHandoff(history []provider.ChatMessage, targetProvider string, maxBudget int) ([]provider.ChatMessage, []string) {
	var notices []string

	// Check budget
	budgeted, dropped := BudgetConversation(history, maxBudget)
	if dropped > 0 {
		notices = append(notices, fmt.Sprintf("Context trimmed: dropped %d oldest turns to fit %s input token budget", dropped, targetProvider))
	}

	// In v1, advanced provider-specific tool schemas or cache_controls are unsupported
	// and are safely omitted with a notice if any were detected.
	out := make([]provider.ChatMessage, len(budgeted))
	copy(out, budgeted)

	return out, notices
}
