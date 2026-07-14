package api

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/tastyeffectco/sandboxd/control-plane/internal/secrets"
	"github.com/tastyeffectco/sandboxd/control-plane/internal/telemetry"
)

// GET /v1/settings returns a stable, safe shape and never leaks the encryption
// key (or any other secret) even when the cipher is configured.
func TestV1SettingsShapeAndNoSecretLeak(t *testing.T) {
	// A real cipher built from a known base64 key. The key string must not
	// appear anywhere in the settings response.
	key := base64.StdEncoding.EncodeToString([]byte("0123456789abcdef0123456789abcdef")) // 32 bytes
	cipher, err := secrets.Load(key, "")
	if err != nil {
		t.Fatalf("load cipher: %v", err)
	}

	s := &Server{
		PreviewDomain:  "ex.sslip.io",
		PublicHTTPPort: "18080",
		Image:          "sandboxd-base:test",
		PreviewTLS:     false,
		Secrets:        cipher, // capability present
		Snapshot:       nil,    // capability absent
		KeepaliveMax:   3600 * time.Second,
		Instance: InstanceInfo{
			Version: "v0.4.0", GitCommit: "abc1234", AuthEnabled: true,
			StorageMode: "directory", EgressMode: "disabled",
			AgentProviders: []string{"opencode"}, IdleReapEnabled: true, IdleThresholdSeconds: 2100,
		},
	}

	w := httptest.NewRecorder()
	s.v1GetSettings(w, httptest.NewRequest("GET", "/v1/settings", nil))
	if w.Code != http.StatusOK {
		t.Fatalf("got %d: %s", w.Code, w.Body)
	}
	body := w.Body.String()

	// No secret leak: the encryption key must never be in the response.
	if strings.Contains(body, key) {
		t.Fatal("settings response leaked the encryption key")
	}

	var m map[string]any
	if err := json.Unmarshal([]byte(body), &m); err != nil {
		t.Fatalf("invalid json: %v", err)
	}
	// Stable top-level shape.
	for _, k := range []string{"version", "networking", "auth", "runtime", "lifecycle", "egress", "agents", "presets", "capabilities"} {
		if _, ok := m[k]; !ok {
			t.Errorf("settings missing %q: %s", k, body)
		}
	}
	// Safe values surfaced correctly.
	if m["auth"].(map[string]any)["enabled"] != true {
		t.Error("auth.enabled should be true")
	}
	if got := m["networking"].(map[string]any)["preview_base"]; got != "http://*.preview.ex.sslip.io:18080" {
		t.Errorf("preview_base = %v", got)
	}
	caps := m["capabilities"].(map[string]any)
	if caps["config_secrets"] != true {
		t.Error("capabilities.config_secrets should be true (cipher set)")
	}
	if caps["snapshots"] != false {
		t.Error("capabilities.snapshots should be false (Snapshot nil)")
	}
	// Presets include the accepted ids (not an exact count).
	ids := map[string]bool{}
	for _, p := range m["presets"].([]any) {
		ids[p.(map[string]any)["id"].(string)] = true
	}
	for _, want := range []string{"react-vite", "nextjs", "fastapi", "worker"} {
		if !ids[want] {
			t.Errorf("settings presets missing %q", want)
		}
	}

	// No update checker wired → update_available is present and false.
	if m["update_available"] != false {
		t.Errorf("update_available should default to false, got %v", m["update_available"])
	}
}

// With an update checker whose cache holds a newer release, GET /v1/settings
// surfaces update_available + latest_version + changelog_url.
func TestV1SettingsUpdateAvailable(t *testing.T) {
	chk := &telemetry.Checker{
		Fetch: func(context.Context) (string, string, error) {
			return "v0.5.0", "https://github.com/tastyeffectco/sandboxd/releases/tag/v0.5.0", nil
		},
	}
	if _, _, err := chk.Latest(context.Background()); err != nil {
		t.Fatalf("warm checker cache: %v", err)
	}

	s := &Server{
		PreviewDomain: "ex.sslip.io",
		Image:         "sandboxd-base:test",
		Update:        chk,
		Instance:      InstanceInfo{Version: "v0.4.0", AgentProviders: []string{"opencode"}},
	}

	w := httptest.NewRecorder()
	s.v1GetSettings(w, httptest.NewRequest("GET", "/v1/settings", nil))
	if w.Code != http.StatusOK {
		t.Fatalf("got %d: %s", w.Code, w.Body)
	}
	var m map[string]any
	if err := json.Unmarshal(w.Body.Bytes(), &m); err != nil {
		t.Fatalf("invalid json: %v", err)
	}
	if m["update_available"] != true {
		t.Errorf("update_available should be true (v0.5.0 > v0.4.0), got %v", m["update_available"])
	}
	if m["latest_version"] != "v0.5.0" {
		t.Errorf("latest_version = %v", m["latest_version"])
	}
	if m["changelog_url"] == "" || m["changelog_url"] == nil {
		t.Errorf("changelog_url should be populated, got %v", m["changelog_url"])
	}
}
