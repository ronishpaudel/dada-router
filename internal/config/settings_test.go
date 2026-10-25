package config

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestNewDefaultSettings(t *testing.T) {
	s := NewDefaultSettings()
	require.NotNil(t, s)
	assert.Equal(t, CurrentSettingsVersion, s.Version)
	assert.Equal(t, DefaultRouteName, s.DefaultRoute)
	assert.Contains(t, s.Routes, DefaultRouteName)
	assert.Contains(t, s.Providers, ProviderGemini)
	assert.Contains(t, s.Providers, ProviderOpenAI)
	assert.Contains(t, s.Providers, ProviderAnthropic)
	assert.Contains(t, s.Providers, ProviderXAI)
	assert.Contains(t, s.Providers, ProviderDeepSeek)
}

func TestSaveAndLoadSettings(t *testing.T) {
	tempDir := t.TempDir()
	t.Setenv("DADA_ROUTER_CONFIG_DIR", tempDir)

	s := NewDefaultSettings()
	s.DefaultRoute = "coding"
	err := SaveSettings(s)
	require.NoError(t, err)

	loaded, err := LoadSettings()
	require.NoError(t, err)
	assert.Equal(t, "coding", loaded.DefaultRoute)
	assert.Equal(t, CurrentSettingsVersion, loaded.Version)
}

func TestLoadSettings_InvalidVersion(t *testing.T) {
	tempDir := t.TempDir()
	t.Setenv("DADA_ROUTER_CONFIG_DIR", tempDir)

	s := NewDefaultSettings()
	s.Version = 999
	data, err := json.Marshal(s)
	require.NoError(t, err)

	settingsFile := filepath.Join(tempDir, "settings.json")
	err = os.WriteFile(settingsFile, data, 0600)
	require.NoError(t, err)

	_, err = LoadSettings()
	require.Error(t, err)
	assert.ErrorIs(t, err, ErrInvalidSettingsVersion)
}
