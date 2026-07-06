package api

import (
	"context"
	"database/sql"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/tastyeffectco/sandboxd/control-plane/internal/auth"
	"github.com/tastyeffectco/sandboxd/control-plane/internal/store"
)

// These tests cover the parts of Phase 4 that don't require Docker: store
// scoping, the per-app history endpoint, and the tenant/snapshot guards on
// restore/fork that return BEFORE any sandbox is created. The actual
// snapshot spin-up (restore/fork success) needs a real Docker host and is
// verified there, not in CI.

func newSnapTestServer(t *testing.T) (*Server, *store.App) {
	t.Helper()
	st, err := store.Open(context.Background(), "file::memory:?_fk=1", "../../migrations")
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	t.Cleanup(func() { st.Close() })
	s := &Server{Store: st, Log: slog.New(slog.NewTextHandler(io.Discard, nil))}
	app := &store.App{ID: "01APPSNAP0000000000000001", OwnerToken: cfgTenant, Name: "App"}
	if err := st.CreateApp(context.Background(), app); err != nil {
		t.Fatal(err)
	}
	return s, app
}

func snapReq(s *Server, method, target, body, tenant string, pv map[string]string, h http.HandlerFunc) *httptest.ResponseRecorder {
	var r *http.Request
	if body != "" {
		r = httptest.NewRequest(method, target, strings.NewReader(body))
	} else {
		r = httptest.NewRequest(method, target, nil)
	}
	r = r.WithContext(auth.WithActor(r.Context(), auth.Actor{Name: tenant, Kind: "service"}))
	for k, v := range pv {
		r.SetPathValue(k, v)
	}
	w := httptest.NewRecorder()
	h(w, r)
	return w
}

func mkSnapshot(t *testing.T, s *Server, owner, appID, status string) string {
	t.Helper()
	id := newULID()
	snap := &store.Snapshot{
		ID: id, Name: "snap", OwnerToken: owner, Status: status,
		SourceAppID: sql.NullString{String: appID, Valid: appID != ""},
		ImagePath:   "/tmp/library/" + id,
	}
	if err := s.Store.CreateSnapshot(context.Background(), snap); err != nil {
		t.Fatal(err)
	}
	return id
}

// Store-level: ListSnapshotsByApp is scoped by both tenant and app, and
// CreateSnapshot persists source_app_id (migration 0015).
func TestListSnapshotsByAppScoping(t *testing.T) {
	s, app := newSnapTestServer(t)
	mine := mkSnapshot(t, s, cfgTenant, app.ID, "ready")
	mkSnapshot(t, s, cfgTenant, "other-app", "ready") // same tenant, different app
	mkSnapshot(t, s, "tenant-2", app.ID, "ready")     // same app id, different tenant

	rows, err := s.Store.ListSnapshotsByApp(context.Background(), cfgTenant, app.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(rows) != 1 || rows[0].ID != mine {
		t.Fatalf("want exactly snapshot %s, got %+v", mine, rows)
	}
	if !rows[0].SourceAppID.Valid || rows[0].SourceAppID.String != app.ID {
		t.Errorf("source_app_id not persisted: %+v", rows[0].SourceAppID)
	}
}

// GET /v1/apps/{id}/snapshots returns this app's history and is tenant-scoped.
func TestAppSnapshotsHistoryEndpoint(t *testing.T) {
	s, app := newSnapTestServer(t)
	mkSnapshot(t, s, cfgTenant, app.ID, "ready")

	w := snapReq(s, "GET", "/v1/apps/"+app.ID+"/snapshots", "", cfgTenant,
		map[string]string{"id": app.ID}, s.v1ListAppSnapshots)
	if w.Code != http.StatusOK {
		t.Fatalf("history: %d %s", w.Code, w.Body)
	}
	var got struct {
		Snapshots []v1Snapshot `json:"snapshots"`
	}
	json.Unmarshal(w.Body.Bytes(), &got)
	if len(got.Snapshots) != 1 || got.Snapshots[0].SourceAppID != app.ID {
		t.Fatalf("history wrong: %+v", got.Snapshots)
	}

	// A different tenant must not see this app at all (404, no existence leak).
	w2 := snapReq(s, "GET", "/v1/apps/"+app.ID+"/snapshots", "", "tenant-2",
		map[string]string{"id": app.ID}, s.v1ListAppSnapshots)
	if w2.Code != http.StatusNotFound {
		t.Errorf("cross-tenant history = %d; want 404", w2.Code)
	}
}

// Restore guards: cross-tenant app, missing/cross-tenant/not-ready snapshot,
// and missing snapshot_id all fail BEFORE any sandbox work.
func TestRestoreGuards(t *testing.T) {
	s, app := newSnapTestServer(t)
	ready := mkSnapshot(t, s, cfgTenant, app.ID, "ready")
	t2 := mkSnapshot(t, s, "tenant-2", "x", "ready")
	notReady := mkSnapshot(t, s, cfgTenant, app.ID, "error")
	P := "/v1/apps/" + app.ID + "/restore"
	pv := map[string]string{"id": app.ID}

	cases := []struct {
		name, tenant, body string
		want               int
	}{
		{"cross-tenant app", "tenant-2", `{"snapshot_id":"` + ready + `"}`, 404},
		{"missing snapshot", cfgTenant, `{"snapshot_id":"01NONEXISTENT00000000000"}`, 404},
		{"cross-tenant snapshot", cfgTenant, `{"snapshot_id":"` + t2 + `"}`, 404},
		{"not-ready snapshot", cfgTenant, `{"snapshot_id":"` + notReady + `"}`, 400},
		{"missing snapshot_id", cfgTenant, `{}`, 400},
	}
	for _, c := range cases {
		w := snapReq(s, "POST", P, c.body, c.tenant, pv, s.v1RestoreApp)
		if w.Code != c.want {
			t.Errorf("%s: got %d, want %d (%s)", c.name, w.Code, c.want, w.Body)
		}
	}
}

// Fork guards: cross-tenant app and missing snapshot fail before any app or
// sandbox is created.
func TestForkGuards(t *testing.T) {
	s, app := newSnapTestServer(t)
	ready := mkSnapshot(t, s, cfgTenant, app.ID, "ready")
	P := "/v1/apps/" + app.ID + "/fork"
	pv := map[string]string{"id": app.ID}

	if w := snapReq(s, "POST", P, `{"snapshot_id":"`+ready+`"}`, "tenant-2", pv, s.v1ForkApp); w.Code != http.StatusNotFound {
		t.Errorf("cross-tenant fork = %d; want 404", w.Code)
	}
	if w := snapReq(s, "POST", P, `{"snapshot_id":"01NOPE0000000000000000000"}`, cfgTenant, pv, s.v1ForkApp); w.Code != http.StatusNotFound {
		t.Errorf("missing-snapshot fork = %d; want 404", w.Code)
	}
	// No app should have been created by the failed forks.
	apps, _ := s.Store.ListAppsForOwner(context.Background(), cfgTenant, "")
	if len(apps) != 1 {
		t.Errorf("failed forks created apps: have %d, want 1", len(apps))
	}
}
