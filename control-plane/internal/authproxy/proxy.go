// Package authproxy is the credential-injecting reverse proxy that lets a
// sandbox's coding agent reach its model provider WITHOUT the credential ever
// entering the sandbox. Every agent (claude-code, opencode, …) is pointed at
// this proxy per provider with a DUMMY key; the proxy — running control-plane
// side, holding the real credentials — strips the dummy auth and injects the
// real one on the wire, then forwards to the provider.
//
// Sandbox base URLs take the form `<proxy>/<agent>/<upstream>/…`, e.g.
// `<proxy>/opencode/zen/v1/chat/completions`. The proxy parses <agent> and
// <upstream>, resolves that agent's stored credential, injects it for the
// upstream, and forwards the remaining path. No credential — API key or OAuth
// token — is ever mounted or env-injected into the sandbox.
//
// Why: mounting/injecting the raw credential exposed it to the untrusted
// workspace AND let a CLI mutate/erase the shared file on a failed refresh.
// Keeping every credential here fixes both: the sandbox can neither read,
// exfiltrate, nor clobber it.
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

	"github.com/tastyeffectco/sandboxd/control-plane/internal/agentauth"
)

// oauthBeta is the anthropic-beta flag Claude Code sends in subscription (OAuth)
// mode — required for the OAuth bearer to be accepted on /v1/messages.
const oauthBeta = "oauth-2025-04-20"

// upstreams the proxy forwards to, keyed by the <upstream> segment of the
// sandbox base URL. A base path (e.g. /zen/v1) is preserved: the incoming path
// after /<agent>/<upstream> is appended to it.
var upstreams = map[string]string{
	"anthropic": "https://api.anthropic.com",
	"openai":    "https://api.openai.com/v1",
	"zen":       "https://opencode.ai/zen/v1",    // opencode's hosted gateway (pay-as-you-go)
	"zengo":     "https://opencode.ai/zen/go/v1", // opencode Zen "go" subscription
}

// Proxy injects the real provider credential into forwarded requests.
type Proxy struct {
	store *agentauth.Store
	log   *slog.Logger
}

// New builds the proxy over the agent-auth store (which holds every provider's
// credential). Returns nil if store is nil (proxy disabled).
func New(store *agentauth.Store, log *slog.Logger) *Proxy {
	if store == nil {
		return nil
	}
	return &Proxy{store: store, log: log}
}

func (p *Proxy) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	if r.URL.Path == "/healthz" {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("ok"))
		return
	}
	// /<agent>/<upstream>/<rest...>
	segs := strings.SplitN(strings.TrimPrefix(r.URL.Path, "/"), "/", 3)
	if len(segs) < 2 {
		http.Error(w, "bad proxy path (want /<agent>/<upstream>/...)", http.StatusBadRequest)
		return
	}
	agent, up := segs[0], segs[1]
	rest := "/"
	if len(segs) == 3 {
		rest = "/" + segs[2]
	}
	base, ok := upstreams[up]
	if !ok {
		http.Error(w, "unknown upstream: "+up, http.StatusBadRequest)
		return
	}
	inject, ok := p.credFor(agent, up)
	if !ok {
		// No usable/proxyable credential — a clear 401 so the task reads
		// "reconnect this agent" rather than a cryptic upstream error.
		http.Error(w, agent+" is not connected (or not proxyable) — connect it in Settings → AI Agents", http.StatusUnauthorized)
		return
	}
	target, _ := url.Parse(base)
	// Preserve the upstream's base path (e.g. /zen/v1) + the request suffix.
	r.URL.Path = strings.TrimRight(target.Path, "/") + rest
	r.URL.RawPath = ""
	inject(r.Header)
	rp := &httputil.ReverseProxy{
		Director: func(req *http.Request) {
			req.URL.Scheme = target.Scheme
			req.URL.Host = target.Host
			req.Host = target.Host
		},
		FlushInterval: -1, // stream SSE token-by-token
		ErrorHandler: func(w http.ResponseWriter, _ *http.Request, err error) {
			if p.log != nil {
				p.log.Warn("authproxy: upstream error", "upstream", up, "err", err.Error())
			}
			http.Error(w, "upstream error", http.StatusBadGateway)
		},
	}
	rp.ServeHTTP(w, r)
}

// credFor returns a header-injector for (agent, upstream) using the agent's
// stored credential, or ok=false when there's no usable/proxyable credential.
func (p *Proxy) credFor(agent, up string) (func(http.Header), bool) {
	switch p.store.Method(agent) {
	case "api_key":
		key := readTrim(filepath.Join(p.store.Dir(agent), agentauth.APIKeyFile))
		if key == "" {
			return nil, false
		}
		return func(h http.Header) {
			h.Del("Authorization")
			h.Del("X-Api-Key")
			if up == "anthropic" {
				h.Set("X-Api-Key", key) // Anthropic API-key header
			} else {
				h.Set("Authorization", "Bearer "+key) // OpenAI / Zen (OpenAI-compatible)
			}
		}, true
	case "oauth":
		// Only claude-code's Anthropic OAuth is proxyable today (opencode/codex
		// OAuth/subscription formats are not — they connect by API key instead).
		if agent == "claude-code" && up == "anthropic" {
			tok := claudeOAuthToken(p.store)
			if tok == "" {
				return nil, false
			}
			return func(h http.Header) {
				h.Del("X-Api-Key")
				h.Del("Authorization")
				h.Set("Authorization", "Bearer "+tok)
				h.Set("anthropic-beta", mergeBeta(h.Get("anthropic-beta")))
			}, true
		}
	}
	return nil, false
}

// claudeOAuthToken reads the current subscription access token from the claude
// credential file. Opaque read; empty when absent/unparseable.
func claudeOAuthToken(store *agentauth.Store) string {
	b, err := os.ReadFile(filepath.Join(store.Dir("claude-code"), ".claude/.credentials.json"))
	if err != nil {
		return ""
	}
	var d struct {
		ClaudeAiOauth struct {
			AccessToken string `json:"accessToken"`
		} `json:"claudeAiOauth"`
	}
	if json.Unmarshal(b, &d) != nil {
		return ""
	}
	return d.ClaudeAiOauth.AccessToken
}

func readTrim(path string) string {
	b, err := os.ReadFile(path)
	if err != nil {
		return ""
	}
	return strings.TrimSpace(string(b))
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
