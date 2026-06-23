package api

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"

	"github.com/sandboxd/control-plane/internal/audit"
	"github.com/sandboxd/control-plane/internal/auth"
	"github.com/sandboxd/control-plane/internal/secrets"
	"github.com/sandboxd/control-plane/internal/store"
)

const cfgTenant = "tenant-1"

func newConfigTestServer(t *testing.T) (*Server, string) {
	t.Helper()
	st, err := store.Open(context.Background(), "file::memory:?_fk=1", "../../migrations")
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	t.Cleanup(func() { st.Close() })
	cipher, err := secrets.Load("", filepath.Join(t.TempDir(), "secrets.key"))
	if err != nil {
		t.Fatal(err)
	}
	s := &Server{Store: st, Secrets: cipher, Log: slog.New(slog.NewTextHandler(io.Discard, nil))}
	app := &store.App{ID: "01APPCFG0000000000000001", OwnerToken: cfgTenant, Name: "App"}
	if err := st.CreateApp(context.Background(), app); err != nil {
		t.Fatal(err)
	}
	return s, app.ID
}

func do(s *Server, method, target, body, tenant string, pathVals map[string]string) *httptest.ResponseRecorder {
	var r *http.Request
	if body != "" {
		r = httptest.NewRequest(method, target, strings.NewReader(body))
	} else {
		r = httptest.NewRequest(method, target, nil)
	}
	r = r.WithContext(auth.WithActor(r.Context(), auth.Actor{Name: tenant, Kind: "service"}))
	for k, v := range pathVals {
		r.SetPathValue(k, v)
	}
	w := httptest.NewRecorder()
	switch {
	case method == "POST" && strings.HasSuffix(target, "/config"):
		s.v1CreateAppConfig(w, r)
	case method == "GET":
		s.v1ListAppConfig(w, r)
	case method == "PATCH":
		s.v1PatchAppConfig(w, r)
	case method == "DELETE":
		s.v1DeleteAppConfig(w, r)
	}
	return w
}

func TestAppConfigSensitiveEncryptedAndRedacted(t *testing.T) {
	s, appID := newConfigTestServer(t)
	const secret = "sk-super-secret-9999"

	w := do(s, "POST", "/v1/apps/"+appID+"/config",
		`{"key":"OPENAI_API_KEY","value":"`+secret+`","sensitive":true}`,
		cfgTenant, map[string]string{"id": appID})
	if w.Code != http.StatusCreated {
		t.Fatalf("create: %d %s", w.Code, w.Body)
	}
	if strings.Contains(w.Body.String(), secret) {
		t.Fatal("create response leaked the plaintext secret")
	}

	// Encrypted at rest: ciphertext present, no plaintext column, and the
	// stored bytes don't contain the secret.
	c, err := s.Store.GetAppConfig(context.Background(), appID, "OPENAI_API_KEY")
	if err != nil {
		t.Fatal(err)
	}
	if len(c.ValueCiphertext) == 0 || c.ValuePlaintext.Valid {
		t.Errorf("sensitive value not stored as ciphertext: %+v", c)
	}
	if bytes.Contains(c.ValueCiphertext, []byte(secret)) {
		t.Error("ciphertext contains the plaintext secret")
	}
	if c.AccessPolicy != "control_plane_only" {
		t.Errorf("default access_policy = %q; want control_plane_only", c.AccessPolicy)
	}

	// GET returns metadata only — never the plaintext.
	g := do(s, "GET", "/v1/apps/"+appID+"/config", "", cfgTenant, map[string]string{"id": appID})
	if strings.Contains(g.Body.String(), secret) {
		t.Fatal("GET leaked the plaintext secret")
	}
	var got struct {
		Config []v1ConfigItem `json:"config"`
	}
	json.Unmarshal(g.Body.Bytes(), &got)
	if len(got.Config) != 1 || got.Config[0].Value != nil || !got.Config[0].ValueSet || !got.Config[0].Sensitive {
		t.Errorf("redaction wrong: %+v", got.Config)
	}
}

func TestAppConfigNonSensitiveReturnsValue(t *testing.T) {
	s, appID := newConfigTestServer(t)
	do(s, "POST", "/v1/apps/"+appID+"/config",
		`{"key":"API_URL","value":"https://api.example.com","sensitive":false,"access_policy":"runtime_access"}`,
		cfgTenant, map[string]string{"id": appID})

	g := do(s, "GET", "/v1/apps/"+appID+"/config", "", cfgTenant, map[string]string{"id": appID})
	var got struct {
		Config []v1ConfigItem `json:"config"`
	}
	json.Unmarshal(g.Body.Bytes(), &got)
	if len(got.Config) != 1 || got.Config[0].Value == nil || *got.Config[0].Value != "https://api.example.com" {
		t.Errorf("non-sensitive value not returned: %+v", got.Config)
	}
	if got.Config[0].AccessPolicy != "runtime_access" {
		t.Errorf("access_policy = %q; want runtime_access", got.Config[0].AccessPolicy)
	}
}

