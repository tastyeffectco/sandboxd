package api

import (
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"reflect"
	"strings"
	"testing"

	"github.com/tastyeffectco/sandboxd/control-plane/internal/instancecfg"
	"github.com/tastyeffectco/sandboxd/control-plane/internal/store"
)

func newSettingsServer(t *testing.T) *Server {
	t.Helper()
	st, err := store.Open(context.Background(), "file::memory:?_fk=1", "../../migrations")
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	t.Cleanup(func() { st.Close() })
	return &Server{
		Store: st,
		Log:   slog.New(slog.NewTextHandler(io.Discard, nil)),
		Live: instancecfg.New(instancecfg.Snapshot{
			IdleEnabled: true, IdleThresholdSeconds: 2100, KeepaliveMaxSeconds: 86400,
		}),
		Instance: InstanceInfo{Version: "test", AgentProviders: []string{"opencode"}},
	}
}

func patchSettings(s *Server, body string) *httptest.ResponseRecorder {
	r := httptest.NewRequest("PATCH", "/v1/settings", strings.NewReader(body))
	w := httptest.NewRecorder()
	s.v1PatchSettings(w, r)
	return w
}

// Only lifecycle tunables are editable; protected/unknown keys are rejected and
// nothing is mutated.
func TestPatchSettingsRejectsProtectedKeys(t *testing.T) {
	protected := []string{
		`{"auth":{"enabled":true}}`,
		`{"egress":{"mode":"enabled"}}`,
		`{"networking":{"preview_domain":"evil.example"}}`,
		`{"version":"9.9.9"}`,
		`{"capabilities":{"snapshots":true}}`,
		`{"runtime":{"base_image":"x"}}`,
		`{"lifecycle":{"idle_threshold_seconds":300,"secret_key":"x"}}`, // unknown nested key
	}
	for _, body := range protected {
		s := newSettingsServer(t)
		before := s.Live.Snapshot()
		w := patchSettings(s, body)
		if w.Code != http.StatusBadRequest {
			t.Errorf("body %s: got %d, want 400", body, w.Code)
		}
		if !reflect.DeepEqual(s.Live.Snapshot(), before) {
			t.Errorf("body %s mutated live settings", body)
		}
		if got, _ := s.Store.GetInstanceSettings(context.Background()); got != nil {
			t.Errorf("body %s persisted something: %+v", body, got)
		}
	}
}

// Out-of-range values are rejected.
func TestPatchSettingsValidation(t *testing.T) {
	for _, body := range []string{
		`{"lifecycle":{"idle_threshold_seconds":5}}`,       // below min
		`{"lifecycle":{"idle_threshold_seconds":999999}}`,  // above max
		`{"lifecycle":{"keepalive_max_seconds":-1}}`,       // negative
		`{"lifecycle":{"keepalive_max_seconds":99999999}}`, // above max
	} {
		s := newSettingsServer(t)
		if w := patchSettings(s, body); w.Code != http.StatusBadRequest {
			t.Errorf("body %s: got %d, want 400", body, w.Code)
		}
	}
}

// A valid PATCH persists, hot-applies (Live updated), and echoes the new state.
func TestPatchSettingsPersistsAndHotApplies(t *testing.T) {
	s := newSettingsServer(t)
	w := patchSettings(s, `{"lifecycle":{"idle_reap_enabled":false,"idle_threshold_seconds":600,"keepalive_max_seconds":120}}`)
	if w.Code != http.StatusOK {
		t.Fatalf("got %d: %s", w.Code, w.Body)
	}
	// Hot-applied to the shared Live config (the reaper/keepalive read this).
	got := s.Live.Snapshot()
	if got.IdleEnabled || got.IdleThresholdSeconds != 600 || got.KeepaliveMaxSeconds != 120 {
		t.Errorf("live not hot-applied: %+v", got)
	}
	// Persisted.
	p, err := s.Store.GetInstanceSettings(context.Background())
	if err != nil || p.IdleThresholdSeconds != 600 || p.IdleReapEnabled || p.KeepaliveMaxSeconds != 120 {
		t.Errorf("not persisted: %+v err=%v", p, err)
	}
	// Echo reflects the new values + marks them editable.
	var resp map[string]any
	json.Unmarshal(w.Body.Bytes(), &resp)
	life := resp["lifecycle"].(map[string]any)
	if life["idle_threshold_seconds"].(float64) != 600 || life["idle_reap_enabled"] != false {
		t.Errorf("echo wrong: %+v", life)
	}
	if len(resp["editable"].([]any)) == 0 {
		t.Error("editable list should be present")
	}
}

