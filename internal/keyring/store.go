package keyring

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/zalando/go-keyring"
	"golang.org/x/term"

	"github.com/ronishpaudel/dada-router/internal/config"
)

var (
	ErrKeyNotFound = errors.New("api key not found")
)

// Store defines the interface for managing provider API keys
type Store interface {
	Get(provider string) (string, error)
	Set(provider string, key string) error
	Delete(provider string) error
	List() (map[string]bool, error)
	Reset() error
}

// SystemKeyringStore uses the OS keychain (macOS Keychain, Linux Secret Service / DBus)
type SystemKeyringStore struct {
	service string
}

// NewSystemKeyringStore creates a new Store backed by the OS keychain
func NewSystemKeyringStore() *SystemKeyringStore {
	return &SystemKeyringStore{
		service: config.KeyringService,
	}
}

// checkEnvKey checks if an environment variable override exists for CI
func checkEnvKey(provider string) string {
	envVar := fmt.Sprintf("DADA_ROUTER_%s_KEY", strings.ToUpper(provider))
	val := os.Getenv(envVar)
	if val != "" && term.IsTerminal(int(os.Stdout.Fd())) {
		fmt.Fprintf(os.Stderr, "Warning: using API key from environment variable %s\n", envVar)
	}
	return val
}

// Get retrieves an API key for a provider
func (s *SystemKeyringStore) Get(provider string) (string, error) {
	// 1. Check CI env var first
	if envKey := checkEnvKey(provider); envKey != "" {
		return envKey, nil
	}

	// 2. Fetch from system keychain
	val, err := keyring.Get(s.service, provider)
	if err != nil {
		if errors.Is(err, keyring.ErrNotFound) {
			return "", fmt.Errorf("%w: for provider %s", ErrKeyNotFound, provider)
		}
		return "", fmt.Errorf("failed to get key from keyring: %w", err)
	}
	return val, nil
}

// Set stores an API key for a provider
func (s *SystemKeyringStore) Set(provider string, key string) error {
	if err := keyring.Set(s.service, provider, key); err != nil {
		return fmt.Errorf("failed to save key in keyring: %w", err)
	}
	return nil
}

// Delete removes an API key
func (s *SystemKeyringStore) Delete(provider string) error {
	if err := keyring.Delete(s.service, provider); err != nil && !errors.Is(err, keyring.ErrNotFound) {
		return fmt.Errorf("failed to delete key: %w", err)
	}
	return nil
}

// List returns a map indicating which supported providers have a configured key
func (s *SystemKeyringStore) List() (map[string]bool, error) {
	result := make(map[string]bool)
	for _, p := range config.AllSupportedProviders() {
		_, err := s.Get(p)
		result[p] = (err == nil)
	}
	return result, nil
}

// Reset clears all keys for all supported providers
func (s *SystemKeyringStore) Reset() error {
	for _, p := range config.AllSupportedProviders() {
		_ = s.Delete(p)
	}
	return nil
}

// FileStoreAdapter adapts EncryptedFileStore to the Store interface
type FileStoreAdapter struct {
	fileStore  *EncryptedFileStore
	passphrase []byte
}

// NewFileStoreAdapter initializes an adapter for encrypted file storage
func NewFileStoreAdapter(filePath string, passphrase []byte) *FileStoreAdapter {
	return &FileStoreAdapter{
		fileStore:  NewEncryptedFileStore(filePath),
		passphrase: passphrase,
	}
}

// Get retrieves a key from the encrypted file store
func (a *FileStoreAdapter) Get(provider string) (string, error) {
	if envKey := checkEnvKey(provider); envKey != "" {
		return envKey, nil
	}

	keys, err := a.fileStore.Load(a.passphrase)
	if err != nil {
		return "", err
	}

	val, ok := keys[provider]
	if !ok || val == "" {
		return "", fmt.Errorf("%w: for provider %s", ErrKeyNotFound, provider)
	}
	return val, nil
}

// Set stores a key in the encrypted file store
func (a *FileStoreAdapter) Set(provider string, key string) error {
	keys, err := a.fileStore.Load(a.passphrase)
	if err != nil && !errors.Is(err, os.ErrNotExist) {
		return err
	}
	if keys == nil {
		keys = make(map[string]string)
	}

	keys[provider] = key
	return a.fileStore.Save(keys, a.passphrase)
}

// Delete removes a key from the encrypted file store
func (a *FileStoreAdapter) Delete(provider string) error {
	keys, err := a.fileStore.Load(a.passphrase)
	if err != nil {
		return err
	}
	delete(keys, provider)
	return a.fileStore.Save(keys, a.passphrase)
}

// List returns configured providers
func (a *FileStoreAdapter) List() (map[string]bool, error) {
	keys, err := a.fileStore.Load(a.passphrase)
	if err != nil && !errors.Is(err, os.ErrNotExist) {
		return nil, err
	}

	result := make(map[string]bool)
	for _, p := range config.AllSupportedProviders() {
		if keys != nil && keys[p] != "" {
			result[p] = true
		} else if checkEnvKey(p) != "" {
			result[p] = true
		} else {
			result[p] = false
		}
	}
	return result, nil
}

// Reset removes the encrypted credentials file
func (a *FileStoreAdapter) Reset() error {
	if err := os.Remove(a.fileStore.filePath); err != nil && !errors.Is(err, os.ErrNotExist) {
		return err
	}
	return nil
}

// GetDefaultEncryptedStorePath returns the path to credentials.enc
func GetDefaultEncryptedStorePath() (string, error) {
	dir, err := config.GetConfigDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(dir, "credentials.enc"), nil
}
