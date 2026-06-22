package store

import (
	"context"
	"database/sql"
	"errors"
	"testing"
)

func TestAppCRUDAndScoping(t *testing.T) {
	st := openTestStore(t)
	ctx := context.Background()

	a := &App{
		ID: "01APP0000000000000000000A1", OwnerToken: "tenant-1",
		Name: "My App", Description: "d", Tags: []string{"web", "demo"},
		ExternalUserID: sql.NullString{String: "u1", Valid: true},
	}
	if err := st.CreateApp(ctx, a); err != nil {
		t.Fatal(err)
	}

	// Stable ID + full readback (incl. tags round-trip).
	got, err := st.GetAppForOwner(ctx, a.ID, "tenant-1")
	if err != nil {
		t.Fatal(err)
	}
	if got.ID != a.ID || got.Name != "My App" || len(got.Tags) != 2 {
		t.Errorf("readback mismatch: %+v", got)
	}

	// Cross-tenant get is ErrNotFound (no existence leak).
	if _, err := st.GetAppForOwner(ctx, a.ID, "tenant-2"); !errors.Is(err, ErrNotFound) {
		t.Errorf("cross-tenant get = %v; want ErrNotFound", err)
	}

	// List is tenant-scoped, with an optional external_user_id filter.
	_ = st.CreateApp(ctx, &App{ID: "01APP0000000000000000000B2", OwnerToken: "tenant-1",
		Name: "B", ExternalUserID: sql.NullString{String: "u2", Valid: true}})
	_ = st.CreateApp(ctx, &App{ID: "01APP0000000000000000000C3", OwnerToken: "tenant-2", Name: "C"})

	t1, _ := st.ListAppsForOwner(ctx, "tenant-1", "")
	if len(t1) != 2 {
		t.Errorf("tenant-1 apps = %d; want 2 (no tenant-2 leakage)", len(t1))
	}
	filtered, _ := st.ListAppsForOwner(ctx, "tenant-1", "u1")
	if len(filtered) != 1 || filtered[0].ID != a.ID {
		t.Errorf("external_user_id filter = %+v; want only %s", filtered, a.ID)
	}

	// Partial PATCH, tenant-scoped.
	newName := "Renamed"
	if err := st.UpdateApp(ctx, a.ID, "tenant-1", AppPatch{Name: &newName}); err != nil {
		t.Fatal(err)
	}
	got2, _ := st.GetAppForOwner(ctx, a.ID, "tenant-1")
	if got2.Name != "Renamed" || got2.Description != "d" {
		t.Errorf("patch: name=%q desc=%q; want Renamed/d", got2.Name, got2.Description)
	}
	if err := st.UpdateApp(ctx, a.ID, "tenant-2", AppPatch{Name: &newName}); !errors.Is(err, ErrNotFound) {
		t.Errorf("cross-tenant patch = %v; want ErrNotFound", err)
	}
}

// The core Phase-1 guarantee: an app outlives its sandbox.
func TestAppSurvivesSandboxDelete(t *testing.T) {
	st := openTestStore(t)
	ctx := context.Background()

	app := &App{ID: "01APPSURV0000000000000001", OwnerToken: "t", Name: "App"}
	if err := st.CreateApp(ctx, app); err != nil {
		t.Fatal(err)
	}

	// No sandbox yet → no current.
	if _, err := st.CurrentSandboxForApp(ctx, app.ID); !errors.Is(err, ErrNotFound) {
		t.Errorf("current with no sandbox = %v; want ErrNotFound", err)
	}

	// Attach a sandbox to the app.
	sb := minimalSandbox("01SBXAPP00000000000000001", "sleep")
	sb.AppID = sql.NullString{String: app.ID, Valid: true}
	if err := st.Create(ctx, sb); err != nil {
		t.Fatal(err)
	}
	cur, err := st.CurrentSandboxForApp(ctx, app.ID)
	if err != nil || cur.ID != sb.ID {
		t.Fatalf("current sandbox = %v, %v; want %s", cur, err, sb.ID)
	}

	// Delete the sandbox — app + its metadata survive; current becomes none.
	if err := st.Delete(ctx, sb.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := st.CurrentSandboxForApp(ctx, app.ID); !errors.Is(err, ErrNotFound) {
		t.Errorf("current after sandbox delete = %v; want ErrNotFound", err)
	}
	survived, err := st.GetAppForOwner(ctx, app.ID, "t")
	if err != nil || survived.Name != "App" {
		t.Errorf("app must survive sandbox delete; got %v, %v", survived, err)
	}
}
