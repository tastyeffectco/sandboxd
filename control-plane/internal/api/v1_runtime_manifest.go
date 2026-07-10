// v1_runtime_manifest.go — manifest validation + guidance (read-only; no writes).
// Core owns the manifest contract via internal/manifest; these endpoints expose
// validation and the effective view. Nothing is ever applied or written.
package api

import (
	"encoding/json"
	"errors"
	"net/http"
	"os"
	"path/filepath"

	"github.com/tastyeffectco/sandboxd/control-plane/internal/manifest"
	"github.com/tastyeffectco/sandboxd/control-plane/internal/preset"
	"github.com/tastyeffectco/sandboxd/control-plane/internal/store"
)

type v1ValidateManifestReq struct {
	Manifest string `json:"manifest"`
}

// POST /v1/runtime/manifest/validate — stateless validation of a manifest blob.
func (s *Server) v1ValidateManifest(w http.ResponseWriter, r *http.Request) {
	var req v1ValidateManifestReq
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeV1Err(w, http.StatusBadRequest, "invalid_request", "invalid json: "+err.Error())
		return
	}
	writeJSON(w, http.StatusOK, manifest.Validate([]byte(req.Manifest)))
}

type v1AppManifestResp struct {
	Present    bool                `json:"present"`
	Reason     string              `json:"reason,omitempty"`
	Source     string              `json:"source,omitempty"` // sandbox.yaml | preset | default
	Manifest   string              `json:"manifest,omitempty"`
	Validation *manifest.Result    `json:"validation,omitempty"`
	Effective  *manifest.Effective `json:"effective,omitempty"`
}

// GET /v1/apps/{id}/runtime/manifest — the app's current sandbox.yaml (or, if
// absent, the preset/default that would apply), validated. Owner-scoped,
// read-only, host-side (works whether the sandbox is running or stopped).
func (s *Server) v1AppManifest(w http.ResponseWriter, r *http.Request) {
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
		writeJSON(w, http.StatusOK, v1AppManifestResp{Present: false, Reason: "no_workspace"})
		return
	}

	appDir := filepath.Join(sb.WorkspaceMnt, "workspace", "app")
	if raw, err := os.ReadFile(filepath.Join(appDir, manifest.File)); err == nil {
		res := manifest.Validate(raw)
		writeJSON(w, http.StatusOK, v1AppManifestResp{
			Present: true, Source: "sandbox.yaml", Manifest: string(raw),
			Validation: &res, Effective: res.Effective,
		})
		return
	}

	// No sandbox.yaml on disk yet. If the app has a preset, show the manifest that
	// runtimed WOULD write on first boot (source: preset). Else: a plain default.
	if app.RuntimePreset.Valid && app.RuntimePreset.String != "" {
		if p, ok := preset.Get(app.RuntimePreset.String); ok {
			res := manifest.Validate([]byte(p.Manifest))
			writeJSON(w, http.StatusOK, v1AppManifestResp{
				Present: false, Source: "preset", Reason: "no sandbox.yaml yet; showing the selected preset's manifest",
				Manifest: p.Manifest, Validation: &res, Effective: res.Effective,
			})
			return
		}
	}
	writeJSON(w, http.StatusOK, v1AppManifestResp{
		Present: false, Source: "default", Reason: "no sandbox.yaml in the workspace",
	})
}