// Per-agent default models: a valid PATCH merges, persists, hot-applies, and
// echoes; an empty value clears one; other agents are preserved.
func TestPatchAgentDefaultModels(t *testing.T) {
	s := newSettingsServer(t)

	// Set two defaults.
	w := patchSettings(s, `{"agents":{"default_models":{"opencode":"glm-5","claude-code":"sonnet"}}}`)
	if w.Code != http.StatusOK {
		t.Fatalf("set: got %d: %s", w.Code, w.Body)
	}
	if got := s.Live.DefaultModel("opencode"); got != "glm-5" {
		t.Errorf("live opencode = %q; want glm-5", got)
	}
	p, _ := s.Store.GetInstanceSettings(context.Background())
	if p == nil || p.AgentDefaultModels["claude-code"] != "sonnet" {
		t.Errorf("not persisted: %+v", p)
	}

	// Merge: change opencode, leave claude-code untouched.
	w = patchSettings(s, `{"agents":{"default_models":{"opencode":"grok-4.5"}}}`)
	if w.Code != http.StatusOK {
		t.Fatalf("merge: got %d: %s", w.Code, w.Body)
	}
	if s.Live.DefaultModel("opencode") != "grok-4.5" || s.Live.DefaultModel("claude-code") != "sonnet" {
		t.Errorf("merge did not preserve others: %+v", s.Live.Snapshot().DefaultModels)
	}

	// Empty value clears a default.
	w = patchSettings(s, `{"agents":{"default_models":{"opencode":""}}}`)
	if w.Code != http.StatusOK {
		t.Fatalf("clear: got %d: %s", w.Code, w.Body)
	}
	if s.Live.DefaultModel("opencode") != "" {
		t.Errorf("opencode default not cleared: %q", s.Live.DefaultModel("opencode"))
	}
	if s.Live.DefaultModel("claude-code") != "sonnet" {
		t.Errorf("clear wiped an unrelated agent")
	}

	// Echo surfaces the map.
	var resp map[string]any
	json.Unmarshal(w.Body.Bytes(), &resp)
	if _, ok := resp["agents"].(map[string]any)["default_models"]; !ok {
		t.Error("echo missing agents.default_models")
	}
}

// Unknown agents and over-long model ids are rejected; lifecycle preserved.
func TestPatchAgentDefaultModelsValidation(t *testing.T) {
	s := newSettingsServer(t)
	long := `{"agents":{"default_models":{"opencode":"` + strings.Repeat("x", 201) + `"}}}`
	for _, body := range []string{
		`{"agents":{"default_models":{"bogus-agent":"x"}}}`, // not runnable
		long,
	} {
		if w := patchSettings(s, body); w.Code != http.StatusBadRequest {
			t.Errorf("body %s: got %d, want 400", body, w.Code)
		}
	}
	if got := s.Live.DefaultModel("opencode"); got != "" {
		t.Errorf("rejected patches must not persist: %q", got)
	}
}

// With no Live config wired (e.g. older deploy/tests), PATCH is unavailable.
func TestPatchSettingsUnavailableWithoutLive(t *testing.T) {
	s := &Server{Log: slog.New(slog.NewTextHandler(io.Discard, nil))}
	if w := patchSettings(s, `{"lifecycle":{"idle_threshold_seconds":600}}`); w.Code != http.StatusServiceUnavailable {
		t.Errorf("got %d, want 503", w.Code)
	}
}
