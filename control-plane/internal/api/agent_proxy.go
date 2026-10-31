package api

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"time"

	"github.com/tastyeffectco/sandboxd/control-plane/internal/auth"
	"github.com/tastyeffectco/sandboxd/control-plane/internal/events"
	"github.com/tastyeffectco/sandboxd/control-plane/internal/store"
)

// AgentProxy serves agents on the credential proxy's port, authenticated by
// the agent secret:
//
//	POST /agents/{id}/v1/chat/completions  the agent's model, through its model connection
//	GET  /agents/{id}/v1/models            that one model
//	POST /agents/{id}/mcp                  sandboxd's app tools (MCP, JSON responses)
//
// The secret grants nothing else: no other route, no upstream key, no other
// connection or model.
func (s *Server) AgentProxy() http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		rest, ok := strings.CutPrefix(r.URL.Path, "/agents/")
		id, op, _ := strings.Cut(rest, "/")
		if !ok || id == "" {
			http.NotFound(w, r)
			return
		}
		token := strings.TrimPrefix(r.Header.Get("Authorization"), "Bearer ")
		h, err := s.Store.GetAgentInstanceBySecretHash(r.Context(), hashSecret(token))
		if token == "" || err != nil || h.ID != id {
			http.Error(w, "invalid agent credentials", http.StatusUnauthorized)
			return
		}
		switch {
		case op == "v1/chat/completions" && r.Method == http.MethodPost:
			s.agentModelRequest(w, r, h)
		case op == "v1/models" && r.Method == http.MethodGet:
			writeJSON(w, http.StatusOK, map[string]any{"object": "list",
				"data": []map[string]string{{"id": h.Model, "object": "model", "owned_by": "sandboxd"}}})
		case op == "mcp" && r.Method == http.MethodPost:
			s.agentMCP(w, r, h)
		case op == "mcp":
			// No server-initiated stream: clients fall back to plain POSTs.
			w.Header().Set("Allow", http.MethodPost)
			w.WriteHeader(http.StatusMethodNotAllowed)
		default:
			http.Error(w, "unsupported agent operation", http.StatusNotFound)
		}
	})
}

// agentModelRequest forwards a chat completion through the model connection
// with a fresh short-lived capability, so the real key never leaves sandboxd.
func (s *Server) agentModelRequest(w http.ResponseWriter, r *http.Request, h *store.AgentInstance) {
	conn, err := s.ModelProviders.Connection(r.Context(), h.ConnectionID, h.Model, s.AgentProxyURL, 15*time.Minute)
	if err != nil {
		http.Error(w, "the agent's model connection is unavailable: "+err.Error(), http.StatusBadGateway)
		return
	}
	r2 := r.Clone(r.Context())
	r2.URL.Path = "/providers/" + conn.ID + "/v1/chat/completions"
	r2.URL.RawPath = ""
	r2.URL.RawQuery = ""
	r2.Header.Set("Authorization", "Bearer "+conn.Token)
	s.ModelProviders.ServeHTTP(w, r2)
}

// --- MCP ---------------------------------------------------------------

type mcpRequest struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      json.RawMessage `json:"id,omitempty"`
	Method  string          `json:"method"`
	Params  json.RawMessage `json:"params,omitempty"`
}

type mcpTool struct {
	Name        string         `json:"name"`
	Description string         `json:"description"`
	InputSchema map[string]any `json:"inputSchema"`
}

func obj(props map[string]any, required ...string) map[string]any {
	return map[string]any{"type": "object", "properties": props, "required": required}
}

var agentTools = []mcpTool{
	{"list_apps", "List the user's sandboxd apps with their status and preview links.", obj(map[string]any{})},
	{"create_app", "Create a new web app on sandboxd from an idea. A coding agent builds it in its own workspace; " +
		"check progress with app_status. Limited per day.",
		obj(map[string]any{
			"idea": map[string]any{"type": "string", "description": "What the app should do, in plain words"},
			"name": map[string]any{"type": "string", "description": "Optional short app name"},
			"stack": map[string]any{"type": "string", "enum": []string{"react-vite", "nextjs", "node-express", "fastapi"},
				"description": "Optional; React/Vite when omitted"},
		}, "idea")},
	{"ask_app", "Ask an app's coding agent to change the app (new feature, fix, design change).",
		obj(map[string]any{
			"app_id":  map[string]any{"type": "string"},
			"request": map[string]any{"type": "string", "description": "The change, in plain words"},
		}, "app_id", "request")},
	{"app_status", "An app's status, preview link and the outcome of its latest build.",
		obj(map[string]any{"app_id": map[string]any{"type": "string"}}, "app_id")},
}

