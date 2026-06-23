// v1_events.go — Phase 5 read API for the durable app_events timeline.
// Tenant-scoped (owner_token = the API token), newest-first, cursor-paginated
// by the `before` ULID. Cross-tenant reads return empty (no existence leak).
package api

import (
	"encoding/json"
	"net/http"
	"strconv"

	"github.com/sandboxd/control-plane/internal/store"
)

// decodeEventPayload turns the stored payload_json back into an object for the
// API response (it was written as valid JSON by the recorder). nil on error.
func decodeEventPayload(s string) map[string]any {
	var m map[string]any
	if err := json.Unmarshal([]byte(s), &m); err != nil {
		return nil
	}
	return m
}

const (
	eventsDefaultLimit = 50
	eventsMaxLimit     = 200
)

type v1Event struct {
	ID         string         `json:"id"`
	Type       string         `json:"type"`
	Severity   string         `json:"severity"`
	Message    string         `json:"message"`
	AppID      string         `json:"app_id,omitempty"`
	SandboxID  string         `json:"sandbox_id,omitempty"`
	TaskID     string         `json:"task_id,omitempty"`
	SnapshotID string         `json:"snapshot_id,omitempty"`
	Payload    map[string]any `json:"payload,omitempty"`
	CreatedAt  string         `json:"created_at"`
}

func v1EventFromRow(e *store.AppEvent) v1Event {
	out := v1Event{
		ID: e.ID, Type: e.Type, Severity: e.Severity, Message: e.Message,
		CreatedAt: e.CreatedAt,
	}
	if e.AppID.Valid {
		out.AppID = e.AppID.String
	}
	if e.SandboxID.Valid {
		out.SandboxID = e.SandboxID.String
	}
	if e.TaskID.Valid {
		out.TaskID = e.TaskID.String
	}
	if e.SnapshotID.Valid {
		out.SnapshotID = e.SnapshotID.String
	}
	// payload_json is stored as valid JSON; surface it as a nested object so
	// integrators don't have to re-parse a string.
	if e.PayloadJSON.Valid && e.PayloadJSON.String != "" {
		out.Payload = decodeEventPayload(e.PayloadJSON.String)
	}
	return out
}

// eventsLimit parses ?limit, clamped to [1, eventsMaxLimit], default 50.
func eventsLimit(r *http.Request) int {
	n, err := strconv.Atoi(r.URL.Query().Get("limit"))
	if err != nil || n <= 0 {
		return eventsDefaultLimit
	}
	if n > eventsMaxLimit {
		return eventsMaxLimit
	}
	return n
}

// v1ListAppEvents — GET /v1/apps/{id}/events?limit=&before=. Newest-first,
// tenant- and app-scoped. A cross-tenant/unknown app is 404.
func (s *Server) v1ListAppEvents(w http.ResponseWriter, r *http.Request) {
	app, ok := s.appForConfig(w, r) // tenant-scoped resolve (404 on cross-tenant)
	if !ok {
		return
	}
	rows, err := s.Store.ListAppEvents(r.Context(), tenantToken(r), app.ID,
		r.URL.Query().Get("before"), eventsLimit(r))
	if err != nil {
		writeV1Err(w, http.StatusInternalServerError, "internal", err.Error())
		return
	}
	writeEventsPage(w, rows)
}

// v1ListTaskEvents — GET /v1/tasks/{id}/events?limit=&before=. Newest-first,
// scoped by owner_token + task_id, so another tenant's task id returns an
// empty page rather than leaking.
func (s *Server) v1ListTaskEvents(w http.ResponseWriter, r *http.Request) {
	rows, err := s.Store.ListTaskEvents(r.Context(), tenantToken(r), r.PathValue("id"),
		r.URL.Query().Get("before"), eventsLimit(r))
	if err != nil {
		writeV1Err(w, http.StatusInternalServerError, "internal", err.Error())
		return
	}
	writeEventsPage(w, rows)
}

// writeEventsPage renders a page newest-first plus a next_before cursor (the
// oldest id in this page) when the page was full — pass it back as ?before=.
func writeEventsPage(w http.ResponseWriter, rows []*store.AppEvent) {
	out := make([]v1Event, 0, len(rows))
	for _, e := range rows {
		out = append(out, v1EventFromRow(e))
	}
	body := map[string]any{"events": out}
	if len(rows) > 0 {
		body["next_before"] = rows[len(rows)-1].ID
	}
	writeJSON(w, http.StatusOK, body)
}
