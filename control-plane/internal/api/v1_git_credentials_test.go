package api

import (
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/tastyeffectco/sandboxd/control-plane/internal/audit"
	"github.com/tastyeffectco/sandboxd/control-plane/internal/auth"
	"github.com/tastyeffectco/sandboxd/control-plane/internal/secrets"
	"github.com/tastyeffectco/sandboxd/control-plane/internal/store"
)

// gitAuditSink records every InsertAudit call so a test can assert no secret
// reaches the audit log.
type gitAuditSink struct{ rows []string }

func (c *gitAuditSink) InsertAudit(_ context.Context, _ int64, kind, name, ip, ext, action, target, detail string) error {
	c.rows = append(c.rows, strings.Join([]string{kind, name, ip, ext, action, target, detail}, "|"))
	return nil
}

func TestGitCredAuditHasNoToken(t *testing.T) {
	ca := &gitAuditSink{}
	s := gitCredServer(t)
	s.Audit = audit.New(ca, slog.New(slog.NewTextHandler(io.Discard, nil)))
	w := httptest.NewRecorder()
	s.v1CreateGitCredential(w, reqAs("POST", "/v1/git-credentials", `{"name":"gh","token":"`+theToken+`"}`, "t"))
	if w.Code != http.StatusCreated {
		t.Fatalf("got %d", w.Code)
	}
	if len(ca.rows) == 0 {
		t.Fatal("no audit entry written")
	}
	for _, row := range ca.rows {
		if strings.Contains(row, theToken) {
			t.Fatalf("audit entry leaked the token: %s", row)
		}
	}
}

const theToken = "ghp_SUPERSECRET_pat_value_DO_NOT_LEAK_0123456789"

func memStore(t *testing.T) *store.Store {
	t.Helper()
	st, err := store.Open(context.Background(), "file::memory:?_fk=1", "../../migrations")
	if err != nil {
		t.Fatal(err)
	}
	return st
}

func gitCredServer(t *testing.T) *Server {
	t.Helper()
	cipher, err := secrets.Load("", t.TempDir()+"/secrets.key")
	if err != nil {
		t.Fatal(err)
	}
	return &Server{Store: memStore(t), Secrets: cipher}
}

// reqAs builds a request carrying an actor (tenant) in context.
func reqAs(method, path, body, actor string) *http.Request {
	r := httptest.NewRequest(method, path, strings.NewReader(body))
	r = r.WithContext(auth.WithActor(r.Context(), auth.Actor{Kind: "service", Name: actor}))
	return r
}

func TestGitCredCreateSealsAndNeverReturnsToken(t *testing.T) {
	s := gitCredServer(t)
	w := httptest.NewRecorder()
	s.v1CreateGitCredential(w, reqAs("POST", "/v1/git-credentials",
		`{"name":"gh","host":"github.com","username":"x-access-token","token":"`+theToken+`"}`, "tenantA"))
	if w.Code != http.StatusCreated {
		t.Fatalf("create: got %d: %s", w.Code, w.Body.String())
	}
	// (sec) token plaintext absent from the create JSON
	if strings.Contains(w.Body.String(), theToken) {
		t.Fatal("create response leaked the token")
	}
	if strings.Contains(w.Body.String(), "token") && !strings.Contains(w.Body.String(), "token_set") {
		t.Fatal("response should not echo a token field")
	}

	// (sec) token plaintext absent from the LIST JSON
	lw := httptest.NewRecorder()
	s.v1ListGitCredentials(lw, reqAs("GET", "/v1/git-credentials", "", "tenantA"))
	if lw.Code != http.StatusOK || strings.Contains(lw.Body.String(), theToken) {
		t.Fatalf("list leaked token or bad status %d", lw.Code)
	}

	// (sec) DB ciphertext is NOT the plaintext, and decrypts back to it
	creds, _ := s.Store.ListGitCredentials(context.Background(), "tenantA")
	if len(creds) != 1 {
		t.Fatalf("want 1 cred, got %d", len(creds))
	}
	enc, nonce, ok, err := s.Store.GetGitCredentialSecret(context.Background(), "tenantA", creds[0].ID)
	if err != nil || !ok {
		t.Fatalf("get secret: %v ok=%v", err, ok)
	}
	if strings.Contains(string(enc), theToken) {
		t.Fatal("DB ciphertext contains the plaintext token")
	}
	pt, err := s.Secrets.Open(enc, nonce)
	if err != nil || string(pt) != theToken {
		t.Fatalf("decrypt mismatch: %v", err)
	}
}