func (s *Server) agentMCP(w http.ResponseWriter, r *http.Request, h *store.AgentInstance) {
	var req mcpRequest
	if err := json.NewDecoder(io.LimitReader(r.Body, 1<<20)).Decode(&req); err != nil {
		writeJSON(w, http.StatusOK, mcpError(nil, -32700, "invalid JSON"))
		return
	}
	if len(req.ID) == 0 { // a notification (e.g. notifications/initialized)
		w.WriteHeader(http.StatusAccepted)
		return
	}
	var result any
	switch req.Method {
	case "initialize":
		version := "2025-06-18"
		var p struct {
			ProtocolVersion string `json:"protocolVersion"`
		}
		if json.Unmarshal(req.Params, &p) == nil && p.ProtocolVersion != "" {
			version = p.ProtocolVersion
		}
		result = map[string]any{"protocolVersion": version, "capabilities": map[string]any{"tools": map[string]any{}},
			"serverInfo": map[string]string{"name": "sandboxd", "version": "1"},
			"instructions": "Use these tools to build and change web apps on the user's sandboxd. " +
				"Building takes minutes: after create_app or ask_app, check app_status."}
	case "ping":
		result = map[string]any{}
	case "tools/list":
		result = map[string]any{"tools": agentTools}
	case "tools/call":
		var p struct {
			Name      string            `json:"name"`
			Arguments map[string]string `json:"arguments"`
		}
		if err := json.Unmarshal(req.Params, &p); err != nil {
			writeJSON(w, http.StatusOK, mcpError(req.ID, -32602, "invalid tool arguments"))
			return
		}
		text, err := s.agentTool(r.Context(), h, p.Name, p.Arguments)
		if err != nil {
			result = map[string]any{"isError": true, "content": []map[string]string{{"type": "text", "text": err.Error()}}}
		} else {
			result = map[string]any{"content": []map[string]string{{"type": "text", "text": text}}}
		}
	default:
		writeJSON(w, http.StatusOK, mcpError(req.ID, -32601, "method not found"))
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"jsonrpc": "2.0", "id": req.ID, "result": result})
}

func mcpError(id json.RawMessage, code int, msg string) map[string]any {
	return map[string]any{"jsonrpc": "2.0", "id": id, "error": map[string]any{"code": code, "message": msg}}
}

// agentTool runs one tool as the agent's owner, through sandboxd's own /v1
// routes (same validation and records as the console), and records it.
func (s *Server) agentTool(ctx context.Context, h *store.AgentInstance, name string, args map[string]string) (string, error) {
	switch name {
	case "list_apps":
		var out struct {
			Apps []struct {
				ID               string `json:"id"`
				Name             string `json:"name"`
				CurrentSandboxID string `json:"current_sandbox_id"`
			} `json:"apps"`
		}
		if err := s.agentCall(ctx, h, http.MethodGet, "/v1/apps", nil, &out); err != nil {
			return "", err
		}
		if len(out.Apps) == 0 {
			return "No apps yet.", nil
		}
		var b strings.Builder
		for _, a := range out.Apps {
			status, preview := s.agentSandboxInfo(ctx, h, a.CurrentSandboxID)
			fmt.Fprintf(&b, "- %s (id %s): %s %s\n", a.Name, a.ID, status, preview)
		}
		return b.String(), nil

	case "create_app":
		idea := strings.TrimSpace(args["idea"])
		if idea == "" {
			return "", errors.New("describe the app in idea")
		}
		day := time.Now().UTC().Format("2006-01-02")
		if ok, err := s.Store.TakeAgentAppSlot(ctx, h.ID, day, agentAppsPerDay); err != nil {
			return "", err
		} else if !ok {
			return "", fmt.Errorf("the daily limit of %d new apps is reached; the user can create more in sandboxd", agentAppsPerDay)
		}
		appName := strings.TrimSpace(args["name"])
		if appName == "" {
			appName = strings.Join(strings.Fields(idea)[:min(6, len(strings.Fields(idea)))], " ")
		}
		var app struct {
			ID string `json:"id"`
		}
		if err := s.agentCall(ctx, h, http.MethodPost, "/v1/apps", map[string]any{
			"name": truncate(appName, 80), "description": truncate(idea, 2000), "runtime_preset": args["stack"],
			"tags": []string{"created-by:" + h.Name},
		}, &app); err != nil {
			return "", err
		}
		var sb struct {
			ID string `json:"id"`
		}
		if err := s.agentCall(ctx, h, http.MethodPost, "/v1/apps/"+app.ID+"/sandbox", map[string]any{}, &sb); err != nil {
			return "", err
		}
		s.agentRecord(ctx, h, app.ID, "agent.app_created", h.Name+" created this app")
		if err := s.agentBuild(ctx, h, sb.ID, idea); err != nil {
			return "", fmt.Errorf("the app %s (id %s) was created, but its build could not start: %w", appName, app.ID, err)
		}
		_, preview := s.agentSandboxInfo(ctx, h, sb.ID)
		return fmt.Sprintf("Created %q (id %s). Its coding agent is building it now; this takes a few minutes. "+
			"Check progress with app_status. Preview: %s", appName, app.ID, preview), nil

	case "ask_app":
		sbID, err := s.agentAppSandbox(ctx, h, args["app_id"])
		if err != nil {
			return "", err
		}
		request := strings.TrimSpace(args["request"])
		if request == "" {
			return "", errors.New("describe the change in request")
		}
		if err := s.agentBuild(ctx, h, sbID, request); err != nil {
			return "", err
		}
		s.agentRecord(ctx, h, args["app_id"], "agent.app_changed", h.Name+" asked for a change")
		return "The app's coding agent is working on it. Check progress with app_status.", nil

	case "app_status":
		sbID, err := s.agentAppSandbox(ctx, h, args["app_id"])
		if err != nil {
			return "", err
		}
		status, preview := s.agentSandboxInfo(ctx, h, sbID)
		var tasks struct {
			Tasks []struct {
				Status       string `json:"status"`
				AgentMessage string `json:"agent_message"`
			} `json:"tasks"`
		}
		_ = s.agentCall(ctx, h, http.MethodGet, "/v1/sandboxes/"+sbID+"/tasks", nil, &tasks)
		last := "no build yet"
		if len(tasks.Tasks) > 0 {
			t := tasks.Tasks[0]
			last = t.Status
			if t.AgentMessage != "" {
				last += ": " + truncate(t.AgentMessage, 600)
			}
		}
		return fmt.Sprintf("Workspace: %s. Preview: %s\nLatest build: %s", status, preview, last), nil
	}
	return "", fmt.Errorf("unknown tool %q", name)
}

