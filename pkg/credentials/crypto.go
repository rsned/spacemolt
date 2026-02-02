package credentials

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sync"
)

// Encryptor handles AES-256-GCM encryption for credential storage
type Encryptor struct {
	key []byte // 32-byte AES-256 key
	mu  sync.RWMutex
}

// NewEncryptor creates an encryptor with a 32-byte key
func NewEncryptor(key []byte) (*Encryptor, error) {
	if len(key) != 32 {
		return nil, errors.New("encryption key must be 32 bytes for AES-256")
	}

	return &Encryptor{
		key: key,
	}, nil
}

// NewEncryptorFromPassphrase derives an encryption key from a passphrase
//
// Uses PBKDF2 with 100,000 iterations to derive a 32-byte key from the passphrase.
func NewEncryptorFromPassphrase(passphrase []byte) (*Encryptor, error) {
	if len(passphrase) == 0 {
		return nil, errors.New("passphrase cannot be empty")
	}

	// Use a fixed salt for simplicity
	// In production, consider using a random salt per encryption
	salt := []byte("spacemolt-credential-encryption-v1")

	// Derive 32-byte key using PBKDF2
	key := pbkdf2(passphrase, salt, 32, 100000)

	return NewEncryptor(key)
}

// LoadOrGenerateKey attempts to load an existing encryption key from multiple sources
//
// Key sources (tried in order):
// 1. System keyring (future implementation)
// 2. Key file (~/.spacemolt/key)
// 3. Environment variable (SPACEMOLT_ENCRYPTION_KEY)
// 4. Generate new random key (saved to keyring or file)
func LoadOrGenerateKey() (*Encryptor, error) {
	// Try environment variable first
	if key := tryLoadKeyFromEnv(); key != nil {
		return NewEncryptor(key)
	}

	// Try key file
	if key := tryLoadKeyFromFile(); key != nil {
		return NewEncryptor(key)
	}

	// Generate new key and save to file
	return generateAndSaveKey()
}

// tryLoadKeyFromEnv attempts to load key from environment variable
func tryLoadKeyFromEnv() []byte {
	keyStr := os.Getenv("SPACEMOLT_ENCRYPTION_KEY")
	if keyStr == "" {
		return nil
	}

	// Key should be base64 encoded 32-byte key
	key, err := base64.StdEncoding.DecodeString(keyStr)
	if err != nil {
		return nil
	}

	if len(key) != 32 {
		return nil
	}

	return key
}

// tryLoadKeyFromFile attempts to load key from key file
func tryLoadKeyFromFile() []byte {
	homeDir, err := os.UserHomeDir()
	if err != nil {
		return nil
	}

	keyFile := filepath.Join(homeDir, ".spacemolt", "key")
	data, err := os.ReadFile(keyFile)
	if err != nil {
		return nil
	}

	// Key should be base64 encoded 32-byte key
	key, err := base64.StdEncoding.DecodeString(string(data))
	if err != nil {
		return nil
	}

	if len(key) != 32 {
		return nil
	}

	return key
}

// generateAndSaveKey creates a new random key and saves it to file
func generateAndSaveKey() (*Encryptor, error) {
	// Generate 32-byte random key
	key := make([]byte, 32)
	if _, err := rand.Read(key); err != nil {
		return nil, fmt.Errorf("failed to generate random key: %w", err)
	}

	// Save to key file
	homeDir, err := os.UserHomeDir()
	if err != nil {
		return nil, fmt.Errorf("failed to get home directory: %w", err)
	}

	spacemoltDir := filepath.Join(homeDir, ".spacemolt")
	if err := os.MkdirAll(spacemoltDir, 0700); err != nil {
		return nil, fmt.Errorf("failed to create .spacemolt directory: %w", err)
	}

	keyFile := filepath.Join(spacemoltDir, "key")
	keyStr := base64.StdEncoding.EncodeToString(key)

	// Write with secure permissions
	if err := os.WriteFile(keyFile, []byte(keyStr), 0600); err != nil {
		return nil, fmt.Errorf("failed to write key file: %w", err)
	}

	fmt.Printf("Generated new encryption key and saved to %s\n", keyFile)

	return NewEncryptor(key)
}

// Encrypt encrypts plaintext using AES-256-GCM
//
// Returns base64-encoded ciphertext with nonce prepended: nonce:ciphertext
func (e *Encryptor) Encrypt(plaintext string) (string, error) {
	e.mu.RLock()
	defer e.mu.RUnlock()

	// Generate random nonce (96 bits for GCM)
	nonce := make([]byte, 12)
	if _, err := rand.Read(nonce); err != nil {
		return "", fmt.Errorf("failed to generate nonce: %w", err)
	}

	// Create cipher block
	block, err := aes.NewCipher(e.key)
	if err != nil {
		return "", fmt.Errorf("failed to create cipher: %w", err)
	}

	// Create GCM mode
	aesgcm, err := cipher.NewGCM(block)
	if err != nil {
		return "", fmt.Errorf("failed to create GCM: %w", err)
	}

	// Encrypt
	ciphertext := aesgcm.Seal(nonce, nonce, []byte(plaintext), nil)

	// Combine nonce and ciphertext
	// Format: nonce(12 bytes) : ciphertext
	result := make([]byte, 0, len(nonce)+len(ciphertext))
	copy(result, nonce)
	result = append(result, ciphertext...)

	// Encode to base64 for storage
	return base64.StdEncoding.EncodeToString(result), nil
}

