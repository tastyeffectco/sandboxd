package authproxy

import (
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"

	"github.com/tastyeffectco/sandboxd/control-plane/internal/agentauth"
)

func writeCred(t *testing.T, st *agentauth.Store, body string) {
	t.Helper()
	dir := filepath.Join(st.Dir("claude-code"), ".claude")
	if err := os.MkdirAll(dir, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, ".credentials.json"), []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
}

func TestMergeBeta(t *testing.T) {
	if got := mergeBeta(""); got != oauthBeta {
		t.Errorf("empty => %q; want %q", got, oauthBeta)
	}
	if got := mergeBeta("foo-2024"); got != "foo-2024,"+oauthBeta {
		t.Errorf("append => %q", got)
	}
	if got := mergeBeta(oauthBeta); got != oauthBeta {
		t.Errorf("already present must not duplicate => %q", got)
	}
}

func TestToken(t *testing.T) {
	st := agentauth.NewStore(t.TempDir())
	p := New(st, nil)
	// No file => not connected.
	if _, ok := p.token(); ok {
		t.Error("no cred => token should be absent")
	}
	// Empty accessToken (the corruption case) => not connected.
	writeCred(t, st, `{"claudeAiOauth":{"accessToken":"","refreshToken":""}}`)
	if _, ok := p.token(); ok {
		t.Error("empty accessToken => token should be absent")
	}
	// Real token => present.
	writeCred(t, st, `{"claudeAiOauth":{"accessToken":"sk-ant-REAL","refreshToken":"r"}}`)
	tok, ok := p.token()
	if !ok || tok != "sk-ant-REAL" {
		t.Errorf("token = %q,%v; want sk-ant-REAL,true", tok, ok)
	}
}

// A request with no usable credential returns 401 with an actionable message,
// and never forwards.
func TestServeNoCredential(t *testing.T) {
	st := agentauth.NewStore(t.TempDir())
	p := New(st, nil)
	w := httptest.NewRecorder()
	p.ServeHTTP(w, httptest.NewRequest("POST", "/v1/messages", nil))
	if w.Code != http.StatusUnauthorized {
		t.Fatalf("got %d; want 401", w.Code)
	}
}

// The proxy injects the real bearer + oauth beta and drops the sandbox's dummy
// key before forwarding — verified against a stub "Anthropic".
func TestInjectsRealBearer(t *testing.T) {
	var gotAuth, gotKey, gotBeta string
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotAuth = r.Header.Get("Authorization")
		gotKey = r.Header.Get("X-Api-Key")
		gotBeta = r.Header.Get("anthropic-beta")
		w.WriteHeader(200)
	}))
	defer upstream.Close()

	st := agentauth.NewStore(t.TempDir())
	writeCred(t, st, `{"claudeAiOauth":{"accessToken":"REAL-BEARER"}}`)
	p := New(st, nil)
	// Point the reverse proxy at the stub instead of api.anthropic.com.
	target := upstream.URL
	p.rp.Director = func(r *http.Request) {
		r.URL.Scheme = "http"
		r.URL.Host = target[len("http://"):]
		r.Host = r.URL.Host
	}

	req := httptest.NewRequest("POST", "/v1/messages", nil)
	req.Header.Set("X-Api-Key", "sandboxd-proxy-injected") // the dummy the sandbox sent
	w := httptest.NewRecorder()
	p.ServeHTTP(w, req)

	if gotAuth != "Bearer REAL-BEARER" {
		t.Errorf("Authorization = %q; want injected real bearer", gotAuth)
	}
	if gotKey != "" {
		t.Errorf("X-Api-Key leaked to upstream: %q (dummy must be stripped)", gotKey)
	}
	if gotBeta != oauthBeta {
		t.Errorf("anthropic-beta = %q; want %q", gotBeta, oauthBeta)
	}
}
