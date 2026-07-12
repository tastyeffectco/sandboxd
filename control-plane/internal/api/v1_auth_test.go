package api

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/tastyeffectco/sandboxd/control-plane/internal/auth"
	"github.com/tastyeffectco/sandboxd/control-plane/internal/store"
)

// authTestServer builds a Server whose Handler is wrapped by a real auth
// middleware + store resolver, so cookie/bearer resolution is exercised end to
// end.
func authTestServer(t *testing.T) (http.Handler, *Server) {
	t.Helper()
	st, err := store.Open(context.Background(), "file::memory:?_fk=1", "../../migrations")
	if err != nil {
		t.Fatal(err)
	}
	s := &Server{Store: st}
	s.Auth = auth.NewMiddleware(&auth.Config{}, NewStoreResolver(st), nil, nil)
	return s.Auth.Wrap(s.Handler()), s
}

func doAuth(t *testing.T, h http.Handler, method, path, body, cookie, bearer string) *httptest.ResponseRecorder {
	t.Helper()
	var rdr *strings.Reader
	if body != "" {
		rdr = strings.NewReader(body)
	} else {
		rdr = strings.NewReader("")
	}
	r := httptest.NewRequest(method, path, rdr)
	r.RemoteAddr = "203.0.113.5:4000"
	if body != "" {
		r.Header.Set("Content-Type", "application/json")
	}
	if cookie != "" {
		r.AddCookie(&http.Cookie{Name: auth.SessionCookie, Value: cookie})
	}
	if bearer != "" {
		r.Header.Set("Authorization", "Bearer "+bearer)
	}
	w := httptest.NewRecorder()
	h.ServeHTTP(w, r)
	return w
}

// sessionFrom extracts the sbx_session cookie value from a response.
func sessionFrom(t *testing.T, w *httptest.ResponseRecorder) string {
	t.Helper()
	for _, c := range w.Result().Cookies() {
		if c.Name == auth.SessionCookie && c.Value != "" {
			return c.Value
		}
	}
	t.Fatalf("no %s cookie in response (code %d)", auth.SessionCookie, w.Code)
	return ""
}

func TestAuthSetupLoginFlow(t *testing.T) {
	h, _ := authTestServer(t)

	// status before setup: password not set, not authed
	w := doAuth(t, h, "GET", "/v1/auth/status", "", "", "")
	if w.Code != 200 || !strings.Contains(w.Body.String(), `"password_set":false`) {
		t.Fatalf("pre-setup status: %d %s", w.Code, w.Body.String())
	}

	// setup with a too-short password → 400
	if w := doAuth(t, h, "POST", "/v1/auth/setup", `{"password":"short"}`, "", ""); w.Code != 400 {
		t.Fatalf("short setup: want 400 got %d", w.Code)
	}

	// setup → 204 + session cookie
	w = doAuth(t, h, "POST", "/v1/auth/setup", `{"password":"correcthorse"}`, "", "")
	if w.Code != 204 {
		t.Fatalf("setup: want 204 got %d %s", w.Code, w.Body.String())
	}
	cookie := sessionFrom(t, w)

	// second setup → 409
	if w := doAuth(t, h, "POST", "/v1/auth/setup", `{"password":"anotherone"}`, "", ""); w.Code != 409 {
		t.Fatalf("second setup: want 409 got %d", w.Code)
	}

	// the session authorizes a protected route
	if w := doAuth(t, h, "GET", "/v1/api-keys", "", cookie, ""); w.Code != 200 {
		t.Fatalf("session on protected route: want 200 got %d", w.Code)
	}

	// status with the cookie: authenticated + password_set
	w = doAuth(t, h, "GET", "/v1/auth/status", "", cookie, "")
	if !strings.Contains(w.Body.String(), `"authenticated":true`) || !strings.Contains(w.Body.String(), `"password_set":true`) {
		t.Fatalf("authed status: %s", w.Body.String())
	}

	// login wrong password → 401
	if w := doAuth(t, h, "POST", "/v1/auth/login", `{"password":"nope"}`, "", ""); w.Code != 401 {
		t.Fatalf("bad login: want 401 got %d", w.Code)
	}
	// login right password → 204 + cookie
	if w := doAuth(t, h, "POST", "/v1/auth/login", `{"password":"correcthorse"}`, "", ""); w.Code != 204 {
		t.Fatalf("good login: want 204 got %d", w.Code)
	}

	// change password with bad current → 401
	if w := doAuth(t, h, "POST", "/v1/auth/password", `{"current_password":"x","new_password":"brandnewpass"}`, cookie, ""); w.Code != 401 {
		t.Fatalf("change-pw bad current: want 401 got %d", w.Code)
	}
	// change password ok → 204
	if w := doAuth(t, h, "POST", "/v1/auth/password", `{"current_password":"correcthorse","new_password":"brandnewpass"}`, cookie, ""); w.Code != 204 {
		t.Fatalf("change-pw: want 204 got %d %s", w.Code, w.Body.String())
	}
	// the OLD session was invalidated by the password change
	if w := doAuth(t, h, "GET", "/v1/api-keys", "", cookie, ""); w.Code != 401 {
		t.Fatalf("old session after pw change: want 401 got %d", w.Code)
	}
}

func TestNoCredentialIsUnauthorized(t *testing.T) {
	h, _ := authTestServer(t)
	if w := doAuth(t, h, "GET", "/v1/api-keys", "", "", ""); w.Code != 401 {
		t.Fatalf("no credential: want 401 got %d", w.Code)
	}
}
