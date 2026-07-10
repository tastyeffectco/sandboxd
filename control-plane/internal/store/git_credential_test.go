package store

import (
	"context"
	"testing"
)

func newGitCredStore(t *testing.T) *Store {
	t.Helper()
	st, err := Open(context.Background(), "file::memory:?_fk=1", "../../migrations")
	if err != nil {
		t.Fatal(err)
	}
	return st
}

func TestGitCredentialStoreCRUDAndOwnerScope(t *testing.T) {
	ctx := context.Background()
	s := newGitCredStore(t)
	enc, nonce := []byte{0xde, 0xad}, []byte{0xbe, 0xef}

	a := &GitCredential{ID: "01A", OwnerToken: "ownerA", Name: "gh", Host: "github.com", Username: "x"}
	if err := s.CreateGitCredential(ctx, a, enc, nonce); err != nil {
		t.Fatal(err)
	}
	// duplicate (owner, name) => ErrConflict
	dup := &GitCredential{ID: "01B", OwnerToken: "ownerA", Name: "gh"}
	if err := s.CreateGitCredential(ctx, dup, enc, nonce); err != ErrConflict {
		t.Fatalf("dup: got %v, want ErrConflict", err)
	}
	// same name under a different owner is fine (owner-scoped uniqueness)
	b := &GitCredential{ID: "01C", OwnerToken: "ownerB", Name: "gh"}
	if err := s.CreateGitCredential(ctx, b, enc, nonce); err != nil {
		t.Fatalf("owner B same name: %v", err)
	}

	// list is owner-scoped + metadata only (struct has no secret field at all)
	la, _ := s.ListGitCredentials(ctx, "ownerA")
	if len(la) != 1 || la[0].ID != "01A" || la[0].Host != "github.com" {
		t.Fatalf("ownerA list = %+v", la)
	}

	// secret round-trips for the owner; cross-owner get returns not-found
	gotEnc, gotNonce, ok, err := s.GetGitCredentialSecret(ctx, "ownerA", "01A")
	if err != nil || !ok || string(gotEnc) != string(enc) || string(gotNonce) != string(nonce) {
		t.Fatalf("get secret: ok=%v err=%v", ok, err)
	}
	if _, _, ok, _ := s.GetGitCredentialSecret(ctx, "ownerB", "01A"); ok {
		t.Fatal("ownerB must not read ownerA's secret")
	}

	// cross-owner delete is a no-op (false); owner delete works (true)
	if del, _ := s.DeleteGitCredential(ctx, "ownerB", "01A"); del {
		t.Fatal("ownerB deleted ownerA's credential")
	}
	if del, _ := s.DeleteGitCredential(ctx, "ownerA", "01A"); !del {
		t.Fatal("ownerA could not delete own credential")
	}
	if del, _ := s.DeleteGitCredential(ctx, "ownerA", "01A"); del {
		t.Fatal("second delete should be false (already gone)")
	}
}