func TestAppConfigTenantScoping(t *testing.T) {
	s, appID := newConfigTestServer(t)
	do(s, "POST", "/v1/apps/"+appID+"/config", `{"key":"K","value":"v"}`, cfgTenant, map[string]string{"id": appID})

	// A different tenant cannot read this app's config — the app is 404 to them.
	w := do(s, "GET", "/v1/apps/"+appID+"/config", "", "tenant-2", map[string]string{"id": appID})
	if w.Code != http.StatusNotFound {
		t.Errorf("cross-tenant GET = %d; want 404", w.Code)
	}
	// ...nor write it.
	c := do(s, "POST", "/v1/apps/"+appID+"/config", `{"key":"X","value":"v"}`, "tenant-2", map[string]string{"id": appID})
	if c.Code != http.StatusNotFound {
		t.Errorf("cross-tenant POST = %d; want 404", c.Code)
	}
}

func TestAppConfigToggleSensitivityReEncrypts(t *testing.T) {
	s, appID := newConfigTestServer(t)
	// Start non-sensitive (plaintext stored), then mark sensitive via PATCH.
	do(s, "POST", "/v1/apps/"+appID+"/config", `{"key":"TOK","value":"plain-then-secret"}`, cfgTenant, map[string]string{"id": appID})
	do(s, "PATCH", "/v1/apps/"+appID+"/config/TOK", `{"sensitive":true}`, cfgTenant, map[string]string{"id": appID, "key": "TOK"})

	c, _ := s.Store.GetAppConfig(context.Background(), appID, "TOK")
	if !c.Sensitive || len(c.ValueCiphertext) == 0 || c.ValuePlaintext.Valid {
		t.Errorf("toggle to sensitive did not encrypt: %+v", c)
	}
	pt, err := s.Secrets.Open(c.ValueCiphertext, c.ValueNonce)
	if err != nil || string(pt) != "plain-then-secret" {
		t.Errorf("re-encrypted value wrong: %q %v", pt, err)
	}
}

// A PATCH that doesn't include `value` must not touch the stored secret.
func TestAppConfigPatchPreservesSecretWithoutValue(t *testing.T) {
	s, appID := newConfigTestServer(t)
	const secret = "sk-keep-me-2222"
	do(s, "POST", "/v1/apps/"+appID+"/config",
		`{"key":"K","value":"`+secret+`","sensitive":true,"access_policy":"control_plane_only"}`,
		cfgTenant, map[string]string{"id": appID})
	before, _ := s.Store.GetAppConfig(context.Background(), appID, "K")

	// Change only the access policy — no `value` field present.
	w := do(s, "PATCH", "/v1/apps/"+appID+"/config/K", `{"access_policy":"agent_access"}`,
		cfgTenant, map[string]string{"id": appID, "key": "K"})
	if w.Code != http.StatusOK {
		t.Fatalf("patch: %d %s", w.Code, w.Body)
	}
	after, _ := s.Store.GetAppConfig(context.Background(), appID, "K")
	if after.AccessPolicy != "agent_access" {
		t.Errorf("policy not updated: %q", after.AccessPolicy)
	}
	if !bytes.Equal(after.ValueCiphertext, before.ValueCiphertext) || !bytes.Equal(after.ValueNonce, before.ValueNonce) {
		t.Error("policy-only PATCH changed the stored secret bytes")
	}
	pt, err := s.Secrets.Open(after.ValueCiphertext, after.ValueNonce)
	if err != nil || string(pt) != secret {
		t.Errorf("secret altered by a value-less PATCH: %q %v", pt, err)
	}
}

type captureAudit struct{ rows []string }

func (c *captureAudit) InsertAudit(_ context.Context, _ int64, _, _, _, _, action, target, detail string) error {
	c.rows = append(c.rows, action+" "+target+" "+detail)
	return nil
}

// Audit rows record the config key, never the secret value.
func TestAppConfigAuditRecordsKeyNotValue(t *testing.T) {
	s, appID := newConfigTestServer(t)
	cap := &captureAudit{}
	s.Audit = audit.New(cap, slog.New(slog.NewTextHandler(io.Discard, nil)))

	const secret = "sk-not-in-audit-7777"
	do(s, "POST", "/v1/apps/"+appID+"/config",
		`{"key":"OPENAI_API_KEY","value":"`+secret+`","sensitive":true}`,
		cfgTenant, map[string]string{"id": appID})

	if len(cap.rows) == 0 {
		t.Fatal("no audit row written for config create")
	}
	joined := strings.Join(cap.rows, "\n")
	if !strings.Contains(joined, "OPENAI_API_KEY") {
		t.Errorf("audit missing the key name: %s", joined)
	}
	if strings.Contains(joined, secret) {
		t.Errorf("audit leaked the secret value: %s", joined)
	}
}

