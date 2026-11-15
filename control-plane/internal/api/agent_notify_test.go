package api

import (
	"context"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/tastyeffectco/sandboxd/control-plane/internal/agentcatalog"
)

// A scheduled job's result reaches the person through message_user: it shows
// in the agent's conversation, after what was already there.
func TestAgentMessageUserPostsInTheChat(t *testing.T) {
	s, st, secret := agentTestServer(t)
	p := s.AgentProxy()
	_, out := mcpCall(t, p, "/agents/h1/mcp", secret, "tools/call",
		map[string]any{"name": "message_user", "arguments": map[string]string{"text": "  The page title is Example Domain.  "}})
	if r := out["result"].(map[string]any); r["isError"] == true {
		t.Fatalf("message_user: %v", out)
	}
	msgs, err := st.ListAgentMessages(context.Background(), "h1", 10)
	if err != nil || len(msgs) != 1 {
		t.Fatalf("messages: %v %v", msgs, err)
	}
	m := msgs[0]
	if m.Role != "assistant" || m.State != "done" || m.Channel != "agent" || m.Content != "The page title is Example Domain." {
		t.Fatalf("message: %+v", m)
	}

	_, out = mcpCall(t, p, "/agents/h1/mcp", secret, "tools/call",
		map[string]any{"name": "message_user", "arguments": map[string]string{"text": " "}})
	if r := out["result"].(map[string]any); r["isError"] != true {
		t.Fatalf("an empty message was accepted: %v", out)
	}
}

func TestAgentMessageUserIsLimited(t *testing.T) {
	var n agentNotes
	at := time.Now()
	for i := 0; i < agentNotesPerHour; i++ {
		if !n.take("h1", at) {
			t.Fatalf("message %d refused", i)
		}
	}
	if n.take("h1", at) {
		t.Fatal("one message too many was allowed")
	}
	if !n.take("h2", at) {
		t.Fatal("another agent was limited too")
	}
	if !n.take("h1", at.Add(time.Hour+time.Second)) {
		t.Fatal("the limit did not reset after an hour")
	}
}

func TestAgentMessageUserKeepsLongMessagesWhole(t *testing.T) {
	s, st, _ := agentTestServer(t)
	h, _ := st.GetAgentInstanceForOwner(context.Background(), "h1", "alice")
	if _, err := s.agentMessageUser(context.Background(), h, strings.Repeat("é", agentNoteMax+5)); err != nil {
		t.Fatal(err)
	}
	msgs, _ := st.ListAgentMessages(context.Background(), "h1", 1)
	if r := []rune(msgs[0].Content); len(r) != agentNoteMax+1 || !strings.HasSuffix(msgs[0].Content, "é…") {
		t.Fatalf("long message: %d runes", len(r))
	}
}

// Agents load sandboxd's tools when they start: a new tool must make their
// containers be made again, or scheduled jobs never see message_user.
func TestAgentContainersFollowTheTools(t *testing.T) {
	if setupRev(agentTools) != setupRev(agentTools) || len(agentSetup) != 12 {
		t.Fatalf("revision is not stable: %q", agentSetup)
	}
	fewer := agentTools[:len(agentTools)-1]
	if setupRev(fewer) == agentSetup {
		t.Fatal("the revision ignores the tools")
	}
}

// OpenClaw's scheduled jobs deliver to the agent's inbox, signed with the
// agent's secret; the result shows in its conversation.
func TestAgentInboxPostsAJobsResult(t *testing.T) {
	s, st, secret := agentTestServer(t)
	p := s.AgentProxy()
	post := func(token, body string) int {
		req := httptest.NewRequest(http.MethodPost, "/agents/h1/inbox", strings.NewReader(body))
		req.Header.Set("Authorization", "Bearer "+token)
		rec := httptest.NewRecorder()
		p.ServeHTTP(rec, req)
		return rec.Code
	}
	if code := post("wrong", `{"summary":"x"}`); code != http.StatusUnauthorized {
		t.Fatalf("a wrong secret got %d", code)
	}
	if code := post(secret, `{"jobName":"Example title","status":"ok","summary":"Example Domain"}`); code != http.StatusNoContent {
		t.Fatalf("inbox: %d", code)
	}
	if code := post(secret, `{"status":"ok","summary":"  "}`); code != http.StatusNoContent {
		t.Fatalf("empty result: %d", code)
	}
	msgs, _ := st.ListAgentMessages(context.Background(), "h1", 10)
	if len(msgs) != 1 || msgs[0].Channel != "agent" || msgs[0].Content != "**Example title**\n\nExample Domain" {
		t.Fatalf("messages: %+v", msgs)
	}
}

func TestInboxText(t *testing.T) {
	for body, want := range map[string]string{
		`{"text":"hello"}`:                           "hello",
		`{"summary":"done","error":""}`:              "done",
		`{"error":"site down","name":"Daily check"}`: "**Daily check**\n\nA scheduled job failed: site down",
		`plain words`:                                "plain words",
		`{"status":"ok"}`:                            "",
	} {
		if got := inboxText([]byte(body)); got != want {
			t.Errorf("%s: got %q, want %q", body, got, want)
		}
	}
}

// sandboxd replaces its own guide but never a file the person wrote there.
func TestSandboxdGuideLeavesThePersonsFile(t *testing.T) {
	dir := t.TempDir()
	file := dir + "/AGENTS.md"
	if !sandboxdGuide(file) {
		t.Fatal("a missing file is free")
	}
	_ = os.WriteFile(file, []byte(agentcatalog.GuideMark+"\nold guide"), 0o600)
	if !sandboxdGuide(file) {
		t.Fatal("sandboxd's own guide is replaced")
	}
	_ = os.WriteFile(file, []byte("# My rules\nAlways answer in French."), 0o600)
	if sandboxdGuide(file) {
		t.Fatal("the person's AGENTS.md would be overwritten")
	}
}

// A guide that went missing comes back; the person's own file stays.
func TestWriteGuideRestoresSandboxdsGuide(t *testing.T) {
	s, st, _ := agentTestServer(t)
	s.AgentRoot, s.AgentProxyURL = t.TempDir(), "http://sandboxd:9100"
	h, _ := st.GetAgentInstanceForOwner(context.Background(), "h1", "alice")
	pkg := agentcatalog.OpenClaw
	file := s.AgentRoot + "/h1/" + pkg.GuideFile
	_ = os.Mkdir(s.AgentRoot+"/h1", 0o700)
	if err := s.writeGuide(h, pkg); err != nil {
		t.Fatal(err)
	}
	b, err := os.ReadFile(file)
	if err != nil || !strings.Contains(string(b), "/agents/h1/inbox") {
		t.Fatalf("guide: %q %v", b, err)
	}
	_ = os.WriteFile(file, []byte("# mine"), 0o600)
	if err := s.writeGuide(h, pkg); err != nil {
		t.Fatal(err)
	}
	if b, _ := os.ReadFile(file); string(b) != "# mine" {
		t.Fatalf("the person's file was replaced: %q", b)
	}
}
