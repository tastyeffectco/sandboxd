package api

import (
	"context"
	"errors"
	"net"
	"net/http"
	"strings"

	"github.com/tastyeffectco/sandboxd/control-plane/internal/store"
)

// GET /edge/tls-allowed?domain=<host> — the managed Caddy front door's
// on-demand TLS "ask" hook (caddy/Caddyfile). Caddy calls it before it asks
// Let's Encrypt for a certificate; 200 lets it, anything else refuses. It is
// unauthenticated (Caddy has no credential) and deliberately cheap: names
// that are not shaped like a host sandboxd serves are refused without
// touching the database, and a preview name costs one primary-key lookup.
//
// Caddy reaches it directly on the Docker network (http://sandboxd:9000), so
// a request that came through Traefik (it carries X-Forwarded-For) is refused:
// the edge never answers "which hostnames exist" to the internet.
func (s *Server) handleEdgeTLSAllowed(w http.ResponseWriter, r *http.Request) {
	if r.Header.Get("X-Forwarded-For") != "" || !s.hostServed(r.Context(), r.URL.Query().Get("domain")) {
		http.NotFound(w, r)
		return
	}
	w.Header().Set("Content-Type", "text/plain; charset=utf-8")
	_, _ = w.Write([]byte("ok\n"))
}

// hostServed reports whether host is a hostname this engine serves and may
// therefore hold a certificate for. Each rule below answers for one family of
// hostnames; add a rule (for example app public hostnames) to extend it.
func (s *Server) hostServed(ctx context.Context, host string) bool {
	host, ok := normalizeEdgeHost(host)
	if !ok {
		return false
	}
	for _, served := range []func(context.Context, string) bool{
		s.servesInstanceHost,
		s.servesPreviewHost,
		s.servesPublishedHost,
	} {
		if served(ctx, host) {
			return true
		}
	}
	return false
}

// servesInstanceHost: the console and the API hostnames.
func (s *Server) servesInstanceHost(_ context.Context, host string) bool {
	if s.PreviewDomain == "" {
		return false
	}
	hs := s.hosts()
	if host == hs.Console() || host == hs.API() {
		return true
	}
	return s.ConsoleHost != "" && host == strings.ToLower(s.ConsoleHost)
}

// servesPreviewHost: a preview hostname under PREVIEW_DOMAIN whose sandbox
// exists. Browsers and SNI lowercase names while ids are usually uppercase
// ULIDs, so the id is tried as written and uppercased.
func (s *Server) servesPreviewHost(ctx context.Context, host string) bool {
	if s.PreviewDomain == "" || s.Store == nil {
		return false
	}
	id := parseSandboxIDFromHost(host, s.hosts())
	if id == "" {
		return false
	}
	for _, cand := range uniqueStrings(id, strings.ToUpper(id)) {
		ok, err := s.Store.SandboxExists(ctx, cand)
		if err != nil && !errors.Is(err, store.ErrNotFound) {
			return false
		}
		if ok {
			return true
		}
	}
	return false
}

// normalizeEdgeHost lowercases host, drops a trailing dot and a :port, and
// rejects anything that is not a plain DNS name (IP literals, wildcards,
// empty labels, over-long names).
func normalizeEdgeHost(host string) (string, bool) {
	host = strings.ToLower(strings.TrimSpace(host))
	if h, _, err := net.SplitHostPort(host); err == nil {
		host = h
	}
	host = strings.TrimSuffix(host, ".")
	if host == "" || len(host) > 253 || net.ParseIP(host) != nil {
		return "", false
	}
	for _, label := range strings.Split(host, ".") {
		if label == "" || len(label) > 63 || label[0] == '-' || label[len(label)-1] == '-' {
			return "", false
		}
		for _, c := range label {
			if (c < 'a' || c > 'z') && (c < '0' || c > '9') && c != '-' {
				return "", false
			}
		}
	}
	return host, true
}

func uniqueStrings(a, b string) []string {
	if a == b {
		return []string{a}
	}
	return []string{a, b}
}

// ValidMiddlewareRef reports whether s is empty or a Traefik middleware
// reference, name[@provider]: name is [A-Za-z0-9_-]+, provider [a-z0-9_-]+.
// SANDBOXD_RATELIMIT_MIDDLEWARE ends up verbatim in a container label, so
// main refuses anything else at startup.
func ValidMiddlewareRef(s string) bool {
	if s == "" {
		return true
	}
	name, provider, hasProvider := strings.Cut(s, "@")
	return validRefPart(name, true) && (!hasProvider || validRefPart(provider, false))
}

func validRefPart(v string, upper bool) bool {
	if v == "" || len(v) > 128 {
		return false
	}
	for _, c := range v {
		switch {
		case c >= 'a' && c <= 'z', c >= '0' && c <= '9', c == '-', c == '_':
		case upper && c >= 'A' && c <= 'Z':
		default:
			return false
		}
	}
	return true
}

// servesPublishedHost: a published app's public hostname (sandbox.public_hosts).
func (s *Server) servesPublishedHost(ctx context.Context, host string) bool {
	if s.Store == nil {
		return false
	}
	used, err := s.Store.PublicHostInUse(ctx, host, "")
	return err == nil && used
}
