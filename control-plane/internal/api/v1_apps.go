// v1_apps.go — durable "app" entities above sandboxes (Phase 1).
// An app owns the user-facing concept (name/description/tags) and
// outlives its sandbox. Scoped to the API tenant (auth.Actor.Name);
// external_* are optional integration tags. The sandbox link lives on
// the sandbox (app_id), so an app's "current sandbox" is derived.
package api

import (
	"encoding/json"
	"errors"
	"net/http"
	"time"

	"github.com/sandboxd/control-plane/internal/audit"
	"github.com/sandboxd/control-plane/internal/events"
	"github.com/sandboxd/control-plane/internal/gitimport"
	"github.com/sandboxd/control-plane/internal/preset"
	"github.com/sandboxd/control-plane/internal/store"
)

// v1AppGit is the git block on create (token referenced by credential_id, never
// inline) and the tokenless git metadata on read.
type v1AppGit struct {
	RepoURL      string `json:"repo_url"`
	Branch       string `json:"branch,omitempty"`
	CredentialID string `json:"credential_id,omitempty"`
	LastImportAt string `json:"last_import_at,omitempty"`
}

type v1App struct {
	ID                string    `json:"id"`
	Name              string    `json:"name"`
	Description       string    `json:"description"`
	Tags              []string  `json:"tags"`
	ExternalUserID    string    `json:"external_user_id,omitempty"`
	ExternalProjectID string    `json:"external_project_id,omitempty"`
	RuntimePreset     string    `json:"runtime_preset,omitempty"`
	Git               *v1AppGit `json:"git,omitempty"`
	CurrentSandboxID  string    `json:"current_sandbox_id,omitempty"`
	CreatedAt         string    `json:"created_at"`
	UpdatedAt         string    `json:"updated_at"`
}

func v1AppFromRow(a *store.App, currentSandboxID string) v1App {
	out := v1App{
		ID:               a.ID,
		Name:             a.Name,
		Description:      a.Description,
		Tags:             a.Tags,
		CurrentSandboxID: currentSandboxID,
		CreatedAt:        a.CreatedAt.UTC().Format(time.RFC3339),
		UpdatedAt:        a.UpdatedAt.UTC().Format(time.RFC3339),
	}
	if out.Tags == nil {
		out.Tags = []string{}
	}
	if a.ExternalUserID.Valid {
		out.ExternalUserID = a.ExternalUserID.String
	}
	if a.ExternalProjectID.Valid {
		out.ExternalProjectID = a.ExternalProjectID.String
	}
	if a.RuntimePreset.Valid {
		out.RuntimePreset = a.RuntimePreset.String
	}
	if a.GitRepoURL.Valid && a.GitRepoURL.String != "" {
		g := &v1AppGit{RepoURL: a.GitRepoURL.String, Branch: a.GitBranch.String}
		if a.GitCredentialID.Valid {
			g.CredentialID = a.GitCredentialID.String
		}
		if a.LastImportAt.Valid {
			g.LastImportAt = time.Unix(a.LastImportAt.Int64, 0).UTC().Format(time.RFC3339)
		}
		out.Git = g
	}
	return out
}

// resolveRuntimePreset picks the explicit per-request preset if given, else the
// app's stored default.
func resolveRuntimePreset(explicit, appDefault string) string {
	if explicit != "" {
		return explicit
	}
	return appDefault
}

// currentSandboxID returns the app's current sandbox id, or "" if none.
func (s *Server) currentSandboxID(r *http.Request, appID string) string {
	sb, err := s.Store.CurrentSandboxForApp(r.Context(), appID)
	if err != nil {
		return ""
	}
	return sb.ID
}

type v1CreateAppReq struct {
	Name              string    `json:"name"`
	Description       string    `json:"description"`
	Tags              []string  `json:"tags"`
	ExternalUserID    string    `json:"external_user_id"`
	ExternalProjectID string    `json:"external_project_id"`
	RuntimePreset     string    `json:"runtime_preset"`
	Git               *v1AppGit `json:"git,omitempty"`
	// Image is rejected on purpose — per-app image selection is not supported (the
	// sandbox image is instance-wide via SANDBOXD_IMAGE). Declared so we 400 it
	// explicitly instead of silently dropping it.
	Image *string `json:"image,omitempty"`
}

// errPerAppImage is returned when a create body tries to pick a per-app image.
const errPerAppImage = "per-app image selection is not supported; set SANDBOXD_IMAGE instance-wide"

