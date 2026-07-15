package api

import (
	"context"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/tastyeffectco/sandboxd/control-plane/internal/store"
)

func newTerminalServer(t *testing.T) *Server {
	t.Helper()
	st, err := store.Open(context.Background(), "file::memory:?_fk=1", "../../migrations")
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	t.Cleanup(func() { st.Close() })
	return &Server{Store: st, Log: slog.New(slog.NewTextHandler(io.Discard, nil))}
}

// callTerminal invokes v1Terminal with the id as a path value (no WebSocket
// upgrade happens on the guard paths, so a plain recorder is fine).
func callTerminal(s *Server, id string) *httptest.ResponseRecorder {
	r := httptest.NewRequest(http.MethodGet, "/v1/sandboxes/"+id+"/terminal", nil)
	r.SetPathValue("id", id)
	w := httptest.NewRecorder()
	s.v1Terminal(w, r)
	return w
}

// The terminal endpoint guards before upgrading: bad id and unknown sandbox 404,
// and a non-running sandbox is a 409 (you must start it first).
func TestTerminalGuards(t *testing.T) {
	s := newTerminalServer(t)

	if w := callTerminal(s, "not-a-ulid"); w.Code != http.StatusNotFound {
		t.Errorf("bad id: got %d, want 404", w.Code)
	}
	if w := callTerminal(s, newULID()); w.Code != http.StatusNotFound {
		t.Errorf("unknown sandbox: got %d, want 404", w.Code)
	}

	stopped := &store.Sandbox{ID: newULID(), Status: "stopped", ExternalUserID: nullStr("local")}
	if err := s.Store.Create(context.Background(), stopped); err != nil {
		t.Fatal(err)
	}
	if w := callTerminal(s, stopped.ID); w.Code != http.StatusConflict {
		t.Errorf("stopped sandbox: got %d, want 409", w.Code)
	}
}
