package api

import (
	"path/filepath"
	"runtime"
	"testing"
)

// requiredPublicSurface is the public /v1 API the console + SDK depend on. The
// parity test (TestOpenAPIContractMatchesRoutes) keeps routes and the spec in
// sync, but parity alone wouldn't notice a route AND its spec entry being
// deleted together. This test pins the surface so a removal fails loudly.
var requiredPublicSurface = []string{
	"GET /v1/settings",
	"PATCH /v1/settings",
	"GET /v1/agents",
	"GET /v1/presets",
	"POST /v1/git-credentials",
	"GET /v1/git-credentials",
	"DELETE /v1/git-credentials/{id}",
	"GET /v1/apps/{id}/runtime-inspect",
	"POST /v1/runtime/manifest/validate",
	"GET /v1/runtime/recipes",
	"GET /v1/apps/{id}/runtime/manifest",
	"GET /v1/apps/{id}/git/status",
	"GET /v1/apps/{id}/git/diff",
	"POST /v1/apps/{id}/git/commit",
	"POST /v1/apps/{id}/git/push",
	"GET /v1/apps",
	"POST /v1/apps",
	"POST /v1/apps/{id}/sandbox",
	"GET /v1/sandboxes/{id}",
	"GET /v1/sandboxes/{id}/processes/{name}/logs",
	"POST /v1/snapshots",
	"GET /v1/snapshots",
	"GET /v1/snapshots/{id}",
	"DELETE /v1/snapshots/{id}",
	"GET /v1/apps/{id}/snapshots",
	"POST /v1/apps/{id}/restore",
	"POST /v1/apps/{id}/fork",
	"POST /v1/apps/{id}/config",
	"GET /v1/apps/{id}/config",
	"PATCH /v1/apps/{id}/config/{key}",
	"DELETE /v1/apps/{id}/config/{key}",
	"GET /v1/apps/{id}/events",
}

func TestPublicAPISurfacePresent(t *testing.T) {
	_, thisFile, _, _ := runtime.Caller(0)
	dir := filepath.Dir(thisFile)
	routes := v1RoutesFromSource(t, filepath.Join(dir, "api.go"))
	spec := v1OpsFromSpec(t, filepath.Join(dir, "..", "..", "..", "docs", "openapi.yaml"))

	for _, op := range requiredPublicSurface {
		if !routes[op] {
			t.Errorf("required public route missing from api.go: %s", op)
		}
		if !spec[op] {
			t.Errorf("required public route missing from docs/openapi.yaml: %s", op)
		}
	}
}
