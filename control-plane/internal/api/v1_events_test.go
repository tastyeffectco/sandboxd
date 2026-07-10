package api

import (
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"path/filepath"
	"strings"
	"testing"

	"github.com/tastyeffectco/sandboxd/control-plane/internal/events"
	"github.com/tastyeffectco/sandboxd/control-plane/internal/runtime"
	"github.com/tastyeffectco/sandboxd/control-plane/internal/secrets"
	"github.com/tastyeffectco/sandboxd/control-plane/internal/store"
)

func newEventsTestServer(t *testing.T) (*Server, *store.App) {
	t.Helper()
	st, err := store.Open(context.Background(), "file::memory:?_fk=1", "../../migrations")
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	t.Cleanup(func() { st.Close() })
	cipher, err := secrets.Load("", filepath.Join(t.TempDir(), "secrets.key"))
	if err != nil {
		t.Fatal(err)
	}
	log := slog.New(slog.NewTextHandler(io.Discard, nil))
	s := &Server{Store: st, Secrets: cipher, Log: log, Events: events.New(st, log)}
	app := &store.App{ID: "01APPEVT0000000000000001", OwnerToken: cfgTenant, Name: "App"}
	if err := st.CreateApp(context.Background(), app); err != nil {
		t.Fatal(err)
	}
	return s, app
}

// The recorder writes a valid, queryable event with valid-JSON payload.
func TestEventRecorderWritesValidEvent(t *testing.T) {
	s, app := newEventsTestServer(t)
	s.Events.Record(context.Background(), events.Event{
		OwnerToken: cfgTenant, AppID: app.ID, Type: events.AppCreated,
		Severity: events.SeverityInfo, Message: "App created",
		Payload: map[string]any{"k": "v"},
	})
	rows, err := s.Store.ListAppEvents(context.Background(), cfgTenant, app.ID, "", 50)
	if err != nil {
		t.Fatal(err)
	}
	if len(rows) != 1 {
		t.Fatalf("want 1 event, got %d", len(rows))
	}
	e := rows[0]
	if e.Type != events.AppCreated || e.Severity != "info" || e.ID == "" || e.CreatedAt == "" {
		t.Errorf("event fields wrong: %+v", e)
	}
	if !e.PayloadJSON.Valid {
		t.Fatal("payload not stored")
	}
	var m map[string]any
	if err := json.Unmarshal([]byte(e.PayloadJSON.String), &m); err != nil || m["k"] != "v" {
		t.Errorf("payload not valid JSON / wrong: %q %v", e.PayloadJSON.String, err)
	}
}

// The app-events endpoint is tenant-scoped: another tenant gets 404 (unknown
// app) and never sees this tenant's events.
func TestAppEventsTenantScoping(t *testing.T) {
	s, app := newEventsTestServer(t)
	s.Events.Record(context.Background(), events.Event{OwnerToken: cfgTenant, AppID: app.ID,
		Type: events.AppCreated, Severity: events.SeverityInfo, Message: "mine"})
	// A foreign-tenant event on the same app id must not leak either.
	s.Events.Record(context.Background(), events.Event{OwnerToken: "tenant-2", AppID: app.ID,
		Type: events.AppCreated, Severity: events.SeverityInfo, Message: "theirs"})

	w := snapReq(s, "GET", "/v1/apps/"+app.ID+"/events", "", cfgTenant, map[string]string{"id": app.ID}, s.v1ListAppEvents)
	if w.Code != http.StatusOK {
		t.Fatalf("owner GET: %d %s", w.Code, w.Body)
	}
	if strings.Contains(w.Body.String(), "theirs") {
		t.Error("tenant leak: saw another tenant's event")
	}
	var got struct {
		Events []v1Event `json:"events"`
	}
	json.Unmarshal(w.Body.Bytes(), &got)
	if len(got.Events) != 1 || got.Events[0].Message != "mine" {
		t.Errorf("owner feed wrong: %+v", got.Events)
	}
	// Cross-tenant app -> 404.
	w2 := snapReq(s, "GET", "/v1/apps/"+app.ID+"/events", "", "tenant-2", map[string]string{"id": app.ID}, s.v1ListAppEvents)
	if w2.Code != http.StatusNotFound {
		t.Errorf("cross-tenant events = %d; want 404", w2.Code)
	}
}

// Pagination: limit caps the page and next_before walks older events.
func TestAppEventsPaginationAndLimit(t *testing.T) {
	s, app := newEventsTestServer(t)
	for i := 0; i < 5; i++ {
		s.Events.Record(context.Background(), events.Event{OwnerToken: cfgTenant, AppID: app.ID,
			Type: events.AppUpdated, Severity: events.SeverityInfo, Message: "e"})
	}
	first := snapReq(s, "GET", "/v1/apps/"+app.ID+"/events?limit=2", "", cfgTenant, map[string]string{"id": app.ID}, s.v1ListAppEvents)
	var p1 struct {
		Events     []v1Event `json:"events"`
		NextBefore string    `json:"next_before"`
	}
	json.Unmarshal(first.Body.Bytes(), &p1)
	if len(p1.Events) != 2 || p1.NextBefore == "" {
		t.Fatalf("page1 wrong: %d events, next=%q", len(p1.Events), p1.NextBefore)
	}
	second := snapReq(s, "GET", "/v1/apps/"+app.ID+"/events?limit=2&before="+p1.NextBefore, "", cfgTenant, map[string]string{"id": app.ID}, s.v1ListAppEvents)
	var p2 struct {
		Events []v1Event `json:"events"`
	}
	json.Unmarshal(second.Body.Bytes(), &p2)
	if len(p2.Events) != 2 {
		t.Fatalf("page2 wrong: %d", len(p2.Events))
	}
	if p1.Events[0].ID == p2.Events[0].ID {
		t.Error("page2 did not advance past the cursor")
	}
	// Over-cap limit is clamped (no error), under-1 falls back to default.
	if w := snapReq(s, "GET", "/v1/apps/"+app.ID+"/events?limit=99999", "", cfgTenant, map[string]string{"id": app.ID}, s.v1ListAppEvents); w.Code != http.StatusOK {
		t.Errorf("clamped limit: %d", w.Code)
	}
}

