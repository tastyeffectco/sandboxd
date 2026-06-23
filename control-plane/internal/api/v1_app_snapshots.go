// v1_app_snapshots.go — Phase 4 (v0.4.0): app-scoped snapshot history,
// restore, and fork, built only on the public /v1/snapshots subsystem
// (tenant-scoped, directory-copy). The internal /sandbox/{id}/snapshots
// zstd subsystem is deliberately NOT exposed here.
//
//   - history: list the snapshots captured from an app.
//   - restore: REPLACE the app's current sandbox with a fresh one cloned
//     from a snapshot. Destructive to un-snapshotted work — the console
//     confirms before calling.
//   - fork: create a NEW app and spin its sandbox from a snapshot, leaving
//     the source app untouched.
//
// Sandbox creation reuses the proven internal create path (handleCreate
// with template_path = the snapshot's image), which clones the workspace
// and resets .git to a clean ownerless repo.
package api

import (
	"encoding/json"
	"errors"
	"net/http"

	"github.com/sandboxd/control-plane/internal/audit"
	"github.com/sandboxd/control-plane/internal/store"
)

// v1ListAppSnapshots — GET /v1/apps/{id}/snapshots. Tenant- and
// app-scoped history, newest first.
func (s *Server) v1ListAppSnapshots(w http.ResponseWriter, r *http.Request) {
	app, ok := s.appForConfig(w, r)
	if !ok {
		return
	}
	rows, err := s.Store.ListSnapshotsByApp(r.Context(), tenantToken(r), app.ID)
	if err != nil {
		writeV1Err(w, http.StatusInternalServerError, "internal", err.Error())
		return
	}
	out := make([]v1Snapshot, 0, len(rows))
	for _, sn := range rows {
		out = append(out, v1SnapshotFromRow(sn))
	}
	writeJSON(w, http.StatusOK, map[string]any{"snapshots": out})
}

// resolveReadySnapshot resolves a tenant-owned, ready snapshot or writes
// the matching v1 error and returns false.
func (s *Server) resolveReadySnapshot(w http.ResponseWriter, r *http.Request, id string) (*store.Snapshot, bool) {
	if id == "" {
		writeV1Err(w, http.StatusBadRequest, "invalid_request", "snapshot_id is required")
		return nil, false
	}
	snap, err := s.snapshotForTenant(r, id)
	if errors.Is(err, store.ErrNotFound) {
		writeV1Err(w, http.StatusNotFound, "not_found", "no such snapshot")
		return nil, false
	}
	if err != nil {
		writeV1Err(w, http.StatusInternalServerError, "internal", err.Error())
		return nil, false
	}
	if snap.Status != "ready" {
		writeV1Err(w, http.StatusBadRequest, "invalid_request", "snapshot is not ready")
		return nil, false
	}
	return snap, true
}

// createAppSandboxFromSnapshot spins a sandbox for app from snap by
// delegating to the internal create path with template_path set. Returns
// the inner handler's (code, body).
func (s *Server) createAppSandboxFromSnapshot(r *http.Request, app *store.App, snap *store.Snapshot) (int, []byte) {
	body, _ := json.Marshal(map[string]any{
		"ports":         []int{3000},
		"app_id":        app.ID,
		"template_path": snap.ImagePath, // internal: clone + .git reset
		"external": map[string]string{
			"user_id":    app.ExternalUserID.String,
			"project_id": app.ExternalProjectID.String,
		},
	})
	return s.delegate(r, s.handleCreate, http.MethodPost, "/sandbox", nil, body)
}

type v1RestoreReq struct {
	SnapshotID string `json:"snapshot_id"`
}

// v1RestoreApp — POST /v1/apps/{id}/restore. Replaces the app's current
// sandbox with a fresh one cloned from the snapshot. DESTRUCTIVE: any
// un-snapshotted work in the current sandbox is lost.
func (s *Server) v1RestoreApp(w http.ResponseWriter, r *http.Request) {
	app, ok := s.appForConfig(w, r)
	if !ok {
		return
	}
	var req v1RestoreReq
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeV1Err(w, http.StatusBadRequest, "invalid_request", "invalid json: "+err.Error())
		return
	}
	snap, ok := s.resolveReadySnapshot(w, r, req.SnapshotID)
	if !ok {
		return
	}

	// Replace the current sandbox, if any: purge it (container + workspace
	// + row) so the snapshot clone starts clean and the app has room for a
	// new current sandbox.
	if cur, err := s.Store.CurrentSandboxForApp(r.Context(), app.ID); err == nil {
		code, body := s.delegate(r, s.handlePurgeSandbox, http.MethodPost,
			"/sandbox/"+cur.ID+"/purge", map[string]string{"id": cur.ID}, nil)
		if code != http.StatusOK {
			relayV1Error(w, code, body)
			return
		}
	} else if !errors.Is(err, store.ErrNotFound) {
		writeV1Err(w, http.StatusInternalServerError, "internal", err.Error())
		return
	}

	code, body := s.createAppSandboxFromSnapshot(r, app, snap)
	if code != http.StatusCreated {
		relayV1Error(w, code, body)
		return
	}
	s.auditAction(r, audit.Entry{Action: "app.restore", Target: app.ID,
		Detail: map[string]any{"snapshot_id": snap.ID}})
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusCreated)
	_, _ = w.Write(body)
}

type v1ForkReq struct {
	SnapshotID string `json:"snapshot_id"`
	Name       string `json:"name"`
}

// v1ForkApp — POST /v1/apps/{id}/fork. Creates a NEW app from a snapshot
// and spins its sandbox from that snapshot. The source app (path id) is
// used only for tenant scoping, integration tags, and default naming; it
// is left untouched.
func (s *Server) v1ForkApp(w http.ResponseWriter, r *http.Request) {
	srcApp, ok := s.appForConfig(w, r)
	if !ok {
		return
	}
	var req v1ForkReq
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeV1Err(w, http.StatusBadRequest, "invalid_request", "invalid json: "+err.Error())
		return
	}
	snap, ok := s.resolveReadySnapshot(w, r, req.SnapshotID)
	if !ok {
		return
	}
	name := req.Name
	if name == "" {
		name = srcApp.Name + " (fork)"
	}

	newApp := &store.App{
		ID:                newULID(),
		OwnerToken:        tenantToken(r),
		Name:              name,
		ExternalUserID:    srcApp.ExternalUserID,
		ExternalProjectID: srcApp.ExternalProjectID,
	}
	if err := s.Store.CreateApp(r.Context(), newApp); err != nil {
		writeV1Err(w, http.StatusInternalServerError, "internal", err.Error())
		return
	}

	// Spin the forked app's sandbox from the snapshot. If this fails the
	// fork still produced a valid app with no sandbox (recoverable), so we
	// surface the error but keep the new app.
	code, body := s.createAppSandboxFromSnapshot(r, newApp, snap)
	s.auditAction(r, audit.Entry{Action: "app.fork", Target: newApp.ID,
		Detail: map[string]any{"source_app_id": srcApp.ID, "snapshot_id": snap.ID}})
	if code != http.StatusCreated {
		writeJSON(w, http.StatusCreated, map[string]any{
			"app":           v1AppFromRow(newApp, ""),
			"sandbox_error": string(body),
		})
		return
	}
	writeJSON(w, http.StatusCreated, map[string]any{
		"app":     v1AppFromRow(newApp, s.currentSandboxID(r, newApp.ID)),
		"sandbox": json.RawMessage(body),
	})
}
