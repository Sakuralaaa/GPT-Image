package service

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
)

// SecretKeyEnv names the environment variable holding the at-rest encryption
// key for account credentials (password, mail_token).
const SecretKeyEnv = "CHATGPT2API_SECRET_KEY"

const (
	secretKeyFileName = "secret.key"
	secretBoxPrefix   = "enc:v1:"
)

var (
	secretBoxMu  sync.RWMutex
	secretBoxKey []byte
)

// SecretKeyInitResult describes where the at-rest key came from.
type SecretKeyInitResult struct {
	Source    string // "env", "file" or "generated"
	Path      string
	Generated bool
}

// InitSecretKey loads the at-rest encryption key from CHATGPT2API_SECRET_KEY
// or, when unset, from <dataDir>/secret.key, generating that file (0600) on
// first start. The env value may be any string; it is stretched with SHA-256.
func InitSecretKey(dataDir string) (SecretKeyInitResult, error) {
	if raw := strings.TrimSpace(os.Getenv(SecretKeyEnv)); raw != "" {
		SetSecretKey(raw)
		return SecretKeyInitResult{Source: "env"}, nil
	}
	path := filepath.Join(dataDir, secretKeyFileName)
	if data, err := os.ReadFile(path); err == nil {
		value := strings.TrimSpace(string(data))
		if value != "" {
			SetSecretKey(value)
			return SecretKeyInitResult{Source: "file", Path: path}, nil
		}
	}
	buf := make([]byte, 32)
	if _, err := rand.Read(buf); err != nil {
		return SecretKeyInitResult{}, err
	}
	value := hex.EncodeToString(buf)
	if err := os.MkdirAll(dataDir, 0o755); err != nil {
		return SecretKeyInitResult{}, err
	}
	if err := os.WriteFile(path, []byte(value+"\n"), 0o600); err != nil {
		return SecretKeyInitResult{}, fmt.Errorf("write secret key file: %w", err)
	}
	SetSecretKey(value)
	return SecretKeyInitResult{Source: "generated", Path: path, Generated: true}, nil
}

// SetSecretKey installs the key used by EncryptSecret/DecryptSecret. An empty
// value disables encryption (values are stored as given).
func SetSecretKey(raw string) {
	secretBoxMu.Lock()
	defer secretBoxMu.Unlock()
	raw = strings.TrimSpace(raw)
	if raw == "" {
		secretBoxKey = nil
		return
	}
	sum := sha256.Sum256([]byte(raw))
	secretBoxKey = sum[:]
}

func currentSecretKey() []byte {
	secretBoxMu.RLock()
	defer secretBoxMu.RUnlock()
	return secretBoxKey
}

// IsEncryptedSecret reports whether value carries the at-rest envelope.
func IsEncryptedSecret(value string) bool {
	return strings.HasPrefix(value, secretBoxPrefix)
}

// EncryptSecret seals plaintext with AES-256-GCM. Empty values, already
// sealed values, and calls without a configured key are returned unchanged.
func EncryptSecret(plain string) (string, error) {
	key := currentSecretKey()
	if plain == "" || key == nil || IsEncryptedSecret(plain) {
		return plain, nil
	}
	block, err := aes.NewCipher(key)
	if err != nil {
		return "", err
	}
	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return "", err
	}
	nonce := make([]byte, gcm.NonceSize())
	if _, err := rand.Read(nonce); err != nil {
		return "", err
	}
	sealed := gcm.Seal(nonce, nonce, []byte(plain), nil)
	return secretBoxPrefix + base64.RawStdEncoding.EncodeToString(sealed), nil
}

// DecryptSecret opens a sealed value; plaintext values pass through.
func DecryptSecret(value string) (string, error) {
	if !IsEncryptedSecret(value) {
		return value, nil
	}
	key := currentSecretKey()
	if key == nil {
		return "", errors.New("secret key is not configured")
	}
	data, err := base64.RawStdEncoding.DecodeString(strings.TrimPrefix(value, secretBoxPrefix))
	if err != nil {
		return "", err
	}
	block, err := aes.NewCipher(key)
	if err != nil {
		return "", err
	}
	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return "", err
	}
	if len(data) < gcm.NonceSize() {
		return "", errors.New("sealed secret is too short")
	}
	plain, err := gcm.Open(nil, data[:gcm.NonceSize()], data[gcm.NonceSize():], nil)
	if err != nil {
		return "", fmt.Errorf("decrypt secret: %w", err)
	}
	return string(plain), nil
}

// MaskSecret renders a secret for API responses without revealing it.
func MaskSecret(value string) string {
	value = strings.TrimSpace(value)
	if value == "" {
		return ""
	}
	runes := []rune(value)
	if len(runes) <= 8 {
		return "********"
	}
	return string(runes[:3]) + "****" + string(runes[len(runes)-4:])
}

// UnmaskSecret returns current when incoming is the masked rendering of
// current (the client echoed the mask back unchanged), otherwise incoming.
func UnmaskSecret(incoming, current string) string {
	if incoming != "" && incoming == MaskSecret(current) {
		return current
	}
	return incoming
}

// accountSecretFields are the account credential fields sealed at rest.
var accountSecretFields = []string{"password", "mail_token"}
