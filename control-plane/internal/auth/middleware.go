package auth

import (
	"context"
	"log/slog"
	"net"
	"net/http"
	"strings"
	"sync/atomic"
)

// Actor identifies the authenticated caller of a request. The auth
// middleware attaches it to the request context; handlers read it via
// ActorFrom to populate the audit log.
type Actor struct {
	Kind string // service | operator | system | unknown
	Name string // token name, or "loopback" for the operator path
	IP   string
}

type actorCtxKey struct{}

// WithActor stores an Actor in ctx.
func WithActor(ctx context.Context, a Actor) context.Context {
	return context.WithValue(ctx, actorCtxKey{}, a)
}

// ActorFrom returns the Actor stored in ctx, or {Kind:"unknown"}.
func ActorFrom(ctx context.Context) Actor {
	if a, ok := ctx.Value(actorCtxKey{}).(Actor); ok {
		return a
	}
	return Actor{Kind: "unknown"}
}

// AuditWriter is the slice of the audit logger the middleware needs.
// Declared here so internal/auth does not import internal/audit
// (internal/audit imports internal/store; keeping the dependency one-
// directional avoids a cycle).
type AuditWriter interface {
	TokenInvalid(ctx context.Context, ip string)
}

// Middleware is the uniform auth gate. Every request (regardless of origin —
// on-host, console-proxied, or Traefik-routed) must carry a valid credential: a
// console session cookie, or a bearer API key (DB-stored or env-configured).
// There is no locality bypass — "if auth is required, it is required."
type Middleware struct {
	cfg      atomic.Pointer[Config]
	resolver CredentialResolver
	audit    AuditWriter
	log      *slog.Logger
}

// NewMiddleware constructs the middleware around an initial config. resolver may
// be nil (env-token-only mode).
func NewMiddleware(initial *Config, resolver CredentialResolver, audit AuditWriter, log *slog.Logger) *Middleware {
	m := &Middleware{resolver: resolver, audit: audit, log: log}
	m.cfg.Store(initial)
	return m
}

// Reload atomically swaps the config — the SIGHUP token-rotation path.
func (m *Middleware) Reload(c *Config) { m.cfg.Store(c) }

// Snapshot returns the current config; callers treat it as read-only.
func (m *Middleware) Snapshot() *Config { return m.cfg.Load() }

// exemptPaths are reachable on the external path without a bearer
// token. /preview-auth and /forward-auth validate their
// own JWTs; /healthz and /readyz carry nothing sensitive.
var exemptPaths = map[string]bool{
	"/healthz":        true,
	"/readyz":         true,
	"/preview-auth":   true,
	"/forward-auth":   true,
	"/llm.txt":        true, // public API contract for integrators (no token)
	"/v1/auth/status": true, // console asks "is auth on / am I logged in / is a password set" pre-login
	"/v1/auth/login":  true, // you cannot be authenticated in order to authenticate
	"/v1/auth/setup":  true, // first-run "create password" (self-guards: 409 once set)
}

// Wrap returns next gated by the uniform credential check.
func (m *Middleware) Wrap(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		cfg := m.cfg.Load()
		ip := ClientIP(r)

		// /metrics is loopback-only — never exposed externally. (This is the
		// only remaining use of loopback detection: it does NOT bypass auth.)
		if r.URL.Path == "/metrics" && !isLoopbackReq(r) {
			http.NotFound(w, r)
			return
		}

		// Resolve a credential, in order: console session cookie, then a bearer
		// API key (DB-stored via the resolver, else env-configured tokens).
		actor, authed := m.resolve(r, cfg, ip)

		// Exempt paths serve regardless of whether a credential was present
		// (they carry nothing sensitive, or self-guard). Attach whatever actor
		// resolved so handlers like /v1/auth/status can report authenticated.
		if exemptPaths[r.URL.Path] {
			next.ServeHTTP(w, r.WithContext(WithActor(r.Context(), actor)))
			return
		}

		// SANDBOXD_API_AUTH_DISABLED rollback — every request runs as the shared
		// tenant, unauthenticated. Explicit opt-out; trips the warning banner.
		if cfg.Disabled {
			next.ServeHTTP(w, r.WithContext(WithActor(r.Context(),
				Actor{Kind: "service", Name: "default", IP: ip})))
			return
		}

		if !authed {
			if m.audit != nil {
				m.audit.TokenInvalid(r.Context(), ip)
			}
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusUnauthorized)
			_, _ = w.Write([]byte(`{"error":"unauthorized"}` + "\n"))
			return
		}
		next.ServeHTTP(w, r.WithContext(WithActor(r.Context(), actor)))
	})
}

// resolve returns the authenticated actor (and true) for a request, or a
// zero-value actor (and false) when no valid credential is present.
func (m *Middleware) resolve(r *http.Request, cfg *Config, ip string) (Actor, bool) {
	// 1. console session cookie.
	if m.resolver != nil {
		if ck, err := r.Cookie(SessionCookie); err == nil && ck.Value != "" {
			if owner, ok := m.resolver.ResolveSession(r.Context(), ck.Value); ok {
				return Actor{Kind: "user", Name: owner, IP: ip}, true
			}
		}
	}
	// 2. bearer API key — DB-stored, then env-configured.
	if tok := bearerToken(r); tok != "" {
		if m.resolver != nil {
			if owner, ok := m.resolver.ResolveAPIKey(r.Context(), tok); ok {
				return Actor{Kind: "service", Name: owner, IP: ip}, true
			}
		}
		if name, ok := MatchToken(tok, cfg.APITokens); ok {
			return Actor{Kind: "service", Name: name, IP: ip}, true
		}
	}
	return Actor{Kind: "unknown", IP: ip}, false
}

// bearerToken extracts the token from an `Authorization: Bearer <t>`
// header, or "" when absent / malformed.
func bearerToken(r *http.Request) string {
	h := r.Header.Get("Authorization")
	const p = "Bearer "
	if len(h) > len(p) && strings.EqualFold(h[:len(p)], p) {
		return strings.TrimSpace(h[len(p):])
	}
	return ""
}

// ClientIP returns the best-effort caller IP: the first hop of
// X-Forwarded-For when present, else the RemoteAddr host.
func ClientIP(r *http.Request) string {
	if xff := r.Header.Get("X-Forwarded-For"); xff != "" {
		if i := strings.IndexByte(xff, ','); i >= 0 {
			return strings.TrimSpace(xff[:i])
		}
		return strings.TrimSpace(xff)
	}
	host, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		return r.RemoteAddr
	}
	return host
}

// isLoopbackReq reports whether the request arrived directly over the
// loopback socket with no X-Forwarded-For — i.e. an on-host operator
// call, not a Traefik-forwarded one.
func isLoopbackReq(r *http.Request) bool {
	if r.Header.Get("X-Forwarded-For") != "" {
		return false
	}
	host, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		host = r.RemoteAddr
	}
	ip := net.ParseIP(host)
	return ip != nil && ip.IsLoopback()
}
