// v1_snapshots.go — snapshots-as-templates
// (ops/design/snapshots-as-templates.md). A snapshot is a reusable,
// frozen copy of a sandbox's workspace directory, stored under
// LibraryRoot and cloned into new sandboxes via the existing
// ProvisionFromTemplate path. Scoped to the API tenant (auth.Actor.Name).
package api

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"syscall"
	"time"

	"github.com/sandboxd/control-plane/internal/audit"
	"github.com/sandboxd/control-plane/internal/auth"
	"github.com/sandboxd/control-plane/internal/events"
	"github.com/sandboxd/control-plane/internal/store"
)

// v1Snapshot is the public snapshot object.
type v1Snapshot struct {
	ID              string `json:"id"`
	Name            string `json:"name"`
	Status          string `json:"status"`
	SourceSandboxID string `json:"source_sandbox_id,omitempty"`
	SourceAppID     string `json:"source_app_id,omitempty"`
	BaseImage       string `json:"base_image"`
	Visibility      string `json:"visibility"`
	SizeBytes       int64  `json:"size_bytes,omitempty"`
	CreatedAt       string `json:"created_at"`
}

func v1SnapshotFromRow(s *store.Snapshot) v1Snapshot {
	out := v1Snapshot{
		ID:         s.ID,
		Name:       s.Name,
		Status:     s.Status,
		BaseImage:  s.BaseImage,
		Visibility: s.Visibility,
		CreatedAt:  s.CreatedAt.UTC().Format(time.RFC3339),
	}
	if s.SourceSandboxID.Valid {
		out.SourceSandboxID = s.SourceSandboxID.String
	}
	if s.SourceAppID.Valid {
		out.SourceAppID = s.SourceAppID.String
	}
	if s.SizeBytes.Valid {
		out.SizeBytes = s.SizeBytes.Int64
	}
	return out
}

// tenantToken is the snapshot ownership boundary: the authenticated
// API token's name (auth.Actor.Name). All the upstream backend traffic carries one
// token, so the upstream backend's snapshots are shared across its end-users — the
// platform cannot and does not scope by the untrusted external user_id.
func tenantToken(r *http.Request) string {
	return auth.ActorFrom(r.Context()).Name
}

type v1CreateSnapshotReq struct {
	SourceSandboxID string `json:"source_sandbox_id"`
	Name            string `json:"name"`
}