// A sensitive config write emits config.created with the KEY but never the
// secret value in payload_json.
func TestConfigCreatedEventHasNoSecret(t *testing.T) {
	s, app := newEventsTestServer(t)
	const secret = "sk-events-secret-1234"
	do(s, "POST", "/v1/apps/"+app.ID+"/config",
		`{"key":"OPENAI_API_KEY","value":"`+secret+`","sensitive":true}`,
		cfgTenant, map[string]string{"id": app.ID})

	rows, _ := s.Store.ListAppEvents(context.Background(), cfgTenant, app.ID, "", 50)
	var found bool
	for _, e := range rows {
		if e.Type != events.ConfigCreated {
			continue
		}
		found = true
		if !strings.Contains(e.PayloadJSON.String, "OPENAI_API_KEY") {
			t.Errorf("config.created payload missing key: %q", e.PayloadJSON.String)
		}
		if strings.Contains(e.PayloadJSON.String, secret) || strings.Contains(e.Message, secret) {
			t.Errorf("config.created LEAKED the secret: msg=%q payload=%q", e.Message, e.PayloadJSON.String)
		}
	}
	if !found {
		t.Fatal("no config.created event emitted")
	}
}

// A failed task produces useful failure events on the app + task timelines,
// and the raw build/preview/agent error text (which can echo secrets the
// app printed) NEVER lands in payload_json or message.
func TestTaskFailureEvents(t *testing.T) {
	s, app := newEventsTestServer(t)
	// A sandbox linked to the app so recordTaskEvents resolves owner/app.
	sb := &store.Sandbox{ID: "01SBXEVT0000000000000001", Status: "running",
		AppID: nullStr(app.ID), ExternalUserID: nullStr("local")}
	if err := s.Store.Create(context.Background(), sb); err != nil {
		t.Fatal(err)
	}
	// Fake secrets planted in the raw error fields.
	const secret = "sk-leak-in-build-output-9999"
	res := &runtime.TaskResult{
		ID: "01TASKEVT000000000000001", Status: runtime.TaskFailed,
		FailureReason:       "agent_error",
		ErrorMessage:        "agent error: " + secret,
		BuildErrorMessage:   "tsc error: API_KEY=" + secret,
		PreviewErrorMessage: "vite 500: " + secret,
		PreviewStatusAfter:  runtime.PreviewError,
		FilesChanged:        []string{},
	}
	s.recordTaskEvents(sb.ID, "01TASKEVT000000000000001", res)

	rows, _ := s.Store.ListAppEvents(context.Background(), cfgTenant, app.ID, "", 50)
	seen := map[string]bool{}
	for _, e := range rows {
		seen[e.Type] = true
		if strings.Contains(e.PayloadJSON.String, secret) || strings.Contains(e.Message, secret) {
			t.Errorf("event %s LEAKED raw error text: msg=%q payload=%q", e.Type, e.Message, e.PayloadJSON.String)
		}
	}
	for _, want := range []string{events.TaskFailed, events.TaskBuildFailed, events.PreviewHealthFailed} {
		if !seen[want] {
			t.Errorf("missing %s event", want)
		}
	}
	// And reachable via the task-scoped feed.
	tr, _ := s.Store.ListTaskEvents(context.Background(), cfgTenant, "01TASKEVT000000000000001", "", 50)
	if len(tr) == 0 {
		t.Error("task feed empty for a failed task")
	}
}

// Monotonic ULIDs: a burst of events recorded in the same millisecond still
// sorts in emission order (newest-first feed returns them reverse-emitted).
func TestEventIDsAreMonotonic(t *testing.T) {
	s, app := newEventsTestServer(t)
	const n = 8
	for i := 0; i < n; i++ {
		s.Events.Record(context.Background(), events.Event{OwnerToken: cfgTenant, AppID: app.ID,
			Type: events.AppUpdated, Severity: events.SeverityInfo, Message: "burst"})
	}
	rows, _ := s.Store.ListAppEvents(context.Background(), cfgTenant, app.ID, "", 50)
	if len(rows) != n {
		t.Fatalf("want %d events, got %d", n, len(rows))
	}
	// Newest-first: ids must be strictly descending (unique + ordered).
	for i := 1; i < len(rows); i++ {
		if rows[i-1].ID <= rows[i].ID {
			t.Errorf("ids not strictly descending at %d: %s <= %s", i, rows[i-1].ID, rows[i].ID)
		}
	}
}
