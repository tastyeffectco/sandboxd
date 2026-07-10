package store

import (
	"context"
	"database/sql"
	"errors"
	"testing"
)

// The full app-delete cascade: DeleteApp wipes the app row and every app-scoped
// row (config, events, snapshots captured from it), and the lookup helpers feed
// the handler the sandbox ids + snapshot files to tear down. Sandbox rows are
// left to the purge path, so DeleteApp must NOT touch them.
func TestDeleteAppCascade(t *testing.T) {
	st := openTestStore(t)
	ctx := context.Background()

	app := &App{ID: "01APP0000000000000000000D1", OwnerToken: "tenant-1", Name: "Doomed"}
	if err := st.CreateApp(ctx, app); err != nil {
		t.Fatal(err)
	}
	// A sandbox linked to the app.
	sb := &Sandbox{
		ID: "01SBX0000000000000000000S1", Status: "stopped",
		Image: "img", WorkspaceImg: "/w.img", WorkspaceMnt: "/w",
		AppID: sql.NullString{String: app.ID, Valid: true},
	}
	if err := st.Create(ctx, sb); err != nil {
		t.Fatal(err)
	}
	// App config, and a snapshot captured from the app.
	if err := st.CreateAppConfig(ctx, &AppConfig{
		ID: "01CFG0000000000000000000C1", AppID: app.ID, Key: "PORT",
		ValuePlaintext: sql.NullString{String: "3000", Valid: true}, AccessPolicy: "both",
	}); err != nil {
		t.Fatal(err)
	}
	if err := st.CreateSnapshot(ctx, &Snapshot{
		ID: "01SNP0000000000000000000N1", Name: "snap", OwnerToken: "tenant-1",
		BaseImage: "img", Visibility: "private", Format: "raw", Status: "ready",
		ImagePath:   "/var/lib/sandboxed/library/01SNP0000000000000000000N1.img",
		SourceAppID: sql.NullString{String: app.ID, Valid: true},
	}); err != nil {
		t.Fatal(err)
	}

	// Lookups the handler uses to drive the teardown.
	if ids, err := st.SandboxIDsForApp(ctx, app.ID); err != nil || len(ids) != 1 || ids[0] != sb.ID {
		t.Fatalf("SandboxIDsForApp = %v, err=%v; want [%s]", ids, err, sb.ID)
	}
	if paths, err := st.SnapshotImagePathsForApp(ctx, app.ID); err != nil || len(paths) != 1 {
		t.Fatalf("SnapshotImagePathsForApp = %v, err=%v; want 1 path", paths, err)
	}

	// Delete everything app-scoped.
	if err := st.DeleteApp(ctx, app.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := st.GetApp(ctx, app.ID); !errors.Is(err, ErrNotFound) {
		t.Errorf("app still present after DeleteApp: %v", err)
	}
	if cfg, _ := st.ListAppConfig(ctx, app.ID); len(cfg) != 0 {
		t.Errorf("app_config not cascaded: %d rows remain", len(cfg))
	}
	if _, err := st.GetSnapshot(ctx, "01SNP0000000000000000000N1"); !errors.Is(err, ErrNotFound) {
		t.Errorf("snapshot not cascaded: %v", err)
	}

	// Idempotent: deleting again is a no-op, not an error.
	if err := st.DeleteApp(ctx, app.ID); err != nil {
		t.Errorf("second DeleteApp should be a no-op, got %v", err)
	}
}