// v1CreateApp — POST /v1/apps.
func (s *Server) v1CreateApp(w http.ResponseWriter, r *http.Request) {
	var req v1CreateAppReq
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeV1Err(w, http.StatusBadRequest, "invalid_request", "invalid json: "+err.Error())
		return
	}
	if req.Image != nil {
		writeV1Err(w, http.StatusBadRequest, "invalid_request", errPerAppImage)
		return
	}
	if req.Name == "" {
		writeV1Err(w, http.StatusBadRequest, "invalid_request", "name is required")
		return
	}
	if req.RuntimePreset != "" && !preset.Valid(req.RuntimePreset) {
		writeV1Err(w, http.StatusBadRequest, "invalid_request", "unknown runtime_preset")
		return
	}
	app := &store.App{
		ID:                newULID(),
		OwnerToken:        tenantToken(r),
		Name:              req.Name,
		Description:       req.Description,
		Tags:              req.Tags,
		ExternalUserID:    nullStr(req.ExternalUserID),
		ExternalProjectID: nullStr(req.ExternalProjectID),
		RuntimePreset:     nullStr(req.RuntimePreset),
	}
	// Optional Git import: validate the tokenless repo URL + branch and that the
	// referenced credential exists for this owner. The token itself is NOT stored
	// on the app — only the credential id; it is decrypted control-plane-side at
	// clone time (sandbox create).
	if req.Git != nil && req.Git.RepoURL != "" {
		if err := gitimport.ValidateRepoURL(req.Git.RepoURL); err != nil {
			writeV1Err(w, http.StatusBadRequest, "invalid_request", "invalid git.repo_url (https only, no credentials)")
			return
		}
		branch := req.Git.Branch
		if branch == "" {
			branch = "main"
		}
		if err := gitimport.ValidateBranch(branch); err != nil {
			writeV1Err(w, http.StatusBadRequest, "invalid_request", "invalid git.branch")
			return
		}
		if req.Git.CredentialID == "" {
			writeV1Err(w, http.StatusBadRequest, "invalid_request", "git.credential_id is required for import")
			return
		}
		// owner-scoped existence check (cross-owner / unknown -> 404)
		if s.Secrets == nil {
			writeV1Err(w, http.StatusServiceUnavailable, "unavailable", "credential store not configured")
			return
		}
		_, _, found, err := s.Store.GetGitCredentialSecret(r.Context(), tenantToken(r), req.Git.CredentialID)
		if err != nil {
			writeV1Err(w, http.StatusInternalServerError, "internal", "credential lookup failed")
			return
		}
		if !found {
			writeV1Err(w, http.StatusNotFound, "not_found", "no such git credential")
			return
		}
		app.GitRepoURL = nullStr(req.Git.RepoURL)
		app.GitBranch = nullStr(branch)
		app.GitCredentialID = nullStr(req.Git.CredentialID)
	}
	if err := s.Store.CreateApp(r.Context(), app); err != nil {
		writeV1Err(w, http.StatusInternalServerError, "internal", err.Error())
		return
	}
	s.auditAction(r, audit.Entry{Action: "app.create", Target: app.ID})
	s.recordEvent(r, events.Event{Type: events.AppCreated, Severity: events.SeverityInfo,
		Message: "App created: " + app.Name, AppID: app.ID})
	writeJSON(w, http.StatusCreated, v1AppFromRow(app, ""))
}

// v1ListApps — GET /v1/apps (tenant-scoped; optional ?external_user_id).
func (s *Server) v1ListApps(w http.ResponseWriter, r *http.Request) {
	apps, err := s.Store.ListAppsForOwner(r.Context(), tenantToken(r), r.URL.Query().Get("external_user_id"))
	if err != nil {
		writeV1Err(w, http.StatusInternalServerError, "internal", err.Error())
		return
	}
	out := make([]v1App, 0, len(apps))
	for _, a := range apps {
		out = append(out, v1AppFromRow(a, s.currentSandboxID(r, a.ID)))
	}
	writeJSON(w, http.StatusOK, map[string]any{"apps": out})
}

// v1GetApp — GET /v1/apps/{id}.
func (s *Server) v1GetApp(w http.ResponseWriter, r *http.Request) {
	app, err := s.Store.GetAppForOwner(r.Context(), r.PathValue("id"), tenantToken(r))
	if errors.Is(err, store.ErrNotFound) {
		writeV1Err(w, http.StatusNotFound, "not_found", "no such app")
		return
	}
	if err != nil {
		writeV1Err(w, http.StatusInternalServerError, "internal", err.Error())
		return
	}
	writeJSON(w, http.StatusOK, v1AppFromRow(app, s.currentSandboxID(r, app.ID)))
}

type v1PatchAppReq struct {
	Name        *string   `json:"name"`
	Description *string   `json:"description"`
	Tags        *[]string `json:"tags"`
}

