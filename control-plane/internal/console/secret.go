// Package console holds the auth primitives for the web console: bcrypt for the
// human password, and sha256 over high-entropy random tokens for API keys and
// session cookies. No custom crypto — bcrypt from x/crypto, sha256 from stdlib.
package console

import (
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"

	"golang.org/x/crypto/bcrypt"
)

const bcryptCost = 12

// HashPassword returns a bcrypt hash of a human password (store this).
func HashPassword(plain string) (string, error) {
	b, err := bcrypt.GenerateFromPassword([]byte(plain), bcryptCost)
	if err != nil {
		return "", err
	}
	return string(b), nil
}

// CheckPassword reports whether plain matches the stored bcrypt hash.
func CheckPassword(hash, plain string) bool {
	return bcrypt.CompareHashAndPassword([]byte(hash), []byte(plain)) == nil
}

// HashToken returns the sha256 hex of a token/cookie value — the form stored and
// looked up. Tokens are 256-bit random, so a fast hash is appropriate (bcrypt is
// only needed for low-entropy human passwords).
func HashToken(plain string) string {
	sum := sha256.Sum256([]byte(plain))
	return hex.EncodeToString(sum[:])
}

// NewToken mints an API key. It returns the plaintext (shown to the user once),
// its sha256 hex (stored), and a short display prefix for the UI.
func NewToken() (plain, hash, prefix string, err error) {
	raw := make([]byte, 32)
	if _, err = rand.Read(raw); err != nil {
		return "", "", "", err
	}
	plain = "sk_" + base64.RawURLEncoding.EncodeToString(raw)
	hash = HashToken(plain)
	prefix = plain[:12] + "…"
	return plain, hash, prefix, nil
}

// NewSessionValue mints an opaque session cookie value and its stored sha256 hex.
func NewSessionValue() (value, hash string, err error) {
	raw := make([]byte, 32)
	if _, err = rand.Read(raw); err != nil {
		return "", "", err
	}
	value = base64.RawURLEncoding.EncodeToString(raw)
	return value, HashToken(value), nil
}
