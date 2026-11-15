package api

import (
	"context"
	"strings"
	"testing"
	"time"
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
