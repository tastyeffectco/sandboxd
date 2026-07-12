package auth

import "context"

// SessionCookie is the name of the console session cookie.
const SessionCookie = "sbx_session"

// CredentialResolver resolves the two DB-backed credential types to an owner
// (tenant). It is implemented in the api package over *store.Store; declaring it
// here keeps internal/auth free of a store dependency (store imports would form
// a cycle). A nil resolver means "only env-configured SANDBOXD_API_TOKENS are
// accepted" — the pre-console behaviour.
type CredentialResolver interface {
	// ResolveSession maps a session cookie value to its owner. ok=false when the
	// cookie is absent, unknown, or expired.
	ResolveSession(ctx context.Context, cookieValue string) (owner string, ok bool)
	// ResolveAPIKey maps a presented bearer key to its owner. ok=false when the
	// key is absent or unknown.
	ResolveAPIKey(ctx context.Context, presented string) (owner string, ok bool)
}
