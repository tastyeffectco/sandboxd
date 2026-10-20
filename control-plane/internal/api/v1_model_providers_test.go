package api

import (
	"context"
	"encoding/json"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/tastyeffectco/sandboxd/control-plane/internal/modelprovider"
	"github.com/tastyeffectco/sandboxd/control-plane/internal/runtime"
	"github.com/tastyeffectco/sandboxd/control-plane/internal/secrets"
)

func providerTestServer(t *testing.T) (*Server, string, string) {
	t.Helper()
	s, id, mnt := newProcLogsServer(t)
	cipher, err := secrets.Load("", filepath.Join(t.TempDir(), "key"))
	if err != nil {
		t.Fatal(err)
	}
	s.ModelProviders, err = modelprovider.New(s.Store, cipher)
	if err != nil {
		t.Fatal(err)
	}
	s.AgentProxyURL = "http://sandboxd:9100"
	return s, id, mnt
}

func TestModelProviderAPIAndTaskDispatch(t *testing.T) {
	s, id, mnt := providerTestServer(t)
	w := httptest.NewRecorder()
	input := `{"name":"Gateway","protocol":"openai-completions","base_url":"http://gateway:4000/v1","models":[{"id":"team/model:latest"}],"api_key":"secret-upstream-key"}`
	s.v1SaveModelProvider(w, httptest.NewRequest("POST", "/v1/model-providers", strings.NewReader(input)))
	if w.Code != 201 || strings.Contains(w.Body.String(), "secret-upstream-key") {
		t.Fatalf("create: %d %s", w.Code, w.Body.String())
	}
	var c modelprovider.Config
	if json.Unmarshal(w.Body.Bytes(), &c) != nil || !c.HasAPIKey {
		t.Fatal("missing connection metadata")
	}
	w = httptest.NewRecorder()
	s.v1ListModelProviders(w, httptest.NewRequest("GET", "/v1/model-providers", nil))
	if w.Code != 200 || strings.Contains(w.Body.String(), "secret-upstream-key") {
		t.Fatal("list leaked key")
	}
	// A real Unix HTTP listener captures the dispatched runtimed contract.
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
		if strings.HasSuffix(r.URL.Path, "/events") {
			parts := strings.Split(r.URL.Path, "/")
			w.Header().Set("Content-Type", "application/x-ndjson")
			json.NewEncoder(w).Encode(runtime.Event{Type: runtime.EventDone,
				Data: json.RawMessage(`{"id":"` + parts[2] + `","status":"succeeded","files_changed":[]}`)})
			return
		}
		w.WriteHeader(404)
	})}
	go server.Serve(listener)
	defer server.Close()
	taskBody := `{"prompt":"test","agent":"pi","provider":"` + c.ID + `","model":"team/model:latest","timeout_s":30}`
	r := httptest.NewRequest("POST", "/v1/sandboxes/"+id+"/tasks", strings.NewReader(taskBody))
	r.SetPathValue("id", id)
	w = httptest.NewRecorder()
	s.v1SubmitTask(w, r)
	if w.Code != 202 {
		t.Fatalf("submit: %d %s", w.Code, w.Body.String())
	}
	got := <-dispatched
	raw, _ := json.Marshal(got)
	if got.Provider == nil || got.Agent != "pi" || got.Model != "team/model:latest" || got.Provider.Token == "" ||
		got.Provider.BaseURL != "http://sandboxd:9100/providers/"+c.ID+"/v1" || strings.Contains(string(raw), "secret-upstream-key") || strings.Contains(string(raw), "gateway:4000") {
		t.Fatal("bad runtime contract or leaked upstream information")
	}
	for _, body := range []string{
		`{"prompt":"test","agent":"pi"}`,
		`{"prompt":"test","agent":"pi","provider":"missing"}`,
		`{"prompt":"test","agent":"pi","provider":"` + c.ID + `","model":"unconfigured"}`,
		`{"prompt":"test","agent":"claude-code","provider":"` + c.ID + `"}`,
	} {
		r = httptest.NewRequest("POST", "/tasks", strings.NewReader(body))
		r.SetPathValue("id", id)
		w = httptest.NewRecorder()
		s.v1SubmitTask(w, r)
		if w.Code != 400 {
			t.Fatalf("invalid selection not rejected: %d %s", w.Code, w.Body.String())
		}
	}
	deadline := time.Now().Add(2 * time.Second)
	for {
		persisted, err := s.Store.GetTask(context.Background(), got.TaskID)
		if err == nil && persisted.Status == "succeeded" {
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("task watcher did not persist the terminal event: task=%+v err=%v", persisted, err)
		}
		time.Sleep(10 * time.Millisecond)
	}
	if err := s.ModelProviders.Delete(context.Background(), c.ID); err != nil {
		t.Fatal(err)
	}
}
