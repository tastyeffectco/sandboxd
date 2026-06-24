package api

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

// These tests pin the JSON SHAPES the console (and any /v1 client) renders.
// sandboxd is the contract; a client mocks exactly these shapes. If a field is
// renamed/removed here, the client's fixtures must change too — so this is where
// such a break should fail first.

// GET /v1/sandboxes/{id} → the object the console's app-detail screen reads.
func TestV1SandboxResponseShape(t *testing.T) {
	s, id, _ := newProcLogsServer(t) // Server + a sandbox row (runtime unreachable in tests)
	r := httptest.NewRequest("GET", "/v1/sandboxes/"+id, nil)
	r.SetPathValue("id", id)
	w := httptest.NewRecorder()
	s.v1GetSandbox(w, r)
	if w.Code != http.StatusOK {
		t.Fatalf("got %d: %s", w.Code, w.Body)
	}
	var m map[string]any
	if err := json.Unmarshal(w.Body.Bytes(), &m); err != nil {
		t.Fatalf("invalid json: %v", err)
	}
	for _, k := range []string{"id", "status", "preview", "processes", "template", "created_at"} {
		if _, ok := m[k]; !ok {
			t.Errorf("v1 sandbox response missing %q: %s", k, w.Body)
		}
	}
	prev, ok := m["preview"].(map[string]any)
	if !ok {
		t.Fatalf("preview is not an object: %s", w.Body)
	}
	for _, k := range []string{"url", "status"} {
		if _, ok := prev[k]; !ok {
			t.Errorf("preview missing %q", k)
		}
	}
}

// GET /v1/presets → the list the console's New-App picker renders.
func TestV1PresetsResponseShape(t *testing.T) {
	s := newPresetTestServer(t)
	w := presetReq(s, "GET", "/v1/presets", "", cfgTenant, nil, s.v1ListPresets)
	if w.Code != http.StatusOK {
		t.Fatalf("got %d", w.Code)
	}
	var d struct {
		Presets []map[string]any `json:"presets"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &d); err != nil {
		t.Fatalf("invalid json: %v", err)
	}
	// Assert the REQUIRED preset ids exist with the fields the picker renders —
	// not an exact count, so adding a preset later doesn't break this test.
	byID := map[string]map[string]any{}
	for _, p := range d.Presets {
		if id, ok := p["id"].(string); ok {
			byID[id] = p
		}
	}
	for _, id := range []string{"react-vite", "nextjs", "node-express", "fastapi", "worker"} {
		p, ok := byID[id]
		if !ok {
			t.Errorf("required preset %q missing", id)
			continue
		}
		for _, k := range []string{"id", "label", "description"} {
			if v, ok := p[k].(string); !ok || v == "" {
				t.Errorf("preset %q missing/empty %q", id, k)
			}
		}
	}
}
