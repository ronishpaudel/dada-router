package cache

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/yourname/dada-router/internal/config"
	"github.com/yourname/dada-router/internal/provider"
)

// CachedResponse stores a completed model response
type CachedResponse struct {
	Content   string                 `json:"content"`
	Usage     *provider.UsageMetrics `json:"usage,omitempty"`
	CreatedAt time.Time              `json:"created_at"`
	ExpiresAt time.Time              `json:"expires_at"`
}

// ResponseCache manages opt-in cached responses
type ResponseCache struct {
	mu       sync.RWMutex
	dir      string
	memCache map[string]CachedResponse
	ttl      time.Duration
}

// NewResponseCache creates a new ResponseCache instance
func NewResponseCache(ttl time.Duration) (*ResponseCache, error) {
	if ttl <= 0 {
		ttl = 24 * time.Hour
	}

	configDir, err := config.GetConfigDir()
	if err != nil {
		return nil, err
	}

	cacheDir := filepath.Join(configDir, "cache")
	_ = os.MkdirAll(cacheDir, 0700)

	return &ResponseCache{
		dir:      cacheDir,
		memCache: make(map[string]CachedResponse),
		ttl:      ttl,
	}, nil
}

// GenerateKey computes sha256(provider, model, route, normalized request)
func GenerateKey(providerName string, model string, routeName string, messages []provider.ChatMessage) string {
	var normalized strings.Builder
	for _, m := range messages {
		normalized.WriteString(strings.TrimSpace(m.Role))
		normalized.WriteString(":")
		normalized.WriteString(strings.TrimSpace(m.Content))
		normalized.WriteString("\n")
	}

	h := sha256.New()
	h.Write([]byte(providerName))
	h.Write([]byte("\x00"))
	h.Write([]byte(model))
	h.Write([]byte("\x00"))
	h.Write([]byte(routeName))
	h.Write([]byte("\x00"))
	h.Write([]byte(normalized.String()))

	return hex.EncodeToString(h.Sum(nil))
}

// Get returns the cached response if present and unexpired
func (c *ResponseCache) Get(key string) (*CachedResponse, bool) {
	c.mu.RLock()
	item, ok := c.memCache[key]
	c.mu.RUnlock()

	now := time.Now()
	if ok {
		if now.Before(item.ExpiresAt) {
			return &item, true
		}
		// Expired in memory
		c.mu.Lock()
		delete(c.memCache, key)
		c.mu.Unlock()
	}

	// Try reading from disk
	filePath := filepath.Join(c.dir, key+".json")
	data, err := os.ReadFile(filePath)
	if err != nil {
		return nil, false
	}

	var diskItem CachedResponse
	if err := json.Unmarshal(data, &diskItem); err != nil {
		_ = os.Remove(filePath)
		return nil, false
	}

	if now.After(diskItem.ExpiresAt) {
		_ = os.Remove(filePath)
		return nil, false
	}

	c.mu.Lock()
	c.memCache[key] = diskItem
	c.mu.Unlock()

	return &diskItem, true
}

// Set saves a response in cache with TTL
func (c *ResponseCache) Set(key string, content string, usage *provider.UsageMetrics) error {
	now := time.Now()
	item := CachedResponse{
		Content:   content,
		Usage:     usage,
		CreatedAt: now,
		ExpiresAt: now.Add(c.ttl),
	}

	c.mu.Lock()
	c.memCache[key] = item
	c.mu.Unlock()

	data, err := json.Marshal(item)
	if err != nil {
		return fmt.Errorf("failed to marshal cache item: %w", err)
	}

	filePath := filepath.Join(c.dir, key+".json")
	return os.WriteFile(filePath, data, 0600)
}

// Reset clears both in-memory and disk cache
func (c *ResponseCache) Reset() error {
	c.mu.Lock()
	defer c.mu.Unlock()

	c.memCache = make(map[string]CachedResponse)
	return os.RemoveAll(c.dir)
}
