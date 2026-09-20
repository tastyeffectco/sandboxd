package auth

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// stubResolver answers from fixed maps.
type stubResolver struct {
	sessions map[string]string // cookie value -> owner
	keys     map[string]string // presented key -> owner
	scopes   map[string][]string
}

func (s stubResolver) ResolveSession(_ context.Context, v string) (string, bool) {
	o, ok := s.sessions[v]
	return o, ok
}
func (s stubResolver) ResolveAPIKey(_ context.Context, v string) (string, []string, bool) {
	o, ok := s.keys[v]
	return o, s.scopes[v], ok
}

func newMW(disabled bool, res CredentialResolver) *Middleware {
	return NewMiddleware(&Config{Disabled: disabled}, res, nil, nil)
}

// echo handler records the actor it saw.
func echoActor(seen *Actor) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		*seen = ActorFrom(r.Context())
		w.WriteHeader(http.StatusOK)
	})
}

func TestMiddlewareUniformEnforcement(t *testing.T) {
	res := stubResolver{
		sessions: map[string]string{"good-cookie": "default"},
		keys:     map[string]string{"good-key": "default"},
	}
	mw := newMW(false, res)

	cases := []struct {
		name       string
		path       string
		cookie     string
		bearer     string
		remoteAddr string
		wantCode   int
		wantKind   string
	}{
		{"no credential non-exempt", "/v1/apps", "", "", "203.0.113.9:5000", 401, ""},
		{"loopback no credential still 401", "/v1/apps", "", "", "127.0.0.1:5000", 401, ""},
		{"valid session cookie", "/v1/apps", "good-cookie", "", "203.0.113.9:5000", 200, "user"},
		{"valid bearer key", "/v1/apps", "", "good-key", "203.0.113.9:5000", 200, "service"},
		{"bad session + bad key", "/v1/apps", "nope", "nope", "203.0.113.9:5000", 401, ""},
		{"exempt path no credential", "/v1/auth/status", "", "", "203.0.113.9:5000", 200, "unknown"},
		{"version exempt no credential", "/version", "", "", "203.0.113.9:5000", 200, "unknown"},
		{"exempt path with session", "/v1/auth/status", "good-cookie", "", "203.0.113.9:5000", 200, "user"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			var seen Actor
			h := mw.Wrap(echoActor(&seen))
			r := httptest.NewRequest("GET", tc.path, nil)
			r.RemoteAddr = tc.remoteAddr
			if tc.cookie != "" {
				r.AddCookie(&http.Cookie{Name: SessionCookie, Value: tc.cookie})
			}
			if tc.bearer != "" {
				r.Header.Set("Authorization", "Bearer "+tc.bearer)
			}
			w := httptest.NewRecorder()
			h.ServeHTTP(w, r)
			if w.Code != tc.wantCode {
				t.Fatalf("code = %d, want %d", w.Code, tc.wantCode)
			}
			if tc.wantCode == 200 && tc.wantKind != "" && seen.Kind != tc.wantKind {
				t.Fatalf("actor kind = %q, want %q", seen.Kind, tc.wantKind)
			}
		})
	}
}

func TestMiddlewareDisabledRollback(t *testing.T) {
	mw := newMW(true, nil)
	var seen Actor
	h := mw.Wrap(echoActor(&seen))
	r := httptest.NewRequest("GET", "/v1/apps", nil)
	r.RemoteAddr = "203.0.113.9:5000"
	w := httptest.NewRecorder()
	h.ServeHTTP(w, r)
	if w.Code != 200 {
		t.Fatalf("disabled: code = %d, want 200", w.Code)
	}
	if seen.Name != "default" {
		t.Fatalf("disabled actor name = %q, want default", seen.Name)
	}
}

func TestScopeTable(t *testing.T) {
	allowed := []string{"GET /v1/upgrade", "POST /v1/upgrade", "GET /healthz", "GET /readyz", "GET /version", "GET /v1/auth/status"}
	for _, route := range allowed {
		m, p, _ := strings.Cut(route, " ")
		if !ScopesAllow([]string{ScopeUpgrade}, m, p) {
			t.Errorf("upgrade scope should allow %s", route)
		}
	}
	denied := []string{"GET /v1/sandboxes", "GET /v1/api-keys", "DELETE /v1/api-keys/x", "DELETE /v1/upgrade", "POST /v1/auth/status", "GET /v1/upgrade/"}
	for _, route := range denied {
		m, p, _ := strings.Cut(route, " ")
		if ScopesAllow([]string{ScopeUpgrade}, m, p) {
			t.Errorf("upgrade scope should deny %s", route)
		}
	}
	if !ScopesAllow(nil, "DELETE", "/v1/api-keys/x") {
		t.Error("empty scopes must be unrestricted")
	}
	if ScopesAllow([]string{"bogus"}, "GET", "/v1/upgrade") {
		t.Error("unknown scope must allow nothing")
	}
	if !ValidScope(ScopeUpgrade) || ValidScope("bogus") {
		t.Error("ValidScope")
	}
	if got := KnownScopes(); len(got) != 1 || got[0] != ScopeUpgrade {
		t.Errorf("KnownScopes = %v", got)
	}
}

func TestMiddlewareScopedKey(t *testing.T) {
	res := stubResolver{
		keys:   map[string]string{"upg-key": "default", "full-key": "default"},
		scopes: map[string][]string{"upg-key": {ScopeUpgrade}},
	}
	mw := newMW(false, res)
	cases := []struct {
		method, path, bearer string
		want                 int
	}{
		{"GET", "/v1/upgrade", "upg-key", 200},
		{"POST", "/v1/upgrade", "upg-key", 200},
		{"GET", "/v1/auth/status", "upg-key", 200},
		{"GET", "/v1/apps", "upg-key", 403},
		{"GET", "/v1/api-keys", "upg-key", 403},
		{"GET", "/v1/apps", "full-key", 200},
	}
	for _, tc := range cases {
		var seen Actor
		h := mw.Wrap(echoActor(&seen))
		r := httptest.NewRequest(tc.method, tc.path, nil)
		r.RemoteAddr = "203.0.113.9:5000"
		r.Header.Set("Authorization", "Bearer "+tc.bearer)
		w := httptest.NewRecorder()
		h.ServeHTTP(w, r)
		if w.Code != tc.want {
			t.Fatalf("%s %s with %s: code = %d, want %d (%s)", tc.method, tc.path, tc.bearer, w.Code, tc.want, w.Body.String())
		}
		if tc.want == 403 && !strings.Contains(w.Body.String(), `"this API key is limited to: upgrade"`) {
			t.Fatalf("403 body = %s", w.Body.String())
		}
		if tc.want == 200 && tc.bearer == "upg-key" && len(seen.Scopes) != 1 {
			t.Fatalf("actor scopes not propagated: %+v", seen)
		}
	}
}
