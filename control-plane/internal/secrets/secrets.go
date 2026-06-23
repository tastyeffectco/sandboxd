// Package secrets encrypts app config values at rest with AES-256-GCM
// (standard-library crypto only). The master key comes from
// SANDBOXD_SECRETS_KEY, or an auto-generated 0600 keyfile under the data
// dir. No custom crypto, and plaintext is never logged.
package secrets

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"encoding/base64"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// Cipher seals/opens secret values.
type Cipher struct{ aead cipher.AEAD }

// Load builds a Cipher. If envKey is set it must be base64 for 32 bytes;
// otherwise a key is read from keyfilePath, or generated and written
// there with 0600 permissions.
func Load(envKey, keyfilePath string) (*Cipher, error) {
	key, err := resolveKey(envKey, keyfilePath)
	if err != nil {
		return nil, err
	}
	block, err := aes.NewCipher(key)
	if err != nil {
		return nil, err
	}
	aead, err := cipher.NewGCM(block)
	if err != nil {
		return nil, err
	}
	return &Cipher{aead: aead}, nil
}

func resolveKey(envKey, keyfilePath string) ([]byte, error) {
	if envKey != "" {
		key, err := base64.StdEncoding.DecodeString(strings.TrimSpace(envKey))
		if err != nil {
			return nil, fmt.Errorf("SANDBOXD_SECRETS_KEY is not valid base64: %w", err)
		}
		if len(key) != 32 {
			return nil, fmt.Errorf("SANDBOXD_SECRETS_KEY must decode to 32 bytes, got %d", len(key))
		}
		return key, nil
	}
	if b, err := os.ReadFile(keyfilePath); err == nil {
		key, derr := base64.StdEncoding.DecodeString(strings.TrimSpace(string(b)))
		if derr != nil || len(key) != 32 {
			return nil, fmt.Errorf("secrets keyfile %s is corrupt (want base64 of 32 bytes)", keyfilePath)
		}
		return key, nil
	}
	// Generate and persist a fresh key.
	key := make([]byte, 32)
	if _, err := rand.Read(key); err != nil {
		return nil, err
	}
	if err := os.MkdirAll(filepath.Dir(keyfilePath), 0o700); err != nil {
		return nil, err
	}
	if err := os.WriteFile(keyfilePath, []byte(base64.StdEncoding.EncodeToString(key)), 0o600); err != nil {
		return nil, fmt.Errorf("write secrets keyfile: %w", err)
	}
	return key, nil
}

// Seal encrypts plaintext, returning the ciphertext and the random nonce
// used (store both; the nonce is not secret).
func (c *Cipher) Seal(plaintext []byte) (ciphertext, nonce []byte, err error) {
	nonce = make([]byte, c.aead.NonceSize())
	if _, err = rand.Read(nonce); err != nil {
		return nil, nil, err
	}
	return c.aead.Seal(nil, nonce, plaintext, nil), nonce, nil
}

// Open decrypts ciphertext sealed with nonce. Fails if either was tampered.
func (c *Cipher) Open(ciphertext, nonce []byte) ([]byte, error) {
	if len(nonce) != c.aead.NonceSize() {
		return nil, errors.New("secrets: wrong nonce length")
	}
	return c.aead.Open(nil, nonce, ciphertext, nil)
}
