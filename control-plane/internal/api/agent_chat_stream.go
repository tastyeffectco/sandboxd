package api

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/tastyeffectco/sandboxd/control-plane/internal/agentcatalog"
)

// One chat screen works for every agent: each agent's own stream is turned
// into these events, and the console only ever reads them.
//
//	event: text      {"delta": "..."}           part of the answer
//	event: thinking  {"delta": "..."}           the agent's visible reasoning
//	event: step      {"id","tool","label","status"}  a tool it runs: running|done|failed
//	event: done      {}
//	event: error     {"message": "..."}         said plainly
type agentEvent struct {
	name string
	data any
}

type agentStep struct {
	ID     string `json:"id"`
	Tool   string `json:"tool"`
	Label  string `json:"label,omitempty"`
	Status string `json:"status"`
}

type v1AgentStreamReq struct {
	v1AgentChatReq
	// ConversationID keeps the agent's own session across turns and restarts.
	ConversationID string `json:"conversation_id"`
}

// POST /v1/agent-instances/{id}/chat/stream — one turn, streamed as server-sent
// events in sandboxd's agent event format.
func (s *Server) v1AgentChatStream(w http.ResponseWriter, r *http.Request) {
	h, ok := s.ownAgentInstance(w, r)
	if !ok {
		return
	}
	var req v1AgentStreamReq
	if err := json.NewDecoder(io.LimitReader(r.Body, 256<<10)).Decode(&req); err != nil || len(req.Messages) == 0 {
		writeV1Err(w, http.StatusBadRequest, "invalid_request", "send at least one message")
		return
	}
	if len(req.ConversationID) > 128 {
		writeV1Err(w, http.StatusBadRequest, "invalid_request", "conversation id is too long")
		return
	}
	pkg, _ := agentcatalog.Get(h.Package)
	apiKey, err := s.Secrets.Open(h.APIKeyCiphertext, h.APIKeyNonce)
	if err != nil {
		writeV1Err(w, http.StatusInternalServerError, "internal", "the agent's API key cannot be read")
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), 15*time.Minute)
	defer cancel()
	url := fmt.Sprintf("http://%s:%d/v1/chat/completions", agentContainer(h.ID), pkg.APIPort)
	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-store")
	w.Header().Set("X-Accel-Buffering", "no")
	w.WriteHeader(http.StatusOK)
	flusher, _ := w.(http.Flusher)
	emit := func(e agentEvent) {
		b, _ := json.Marshal(e.data)
		fmt.Fprintf(w, "event: %s\ndata: %s\n\n", e.name, b)
		if flusher != nil {
			flusher.Flush()
		}
	}
	if err := streamAgentChat(ctx, url, string(apiKey), pkg.ChatModel, req, emit); err != nil {
		s.Log.Warn("agent chat stream failed", "agent", h.ID, "err", err.Error())
		if ctx.Err() == nil || ctx.Err() == context.DeadlineExceeded {
			emit(agentEvent{"error", map[string]string{"message": plainAgentError(h.Name, err)}})
		}
		return
	}
	emit(agentEvent{"done", struct{}{}})
}

type agentUnavailable struct{ error }

func plainAgentError(name string, err error) string {
	if _, ok := err.(agentUnavailable); ok {
		return name + " isn't answering. If it was just started, wait a minute and try again."
	}
	return name + " couldn't finish this reply. Try again, or ask in a simpler way. If it keeps happening, choose another AI model for it."
}

