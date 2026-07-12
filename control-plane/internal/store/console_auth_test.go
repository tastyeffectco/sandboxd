package store

import (
	"context"
	"testing"
	"time"
)

func newConsoleStore(t *testing.T) *Store {
	t.Helper()
	st, err := Open(context.Background(), "file::memory:?_fk=1", "../../migrations")
	if err != nil {
		t.Fatal(err)
	}
	return st
}

func TestConsoleAuthPassword(t *testing.T) {
	ctx := context.Background()
	s := newConsoleStore(t)

	if _, err := s.GetPasswordHash(ctx); err != ErrNotFound {
		t.Fatalf("unset password: got %v, want ErrNotFound", err)
	}
	if err := s.SetPasswordHash(ctx, "hash-v1"); err != nil {
		t.Fatal(err)
	}
	if h, err := s.GetPasswordHash(ctx); err != nil || h != "hash-v1" {
		t.Fatalf("get: h=%q err=%v", h, err)
	}
	// upsert replaces
	if err := s.SetPasswordHash(ctx, "hash-v2"); err != nil {
		t.Fatal(err)
	}
	if h, _ := s.GetPasswordHash(ctx); h != "hash-v2" {
		t.Fatalf("upsert: h=%q", h)
	}
}

func TestConsoleAuthSessions(t *testing.T) {
	ctx := context.Background()
	s := newConsoleStore(t)
	now := time.Now().Unix()

	if err := s.CreateSession(ctx, "th1", DefaultTenant, now, now, now+3600); err != nil {
		t.Fatal(err)
	}
	owner, exp, found, err := s.LookupSession(ctx, "th1")
	if err != nil || !found || owner != DefaultTenant || exp != now+3600 {
		t.Fatalf("lookup: owner=%q exp=%d found=%v err=%v", owner, exp, found, err)
	}
	if _, _, found, _ := s.LookupSession(ctx, "missing"); found {
		t.Fatal("missing session reported found")
	}
	if err := s.TouchSession(ctx, "th1", now+10); err != nil {
		t.Fatal(err)
	}
	if err := s.DeleteSession(ctx, "th1"); err != nil {
		t.Fatal(err)
	}
	if _, _, found, _ := s.LookupSession(ctx, "th1"); found {
		t.Fatal("deleted session still found")
	}
	// delete-all
	_ = s.CreateSession(ctx, "a", DefaultTenant, now, now, now+3600)
	_ = s.CreateSession(ctx, "b", DefaultTenant, now, now, now+3600)
	if err := s.DeleteAllSessions(ctx); err != nil {
		t.Fatal(err)
	}
	if _, _, found, _ := s.LookupSession(ctx, "a"); found {
		t.Fatal("delete-all left a session")
	}
}

func TestConsoleAuthAPIKeys(t *testing.T) {
	ctx := context.Background()
	s := newConsoleStore(t)
	now := time.Now().Unix()

	if err := s.CreateAPIKey(ctx, "01A", "default", "hashA", "sk_aaa…", now); err != nil {
		t.Fatal(err)
	}
	// duplicate name => ErrConflict
	if err := s.CreateAPIKey(ctx, "01B", "default", "hashB", "sk_bbb…", now); err != ErrConflict {
		t.Fatalf("dup name: got %v, want ErrConflict", err)
	}
	if err := s.CreateAPIKey(ctx, "01C", "ci-bot", "hashC", "sk_ccc…", now); err != nil {
		t.Fatal(err)
	}

	keys, err := s.ListAPIKeys(ctx)
	if err != nil || len(keys) != 2 {
		t.Fatalf("list: n=%d err=%v", len(keys), err)
	}
	// metadata only — prefix present, and no field can carry the hash
	if keys[0].Prefix == "" {
		t.Fatal("prefix missing from list")
	}

	id, found, err := s.LookupAPIKey(ctx, "hashA")
	if err != nil || !found || id != "01A" {
		t.Fatalf("lookup: id=%q found=%v err=%v", id, found, err)
	}
	if _, found, _ := s.LookupAPIKey(ctx, "nope"); found {
		t.Fatal("unknown hash reported found")
	}
	if err := s.TouchAPIKey(ctx, "01A", now+5); err != nil {
		t.Fatal(err)
	}
	if del, _ := s.DeleteAPIKey(ctx, "01A"); !del {
		t.Fatal("delete own key returned false")
	}
	if del, _ := s.DeleteAPIKey(ctx, "01A"); del {
		t.Fatal("second delete should be false")
	}
}
