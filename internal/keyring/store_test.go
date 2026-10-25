package keyring

import (
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/ronishpaudel/dada-router/internal/config"
)

func TestEncryptedFileStore(t *testing.T) {
	tempDir := t.TempDir()
	storePath := filepath.Join(tempDir, "test_credentials.enc")

	passphrase := []byte("secret-passphrase-12345")
	wrongPassphrase := []byte("wrong-passphrase")

	store := NewEncryptedFileStore(storePath)

	// Initially empty
	keys, err := store.Load(passphrase)
	require.NoError(t, err)
	assert.Empty(t, keys)

	// Save keys
	data := map[string]string{
		config.ProviderOpenAI: "sk-test-openai-key-1234567890",
		config.ProviderGemini: "AIzaTestGeminiKey1234567890abcdef1234567",
	}
	err = store.Save(data, passphrase)
	require.NoError(t, err)

	// Load with wrong passphrase
	_, err = store.Load(wrongPassphrase)
	require.ErrorIs(t, err, ErrDecryptionFailed)

	// Load with correct passphrase
	loaded, err := store.Load(passphrase)
	require.NoError(t, err)
	assert.Equal(t, data[config.ProviderOpenAI], loaded[config.ProviderOpenAI])
	assert.Equal(t, data[config.ProviderGemini], loaded[config.ProviderGemini])
}

func TestFileStoreAdapter(t *testing.T) {
	tempDir := t.TempDir()
	storePath := filepath.Join(tempDir, "creds.enc")
	passphrase := []byte("my-test-password")

	adapter := NewFileStoreAdapter(storePath, passphrase)

	// Set a key
	err := adapter.Set(config.ProviderAnthropic, "sk-ant-test-anthropic-key-12345")
	require.NoError(t, err)

	// Get the key
	val, err := adapter.Get(config.ProviderAnthropic)
	require.NoError(t, err)
	assert.Equal(t, "sk-ant-test-anthropic-key-12345", val)

	// Get missing key
	_, err = adapter.Get(config.ProviderXAI)
	require.ErrorIs(t, err, ErrKeyNotFound)

	// List
	list, err := adapter.List()
	require.NoError(t, err)
	assert.True(t, list[config.ProviderAnthropic])
	assert.False(t, list[config.ProviderXAI])

	// Delete
	err = adapter.Delete(config.ProviderAnthropic)
	require.NoError(t, err)
	_, err = adapter.Get(config.ProviderAnthropic)
	require.ErrorIs(t, err, ErrKeyNotFound)
}

func TestEnvKeyOverride(t *testing.T) {
	tempDir := t.TempDir()
	storePath := filepath.Join(tempDir, "creds.enc")
	passphrase := []byte("pw")

	adapter := NewFileStoreAdapter(storePath, passphrase)

	t.Setenv("DADA_ROUTER_OPENAI_KEY", "sk-env-override-1234567890")
	val, err := adapter.Get(config.ProviderOpenAI)
	require.NoError(t, err)
	assert.Equal(t, "sk-env-override-1234567890", val)
}
