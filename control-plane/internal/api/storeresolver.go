package api

import (
	"context"
	"time"

	"github.com/tastyeffectco/sandboxd/control-plane/internal/auth"
	"github.com/tastyeffectco/sandboxd/control-plane/internal/console"
	"github.com/tastyeffectco/sandboxd/control-plane/internal/store"
)

// storeResolver implements auth.CredentialResolver over the SQLite store: it
// maps session cookies and API keys to the (single, shared) tenant. Wired into
// the auth middleware in main.go.
type storeResolver struct{ st *store.Store }

// NewStoreResolver returns an auth.CredentialResolver backed by st.
func NewStoreResolver(st *store.Store) auth.CredentialResolver { return storeResolver{st: st} }

func (r storeResolver) ResolveSession(ctx context.Context, cookieValue string) (string, bool) {
	if r.st == nil || cookieValue == "" {
		return "", false
	}
	h := console.HashToken(cookieValue)
	owner, expires, found, err := r.st.LookupSession(ctx, h)
	if err != nil || !found || time.Now().Unix() > expires {
		return "", false
	}
	_ = r.st.TouchSession(ctx, h, time.Now().Unix())
	return owner, true
}

func (r storeResolver) ResolveAPIKey(ctx context.Context, presented string) (string, bool) {
	if r.st == nil || presented == "" {
		return "", false
	}
	id, found, err := r.st.LookupAPIKey(ctx, console.HashToken(presented))
	if err != nil || !found {
		return "", false
	}
	_ = r.st.TouchAPIKey(ctx, id, time.Now().Unix())
	return store.DefaultTenant, true // single shared tenant
}
