package agentauth

import (
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"strings"
	"sync"
	"time"
)

// oauth.go — the guided Claude Code subscription login: the console shows the
// user an authorize link, they approve in a browser and paste back the code, and
// we exchange it for tokens stored on the host. The proxy (internal/authproxy)
// then injects them; this file also refreshes them so they never expire out from
// under a running sandbox. This uses Claude Code's own public OAuth client — the
// tokens are ultimately used by the genuine Claude Code CLI running in the
// sandbox. Endpoints are env-overridable so a change on Anthropic's side is a
// config fix, not a rebuild.
// Defaults captured from the real `claude setup-token` flow (Claude Code 2.1.x):
// authorize on claude.com/cai, token + redirect on platform.claude.com, scope
// user:inference. All env-overridable in case Anthropic moves them again.
var (
	oauthClientID  = envOr("SANDBOXD_CLAUDE_CLIENT_ID", "9d1c250a-e61b-44d9-88ed-5944d1962f5e")
	oauthAuthorize = envOr("SANDBOXD_CLAUDE_AUTHORIZE_URL", "https://claude.com/cai/oauth/authorize")
	oauthTokenURL  = envOr("SANDBOXD_CLAUDE_TOKEN_URL", "https://platform.claude.com/v1/oauth/token")
	oauthRedirect  = envOr("SANDBOXD_CLAUDE_REDIRECT_URI", "https://platform.claude.com/oauth/code/callback")
	oauthScopes    = envOr("SANDBOXD_CLAUDE_SCOPES", "user:inference")
)

const claudeCredRel = ".claude/.credentials.json"

func envOr(k, def string) string {
	if v := os.Getenv(k); v != "" {
		return v
	}
	return def
}

// OAuth drives the claude-code subscription login + token refresh over the store.
type OAuth struct {
	store   *Store
	hc      *http.Client
	mu      sync.Mutex
	pending map[string]pending // state -> PKCE verifier
}

type pending struct {
	verifier string
	at       time.Time
}

// NewOAuth returns nil if store is nil (feature disabled).
func NewOAuth(store *Store) *OAuth {
	if store == nil {
		return nil
	}
	return &OAuth{store: store, hc: &http.Client{Timeout: 30 * time.Second}, pending: map[string]pending{}}
}

// claudeCred is the on-disk credential shape the CLI (and our proxy) read.
type claudeCred struct {
	AccessToken      string   `json:"accessToken"`
	RefreshToken     string   `json:"refreshToken"`
	ExpiresAt        int64    `json:"expiresAt"` // unix millis
	Scopes           []string `json:"scopes,omitempty"`
	SubscriptionType string   `json:"subscriptionType,omitempty"`
}

// Start builds the authorize URL the user opens, stashing the PKCE verifier
// under the generated state so Finish can complete the exchange.
func (o *OAuth) Start() (string, error) {
	verifier := randURL(32)
	state := randURL(32)
	challenge := s256(verifier)

	o.mu.Lock()
	o.pending[state] = pending{verifier: verifier, at: time.Now()}
	o.gcLocked()
	o.mu.Unlock()

	// Build the query in the EXACT order the real claude CLI emits (its authorize
	// front-end rejects a reordered request as "Invalid request format"). Go's
	// url.Values.Encode() sorts keys, so assemble the string by hand.
	esc := url.QueryEscape
	pairs := [][2]string{
		{"code", "true"},
		{"client_id", oauthClientID},
		{"response_type", "code"},
		{"redirect_uri", oauthRedirect},
		{"scope", oauthScopes},
		{"code_challenge", challenge},
		{"code_challenge_method", "S256"},
		{"state", state},
	}
	var b strings.Builder
	for i, p := range pairs {
		if i > 0 {
			b.WriteByte('&')
		}
		b.WriteString(p[0] + "=" + esc(p[1]))
	}
	return oauthAuthorize + "?" + b.String(), nil
}

