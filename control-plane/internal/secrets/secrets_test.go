package secrets

import (
	"bytes"
	"encoding/base64"
	"os"
	"path/filepath"
	"testing"
)

func TestSealOpenRoundTrip(t *testing.T) {
	c, err := Load("", filepath.Join(t.TempDir(), "secrets.key"))
	if err != nil {
		t.Fatal(err)
	}
	pt := []byte("sk-super-secret-value")
	ct, nonce, err := c.Seal(pt)
	if err != nil {
		t.Fatal(err)
	}
	if bytes.Contains(ct, pt) {
		t.Fatal("ciphertext contains the plaintext")
	}
	got, err := c.Open(ct, nonce)
	if err != nil || !bytes.Equal(got, pt) {
		t.Fatalf("open = %q, %v; want round-trip", got, err)
	}
}

func TestSealUsesFreshNonce(t *testing.T) {
	c, _ := Load("", filepath.Join(t.TempDir(), "k"))
	_, n1, _ := c.Seal([]byte("x"))
	_, n2, _ := c.Seal([]byte("x"))
	if bytes.Equal(n1, n2) {
		t.Error("nonce reused across two Seals")
	}
}

func TestOpenRejectsTamper(t *testing.T) {
	c, _ := Load("", filepath.Join(t.TempDir(), "k"))
	ct, nonce, _ := c.Seal([]byte("value"))
	ct[0] ^= 0xff // flip a bit
	if _, err := c.Open(ct, nonce); err == nil {
		t.Error("tampered ciphertext decrypted without error")
	}
}

func TestKeyfileGeneratedWith0600(t *testing.T) {
	path := filepath.Join(t.TempDir(), "sub", "secrets.key")
	if _, err := Load("", path); err != nil {
		t.Fatal(err)
	}
	fi, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if fi.Mode().Perm() != 0o600 {
		t.Errorf("keyfile perms = %v; want 0600", fi.Mode().Perm())
	}
	// A second Load reuses the same key (stable across restarts).
	c1, _ := Load("", path)
	c2, _ := Load("", path)
	ct, nonce, _ := c1.Seal([]byte("persisted"))
	got, err := c2.Open(ct, nonce)
	if err != nil || string(got) != "persisted" {
		t.Errorf("key not stable across loads: %q %v", got, err)
	}
}

func TestEnvKeyValidation(t *testing.T) {
	good := base64.StdEncoding.EncodeToString(make([]byte, 32))
	if _, err := Load(good, ""); err != nil {
		t.Errorf("valid 32-byte env key rejected: %v", err)
	}
	if _, err := Load(base64.StdEncoding.EncodeToString(make([]byte, 16)), ""); err == nil {
		t.Error("16-byte key accepted; want error")
	}
	if _, err := Load("not-base64!!", ""); err == nil {
		t.Error("non-base64 key accepted; want error")
	}
}
