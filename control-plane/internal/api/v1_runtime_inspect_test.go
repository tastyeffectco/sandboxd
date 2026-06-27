package api

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/sandboxd/control-plane/internal/store"
)

func inspectServer(t *testing.T) (*Server, *store.App) {
	t.Helper()
	s := &Server{Store: memStore(t)}
	app := &store.App{ID: newULID(), OwnerToken: "tenantA", Name: "imp"}
	if err := s.Store.CreateApp(context.Background(), app); err != nil {
		t.Fatal(err)
	}
	return s, app
}

func TestRuntimeInspectTenantScope(t *testing.T) {
	s, app := inspectServer(t)

	// owner: 200 (no sandbox yet => no-workspace result, not an error)
	r := reqAs("GET", "/v1/apps/"+app.ID+"/runtime-inspect", "", "tenantA")
	r.SetPathValue("id", app.ID)
	w := httptest.NewRecorder()
	s.v1RuntimeInspect(w, r)
	if w.Code != http.StatusOK {
		t.Fatalf("owner: got %d: %s", w.Code, w.Body.String())
	}
	if !strings.Contains(w.Body.String(), "no workspace yet") {
		t.Errorf("expected no-workspace result; got %s", w.Body.String())
	}

	// cross-owner: 404 (owner-scoped lookup)
	r2 := reqAs("GET", "/v1/apps/"+app.ID+"/runtime-inspect", "", "tenantB")
	r2.SetPathValue("id", app.ID)
	w2 := httptest.NewRecorder()
	s.v1RuntimeInspect(w2, r2)
	if w2.Code != http.StatusNotFound {
		t.Fatalf("cross-owner: got %d; want 404", w2.Code)
	}

	// unknown app: 404
	r3 := reqAs("GET", "/v1/apps/NOPE/runtime-inspect", "", "tenantA")
	r3.SetPathValue("id", "NOPE")
	w3 := httptest.NewRecorder()
	s.v1RuntimeInspect(w3, r3)
	if w3.Code != http.StatusNotFound {
		t.Fatalf("unknown app: got %d; want 404", w3.Code)
	}
}
