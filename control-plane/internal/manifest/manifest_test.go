package manifest

import "testing"

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
