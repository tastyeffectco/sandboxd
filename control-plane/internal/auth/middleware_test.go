package auth

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
)

// stubResolver answers from fixed maps.
type stubResolver struct {
	sessions map[string]string // cookie value -> owner
	keys     map[string]string // presented key -> owner
}

func (s stubResolver) ResolveSession(_ context.Context, v string) (string, bool) {
	o, ok := s.sessions[v]
	return o, ok
}
func (s stubResolver) ResolveAPIKey(_ context.Context, v string) (string, bool) {
	o, ok := s.keys[v]
	return o, ok
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
