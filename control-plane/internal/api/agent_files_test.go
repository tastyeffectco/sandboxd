package api

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"

	"github.com/tastyeffectco/sandboxd/control-plane/internal/agentcatalog"
)

func upload(t *testing.T, s *Server, name, body string) (int, agentAttachment) {
	t.Helper()
	req := asAlice(httptest.NewRequest(http.MethodPost, "/v1/agent-instances/h1/files?name="+url.QueryEscape(name), strings.NewReader(body)))
	req.SetPathValue("id", "h1")
	rec := httptest.NewRecorder()
	s.v1UploadAgentFile(rec, req)
	var a agentAttachment
	_ = json.Unmarshal(rec.Body.Bytes(), &a)
	return rec.Code, a
}

// A file attached in chat lands in the agent's own folder, and the agent is
// told where it is.
func TestAgentFileAttachedAndShownToTheAgent(t *testing.T) {
	s, _, _ := agentTestServer(t)
	s.AgentRoot = t.TempDir()
	code, a := upload(t, s, "../../Q3 report.pdf", "%PDF-1.4 numbers")
	if code != http.StatusCreated || a.Name != "Q3 report.pdf" || a.Size != 16 || a.Type != "application/pdf" {
		t.Fatalf("HTTP %d %+v", code, a)
	}
	if strings.Contains(a.ID, "/") || !strings.HasSuffix(a.ID, "-Q3 report.pdf") {
		t.Errorf("id %q", a.ID)
	}
	h, _ := s.Store.GetAgentInstanceForOwner(t.Context(), "h1", "alice")
	files, err := s.takeAttachments(h, []string{a.ID})
	if err != nil || len(files) != 1 {
		t.Fatalf("%v %v", files, err)
	}
	b, _ := json.Marshal(files)
	got := withAttachments("What's in it?", string(b), agentcatalog.Hermes)
	if !strings.Contains(got, "What's in it?") || !strings.Contains(got, "/opt/data/uploads/"+a.ID) || !strings.Contains(got, "Q3 report.pdf") {
		t.Errorf("the agent reads:\n%s", got)
	}
	// It can be opened again from the conversation, as a download.
	req := asAlice(httptest.NewRequest(http.MethodGet, "/v1/agent-instances/h1/files/x", nil))
	req.SetPathValue("id", "h1")
	req.SetPathValue("file", a.ID)
	rec := httptest.NewRecorder()
	s.v1GetAgentFile(rec, req)
	if rec.Code != 200 || rec.Body.String() != "%PDF-1.4 numbers" || !strings.Contains(rec.Header().Get("Content-Disposition"), "attachment") {
		t.Errorf("HTTP %d %v %q", rec.Code, rec.Header(), rec.Body.String())
	}
}

func TestAgentFilesStayInTheUploadsFolder(t *testing.T) {
	s, _, _ := agentTestServer(t)
	s.AgentRoot = t.TempDir()
	h, _ := s.Store.GetAgentInstanceForOwner(t.Context(), "h1", "alice")
	for _, id := range []string{"../config.yaml", "01m7c3x5a07qfrmwar40qjgs0w-../../config.yaml", "config.yaml", ""} {
		if _, err := s.takeAttachments(h, []string{id}); err == nil {
			t.Errorf("%q was accepted", id)
		}
		req := asAlice(httptest.NewRequest(http.MethodGet, "/v1/agent-instances/h1/files/x", nil))
		req.SetPathValue("id", "h1")
		req.SetPathValue("file", id)
		rec := httptest.NewRecorder()
		s.v1GetAgentFile(rec, req)
		if rec.Code != http.StatusNotFound {
			t.Errorf("%q: HTTP %d", id, rec.Code)
		}
	}
	if code, _ := upload(t, s, "empty.txt", ""); code != http.StatusBadRequest {
		t.Errorf("empty file: HTTP %d", code)
	}
	ids := make([]string, 11)
	if _, err := s.takeAttachments(h, ids); err == nil {
		t.Error("11 files at once were accepted")
	}
}