// Finish exchanges the pasted code (form "code#state", or bare code when only one
// login is pending) for tokens and writes the credential to the store.
func (o *OAuth) Finish(pasted string) error {
	pasted = strings.TrimSpace(pasted)
	if pasted == "" {
		return errors.New("empty code")
	}
	code, state := pasted, ""
	if i := strings.IndexByte(pasted, '#'); i >= 0 {
		code, state = pasted[:i], pasted[i+1:]
	}

	o.mu.Lock()
	p, ok := o.pending[state]
	if ok {
		delete(o.pending, state)
	} else if len(o.pending) == 1 { // tolerate a missing state fragment
		for st, pp := range o.pending {
			p, state = pp, st
			delete(o.pending, st)
			ok = true
		}
	}
	o.mu.Unlock()
	if !ok {
		return errors.New("no pending login — start the connect flow again")
	}

	cred, err := o.exchange(map[string]string{
		"grant_type":    "authorization_code",
		"code":          code,
		"state":         state,
		"client_id":     oauthClientID,
		"redirect_uri":  oauthRedirect,
		"code_verifier": p.verifier,
	})
	if err != nil {
		return err
	}
	return o.write(cred)
}

// Refresh renews the stored token when it is within skew of expiry, using the
// stored refresh token. No-op when absent or still fresh. Safe to call often.
func (o *OAuth) Refresh() error {
	cur, err := o.read()
	if err != nil || cur.RefreshToken == "" {
		return err
	}
	if cur.ExpiresAt != 0 && time.Now().Add(15*time.Minute).UnixMilli() < cur.ExpiresAt {
		return nil // still fresh
	}
	cred, err := o.exchange(map[string]string{
		"grant_type":    "refresh_token",
		"refresh_token": cur.RefreshToken,
		"client_id":     oauthClientID,
	})
	if err != nil {
		return err
	}
	// Anthropic may omit a rotated refresh token; keep the old one then.
	if cred.RefreshToken == "" {
		cred.RefreshToken = cur.RefreshToken
	}
	if cred.SubscriptionType == "" {
		cred.SubscriptionType = cur.SubscriptionType
	}
	return o.write(cred)
}

// exchange POSTs to the token endpoint and normalizes the response.
func (o *OAuth) exchange(body map[string]string) (claudeCred, error) {
	buf, _ := json.Marshal(body)
	req, _ := http.NewRequest("POST", oauthTokenURL, strings.NewReader(string(buf)))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json")
	resp, err := o.hc.Do(req)
	if err != nil {
		return claudeCred{}, err
	}
	defer resp.Body.Close()
	raw, _ := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if resp.StatusCode/100 != 2 {
		return claudeCred{}, fmt.Errorf("token endpoint %d: %s", resp.StatusCode, snippet(raw))
	}
	var tr struct {
		AccessToken  string `json:"access_token"`
		RefreshToken string `json:"refresh_token"`
		ExpiresIn    int64  `json:"expires_in"`
	}
	if err := json.Unmarshal(raw, &tr); err != nil {
		return claudeCred{}, fmt.Errorf("token response parse: %w", err)
	}
	if tr.AccessToken == "" {
		return claudeCred{}, errors.New("token endpoint returned no access_token")
	}
	exp := int64(0)
	if tr.ExpiresIn > 0 {
		exp = time.Now().Add(time.Duration(tr.ExpiresIn) * time.Second).UnixMilli()
	}
	return claudeCred{AccessToken: tr.AccessToken, RefreshToken: tr.RefreshToken, ExpiresAt: exp}, nil
}

func (o *OAuth) read() (claudeCred, error) {
	b, err := os.ReadFile(o.credPath())
	if err != nil {
		return claudeCred{}, err
	}
	var w struct {
		C claudeCred `json:"claudeAiOauth"`
	}
	if err := json.Unmarshal(b, &w); err != nil {
		return claudeCred{}, err
	}
	return w.C, nil
}

func (o *OAuth) write(c claudeCred) error {
	wrapped, _ := json.Marshal(struct {
		C claudeCred `json:"claudeAiOauth"`
	}{c})
	return o.store.ImportCredential("claude-code", claudeCredRel, wrapped)
}

func (o *OAuth) credPath() string {
	return o.store.Dir("claude-code") + "/" + claudeCredRel
}

func (o *OAuth) gcLocked() {
	for st, p := range o.pending {
		if time.Since(p.at) > 15*time.Minute {
			delete(o.pending, st)
		}
	}
}

// --- PKCE helpers ---------------------------------------------------

func randURL(n int) string {
	b := make([]byte, n)
	_, _ = rand.Read(b)
	return base64.RawURLEncoding.EncodeToString(b)
}

func s256(v string) string {
	h := sha256.Sum256([]byte(v))
	return base64.RawURLEncoding.EncodeToString(h[:])
}

func snippet(b []byte) string {
	s := strings.TrimSpace(string(b))
	if len(s) > 200 {
		s = s[:200]
	}
	return s
}
