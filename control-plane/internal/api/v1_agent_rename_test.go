package api

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func patchAgent(t *testing.T, s *Server, body string) *httptest.ResponseRecorder {
	t.Helper()
	req := asAlice(httptest.NewRequest(http.MethodPatch, "/v1/agent-instances/h1", strings.NewReader(body)))
	req.SetPathValue("id", "h1")
	rec := httptest.NewRecorder()
	s.v1UpdateAgentInstance(rec, req)
	return rec
}

// A name alone renames the agent (the sidebar shows it); nothing restarts.
func TestRenameAgentInstance(t *testing.T) {
	s, st, _ := agentTestServer(t)
	if rec := patchAgent(t, s, `{"name":"  Hermes for work "}`); rec.Code != http.StatusOK || !strings.Contains(rec.Body.String(), `"name":"Hermes for work"`) {
		t.Fatalf("rename: %d %s", rec.Code, rec.Body.String())
	}
	h, err := st.GetAgentInstanceForOwner(t.Context(), "h1", "alice")
	if err != nil || h.Name != "Hermes for work" || h.ConnectionID != "c1" || h.Model != "m1" {
		t.Fatalf("stored %+v %v", h, err)
	}
	for _, bad := range []string{`{"name":"   "}`, `{"name":"` + strings.Repeat("x", 61) + `"}`, `{}`} {
		if rec := patchAgent(t, s, bad); rec.Code != http.StatusBadRequest {
			t.Errorf("%s: %d %s", bad, rec.Code, rec.Body.String())
		}
	}
	h, _ = st.GetAgentInstanceForOwner(t.Context(), "h1", "alice")
	if h.Name != "Hermes for work" {
		t.Errorf("a refused rename changed the name to %q", h.Name)
	}
}