func TestGitCredCrossOwnerIsolation(t *testing.T) {
	s := gitCredServer(t)
	// tenantA creates a credential
	cw := httptest.NewRecorder()
	s.v1CreateGitCredential(cw, reqAs("POST", "/v1/git-credentials", `{"name":"gh","token":"`+theToken+`"}`, "tenantA"))
	idA := mustField(t, cw.Body.String(), "id")

	// tenantB cannot see it
	lw := httptest.NewRecorder()
	s.v1ListGitCredentials(lw, reqAs("GET", "/v1/git-credentials", "", "tenantB"))
	if strings.Contains(lw.Body.String(), idA) {
		t.Fatal("tenantB can see tenantA's credential")
	}
	// tenantB cannot delete it -> 404
	dw := httptest.NewRecorder()
	dr := reqAs("DELETE", "/v1/git-credentials/"+idA, "", "tenantB")
	dr.SetPathValue("id", idA)
	s.v1DeleteGitCredential(dw, dr)
	if dw.Code != http.StatusNotFound {
		t.Fatalf("cross-owner delete: got %d; want 404", dw.Code)
	}
	// owner can delete it -> 204
	ow := httptest.NewRecorder()
	or := reqAs("DELETE", "/v1/git-credentials/"+idA, "", "tenantA")
	or.SetPathValue("id", idA)
	s.v1DeleteGitCredential(ow, or)
	if ow.Code != http.StatusNoContent {
		t.Fatalf("owner delete: got %d; want 204", ow.Code)
	}
}

func TestGitCredValidation(t *testing.T) {
	s := gitCredServer(t)
	cases := []struct {
		body string
		want int
		why  string
	}{
		{`{"name":"gh","token":"` + theToken + `","provider":"github"}`, 400, "unknown field rejected"},
		{`{"name":"gh"}`, 400, "missing token"},
		{`{"name":"","token":"x"}`, 400, "empty name"},
		{"{\"name\":\"gh\",\"token\":\"ab\\ncd\"}", 400, "token with newline rejected"},
		{"{\"name\":\"gh\",\"token\":\"ab\\u0000cd\"}", 400, "token with NUL rejected"},
	}
	for _, c := range cases {
		w := httptest.NewRecorder()
		s.v1CreateGitCredential(w, reqAs("POST", "/v1/git-credentials", c.body, "t"))
		if w.Code != c.want {
			t.Errorf("%s: got %d want %d (%s)", c.why, w.Code, c.want, w.Body.String())
		}
	}
}

func TestGitCredDuplicateName409(t *testing.T) {
	s := gitCredServer(t)
	for i, want := range []int{http.StatusCreated, http.StatusConflict} {
		w := httptest.NewRecorder()
		s.v1CreateGitCredential(w, reqAs("POST", "/v1/git-credentials", `{"name":"dup","token":"`+theToken+`"}`, "t"))
		if w.Code != want {
			t.Errorf("create #%d: got %d want %d", i, w.Code, want)
		}
	}
}

func TestGitCredNilStoreAndSecrets503(t *testing.T) {
	// nil Store/Secrets => 503, never a panic
	empty := &Server{}
	for _, fn := range []http.HandlerFunc{empty.v1CreateGitCredential, empty.v1ListGitCredentials, empty.v1DeleteGitCredential} {
		w := httptest.NewRecorder()
		fn(w, reqAs("POST", "/v1/git-credentials", `{"name":"x","token":"y"}`, "t"))
		if w.Code != http.StatusServiceUnavailable {
			t.Errorf("got %d; want 503", w.Code)
		}
	}
	// Store present but Secrets nil => create still 503 (can't encrypt)
	s := &Server{Store: memStore(t)}
	w := httptest.NewRecorder()
	s.v1CreateGitCredential(w, reqAs("POST", "/v1/git-credentials", `{"name":"x","token":"y"}`, "t"))
	if w.Code != http.StatusServiceUnavailable {
		t.Errorf("nil secrets: got %d; want 503", w.Code)
	}
}

// mustField pulls a top-level string field out of a tiny JSON object.
func mustField(t *testing.T, body, field string) string {
	t.Helper()
	var m map[string]any
	if err := json.Unmarshal([]byte(body), &m); err != nil {
		t.Fatalf("json: %v", err)
	}
	v, _ := m[field].(string)
	if v == "" {
		t.Fatalf("field %q empty in %s", field, body)
	}
	return v
}
