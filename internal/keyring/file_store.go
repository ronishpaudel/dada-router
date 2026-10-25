package keyring

import (
	"crypto/rand"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"time"

	"golang.org/x/crypto/argon2"
	"golang.org/x/crypto/chacha20poly1305"
)

var (
	ErrDecryptionFailed = errors.New("failed to decrypt key store: incorrect passphrase or corrupted file")
	ErrInvalidStoreData = errors.New("invalid or corrupted key store data")
)

const (
	fileStoreMagic = "DADK" // 4 bytes magic header
	saltSize       = 16
)

// ZeroBytes clears byte slices in memory on best-effort basis
func ZeroBytes(b []byte) {
	for i := range b {
		b[i] = 0
	}
}

// EncryptedFileStore provides file-based encrypted credential storage
type EncryptedFileStore struct {
	filePath string
}

// NewEncryptedFileStore creates a new EncryptedFileStore pointing to the given path
func NewEncryptedFileStore(filePath string) *EncryptedFileStore {
	return &EncryptedFileStore{
		filePath: filePath,
	}
}

// deriveKey derives a 32-byte encryption key using Argon2id
func deriveKey(passphrase []byte, salt []byte) []byte {
	// Argon2id with recommended OWASP parameters
	return argon2.IDKey(passphrase, salt, 3, 64*1024, 4, 32)
}

// Load reads and decrypts the credentials map from the encrypted file
func (s *EncryptedFileStore) Load(passphrase []byte) (map[string]string, error) {
	data, err := os.ReadFile(s.filePath)
	if errors.Is(err, os.ErrNotExist) {
		return make(map[string]string), nil
	}
	if err != nil {
		return nil, fmt.Errorf("failed to read key store file: %w", err)
	}

	minLen := len(fileStoreMagic) + saltSize + chacha20poly1305.NonceSizeX
	if len(data) < minLen {
		return nil, ErrInvalidStoreData
	}

	if string(data[:len(fileStoreMagic)]) != fileStoreMagic {
		return nil, ErrInvalidStoreData
	}

	offset := len(fileStoreMagic)
	salt := data[offset : offset+saltSize]
	offset += saltSize

	nonce := data[offset : offset+chacha20poly1305.NonceSizeX]
	offset += chacha20poly1305.NonceSizeX

	ciphertext := data[offset:]

	derivedKey := deriveKey(passphrase, salt)
	defer ZeroBytes(derivedKey)

	aead, err := chacha20poly1305.NewX(derivedKey)
	if err != nil {
		return nil, fmt.Errorf("failed to create AEAD cipher: %w", err)
	}

	plaintext, err := aead.Open(nil, nonce, ciphertext, []byte(fileStoreMagic))
	if err != nil {
		return nil, ErrDecryptionFailed
	}
	defer ZeroBytes(plaintext)

	var keys map[string]string
	if err := json.Unmarshal(plaintext, &keys); err != nil {
		return nil, fmt.Errorf("failed to unmarshal credentials: %w", err)
	}

	return keys, nil
}

// Save encrypts and writes the credentials map to disk atomically
func (s *EncryptedFileStore) Save(keys map[string]string, passphrase []byte) error {
	plaintext, err := json.Marshal(keys)
	if err != nil {
		return fmt.Errorf("failed to serialize credentials: %w", err)
	}
	defer ZeroBytes(plaintext)

	salt := make([]byte, saltSize)
	if _, err := io.ReadFull(rand.Reader, salt); err != nil {
		return fmt.Errorf("failed to generate random salt: %w", err)
	}

	nonce := make([]byte, chacha20poly1305.NonceSizeX)
	if _, err := io.ReadFull(rand.Reader, nonce); err != nil {
		return fmt.Errorf("failed to generate random nonce: %w", err)
	}

	derivedKey := deriveKey(passphrase, salt)
	defer ZeroBytes(derivedKey)

	aead, err := chacha20poly1305.NewX(derivedKey)
	if err != nil {
		return fmt.Errorf("failed to initialize cipher: %w", err)
	}

	ciphertext := aead.Seal(nil, nonce, plaintext, []byte(fileStoreMagic))

	dir := filepath.Dir(s.filePath)
	if err := os.MkdirAll(dir, 0700); err != nil {
		return fmt.Errorf("failed to create directory for keystore: %w", err)
	}

	// Buffer: magic + salt + nonce + ciphertext
	out := make([]byte, 0, len(fileStoreMagic)+len(salt)+len(nonce)+len(ciphertext))
	out = append(out, []byte(fileStoreMagic)...)
	out = append(out, salt...)
	out = append(out, nonce...)
	out = append(out, ciphertext...)

	tmpPath := fmt.Sprintf("%s.tmp.%d", s.filePath, time.Now().UnixNano())
	f, err := os.OpenFile(tmpPath, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, 0600)
	if err != nil {
		return fmt.Errorf("failed to open temporary keystore: %w", err)
	}

	if _, err := f.Write(out); err != nil {
		_ = f.Close()
		_ = os.Remove(tmpPath)
		return fmt.Errorf("failed to write keystore: %w", err)
	}

	if err := f.Sync(); err != nil {
		_ = f.Close()
		_ = os.Remove(tmpPath)
		return fmt.Errorf("failed to fsync keystore: %w", err)
	}

	if err := f.Close(); err != nil {
		_ = os.Remove(tmpPath)
		return fmt.Errorf("failed to close temporary keystore: %w", err)
	}

	if err := os.Rename(tmpPath, s.filePath); err != nil {
		_ = os.Remove(tmpPath)
		return fmt.Errorf("failed to rename temporary keystore: %w", err)
	}

	return nil
}