// v1CreateSnapshot — POST /v1/snapshots. Synchronous: stopped-source
// check → raw cp under the source's id-lock → row. 409 if the source
// is running (cp of a live loopback would be inconsistent).
func (s *Server) v1CreateSnapshot(w http.ResponseWriter, r *http.Request) {
	if s.LibraryRoot == "" {
		writeV1Err(w, http.StatusServiceUnavailable, "internal", "snapshots not configured on this host")
		return
	}
	var req v1CreateSnapshotReq
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeV1Err(w, http.StatusBadRequest, "invalid_request", "invalid json: "+err.Error())
		return
	}
	if req.SourceSandboxID == "" || req.Name == "" {
		writeV1Err(w, http.StatusBadRequest, "invalid_request", "source_sandbox_id and name are required")
		return
	}

	src, err := s.Store.Get(r.Context(), req.SourceSandboxID)
	if errors.Is(err, store.ErrNotFound) {
		writeV1Err(w, http.StatusNotFound, "not_found", "no such source sandbox")
		return
	}
	if err != nil {
		writeV1Err(w, http.StatusInternalServerError, "internal", err.Error())
		return
	}
	if src.Status == "running" {
		writeV1Err(w, http.StatusConflict, "conflict",
			"source sandbox is running; stop it first (POST /v1/sandboxes/{id}/stop) then snapshot")
		return
	}

	srcImg, _ := s.Loopback.Paths(src.ID)
	if _, err := os.Stat(srcImg); err != nil {
		writeV1Err(w, http.StatusNotFound, "not_found", "source workspace image not found on disk")
		return
	}

	snapID := newULID()
	imgPath := filepath.Join(s.LibraryRoot, snapID)

	// Capture under the source's id-lock so a concurrent wake can't
	// start the container and write the workspace mid-copy. The lock is
	// released before returning; we never hold it across stop/wake.
	if s.Locks != nil {
		s.Locks.Lock(src.ID)
		defer s.Locks.Unlock(src.ID)
	}
	// Re-check running under the lock (a wake could have raced in
	// between the Get above and acquiring the lock).
	if again, err := s.Store.Get(r.Context(), src.ID); err == nil && again.Status == "running" {
		writeV1Err(w, http.StatusConflict, "conflict", "source sandbox started running; stop it first")
		return
	}

	size, capErr := captureImage(r.Context(), srcImg, imgPath, s.LibraryRoot)
	if capErr != nil {
		s.loggerFor(r, src.ID).Error("snapshot capture failed", "snapshot", snapID, "err", capErr.Error())
		s.recordEvent(r, events.Event{Type: events.SnapshotCaptureFailed, Severity: events.SeverityError,
			Message: "Snapshot capture failed: " + req.Name, AppID: src.AppID.String, SandboxID: src.ID,
			Payload: map[string]any{"name": req.Name, "reason": "capture_failed"}})
		writeV1Err(w, http.StatusInternalServerError, "internal", "capture: "+capErr.Error())
		return
	}

	snap := &store.Snapshot{
		ID:              snapID,
		Name:            req.Name,
		OwnerToken:      tenantToken(r),
		SourceSandboxID: sql.NullString{String: src.ID, Valid: true},
		SourceAppID:     src.AppID,          // per-app history survives the sandbox (0015)
		CreatedByUserID: src.ExternalUserID, // provenance only
		BaseImage:       src.Image,
		Visibility:      "private",
		Format:          "raw",
		Status:          "ready",
		ImagePath:       imgPath,
		SizeBytes:       sql.NullInt64{Int64: size, Valid: true},
	}
	if err := s.Store.CreateSnapshot(r.Context(), snap); err != nil {
		_ = os.RemoveAll(imgPath) // roll back the orphaned snapshot
		writeV1Err(w, http.StatusInternalServerError, "internal", "store: "+err.Error())
		return
	}
	s.auditAction(r, audit.Entry{
		Action: "snapshot.create", Target: snapID,
		Detail: map[string]any{"source_sandbox_id": src.ID, "name": req.Name},
	})
	s.recordEvent(r, events.Event{Type: events.SnapshotCaptured, Severity: events.SeverityInfo,
		Message: "Snapshot captured: " + req.Name, AppID: src.AppID.String, SandboxID: src.ID,
		SnapshotID: snapID, Payload: map[string]any{"name": req.Name}})
	writeJSON(w, http.StatusCreated, v1SnapshotFromRow(snap))
}

