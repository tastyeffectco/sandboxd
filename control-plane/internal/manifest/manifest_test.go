package manifest

import (
	"strings"
	"testing"
)

func TestParseWebPort(t *testing.T) {
	cases := []struct {
		name string
		yaml string
		port int
	}{
		{"astro 4321", "version: 1\nweb:\n  command: \"astro dev\"\n  port: 4321\n  health_path: /\n", 4321},
		{"vite 3000", "version: 1\nweb:\n  command: \"pnpm dev\"\n  port: 3000\n", 3000},
		{"worker-only (no web)", "version: 1\nworkers:\n  - name: w\n    command: \"bash worker.sh\"\n", 0},
		{"web without port", "version: 1\nweb:\n  command: \"pnpm dev\"\n", 0},
		{"empty", "", 0},
	}
	for _, c := range cases {
		m, err := Parse([]byte(c.yaml))
		if err != nil {
			t.Fatalf("%s: parse err: %v", c.name, err)
		}
		if got := m.WebPort(); got != c.port {
			t.Errorf("%s: WebPort = %d; want %d", c.name, got, c.port)
		}
	}
}

func TestParseInvalidYAML(t *testing.T) {
	if _, err := Parse([]byte("web: [this is: not valid")); err == nil {
		t.Error("expected a parse error for malformed yaml")
	}
}

// A nil manifest is safe (WebPort 0).
func TestNilWebPort(t *testing.T) {
	var m *Manifest
	if m.WebPort() != 0 {
		t.Error("nil manifest WebPort should be 0")
	}
}

// --- Validate ---------------------------------------------------------

func hasMatch(ss []string, sub string) bool {
	for _, s := range ss {
		if strings.Contains(s, sub) {
			return true
		}
	}
	return false
}

func TestValidateGoodManifest(t *testing.T) {
	r := Validate([]byte("version: 1\nweb:\n  command: \"pnpm dev --host 0.0.0.0 --port 3000\"\n  port: 3000\n  health_path: \"/\"\n"))
	if !r.Valid || len(r.Errors) != 0 {
		t.Fatalf("expected valid, got %+v", r)
	}
	if len(r.Warnings) != 0 {
		t.Errorf("clean manifest should have no warnings: %v", r.Warnings)
	}
	if r.Effective == nil || r.Effective.Web == nil || r.Effective.Web.Port != 3000 || r.Effective.Web.HealthPath != "/" {
		t.Errorf("effective wrong: %+v", r.Effective)
	}
}

func TestValidateTopLevelCommandRejected(t *testing.T) {
	r := Validate([]byte("version: 1\ncommand: \"pnpm dev\"\n"))
	if r.Valid || !hasMatch(r.Errors, "web.command") {
		t.Errorf("top-level command must be an error pointing to web.command: %+v", r)
	}
}

func TestValidateUnknownTopLevelWarns(t *testing.T) {
	r := Validate([]byte("version: 1\nweb:\n  command: x\n  port: 3000\nwbe: oops\n"))
	if !r.Valid {
		t.Errorf("unknown key should NOT invalidate (forward-compat): %+v", r)
	}
	if !hasMatch(r.Warnings, "wbe") {
		t.Errorf("unknown key should warn: %v", r.Warnings)
	}
}

func TestValidateInvalidYAML(t *testing.T) {
	r := Validate([]byte("web: [this is: not valid"))
	if r.Valid || !hasMatch(r.Errors, "invalid YAML") {
		t.Errorf("expected invalid YAML error: %+v", r)
	}
}

func TestValidatePortRange(t *testing.T) {
	r := Validate([]byte("version: 1\nweb:\n  command: x\n  port: 70000\n"))
	if r.Valid || !hasMatch(r.Errors, "out of range") {
		t.Errorf("expected port range error: %+v", r)
	}
}

func TestValidateMissingPortIsError(t *testing.T) {
	r := Validate([]byte("version: 1\nweb:\n  command: \"pnpm dev --host 0.0.0.0\"\n"))
	if r.Valid || !hasMatch(r.Errors, "must declare web.port") {
		t.Errorf("a custom web.command without web.port must be an error: %+v", r)
	}
	// invalid -> no effective view exposed
	if r.Effective != nil {
		t.Errorf("effective must be omitted for an invalid manifest: %+v", r.Effective)
	}
}

func TestValidateEffectiveOmittedWhenInvalid(t *testing.T) {
	r := Validate([]byte("version: 1\nweb:\n  command: x\n  port: 70000\n"))
	if r.Valid || r.Effective != nil {
		t.Errorf("invalid manifest must not expose effective: %+v", r)
	}
}

func TestValidateLocalhostWarns(t *testing.T) {
	r := Validate([]byte("version: 1\nweb:\n  command: \"pnpm dev --host localhost\"\n  port: 3000\n"))
	if !hasMatch(r.Warnings, "localhost") {
		t.Errorf("expected localhost-bind warning: %v", r.Warnings)
	}
}

func TestValidatePortMismatchWarns(t *testing.T) {
	r := Validate([]byte("version: 1\nweb:\n  command: \"pnpm dev --host 0.0.0.0 --port 5173\"\n  port: 3000\n"))
	if !hasMatch(r.Warnings, "5173") {
		t.Errorf("expected port-mismatch warning: %v", r.Warnings)
	}
}

func TestValidateWorkerRules(t *testing.T) {
	r := Validate([]byte("version: 1\nworkers:\n  - name: \"bad name!\"\n    command: \"\"\n"))
	if r.Valid {
		t.Error("invalid worker name + empty command should be errors")
	}
	if !hasMatch(r.Errors, "invalid worker name") || !hasMatch(r.Errors, "no command") {
		t.Errorf("expected worker errors: %v", r.Errors)
	}
	// valid worker passes
	ok := Validate([]byte("version: 1\nworkers:\n  - name: queue\n    command: \"node worker.js\"\n"))
	if !ok.Valid {
		t.Errorf("valid worker should pass: %+v", ok)
	}
}
