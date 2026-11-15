package api

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/tastyeffectco/sandboxd/control-plane/internal/auth"
	"github.com/tastyeffectco/sandboxd/control-plane/internal/store"
)

func asAlice(r *http.Request) *http.Request {
	return r.WithContext(auth.WithActor(r.Context(), auth.Actor{Name: "alice"}))
}

func listMessages(t *testing.T, s *Server) (msgs []v1AgentMessage, working bool) {
	t.Helper()
	req := asAlice(httptest.NewRequest(http.MethodGet, "/v1/agent-instances/h1/messages", nil))
	req.SetPathValue("id", "h1")
	rec := httptest.NewRecorder()
	s.v1ListAgentMessages(rec, req)
	var out struct {
		Messages []v1AgentMessage `json:"messages"`
		Working  bool             `json:"working"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &out); err != nil {
		t.Fatalf("%d %s", rec.Code, rec.Body.String())
	}
	return out.Messages, out.Working
}

// A reply runs in the engine and is saved, steps included, so the person
// finds it after closing the window.
func TestAgentTurnRunsAndIsKept(t *testing.T) {
	s, st, _ := agentTestServer(t)
	release := make(chan struct{})
	var sent map[string]any
	agent := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		_ = json.Unmarshal(body, &sent)
		w.Header().Set("Content-Type", "text/event-stream")
		io.WriteString(w, "event: hermes.tool.progress\ndata: {\"tool\":\"mcp__sandboxd__list_apps\",\"toolCallId\":\"c1\",\"status\":\"running\"}\n\n")
		w.(http.Flusher).Flush()
		<-release // the window is "closed" here; the turn keeps going
		io.WriteString(w, "data: {\"choices\":[{\"delta\":{\"content\":\"You have 7 apps.\"}}]}\n\ndata: [DONE]\n\n")
	}))
	defer agent.Close()

	ctx := context.Background()
	old := &store.AgentMessage{ID: "01A", AgentID: "h1", Role: "user", Content: "Call me Abdel", Steps: "[]", State: "done", Channel: "app", CreatedAt: "t", UpdatedAt: "t"}
	oldReply := &store.AgentMessage{ID: "01B", AgentID: "h1", Role: "assistant", Content: "Hi Abdel!", Steps: "[]", State: "done", Channel: "app", CreatedAt: "t", UpdatedAt: "t"}
	user := &store.AgentMessage{ID: "01C", AgentID: "h1", Role: "user", Content: "Which apps?", Steps: "[]", State: "done", Channel: "app", CreatedAt: "t", UpdatedAt: "t"}
	reply := &store.AgentMessage{ID: "01D", AgentID: "h1", Role: "assistant", Steps: "[]", State: "working", Channel: "app", CreatedAt: "t", UpdatedAt: "t"}
	for _, m := range []*store.AgentMessage{old, oldReply, user, reply} {
		if err := st.AddAgentMessage(ctx, m); err != nil {
			t.Fatal(err)
		}
	}
	turnCtx, cancel := context.WithCancel(ctx)
	live := &liveTurn{cancel: cancel, msg: *reply}
	s.turns.live = map[string]*liveTurn{"h1": live}
	var turn v1AgentStreamReq
	for _, m := range []*store.AgentMessage{old, oldReply, user} {
		turn.Messages = append(turn.Messages, struct {
			Role    string `json:"role"`
			Content string `json:"content"`
		}{m.Role, m.Content})
	}
	done := make(chan struct{})
	go func() {
		s.runAgentTurn(turnCtx, cancel, "h1", "Hermes", agent.URL, "k", "hermes-agent", turn, live)
		close(done)
	}()

	// While it works, the list shows the reply in progress with its step.
	deadline := time.Now().Add(3 * time.Second)
	for {
		msgs, working := listMessages(t, s)
		if working && strings.Contains(string(msgs[3].Steps), "list_apps") {
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("no live step: %+v", msgs)
		}
		time.Sleep(20 * time.Millisecond)
	}
	close(release)
	<-done
	msgs, working := listMessages(t, s)
	if working || len(msgs) != 4 || msgs[3].Content != "You have 7 apps." || msgs[3].State != "done" ||
		!strings.Contains(string(msgs[3].Steps), `"status":"done"`) {
		t.Fatalf("saved reply: working=%v %+v", working, msgs)
	}
	if got := len(sent["messages"].([]any)); got != 3 {
		t.Fatalf("history sent to the agent: %d messages", got)
	}
}

func TestAgentTurnStopAndRestart(t *testing.T) {
	s, st, _ := agentTestServer(t)
	agent := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		io.WriteString(w, "data: {\"choices\":[{\"delta\":{\"content\":\"Working on it\"}}]}\n\n")
		w.(http.Flusher).Flush()
		<-r.Context().Done()
	}))
	defer agent.Close()
	ctx := context.Background()
	reply := &store.AgentMessage{ID: "01D", AgentID: "h1", Role: "assistant", Steps: "[]", State: "working", Channel: "app", CreatedAt: "t", UpdatedAt: "t"}
	_ = st.AddAgentMessage(ctx, reply)
	turnCtx, cancel := context.WithCancel(ctx)
	live := &liveTurn{cancel: cancel, msg: *reply}
	s.turns.live = map[string]*liveTurn{"h1": live}
	done := make(chan struct{})
	go func() {
		s.runAgentTurn(turnCtx, cancel, "h1", "Hermes", agent.URL, "k", "m", v1AgentStreamReq{}, live)
		close(done)
	}()
	time.Sleep(100 * time.Millisecond)

	req := asAlice(httptest.NewRequest(http.MethodPost, "/v1/agent-instances/h1/messages/stop", nil))
	req.SetPathValue("id", "h1")
	s.v1StopAgentMessage(httptest.NewRecorder(), req)
	<-done
	msgs, _ := listMessages(t, s)
	if msgs[0].State != "stopped" || msgs[0].Content != "Working on it" || string(msgs[0].Steps) != "[]" {
		t.Fatalf("stopped reply: %+v", msgs[0])
	}

	// A reply left "working" by an engine restart is shown as stopped.
	_ = st.AddAgentMessage(ctx, &store.AgentMessage{ID: "01E", AgentID: "h1", Role: "assistant", Steps: "[]", State: "working", Channel: "app", CreatedAt: "t", UpdatedAt: "t"})
	msgs, _ = listMessages(t, s)
	if msgs[1].State != "stopped" {
		t.Fatalf("orphaned reply: %+v", msgs[1])
	}

	// Clearing empties the thread.
	req = asAlice(httptest.NewRequest(http.MethodDelete, "/v1/agent-instances/h1/messages", nil))
	req.SetPathValue("id", "h1")
	rec := httptest.NewRecorder()
	s.v1ClearAgentMessages(rec, req)
	if msgs, _ := listMessages(t, s); rec.Code != http.StatusNoContent || len(msgs) != 0 {
		t.Fatalf("clear: %d, %d messages left", rec.Code, len(msgs))
	}
}

// An agent still starting answers with an error: the turn is tried again and
// succeeds. Once the agent has said something, a failure is not retried.
func TestAgentTurnRetriesAnAgentStillStarting(t *testing.T) {
	old := agentRetryDelays
	agentRetryDelays = []time.Duration{time.Millisecond, time.Millisecond}
	defer func() { agentRetryDelays = old }()
	s, st, _ := agentTestServer(t)
	calls := 0
	agent := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls++
		if calls == 1 {
			http.Error(w, `{"error":{"message":"internal error"}}`, http.StatusInternalServerError)
			return
		}
		w.Header().Set("Content-Type", "text/event-stream")
		io.WriteString(w, "data: {\"choices\":[{\"delta\":{\"content\":\"Ready now\"}}]}\n\ndata: [DONE]\n\n")
	}))
	defer agent.Close()
	ctx := context.Background()
	reply := &store.AgentMessage{ID: "01F", AgentID: "h1", Role: "assistant", Steps: "[]", State: "working", Channel: "app", CreatedAt: "t", UpdatedAt: "t"}
	_ = st.AddAgentMessage(ctx, reply)
	turnCtx, cancel := context.WithCancel(ctx)
	live := &liveTurn{cancel: cancel, msg: *reply}
	s.turns.live = map[string]*liveTurn{"h1": live}
	s.runAgentTurn(turnCtx, cancel, "h1", "OpenClaw", agent.URL, "k", "m", v1AgentStreamReq{}, live)
	msgs, _ := listMessages(t, s)
	if calls != 2 || msgs[0].State != "done" || msgs[0].Content != "Ready now" {
		t.Fatalf("calls=%d reply=%+v", calls, msgs[0])
	}
}

// The agent says nothing while its model keeps failing (it retries for
// minutes): the reply ends at once and says what to do.
func TestAgentTurnEndsWhenItsModelKeepsFailing(t *testing.T) {
	oldWatch := agentModelWatch
	agentModelWatch = 10 * time.Millisecond
	defer func() { agentModelWatch = oldWatch }()
	s, st, _ := agentTestServer(t)
	agent := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		w.(http.Flusher).Flush()
		s.modelTrouble.record("h1", http.StatusBadGateway) // its model calls fail
		s.modelTrouble.record("h1", http.StatusBadGateway)
		<-r.Context().Done()
	}))
	defer agent.Close()
	ctx := context.Background()
	reply := &store.AgentMessage{ID: "01G", AgentID: "h1", Role: "assistant", Steps: "[]", State: "working", Channel: "app", CreatedAt: "t", UpdatedAt: "t"}
	_ = st.AddAgentMessage(ctx, reply)
	turnCtx, cancel := context.WithCancel(ctx)
	live := &liveTurn{cancel: cancel, msg: *reply, conn: "Ollama on this PC"}
	s.turns.live = map[string]*liveTurn{"h1": live}
	done := make(chan struct{})
	go func() {
		s.runAgentTurn(turnCtx, cancel, "h1", "Hermes", agent.URL, "k", "m", v1AgentStreamReq{}, live)
		close(done)
	}()
	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("the reply kept waiting on a failing model")
	}
	msgs, _ := listMessages(t, s)
	if msgs[0].State != "error" || !strings.Contains(msgs[0].Error, "can't reach its AI model (Ollama on this PC)") {
		t.Fatalf("reply: %+v", msgs[0])
	}
	if got := modelFailureText("Hermes", "OpenRouter", http.StatusUnauthorized); !strings.Contains(got, "refused its key") {
		t.Fatal(got)
	}
}