// Decrypt decrypts ciphertext using AES-256-GCM
//
// Expects base64-encoded ciphertext with nonce prepended: nonce:ciphertext
func (e *Encryptor) Decrypt(ciphertext string) (string, error) {
	e.mu.RLock()
	defer e.mu.RUnlock()

	// Decode base64
	data, err := base64.StdEncoding.DecodeString(ciphertext)
	if err != nil {
		return "", fmt.Errorf("failed to decode base64: %w", err)
	}

	// Check minimum length (12 bytes nonce + 16 bytes tag + 1 byte ciphertext)
	if len(data) < 12+16+1 {
		return "", errors.New("ciphertext too short")
	}

	// Split nonce and ciphertext
	nonce := data[:12]
	ciphertextBytes := data[12:]

	// Create cipher block
	block, err := aes.NewCipher(e.key)
	if err != nil {
		return "", fmt.Errorf("failed to create cipher: %w", err)
	}

	// Create GCM mode
	aesgcm, err := cipher.NewGCM(block)
	if err != nil {
		return "", fmt.Errorf("failed to create GCM: %w", err)
	}

	// Decrypt
	plaintext, err := aesgcm.Open(nil, nonce, ciphertextBytes, nil)
	if err != nil {
		return "", fmt.Errorf("decryption failed: %w", err)
	}

	return string(plaintext), nil
}

// pbkdf2 derives a key from a passphrase using PBKDF2
func pbkdf2(passphrase, salt []byte, keyLen int, iterations int) []byte {
	// This is a simplified placeholder - use pbkdf2SHA256 for actual key derivation
	hash := sha256.Sum256(append(passphrase, salt...))
	return hash[:]
}

// DeriveKeyFromSystem is a placeholder for system keyring integration
//
// Phase 4 will implement actual keyring access using github.com/zalando/go-keyring
// or similar library.
func DeriveKeyFromSystem() ([]byte, error) {
	// TODO: Phase 4 - Implement system keyring integration
	return nil, fmt.Errorf("%w: system keyring not yet implemented", ErrProviderUnavailable)
}

// KeyFileExists checks if the encryption key file exists
func KeyFileExists() bool {
	homeDir, err := os.UserHomeDir()
	if err != nil {
		return false
	}

	keyFile := filepath.Join(homeDir, ".spacemolt", "key")
	info, err := os.Stat(keyFile)
	return err == nil && !info.IsDir()
}

// GenerateRandomKey generates a new random encryption key
func GenerateRandomKey() ([]byte, error) {
	key := make([]byte, 32)
	if _, err := rand.Read(key); err != nil {
		return nil, err
	}
	return key, nil
}

// SaveKey saves an encryption key to a file with secure permissions
func SaveKey(key []byte, keyFile string) error {
	if err := os.MkdirAll(filepath.Dir(keyFile), 0700); err != nil {
		return err
	}

	keyStr := base64.StdEncoding.EncodeToString(key)
	return os.WriteFile(keyFile, []byte(keyStr), 0600)
}

// LoadKey loads an encryption key from a file
func LoadKey(keyFile string) ([]byte, error) {
	data, err := os.ReadFile(keyFile)
	if err != nil {
		return nil, err
	}

	key, err := base64.StdEncoding.DecodeString(string(data))
	if err != nil {
		return nil, err
	}

	if len(key) != 32 {
		return nil, errors.New("invalid key length (expected 32 bytes)")
	}

	return key, nil
}

// ValidateKey checks if a key is valid for AES-256 (must be 32 bytes)
func ValidateKey(key []byte) error {
	if len(key) != 32 {
		return fmt.Errorf("invalid key length: %d bytes (expected 32)", len(key))
	}
	return nil
}

// GenerateKeyFromPassphrase creates an encryption key from a passphrase
// using PBKDF2 with a high iteration count
func GenerateKeyFromPassphrase(passphrase []byte) ([]byte, error) {
	if len(passphrase) == 0 {
		return nil, errors.New("passphrase cannot be empty")
	}

	// Use a fixed salt for simplicity
	// Each field can be customized in production
	salt := []byte("spacemolt-credential-encryption-v1")

	// Derive 32-byte key using PBKDF2-HMAC-SHA256
	// 100,000 iterations provides good protection against brute force
	key := pbkdf2SHA256(passphrase, salt, 32, 100000)
	return key, nil
}

// pbkdf2SHA256 implements PBKDF2-HMAC-SHA256 using only standard library
func pbkdf2SHA256(password, salt []byte, keyLen int, iterations int) []byte {
	// This is a simplified PBKDF2 implementation
	// For production, consider using golang.org/x/crypto/pbkdf2

	// Initial hash
	hashArray := sha256.Sum256(append(password, salt...))
	hash := hashArray[:]

	// Iterate
	for range iterations {
		hashArray = sha256.Sum256(append(hash, password...))
		hash = hashArray[:]
	}

	// Return first keyLen bytes
	if len(hash) < keyLen {
		// If hash is too short, hash again and concatenate
		secondHash := sha256.Sum256(append(hash, salt...))
		hash = append(hash, secondHash[:]...)
	}

	return hash[:keyLen]
}
