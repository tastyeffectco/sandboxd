package api

import (
	"os"
	"path/filepath"
	"regexp"
	"runtime"
	"sort"
	"strings"
	"testing"
)

// TestOpenAPIContractMatchesRoutes is the console<->sandboxd contract
// guard: every public /v1 route registered in api.go must be documented
// in docs/openapi.yaml, and vice versa. The console (and any external
// integration) binds to that spec, so drift in either direction is a
// breaking-change waiting to happen.
func TestOpenAPIContractMatchesRoutes(t *testing.T) {
	_, thisFile, _, _ := runtime.Caller(0)
	dir := filepath.Dir(thisFile)
	specPath := filepath.Join(dir, "..", "..", "..", "docs", "openapi.yaml")
	apiPath := filepath.Join(dir, "api.go")

	routes := v1RoutesFromSource(t, apiPath)
	spec := v1OpsFromSpec(t, specPath)

	if missing := diff(routes, spec); len(missing) > 0 {
		t.Errorf("routes NOT documented in docs/openapi.yaml:\n  %s", strings.Join(missing, "\n  "))
	}
	if extra := diff(spec, routes); len(extra) > 0 {
		t.Errorf("docs/openapi.yaml documents endpoints with no matching route:\n  %s", strings.Join(extra, "\n  "))
	}
}

// v1RoutesFromSource extracts `"METHOD /v1/..."` patterns registered in api.go.
func v1RoutesFromSource(t *testing.T, path string) map[string]bool {
	t.Helper()
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read %s: %v", path, err)
	}
	re := regexp.MustCompile(`"(GET|POST|PUT|PATCH|DELETE) (/v1/[^"]+)"`)
	out := map[string]bool{}
	for _, m := range re.FindAllStringSubmatch(string(b), -1) {
		out[m[1]+" "+m[2]] = true
	}
	if len(out) == 0 {
		t.Fatal("no /v1 routes found in api.go")
	}
	return out
}

// v1OpsFromSpec extracts METHOD+path operations under /v1 from the
// OpenAPI YAML using a small indentation-aware scan (no yaml dep).
func v1OpsFromSpec(t *testing.T, path string) map[string]bool {
	t.Helper()
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read %s: %v", path, err)
	}
	pathRe := regexp.MustCompile(`^  (/v1/\S+):\s*$`)
	methodRe := regexp.MustCompile(`^    (get|post|put|patch|delete):\s*$`)
	out := map[string]bool{}
	cur := ""
	for _, line := range strings.Split(string(b), "\n") {
		if m := pathRe.FindStringSubmatch(line); m != nil {
			cur = m[1]
			continue
		}
		// any 2-space top-level key ends the current path block
		if strings.HasPrefix(line, "  ") && !strings.HasPrefix(line, "   ") && strings.TrimSpace(line) != "" {
			if pathRe.FindStringSubmatch(line) == nil {
				cur = ""
			}
		}
		if m := methodRe.FindStringSubmatch(line); m != nil && cur != "" {
			out[strings.ToUpper(m[1])+" "+cur] = true
		}
	}
	if len(out) == 0 {
		t.Fatal("no /v1 operations found in openapi.yaml")
	}
	return out
}

func diff(a, b map[string]bool) []string {
	var out []string
	for k := range a {
		if !b[k] {
			out = append(out, k)
		}
	}
	sort.Strings(out)
	return out
}
