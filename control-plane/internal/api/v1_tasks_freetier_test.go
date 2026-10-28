package api

import (
	"encoding/json"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"

	"github.com/tastyeffectco/sandboxd/control-plane/internal/agentauth"
	"github.com/tastyeffectco/sandboxd/control-plane/internal/runtime"
)

// dispatchTask submits body to v1SubmitTask and returns the request runtimed received.
func dispatchTask(t *testing.T, s *Server, id, mnt, body string) runtime.StartTaskRequest {
	t.Helper()
	listener, err := net.Listen("unix", filepath.Join(mnt, ".runtimed", "sock"))
	if err != nil {
		t.Fatal(err)
	}
	dispatched := make(chan runtime.StartTaskRequest, 1)
	server := &http.Server{Handler: http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == "POST" && r.URL.Path == "/tasks" {
			var req runtime.StartTaskRequest
			if json.NewDecoder(r.Body).Decode(&req) != nil {
				t.Error("bad runtime JSON")
			}
			dispatched <- req
			w.WriteHeader(202)
			io.WriteString(w, `{"status":"running"}`)
			return
		}
		w.WriteHeader(404)
	})}
	go server.Serve(listener)
	t.Cleanup(func() { server.Close() })
	r := httptest.NewRequest("POST", "/v1/sandboxes/"+id+"/tasks", strings.NewReader(body))
	r.SetPathValue("id", id)
	w := httptest.NewRecorder()
	s.v1SubmitTask(w, r)
	if w.Code != 202 {
		t.Fatalf("submit: %d %s", w.Code, w.Body.String())
	}
	return <-dispatched
}

// Zen serves its free models only to OpenCode's own client, so with nothing
// connected the free model runs on OpenCode's built-in provider.
func TestOpencodeFreeTierRunsWithoutProxy(t *testing.T) {
	for _, tc := range []struct {
		name, body, model string
		free              bool
	}{
		{"default", `{"prompt":"x","agent":"opencode"}`, opencodeFreeModel(), true},
		{"free", `{"prompt":"x","agent":"opencode","model":"opencode/big-pickle"}`, "opencode/big-pickle", true},
		{"paid", `{"prompt":"x","agent":"opencode","model":"opencode/glm-5"}`, "opencode/glm-5", false},
		{"minimax", `{"prompt":"x","agent":"opencode","model":"MiniMax-M3.1-Flash"}`, "MiniMax-M3.1-Flash", false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			s, id, mnt := newProcLogsServer(t)
			got := dispatchTask(t, s, id, mnt, tc.body)
			if got.Model != tc.model || got.FreeTier != tc.free {
				t.Fatalf("model=%q free=%v; want %q free=%v", got.Model, got.FreeTier, tc.model, tc.free)
			}
		})
	}
}

// A connected OpenCode account always goes through the credential proxy.
func TestOpencodeConnectedAccountKeepsProxy(t *testing.T) {
	s, id, mnt := newProcLogsServer(t)
	st := agentauth.NewStore(t.TempDir())
	if err := st.EnsureRoot(); err != nil {
		t.Fatal(err)
	}
	connect(t, st, "opencode")
	s.AgentAuth = st
	got := dispatchTask(t, s, id, mnt, `{"prompt":"x","agent":"opencode","model":"opencode/big-pickle"}`)
	if got.FreeTier {
		t.Fatal("a connected account must not use the free tier")
	}
}

func TestBareModel(t *testing.T) {
	for in, want := range map[string]string{"opencode/big-pickle": "big-pickle", "big-pickle": "big-pickle", "": ""} {
		if got := bareModel(in); got != want {
			t.Errorf("bareModel(%q) = %q; want %q", in, got, want)
		}
	}
}
