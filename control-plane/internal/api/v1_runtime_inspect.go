// v1_runtime_inspect.go — A1.5b: advisory runtime detection for an app's
// workspace. Read-only; it inspects workspace files host-side and returns
// suggestions/warnings. It NEVER runs app code, installs deps, touches a Git
// credential/token, or applies anything. Owner-scoped.
package api

import (
	"errors"
	"net/http"
	"path/filepath"

	"github.com/tastyeffectco/sandboxd/control-plane/internal/detect"
	"github.com/tastyeffectco/sandboxd/control-plane/internal/store"
)

// GET /v1/apps/{id}/runtime-inspect
func (s *Server) v1RuntimeInspect(w http.ResponseWriter, r *http.Request) {
	app, err := s.Store.GetAppForOwner(r.Context(), r.PathValue("id"), tenantToken(r))
	if errors.Is(err, store.ErrNotFound) {
		writeV1Err(w, http.StatusNotFound, "not_found", "no such app")
		return
	}
	if err != nil {
		writeV1Err(w, http.StatusInternalServerError, "internal", "lookup failed")
		return
	}
	sb, err := s.Store.CurrentSandboxForApp(r.Context(), app.ID)
	if err != nil {
		// No sandbox => no workspace to inspect yet.
		writeJSON(w, http.StatusOK, detect.Result{
			ExistingManifest: &detect.ManifestSummary{Present: false},
			Suggestions:      []detect.Suggestion{},
			Warnings:         []string{"no workspace yet — create a sandbox for this app, then inspect"},
		})
		return
	}
	appDir := filepath.Join(sb.WorkspaceMnt, "workspace", "app")
	writeJSON(w, http.StatusOK, detect.Inspect(detect.OSFiles(appDir)))
}
