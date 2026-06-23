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
	"github.com/sandboxd/control-plane/internal/store"
)

type v1App struct {
	ID                string   `json:"id"`
	Name              string   `json:"name"`
	Description       string   `json:"description"`
	Tags              []string `json:"tags"`
	ExternalUserID    string   `json:"external_user_id,omitempty"`
	ExternalProjectID string   `json:"external_project_id,omitempty"`
	CurrentSandboxID  string   `json:"current_sandbox_id,omitempty"`
	CreatedAt         string   `json:"created_at"`
	UpdatedAt         string   `json:"updated_at"`
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
	return out
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
	Name              string   `json:"name"`
	Description       string   `json:"description"`
	Tags              []string `json:"tags"`
	ExternalUserID    string   `json:"external_user_id"`
	ExternalProjectID string   `json:"external_project_id"`
}

// v1CreateApp — POST /v1/apps.
func (s *Server) v1CreateApp(w http.ResponseWriter, r *http.Request) {
	var req v1CreateAppReq
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeV1Err(w, http.StatusBadRequest, "invalid_request", "invalid json: "+err.Error())
		return
	}
	if req.Name == "" {
		writeV1Err(w, http.StatusBadRequest, "invalid_request", "name is required")
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
	Template string `json:"template,omitempty"`
	Ports    []int  `json:"ports,omitempty"`
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
	if req.Template != "" {
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
