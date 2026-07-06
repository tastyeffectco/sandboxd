package api

import (
	"context"
	"database/sql"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/tastyeffectco/sandboxd/control-plane/internal/manifest"
	"github.com/tastyeffectco/sandboxd/control-plane/internal/store"
)

func TestValidateManifestEndpoint(t *testing.T) {
	s := &Server{Store: memStore(t)}
	call := func(body string) (*httptest.ResponseRecorder, manifest.Result) {
		r := reqAs("POST", "/v1/runtime/manifest/validate", body, "t")
		w := httptest.NewRecorder()
		s.v1ValidateManifest(w, r)
		var res manifest.Result
		json.Unmarshal(w.Body.Bytes(), &res)
		return w, res
	}
	// good
	_, ok := call(`{"manifest":"version: 1\nweb:\n  command: \"pnpm dev --host 0.0.0.0 --port 3000\"\n  port: 3000\n"}`)
	if !ok.Valid || ok.Effective == nil || ok.Effective.Web.Port != 3000 {
		t.Fatalf("good manifest: %+v", ok)
	}
	// top-level command rejected
	_, bad := call(`{"manifest":"version: 1\ncommand: x\n"}`)
	if bad.Valid {
		t.Errorf("top-level command should be invalid: %+v", bad)
	}
	// invalid json -> 400
	if w := reqAs("POST", "/v1/runtime/manifest/validate", "{not json", "t"); true {
		rr := httptest.NewRecorder()
		s.v1ValidateManifest(rr, w)
		if rr.Code != http.StatusBadRequest {
			t.Errorf("bad json: got %d", rr.Code)
		}
	}
}

func manifestServer(t *testing.T) (*Server, *store.App) {
	t.Helper()
	s := &Server{Store: memStore(t)}
	app := &store.App{ID: newULID(), OwnerToken: "tenantA", Name: "imp"}
	if err := s.Store.CreateApp(context.Background(), app); err != nil {
		t.Fatal(err)
	}
	return s, app
}

func getManifest(s *Server, appID, owner string) *httptest.ResponseRecorder {
	r := reqAs("GET", "/v1/apps/"+appID+"/runtime/manifest", "", owner)
	r.SetPathValue("id", appID)
	w := httptest.NewRecorder()
	s.v1AppManifest(w, r)
	return w
}

func TestAppManifestOwnerScopedAndNoWorkspace(t *testing.T) {
	s, app := manifestServer(t)
	// cross-owner -> 404
	if w := getManifest(s, app.ID, "tenantB"); w.Code != http.StatusNotFound {
		t.Errorf("cross-owner: got %d", w.Code)
	}
	// owner, no sandbox -> present:false reason no_workspace (not 500)
	w := getManifest(s, app.ID, "tenantA")
	if w.Code != http.StatusOK || !strings.Contains(w.Body.String(), "no_workspace") {
		t.Errorf("no workspace: %d %s", w.Code, w.Body.String())
	}
}

func TestAppManifestFromFile(t *testing.T) {
	s, app := manifestServer(t)
	mnt := t.TempDir()
	appDir := filepath.Join(mnt, "workspace", "app")
	os.MkdirAll(appDir, 0o755)
	os.WriteFile(filepath.Join(appDir, "sandbox.yaml"),
		[]byte("version: 1\nweb:\n  command: \"pnpm dev --host 0.0.0.0 --port 3000\"\n  port: 3000\n"), 0o644)
	sb := &store.Sandbox{ID: newULID(), Status: "stopped", WorkspaceMnt: mnt,
		AppID: sql.NullString{String: app.ID, Valid: true}}
	s.Store.Create(context.Background(), sb)

	w := getManifest(s, app.ID, "tenantA")
	var resp v1AppManifestResp
	json.Unmarshal(w.Body.Bytes(), &resp)
	if !resp.Present || resp.Source != "sandbox.yaml" || resp.Validation == nil || !resp.Validation.Valid {
		t.Fatalf("from file: %+v", resp)
	}
	if !strings.Contains(resp.Manifest, "pnpm dev") {
		t.Errorf("raw manifest not returned")
	}
}

func TestAppManifestFromPreset(t *testing.T) {
	s := &Server{Store: memStore(t)}
	app := &store.App{ID: newULID(), OwnerToken: "tenantA", Name: "imp", RuntimePreset: nullStr("nextjs")}
	s.Store.CreateApp(context.Background(), app)
	// a sandbox with an empty workspace (no sandbox.yaml on disk yet)
	sb := &store.Sandbox{ID: newULID(), Status: "stopped", WorkspaceMnt: t.TempDir(),
		AppID: sql.NullString{String: app.ID, Valid: true}}
	s.Store.Create(context.Background(), sb)

	w := getManifest(s, app.ID, "tenantA")
	var resp v1AppManifestResp
	json.Unmarshal(w.Body.Bytes(), &resp)
	if resp.Present || resp.Source != "preset" || resp.Validation == nil {
		t.Fatalf("from preset: %+v", resp)
	}
	if !strings.Contains(resp.Manifest, "web:") {
		t.Errorf("preset manifest not returned: %q", resp.Manifest)
	}
}