// agentBuild starts a coding task with the default agent (OpenCode's free
// model when nothing is connected), waiting briefly for a new workspace.
func (s *Server) agentBuild(ctx context.Context, h *store.AgentInstance, sandboxID, prompt string) error {
	deadline := time.Now().Add(2 * time.Minute)
	for {
		err := s.agentCall(ctx, h, http.MethodPost, "/v1/sandboxes/"+sandboxID+"/tasks",
			map[string]any{"prompt": prompt, "agent": "opencode"}, nil)
		// A new workspace is still starting: "sandbox is creating — cannot run a
		// task", or its supervisor's socket is not up yet ("runtimed: …").
		starting := err != nil && (strings.Contains(err.Error(), "cannot run a task") || strings.HasPrefix(err.Error(), "runtimed:"))
		if !starting || time.Now().After(deadline) {
			return err
		}
		time.Sleep(3 * time.Second)
	}
}

func (s *Server) agentAppSandbox(ctx context.Context, h *store.AgentInstance, appID string) (string, error) {
	var app struct {
		CurrentSandboxID string `json:"current_sandbox_id"`
	}
	if err := s.agentCall(ctx, h, http.MethodGet, "/v1/apps/"+appID, nil, &app); err != nil {
		return "", err
	}
	if app.CurrentSandboxID == "" {
		return "", errors.New("this app has no workspace; open it in sandboxd to start one")
	}
	return app.CurrentSandboxID, nil
}

func (s *Server) agentSandboxInfo(ctx context.Context, h *store.AgentInstance, sandboxID string) (status, preview string) {
	if sandboxID == "" {
		return "no workspace", ""
	}
	var sb struct {
		Status  string `json:"status"`
		Preview struct {
			URL string `json:"url"`
		} `json:"preview"`
	}
	if err := s.agentCall(ctx, h, http.MethodGet, "/v1/sandboxes/"+sandboxID, nil, &sb); err != nil {
		return "unknown", ""
	}
	return sb.Status, sb.Preview.URL
}

// agentCall runs a /v1 request in-process as the agent's owner. The tool
// list above is the permission boundary; this reuses the routes' validation.
func (s *Server) agentCall(ctx context.Context, h *store.AgentInstance, method, path string, body, out any) error {
	if s.inner == nil {
		return errors.New("sandboxd is not ready")
	}
	var rd io.Reader
	if body != nil {
		b, _ := json.Marshal(body)
		rd = bytes.NewReader(b)
	}
	req := httptest.NewRequest(method, path, rd).WithContext(
		auth.WithActor(ctx, auth.Actor{Kind: "service", Name: h.OwnerToken}))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	s.inner.ServeHTTP(rec, req)
	if rec.Code >= 300 {
		var e struct {
			Error struct {
				Message string `json:"message"`
			} `json:"error"`
		}
		_ = json.Unmarshal(rec.Body.Bytes(), &e)
		if e.Error.Message == "" {
			e.Error.Message = strings.TrimSpace(rec.Body.String())
		}
		return errors.New(e.Error.Message)
	}
	if out != nil {
		return json.Unmarshal(rec.Body.Bytes(), out)
	}
	return nil
}

func (s *Server) agentRecord(ctx context.Context, h *store.AgentInstance, appID, typ, msg string) {
	if s.Events == nil {
		return
	}
	s.Events.Record(ctx, events.Event{OwnerToken: h.OwnerToken, AppID: appID, Type: typ,
		Severity: events.SeverityInfo, Message: msg, Payload: map[string]any{"agent_id": h.ID, "agent": h.Name}})
}

func truncate(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n]
}
