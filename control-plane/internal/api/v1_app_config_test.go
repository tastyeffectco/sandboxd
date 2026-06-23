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
