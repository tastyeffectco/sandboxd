// Package authproxy is the credential-injecting reverse proxy that lets a
// sandbox's Claude Code CLI reach Anthropic WITHOUT the subscription credential
// ever entering the sandbox. The sandbox gets only ANTHROPIC_BASE_URL (this
// proxy) + a dummy ANTHROPIC_API_KEY (enough for the CLI to skip its local
// "Not logged in" gate). This proxy — running control-plane-side, holding the
// real credential — strips the dummy auth and injects the real subscription
// OAuth bearer on the wire, then forwards to api.anthropic.com.
//
// Why: mounting the raw credential into the sandbox exposed it to the untrusted
// workspace AND let the CLI mutate/erase the shared file on a failed refresh
// (which corrupted it for every task). Keeping the credential here fixes both:
// the sandbox can neither read, exfiltrate, nor clobber it.
package authproxy

import (
	"encoding/json"
	"log/slog"
	"net/http"
	"net/http/httputil"
	"net/url"
	"os"
	"path/filepath"
	"strings"

	"github.com/sandboxd/control-plane/internal/agentauth"
)

const (
	anthropicBase = "https://api.anthropic.com"
	// oauthBeta is the anthropic-beta flag Claude Code sends in subscription
	// (OAuth) mode — extracted from the claude binary. Required for the OAuth
	// bearer to be accepted on /v1/messages.
	oauthBeta = "oauth-2025-04-20"
	credRel   = ".claude/.credentials.json"
)

// Proxy injects the real Claude subscription bearer into forwarded requests.
type Proxy struct {
	store *agentauth.Store
	rp    *httputil.ReverseProxy
	log   *slog.Logger
}

// New builds the proxy over the agent-auth store (which holds the claude-code
// credential). Returns nil if store is nil (proxy disabled).
func New(store *agentauth.Store, log *slog.Logger) *Proxy {
	if store == nil {
		return nil
	}
	target, _ := url.Parse(anthropicBase)
	p := &Proxy{store: store, log: log}
	p.rp = &httputil.ReverseProxy{
		// Director only fixes the destination; auth is injected in ServeHTTP so
		// the (possibly refreshed) token is read per request.
		Director: func(r *http.Request) {
			r.URL.Scheme = target.Scheme
			r.URL.Host = target.Host
			r.Host = target.Host
		},
		FlushInterval: -1, // stream SSE token-by-token, no buffering
		ErrorHandler: func(w http.ResponseWriter, _ *http.Request, err error) {
			if log != nil {
				log.Warn("authproxy: upstream error", "err", err.Error())
			}
			http.Error(w, "upstream error", http.StatusBadGateway)
		},
	}
	return p
}

// ServeHTTP forwards to Anthropic with the real subscription bearer swapped in.
func (p *Proxy) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	if r.URL.Path == "/healthz" {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("ok"))
		return
	}
	tok, ok := p.token()
	if !ok {
		// No usable credential — surface a clear 401 so the task result reads
		// "reconnect your subscription" rather than a cryptic upstream error.
		http.Error(w, "claude-code not connected: import a valid subscription credential in Settings → AI Agents", http.StatusUnauthorized)
		return
	}
	// Drop whatever the sandbox sent (the dummy key / any bearer) and inject the
	// real subscription auth. The token never travels back to the sandbox.
	r.Header.Del("X-Api-Key")
	r.Header.Del("Authorization")
	r.Header.Set("Authorization", "Bearer "+tok)
	r.Header.Set("anthropic-beta", mergeBeta(r.Header.Get("anthropic-beta")))
	p.rp.ServeHTTP(w, r)
}

// token reads the current subscription access token from the store. Opaque read
// of the CLI's own credential file; empty/absent token => not connected.
func (p *Proxy) token() (string, bool) {
	b, err := os.ReadFile(filepath.Join(p.store.Dir("claude-code"), credRel))
	if err != nil {
		return "", false
	}
	var d struct {
		ClaudeAiOauth struct {
			AccessToken string `json:"accessToken"`
		} `json:"claudeAiOauth"`
	}
	if json.Unmarshal(b, &d) != nil || d.ClaudeAiOauth.AccessToken == "" {
		return "", false
	}
	return d.ClaudeAiOauth.AccessToken, true
}

// mergeBeta ensures the OAuth beta flag is present without dropping any the CLI
// already set (comma-joined, deduped).
func mergeBeta(existing string) string {
	if existing == "" {
		return oauthBeta
	}
	for _, f := range strings.Split(existing, ",") {
		if strings.TrimSpace(f) == oauthBeta {
			return existing
		}
	}
	return existing + "," + oauthBeta
}
