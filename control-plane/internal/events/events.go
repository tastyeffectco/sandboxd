// Package events is the Phase 5 observability recorder: a thin, centralized
// writer that handlers/services call to append one row to the durable
// `app_events` timeline (migrations/0016). It is deliberately the ONLY place
// that builds event rows — no raw SQL inserts scattered across handlers.
//
// Best-effort, like internal/audit: a write failure is logged but never
// breaks the caller's request, and it uses a detached context so the row
// still lands if the request was cancelled. payload is small JSON and must
// never carry secrets or large logs (callers pass keys/ids, not values).
package events

import (
	"context"
	"crypto/rand"
	"encoding/json"
	"log/slog"
	"sync"
	"time"

	"github.com/oklog/ulid/v2"
)

// Store is the slice of the SQLite store the recorder needs, declared as an
// interface so this package doesn't import internal/store (mirrors
// internal/audit). *store.Store satisfies it.
type Store interface {
	InsertAppEvent(ctx context.Context, id, ownerToken, appID, sandboxID, taskID, snapshotID, typ, severity, message, payloadJSON, createdAt string) error
}

// Severity values (kept small + stable for export).
const (
	SeverityInfo    = "info"
	SeverityWarning = "warning"
	SeverityError   = "error"
)

// Event type names — stable, machine-readable. Treat as a public contract:
// add new ones, don't rename. (Messages are human-readable and must NOT be
// used for programmatic logic.)
const (
	AppCreated = "app.created"
	AppUpdated = "app.updated"

	SandboxCreateStarted = "sandbox.create.started"
	SandboxCreateFailed  = "sandbox.create.failed"

	// Git import (A1). Payloads carry repo_url (tokenless) + branch — NEVER a token.
	GitRepoCloneStarted = "git.repo.clone_started"
	GitRepoCloned       = "git.repo.cloned"
	GitRepoCloneFailed  = "git.repo.clone_failed"
	SandboxStarted      = "sandbox.started"
	SandboxStopped      = "sandbox.stopped"
	SandboxDeleted      = "sandbox.deleted"

	TaskStarted     = "task.started"
	TaskCompleted   = "task.completed"
	TaskFailed      = "task.failed"
	TaskBuildFailed = "task.build.failed"

	PreviewHealthOK     = "preview.health.ok"
	PreviewHealthFailed = "preview.health.failed"

	SnapshotCaptured      = "snapshot.captured"
	SnapshotCaptureFailed = "snapshot.capture.failed"
	SnapshotRestored      = "snapshot.restored"
	SnapshotForked        = "snapshot.forked"

	ConfigCreated = "config.created"
	ConfigUpdated = "config.updated"
	ConfigDeleted = "config.deleted"
)

// Event is one timeline entry. OwnerToken (tenant) + Type + Severity +
// Message are required; the id columns and Payload are optional. Payload is
// JSON-encoded into payload_json — pass keys/ids/reasons, never secret values.
type Event struct {
	OwnerToken string
	AppID      string
	SandboxID  string
	TaskID     string
	SnapshotID string
	Type       string
	Severity   string
	Message    string
	Payload    map[string]any
}

// Recorder appends events. Safe for concurrent use; nil-safe.
type Recorder struct {
	store Store
	log   *slog.Logger

	// Monotonic ULID source so events emitted in the same millisecond (e.g.
	// a task's completion burst) still sort in emission order by id — which
	// is the timeline's page cursor. MonotonicEntropy is not concurrency-
	// safe, so id generation is guarded by mu.
	mu      sync.Mutex
	entropy *ulid.MonotonicEntropy
}

// New constructs a Recorder. A nil store yields a no-op recorder (nil-safe
// for tests / partial wiring).
func New(store Store, log *slog.Logger) *Recorder {
	return &Recorder{store: store, log: log, entropy: ulid.Monotonic(rand.Reader, 0)}
}

// nextID returns a monotonic ULID (unique + sortable in emission order).
func (r *Recorder) nextID() string {
	r.mu.Lock()
	defer r.mu.Unlock()
	return ulid.MustNew(ulid.Now(), r.entropy).String()
}

// Record appends one event, best-effort. Generates a ULID id (time-sortable
// page cursor) and an RFC3339 UTC created_at. A bad payload is dropped (the
// event still records) rather than failing the write.
func (r *Recorder) Record(_ context.Context, e Event) {
	if r == nil || r.store == nil {
		return
	}
	if e.Severity == "" {
		e.Severity = SeverityInfo
	}
	payload := ""
	if len(e.Payload) > 0 {
		if b, err := json.Marshal(e.Payload); err == nil {
			payload = string(b)
		} else if r.log != nil {
			r.log.Warn("events: payload marshal failed (dropping payload)", "type", e.Type, "err", err.Error())
		}
	}
	id := r.nextID()
	createdAt := time.Now().UTC().Format(time.RFC3339)

	wctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := r.store.InsertAppEvent(wctx, id, e.OwnerToken, e.AppID, e.SandboxID,
		e.TaskID, e.SnapshotID, e.Type, e.Severity, e.Message, payload, createdAt); err != nil && r.log != nil {
		r.log.Warn("events: write failed", "type", e.Type, "err", err.Error())
	}
}