// v1PatchApp — PATCH /v1/apps/{id}.
func (s *Server) v1PatchApp(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	var req v1PatchAppReq
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeV1Err(w, http.StatusBadRequest, "invalid_request", "invalid json: "+err.Error())
		return
	}
	if req.Name != nil && *req.Name == "" {
		writeV1Err(w, http.StatusBadRequest, "invalid_request", "name cannot be empty")
		return
	}
	err := s.Store.UpdateApp(r.Context(), id, tenantToken(r), store.AppPatch{
		Name: req.Name, Description: req.Description, Tags: req.Tags,
	})
	if errors.Is(err, store.ErrNotFound) {
		writeV1Err(w, http.StatusNotFound, "not_found", "no such app")
		return
	}
	if err != nil {
		writeV1Err(w, http.StatusInternalServerError, "internal", err.Error())
		return
	}
	app, err := s.Store.GetAppForOwner(r.Context(), id, tenantToken(r))
	if err != nil {
		writeV1Err(w, http.StatusInternalServerError, "internal", err.Error())
		return
	}
	s.auditAction(r, audit.Entry{Action: "app.update", Target: id})
	s.recordEvent(r, events.Event{Type: events.AppUpdated, Severity: events.SeverityInfo,
		Message: "App updated", AppID: id})
	writeJSON(w, http.StatusOK, v1AppFromRow(app, s.currentSandboxID(r, id)))
}

type v1CreateAppSandboxReq struct {
	Template      string  `json:"template,omitempty"`
	Ports         []int   `json:"ports,omitempty"`
	RuntimePreset string  `json:"runtime_preset,omitempty"`
	Image         *string `json:"image,omitempty"` // rejected — see errPerAppImage
}

// v1CreateAppSandbox — POST /v1/apps/{id}/sandbox. Creates the app's
// current sandbox (one live sandbox per app for now). Delegates to the
// proven internal create path with the app's id + integration tags.
func (s *Server) v1CreateAppSandbox(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	app, err := s.Store.GetAppForOwner(r.Context(), id, tenantToken(r))
	if errors.Is(err, store.ErrNotFound) {
		writeV1Err(w, http.StatusNotFound, "not_found", "no such app")
		return
	}
	if err != nil {
		writeV1Err(w, http.StatusInternalServerError, "internal", err.Error())
		return
	}
	if cur, cerr := s.Store.CurrentSandboxForApp(r.Context(), id); cerr == nil {
		writeV1Err(w, http.StatusConflict, "conflict",
			"app already has a sandbox ("+cur.ID+"); delete it first")
		return
	}

	var req v1CreateAppSandboxReq
	if r.Body != nil {
		_ = json.NewDecoder(r.Body).Decode(&req) // body optional
	}
	if req.Image != nil {
		writeV1Err(w, http.StatusBadRequest, "invalid_request", errPerAppImage)
		return
	}
	ports := req.Ports
	if len(ports) == 0 {
		ports = []int{3000}
	}
	createBody := map[string]any{
		"ports":  ports,
		"app_id": app.ID,
		"external": map[string]string{
			"user_id":    app.ExternalUserID.String,
			"project_id": app.ExternalProjectID.String,
		},
	}
	// Git-imported app: pass the tokenless repo metadata + credential id (and the
	// owner, for the owner-scoped decrypt) so the control plane clones the repo
	// into the new workspace before the container starts. The TOKEN is never put
	// here — handleCreate decrypts it from the credential store.
	if app.GitRepoURL.Valid && app.GitRepoURL.String != "" {
		createBody["git"] = map[string]string{
			"repo_url":      app.GitRepoURL.String,
			"branch":        app.GitBranch.String,
			"credential_id": app.GitCredentialID.String,
			"owner":         app.OwnerToken,
		}
	}
	// Resolve the runtime preset: explicit on the request, else the app's
	// stored default. A preset supplies the template, so it takes precedence
	// over an explicit `template`.
	rp := resolveRuntimePreset(req.RuntimePreset, app.RuntimePreset.String)
	if rp != "" {
		if !preset.Valid(rp) {
			writeV1Err(w, http.StatusBadRequest, "invalid_request", "unknown runtime_preset")
			return
		}
		createBody["runtime_preset"] = rp
	} else if req.Template != "" {
		createBody["template"] = req.Template
	}
	internal, _ := json.Marshal(createBody)
	code, body := s.delegate(r, s.handleCreate, http.MethodPost, "/sandbox", nil, internal)
	if code != http.StatusCreated {
		relayV1Error(w, code, body)
		return
	}
	s.auditAction(r, audit.Entry{Action: "app.sandbox.create", Target: app.ID})
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusCreated)
	_, _ = w.Write(body)
}
