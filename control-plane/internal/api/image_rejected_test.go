package api

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// Per-app image selection is not supported (the image is instance-wide via
// SANDBOXD_IMAGE). Every create body must reject an `image` field with a clear
// 400 instead of silently dropping it.
func TestImageFieldRejected(t *testing.T) {
	s := &Server{Store: memStore(t)}

	check := func(name string, h http.HandlerFunc, path, body string) {
		t.Run(name, func(t *testing.T) {
			w := httptest.NewRecorder()
			h(w, reqAs("POST", path, body, "tenantA"))
			if w.Code != http.StatusBadRequest {
				t.Fatalf("%s: got %d, want 400; body=%s", name, w.Code, w.Body.String())
			}
			if !strings.Contains(w.Body.String(), "per-app image selection is not supported") {
				t.Errorf("%s: missing explanatory message: %s", name, w.Body.String())
			}
		})
	}

	check("v1CreateApp", s.v1CreateApp, "/v1/apps",
		`{"name":"x","image":"sandboxd-go:test"}`)
	check("v1CreateSandbox", s.v1CreateSandbox, "/v1/sandboxes",
		`{"project":{"id":"p","user_id":"u"},"image":"sandboxd-go:test"}`)
	check("handleCreate", s.handleCreate, "/sandbox",
		`{"ports":[3000],"image":"sandboxd-go:test"}`)
}

// Control: a create body WITHOUT image is not rejected by the image check (it
// fails later for its own reasons, not with the image message).
func TestNoImageFieldNotRejectedForImage(t *testing.T) {
	s := &Server{Store: memStore(t)}
	w := httptest.NewRecorder()
	s.v1CreateApp(w, reqAs("POST", "/v1/apps", `{"name":"x"}`, "tenantA"))
	if strings.Contains(w.Body.String(), "per-app image selection") {
		t.Errorf("a body without image must not trip the image check: %s", w.Body.String())
	}
}
