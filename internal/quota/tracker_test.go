package quota

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/ronishpaudel/dada-router/internal/config"
)

func TestTracker_SpendGuard(t *testing.T) {
	settings := config.NewDefaultSettings()
	settings.SpendGuard.DailyTokenLimit = 1000

	tracker := NewTracker(settings)

	// Below limit
	err := tracker.CheckSpendGuard(500)
	require.NoError(t, err)

	// Record usage
	tracker.RecordUsage(config.ProviderOpenAI, 900)

	// Exceeding ceiling
	err = tracker.CheckSpendGuard(200)
	require.ErrorIs(t, err, ErrSpendGuardExceeded)
}

func TestTracker_RateLimit(t *testing.T) {
	settings := config.NewDefaultSettings()
	tracker := NewTracker(settings)

	assert.True(t, tracker.CanCallProvider(config.ProviderAnthropic))

	// Mark rate limited for 5 seconds
	tracker.RecordRateLimit(config.ProviderAnthropic, 5*time.Second)

	assert.False(t, tracker.CanCallProvider(config.ProviderAnthropic))
}

func TestRenderUsageBars(t *testing.T) {
	settings := config.NewDefaultSettings()
	tracker := NewTracker(settings)

	tracker.RecordUsage(config.ProviderGemini, 50000)

	out := RenderUsageBars(tracker)
	assert.Contains(t, out, "Provider Quotas & Usage (estimated)")
	assert.Contains(t, out, "GEMINI")
	assert.Contains(t, out, "verified:")
}