// v1ListSnapshots — GET /v1/snapshots (tenant-scoped).
func (s *Server) v1ListSnapshots(w http.ResponseWriter, r *http.Request) {
	rows, err := s.Store.ListSnapshotsByOwner(r.Context(), tenantToken(r))
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

// v1GetSnapshot — GET /v1/snapshots/{id} (tenant-scoped).
func (s *Server) v1GetSnapshot(w http.ResponseWriter, r *http.Request) {
	snap, err := s.snapshotForTenant(r, r.PathValue("id"))
	if err != nil {
		s.writeSnapshotLookupErr(w, err)
		return
	}
	writeJSON(w, http.StatusOK, v1SnapshotFromRow(snap))
}

// v1DeleteSnapshot — DELETE /v1/snapshots/{id}. Removes the image file
// + row. Safe: sandboxes cloned from it are independent copies (ext4,
// no CoW), so deletion never affects them.
func (s *Server) v1DeleteSnapshot(w http.ResponseWriter, r *http.Request) {
	snap, err := s.snapshotForTenant(r, r.PathValue("id"))
	if err != nil {
		s.writeSnapshotLookupErr(w, err)
		return
	}
	if err := os.RemoveAll(snap.ImagePath); err != nil && !os.IsNotExist(err) {
		writeV1Err(w, http.StatusInternalServerError, "internal", "remove snapshot: "+err.Error())
		return
	}
	if err := s.Store.DeleteSnapshot(r.Context(), snap.ID); err != nil {
		writeV1Err(w, http.StatusInternalServerError, "internal", err.Error())
		return
	}
	s.auditAction(r, audit.Entry{Action: "snapshot.delete", Target: snap.ID})
	w.WriteHeader(http.StatusNoContent)
}

// snapshotForTenant fetches a snapshot and enforces tenant ownership.
// Returns store.ErrNotFound for both missing and cross-tenant snapshots
// (don't leak existence across tenants).
func (s *Server) snapshotForTenant(r *http.Request, id string) (*store.Snapshot, error) {
	snap, err := s.Store.GetSnapshot(r.Context(), id)
	if err != nil {
		return nil, err
	}
	if snap.OwnerToken != tenantToken(r) {
		return nil, store.ErrNotFound
	}
	return snap, nil
}

func (s *Server) writeSnapshotLookupErr(w http.ResponseWriter, err error) {
	if errors.Is(err, store.ErrNotFound) {
		writeV1Err(w, http.StatusNotFound, "not_found", "no such snapshot")
		return
	}
	writeV1Err(w, http.StatusInternalServerError, "internal", err.Error())
}

// captureImage copies the workspace directory srcDir → dst
// crash-consistently: cp -a into a sibling .tmp directory (preserving
// ownership/perms), sync file data + metadata to disk, atomically
// rename the directory into place, then fsync the parent. The caller
// guarantees no writer (source stopped + id-lock held). Returns the
// captured tree's allocated size in bytes.
func captureImage(ctx context.Context, srcDir, dst, root string) (int64, error) {
	if err := os.MkdirAll(root, 0o750); err != nil {
		return 0, err
	}
	tmp := dst + ".tmp"
	_ = os.RemoveAll(tmp) // clear any leftover from a prior crash
	if err := os.MkdirAll(tmp, 0o755); err != nil {
		return 0, err
	}
	// Trailing /. copies directory contents into tmp; cp -a preserves
	// ownership/perms (mirrors loopback.ProvisionFromTemplate).
	if out, err := exec.CommandContext(ctx, "cp", "-a", strings.TrimRight(srcDir, "/")+"/.", tmp+"/").CombinedOutput(); err != nil {
		_ = os.RemoveAll(tmp)
		return 0, errorsWrap("cp", err, out)
	}
	// Flush the copied tree to disk, then atomically publish the
	// directory and fsync the parent so the rename survives a crash.
	_ = exec.CommandContext(ctx, "sync").Run()
	if err := os.Rename(tmp, dst); err != nil {
		_ = os.RemoveAll(tmp)
		return 0, err
	}
	_ = fsyncPath(root)
	return dirAllocatedBytes(dst), nil
}

// dirAllocatedBytes returns the real on-disk (allocated) size of a
// directory tree — the meaningful number for a workspace snapshot, and
// what `du` reports. Falls back to the apparent size for any entry
// where the syscall stat is unavailable.
func dirAllocatedBytes(root string) int64 {
	var total int64
	_ = filepath.Walk(root, func(_ string, fi os.FileInfo, err error) error {
		if err != nil {
			return nil // best-effort: skip unreadable entries
		}
		if st, ok := fi.Sys().(*syscall.Stat_t); ok {
			total += st.Blocks * 512 // st_blocks is in 512-byte units
		} else {
			total += fi.Size()
		}
		return nil
	})
	return total
}

func fsyncPath(p string) error {
	f, err := os.Open(p)
	if err != nil {
		return err
	}
	defer f.Close()
	return f.Sync()
}

func errorsWrap(stage string, err error, out []byte) error {
	if len(out) > 0 {
		return errors.New(stage + ": " + err.Error() + ": " + string(out))
	}
	return errors.New(stage + ": " + err.Error())
}