// streamAgentChat sends one turn to an agent's OpenAI-compatible chat API with
// streaming on and translates what comes back. It understands the standard
// stream (text, reasoning, tool calls) and Hermes's tool progress events.
func streamAgentChat(ctx context.Context, url, key, model string, req v1AgentStreamReq, emit func(agentEvent)) error {
	payload := map[string]any{"model": model, "messages": req.Messages, "stream": true}
	if req.ConversationID != "" {
		payload["user"] = req.ConversationID // OpenClaw keys its session on this
	}
	body, _ := json.Marshal(payload)
	up, _ := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(body))
	up.Header.Set("Authorization", "Bearer "+key)
	up.Header.Set("Content-Type", "application/json")
	up.Header.Set("Accept", "text/event-stream")
	if req.ConversationID != "" {
		up.Header.Set("X-Hermes-Session-Id", req.ConversationID)
		up.Header.Set("X-Openclaw-Session-Key", req.ConversationID)
	}
	resp, err := http.DefaultClient.Do(up)
	if err != nil {
		return agentUnavailable{err}
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		raw, _ := io.ReadAll(io.LimitReader(resp.Body, 600))
		return fmt.Errorf("agent answered %d: %s", resp.StatusCode, strings.TrimSpace(string(raw)))
	}
	// A plain JSON answer (an agent without streaming) is still one reply.
	if !strings.Contains(resp.Header.Get("Content-Type"), "event-stream") {
		var out struct {
			Choices []struct {
				Message struct {
					Content string `json:"content"`
				} `json:"message"`
			} `json:"choices"`
		}
		raw, _ := io.ReadAll(io.LimitReader(resp.Body, 4<<20))
		if json.Unmarshal(raw, &out) != nil || len(out.Choices) == 0 {
			return fmt.Errorf("unexpected agent answer: %.300s", raw)
		}
		emit(agentEvent{"text", map[string]string{"delta": out.Choices[0].Message.Content}})
		return nil
	}
	return translateAgentStream(resp.Body, emit)
}

func translateAgentStream(r io.Reader, emit func(agentEvent)) error {
	scanner := bufio.NewScanner(r)
	scanner.Buffer(make([]byte, 64<<10), 4<<20)
	event := ""
	steps := map[string]bool{}       // tool calls already announced
	callNames := map[int]agentStep{} // streamed tool calls by index
	for scanner.Scan() {
		line := scanner.Text()
		switch {
		case line == "":
			event = ""
		case strings.HasPrefix(line, "event:"):
			event = strings.TrimSpace(strings.TrimPrefix(line, "event:"))
		case strings.HasPrefix(line, "data:"):
			data := strings.TrimSpace(strings.TrimPrefix(line, "data:"))
			if data == "[DONE]" {
				return nil
			}
			if event == "hermes.tool.progress" {
				var p struct {
					Tool, Label, ToolCallID, Status string
				}
				if json.Unmarshal([]byte(data), &p) == nil && p.Tool != "" {
					status := "running"
					switch p.Status {
					case "completed", "done", "success":
						status = "done"
					case "failed", "error":
						status = "failed"
					}
					emit(agentEvent{"step", agentStep{ID: p.ToolCallID, Tool: p.Tool, Label: p.Label, Status: status}})
				}
				continue
			}
			var chunk struct {
				Choices []struct {
					Delta struct {
						Content          string `json:"content"`
						ReasoningContent string `json:"reasoning_content"`
						Reasoning        string `json:"reasoning"`
						ToolCalls        []struct {
							Index    int    `json:"index"`
							ID       string `json:"id"`
							Function struct {
								Name string `json:"name"`
							} `json:"function"`
						} `json:"tool_calls"`
					} `json:"delta"`
					FinishReason string `json:"finish_reason"`
				} `json:"choices"`
				Error *struct {
					Message string `json:"message"`
				} `json:"error"`
			}
			if json.Unmarshal([]byte(data), &chunk) != nil {
				continue
			}
			if chunk.Error != nil {
				return fmt.Errorf("agent error: %s", chunk.Error.Message)
			}
			for _, c := range chunk.Choices {
				if t := c.Delta.ReasoningContent + c.Delta.Reasoning; t != "" {
					emit(agentEvent{"thinking", map[string]string{"delta": t}})
				}
				if c.Delta.Content != "" {
					emit(agentEvent{"text", map[string]string{"delta": c.Delta.Content}})
				}
				for _, tc := range c.Delta.ToolCalls {
					st := callNames[tc.Index]
					if tc.ID != "" {
						st.ID = tc.ID
					}
					st.Tool += tc.Function.Name
					callNames[tc.Index] = st
					if st.ID != "" && st.Tool != "" && !steps[st.ID] {
						steps[st.ID] = true
						emit(agentEvent{"step", agentStep{ID: st.ID, Tool: st.Tool, Status: "running"}})
					}
				}
				if c.FinishReason != "" {
					for _, st := range callNames {
						if steps[st.ID] {
							emit(agentEvent{"step", agentStep{ID: st.ID, Tool: st.Tool, Status: "done"}})
						}
					}
					callNames = map[int]agentStep{}
				}
			}
		}
	}
	return scanner.Err()
}
