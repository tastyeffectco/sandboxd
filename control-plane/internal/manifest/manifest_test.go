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

// Unknown top-level keys are ERRORS in v1 (closed key set) — they used to warn,
// which let processes:/services: parse to an empty runtime and silently run the
// default template (QA footgun).
func TestValidateUnknownTopLevelErrors(t *testing.T) {
	r := Validate([]byte("version: 1\nweb:\n  command: x\n  port: 3000\nwbe: oops\n"))
	if r.Valid || !hasMatch(r.Errors, "wbe") {
		t.Errorf("unknown top-level key must be an error: %+v", r)
	}
}

// The exact footgun keys get pointed errors; none silently validate.
func TestValidateFootgunTopLevelKeys(t *testing.T) {
	cases := []struct{ yaml, want string }{
		{"version: 1\nprocesses:\n  - cmd: x\n", "workers"},                      // did you mean workers/web
		{"version: 1\nservices:\n  db: { image: postgres }\n", "Docker Compose"}, // not compose
		{"version: 1\ncommand: \"pnpm dev\"\n", "web.command"},                   // belongs under web
		{"version: 1\nworkers:\n  - name: w\n    command: x\n", ""},              // worker-only is VALID
		{"version: 1\nweb:\n  command: x\n  port: 3000\n", ""},                   // web is VALID
		{"version: 1\n", "nothing to run"},                                       // no web/workers
	}
	for _, c := range cases {
		r := Validate([]byte(c.yaml))
		if c.want == "" {
			if !r.Valid {
				t.Errorf("expected valid for %q: %+v", c.yaml, r)
			}
			continue
		}
		if r.Valid || !hasMatch(r.Errors, c.want) {
			t.Errorf("expected invalid (%q) for %q: %+v", c.want, c.yaml, r)
		}
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
		t.Errorf("expected localhost-bind warning for explicit localhost: %v", r.Warnings)
	}
}

// No false-positive "may bind localhost" for servers that bind all interfaces by
// default (Nest, node/express, Bun, uvicorn-with-host).
func TestValidateNoLocalhostFalsePositive(t *testing.T) {
	for _, cmd := range []string{
		"pnpm exec nest start",
		"node server.js",
		"bun run src/index.ts",
		".venv/bin/uvicorn main:app --host 0.0.0.0 --port 3000",
	} {
		r := Validate([]byte("version: 1\nweb:\n  command: \"" + cmd + "\"\n  port: 3000\n"))
		if hasMatch(r.Warnings, "localhost") {
			t.Errorf("false-positive localhost warning for %q: %v", cmd, r.Warnings)
		}
	}
}

// `parsed` (as-declared) is present even for an invalid manifest, so a caller can
// confirm the web command was understood; `effective` stays omitted when invalid.
func TestValidateParsedAlwaysPresent(t *testing.T) {
	r := Validate([]byte("version: 1\nweb:\n  command: \"pnpm dev --host 0.0.0.0\"\n")) // no port -> invalid
	if r.Valid || r.Effective != nil {
		t.Fatalf("expected invalid + no effective: %+v", r)
	}
	if r.Parsed == nil || r.Parsed.Web == nil || r.Parsed.Web.Command == "" {
		t.Fatalf("parsed.web should echo the declared command: %+v", r.Parsed)
	}
	if r.Parsed.Web.Port != 0 {
		t.Errorf("parsed.web.port should be the as-declared 0 (no default): %+v", r.Parsed.Web)
	}
}

// version is required and must be 1 (QA footgun: a versionless manifest parsed
// to empty {workers:[]} and returned valid:true).
func TestValidateMissingVersionInvalid(t *testing.T) {
	r := Validate([]byte("web:\n  command: \"pnpm dev --host 0.0.0.0\"\n  port: 3000\n"))
	if r.Valid || !hasMatch(r.Errors, "version") {
		t.Errorf("a manifest with no version must be invalid: %+v", r)
	}
	if r.Effective != nil {
		t.Errorf("no effective view for an invalid (versionless) manifest: %+v", r.Effective)
	}
}

func TestValidateUnsupportedVersionInvalid(t *testing.T) {
	r := Validate([]byte("version: 2\nweb:\n  command: x\n  port: 3000\n"))
	if r.Valid || !hasMatch(r.Errors, "unsupported version") {
		t.Errorf("version 2 must be invalid: %+v", r)
	}
}

func TestValidateVersion1Valid(t *testing.T) {
	r := Validate([]byte("version: 1\nweb:\n  command: \"node server.js\"\n  port: 3000\n"))
	if !r.Valid {
		t.Errorf("version 1 must be valid: %+v", r)
	}
}

// An empty manifest is invalid (it has no version) — there is no supported
// empty case.
func TestValidateEmptyInvalid(t *testing.T) {
	r := Validate([]byte(""))
	if r.Valid || !hasMatch(r.Errors, "version") {
		t.Errorf("empty manifest must be invalid (missing version): %+v", r)
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
