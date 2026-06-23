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

	"github.com/sandboxd/control-plane/internal/auth"
	"github.com/sandboxd/control-plane/internal/store"
)

func newPresetTestServer(t *testing.T) *Server {
	t.Helper()
	st, err := store.Open(context.Background(), "file::memory:?_fk=1", "../../migrations")
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	t.Cleanup(func() { st.Close() })
	return &Server{Store: st, Log: slog.New(slog.NewTextHandler(io.Discard, nil))}
}

func presetReq(s *Server, method, target, body, tenant string, pv map[string]string, h http.HandlerFunc) *httptest.ResponseRecorder {
	var r *http.Request
	if body != "" {
		r = httptest.NewRequest(method, target, strings.NewReader(body))
	} else {
		r = httptest.NewRequest(method, target, nil)
	}
	r = r.WithContext(auth.WithActor(r.Context(), auth.Actor{Name: tenant, Kind: "service"}))
	for k, v := range pv {
		r.SetPathValue(k, v)
	}
	w := httptest.NewRecorder()
	h(w, r)
	return w
}

// GET /v1/presets lists the registry.
func TestListPresets(t *testing.T) {
	s := newPresetTestServer(t)
	w := presetReq(s, "GET", "/v1/presets", "", cfgTenant, nil, s.v1ListPresets)
	if w.Code != http.StatusOK {
		t.Fatalf("got %d", w.Code)
	}
	var got struct {
		Presets []v1Preset `json:"presets"`
	}
	json.Unmarshal(w.Body.Bytes(), &got)
	ids := map[string]bool{}
	for _, p := range got.Presets {
		ids[p.ID] = true
	}
	for _, want := range []string{"react-vite", "nextjs", "node-express", "fastapi", "worker"} {
		if !ids[want] {
			t.Errorf("missing preset %q in %v", want, ids)
		}
	}
}

// App create stores a valid runtime_preset; an unknown one is rejected.
func TestCreateAppRuntimePreset(t *testing.T) {
	s := newPresetTestServer(t)

	w := presetReq(s, "POST", "/v1/apps", `{"name":"API","runtime_preset":"fastapi"}`, cfgTenant, nil, s.v1CreateApp)
	if w.Code != http.StatusCreated {
		t.Fatalf("create: %d %s", w.Code, w.Body)
	}
	var app v1App
	json.Unmarshal(w.Body.Bytes(), &app)
	if app.RuntimePreset != "fastapi" {
		t.Errorf("runtime_preset not returned: %+v", app)
	}
	// Persisted on the row.
	got, _ := s.Store.GetAppForOwner(context.Background(), app.ID, cfgTenant)
	if !got.RuntimePreset.Valid || got.RuntimePreset.String != "fastapi" {
		t.Errorf("runtime_preset not stored: %+v", got.RuntimePreset)
	}

	// Unknown preset -> 400.
	if w := presetReq(s, "POST", "/v1/apps", `{"name":"X","runtime_preset":"bogus"}`, cfgTenant, nil, s.v1CreateApp); w.Code != http.StatusBadRequest {
		t.Errorf("unknown preset create = %d; want 400", w.Code)
	}
}

// Sandbox create rejects an explicit unknown preset before any sandbox work.
func TestCreateAppSandboxUnknownPreset(t *testing.T) {
	s := newPresetTestServer(t)
	app := &store.App{ID: newULID(), OwnerToken: cfgTenant, Name: "App"}
	if err := s.Store.CreateApp(context.Background(), app); err != nil {
		t.Fatal(err)
	}
	w := presetReq(s, "POST", "/v1/apps/"+app.ID+"/sandbox", `{"runtime_preset":"bogus"}`,
		cfgTenant, map[string]string{"id": app.ID}, s.v1CreateAppSandbox)
	if w.Code != http.StatusBadRequest {
		t.Errorf("unknown preset sandbox = %d; want 400 (%s)", w.Code, w.Body)
	}
}

// resolveRuntimePreset prefers the explicit value, else the app default.
func TestResolveRuntimePreset(t *testing.T) {
	if got := resolveRuntimePreset("nextjs", "fastapi"); got != "nextjs" {
		t.Errorf("explicit should win: %q", got)
	}
	if got := resolveRuntimePreset("", "fastapi"); got != "fastapi" {
		t.Errorf("should fall back to app default: %q", got)
	}
	if got := resolveRuntimePreset("", ""); got != "" {
		t.Errorf("no preset -> empty: %q", got)
	}
}