// TestAppConfigSecretDoesNotLeak is the v0.3.0 CI safety guard for
// app-scoped secrets. It runs the full lifecycle for one sensitive value
// and asserts the secret never escapes through any of the four leak
// vectors at once — API responses, DB plaintext columns, audit rows, and
// server logs — while a non-sensitive value still round-trips plainly.
// The other tests in this file cover encryption/redaction in depth; this
// one is the single consolidated no-leak assertion and adds the log-output
// vector the others don't capture.
func TestAppConfigSecretDoesNotLeak(t *testing.T) {
	const secret = "sk-test-secret-ci"

	st, err := store.Open(context.Background(), "file::memory:?_fk=1", "../../migrations")
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	t.Cleanup(func() { st.Close() })
	cipher, err := secrets.Load("", filepath.Join(t.TempDir(), "secrets.key"))
	if err != nil {
		t.Fatal(err)
	}
	// Capture everything the server logs so we can prove the secret never
	// reaches log output (handler logs + the audit logger's own warnings).
	var logBuf bytes.Buffer
	logger := slog.New(slog.NewTextHandler(&logBuf, &slog.HandlerOptions{Level: slog.LevelDebug}))
	capAudit := &captureAudit{}
	s := &Server{Store: st, Secrets: cipher, Log: logger, Audit: audit.New(capAudit, logger)}

	// 1. Create an app.
	app := &store.App{ID: "01APPCFG0000000000000010", OwnerToken: cfgTenant, Name: "App"}
	if err := st.CreateApp(context.Background(), app); err != nil {
		t.Fatal(err)
	}
	cfgURL := "/v1/apps/" + app.ID + "/config"
	pv := map[string]string{"id": app.ID}

	// 2. Create a sensitive config value.
	w := do(s, "POST", cfgURL, `{"key":"OPENAI_API_KEY","value":"`+secret+`","sensitive":true}`, cfgTenant, pv)
	if w.Code != http.StatusCreated {
		t.Fatalf("create sensitive: %d %s", w.Code, w.Body)
	}
	// 3. The create response and a subsequent GET must not return the secret.
	if strings.Contains(w.Body.String(), secret) {
		t.Error("API: create response leaked the secret")
	}
	g := do(s, "GET", cfgURL, "", cfgTenant, pv)
	if strings.Contains(g.Body.String(), secret) {
		t.Error("API: GET /config leaked the secret")
	}

	// 4. The stored row must hold ciphertext + nonce and no plaintext.
	c, err := st.GetAppConfig(context.Background(), app.ID, "OPENAI_API_KEY")
	if err != nil {
		t.Fatal(err)
	}
	if len(c.ValueCiphertext) == 0 || len(c.ValueNonce) == 0 {
		t.Errorf("DB: sensitive row missing ciphertext/nonce: ct=%d nonce=%d", len(c.ValueCiphertext), len(c.ValueNonce))
	}
	if c.ValuePlaintext.Valid {
		t.Error("DB: sensitive value stored in the plaintext column")
	}
	if bytes.Contains(c.ValueCiphertext, []byte(secret)) {
		t.Error("DB: ciphertext contains the plaintext secret")
	}
	if c.AccessPolicy != "control_plane_only" {
		t.Errorf("default access_policy = %q; want control_plane_only", c.AccessPolicy)
	}

	// 5. Audit rows record the key name, never the value.
	auditRows := strings.Join(capAudit.rows, "\n")
	if !strings.Contains(auditRows, "OPENAI_API_KEY") {
		t.Errorf("audit: missing the key name: %s", auditRows)
	}
	if strings.Contains(auditRows, secret) {
		t.Errorf("audit: leaked the secret value: %s", auditRows)
	}

	// 6. A non-sensitive value still round-trips plainly.
	do(s, "POST", cfgURL, `{"key":"API_URL","value":"https://api.example.com","sensitive":false}`, cfgTenant, pv)
	g2 := do(s, "GET", cfgURL, "", cfgTenant, pv)
	if !strings.Contains(g2.Body.String(), "https://api.example.com") {
		t.Error("non-sensitive value was not returned plainly")
	}

	// Logs (handler + audit logger) must never contain the secret.
	if strings.Contains(logBuf.String(), secret) {
		t.Errorf("logs leaked the secret:\n%s", logBuf.String())
	}
}
