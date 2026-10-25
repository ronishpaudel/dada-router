package cache

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/ronishpaudel/dada-router/internal/provider"
)

func TestGenerateKey(t *testing.T) {
	messages := []provider.ChatMessage{
		{Role: "user", Content: "Hello world"},
	}

	key1 := GenerateKey("openai", "gpt-4o", "dada", messages)
	key2 := GenerateKey("openai", "gpt-4o", "dada", messages)
	assert.Equal(t, key1, key2)
	assert.Len(t, key1, 64)

	// Different provider produces different key
	key3 := GenerateKey("gemini", "gemini-1.5-flash", "dada", messages)
	assert.NotEqual(t, key1, key3)
}

func TestResponseCache(t *testing.T) {
	tempDir := t.TempDir()
	t.Setenv("DADA_ROUTER_CONFIG_DIR", tempDir)

	cache, err := NewResponseCache(2 * time.Second)
	require.NoError(t, err)

	key := "test-key-12345"
	_, found := cache.Get(key)
	assert.False(t, found)

	err = cache.Set(key, "cached content", &provider.UsageMetrics{TotalTokens: 42})
	require.NoError(t, err)

	item, found := cache.Get(key)
	require.True(t, found)
	assert.Equal(t, "cached content", item.Content)
	assert.Equal(t, 42, item.Usage.TotalTokens)

	// Reset
	err = cache.Reset()
	require.NoError(t, err)

	_, found = cache.Get(key)
	assert.False(t, found)
}
