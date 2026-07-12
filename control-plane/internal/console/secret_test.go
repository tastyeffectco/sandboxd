package console

import (
	"strings"
	"testing"
)

func TestPassword(t *testing.T) {
	h, err := HashPassword("correct horse")
	if err != nil {
		t.Fatal(err)
	}
	if !CheckPassword(h, "correct horse") {
		t.Fatal("correct password rejected")
	}
	if CheckPassword(h, "wrong") {
		t.Fatal("wrong password accepted")
	}
}

func TestNewToken(t *testing.T) {
	plain, hash, prefix, err := NewToken()
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(plain, "sk_") {
		t.Fatalf("plain %q missing sk_ prefix", plain)
	}
	if HashToken(plain) != hash {
		t.Fatal("HashToken(plain) != returned hash")
	}
	if len([]rune(prefix)) > 16 {
		t.Fatalf("prefix too long: %q", prefix)
	}
	plain2, _, _, _ := NewToken()
	if plain == plain2 {
		t.Fatal("two tokens collided")
	}
}

func TestNewSessionValue(t *testing.T) {
	v, h, err := NewSessionValue()
	if err != nil {
		t.Fatal(err)
	}
	if v == "" || HashToken(v) != h {
		t.Fatalf("session value/hash mismatch: v=%q", v)
	}
}
