package api

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"slices"
	"strings"
	"sync"
	"time"

	"github.com/tastyeffectco/sandboxd/control-plane/internal/auth"
	"github.com/tastyeffectco/sandboxd/control-plane/internal/store"
)

// The sandboxd agent: one main assistant per person. It is the coding agent
// (OpenCode, Claude Code, Codex or Pi, on whatever subscription or model the
// person picks) of a hidden project, with sandboxd's tools over MCP: it
// creates and changes apps, installs open-source projects, adds agents, hands
// them tasks and reads their memories, so the person has one correspondent.
//
//	GET  /v1/assistant   the person's sandboxd agent ({app_id}), 404 when none
//	POST /v1/assistant   make it, or refresh its tool settings; returns {app_id}
//	POST <agent proxy>/assistant/mcp   its tools, authenticated by its secret

// assistantID stands for the assistant where the agent tools expect an agent
// (events, limits).
const assistantID = "sandboxd-assistant"

// AssistantTag marks the assistant's hidden project; the console keeps it out
// of the project list.
const assistantTag = "sandboxd:assistant"

// assistantRegistry remembers, in memory, which workspace is whose sandboxd
// agent and which AI the person last chose for it, so the builds and installs
// it starts run on that same AI (a subscription or model connection) instead
// of the free tier. After an engine restart it is filled again when the
// assistant is opened (POST /v1/assistant) and talked to.
type assistantRegistry struct {
	mu        sync.Mutex
	sandboxes map[string]string          // workspace id -> owner
	choices   map[string]assistantChoice // owner -> latest choice
}

type assistantChoice struct{ Agent, Provider, Model string }

func (a *assistantRegistry) addSandbox(sandboxID, owner string) {
	a.mu.Lock()
	defer a.mu.Unlock()
	if a.sandboxes == nil {
		a.sandboxes = map[string]string{}
	}
	a.sandboxes[sandboxID] = owner
}

// assistantTaskTimeoutS: a message to the sandboxd agent may run 30 minutes.
const assistantTaskTimeoutS = 1800

// note records the AI of a task sent to a sandboxd agent's workspace, and
// reports whether the workspace is one.
func (a *assistantRegistry) note(sandboxID string, c assistantChoice) bool {
	a.mu.Lock()
	defer a.mu.Unlock()
	owner, ok := a.sandboxes[sandboxID]
	if !ok {
		return false
	}
	if a.choices == nil {
		a.choices = map[string]assistantChoice{}
	}
	a.choices[owner] = c
	return true
}

func (a *assistantRegistry) choice(owner string) assistantChoice {
	a.mu.Lock()
	defer a.mu.Unlock()
	return a.choices[owner]
}

func assistantActor(owner string) *store.AgentInstance {
	return &store.AgentInstance{ID: assistantID, OwnerToken: owner, Name: "sandboxd"}
}

// GET /v1/assistant
func (s *Server) v1GetAssistant(w http.ResponseWriter, r *http.Request) {
	a, err := s.Store.GetAssistant(r.Context(), tenantToken(r))
	if errors.Is(err, store.ErrNotFound) {
		writeV1Err(w, http.StatusNotFound, "not_found", "no sandboxd agent yet")
		return
	} else if err != nil {
		writeV1Err(w, http.StatusInternalServerError, "internal", err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"app_id": a.AppID})
}

// POST /v1/assistant — make the person's sandboxd agent, or bring its tool
// settings up to date (a fresh secret and sandboxd's current address; the
// engine's address can change when it restarts). Safe to call every time the
// assistant is opened.
func (s *Server) v1EnsureAssistant(w http.ResponseWriter, r *http.Request) {
	owner := tenantToken(r)
	unlock := s.agentLocks.lock("assistant:" + owner)
	defer unlock()
	appID, err := s.ensureAssistant(r.Context(), owner)
	if err != nil {
		writeV1Err(w, http.StatusInternalServerError, "internal", "could not set up the sandboxd agent: "+err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"app_id": appID})
}

func (s *Server) ensureAssistant(ctx context.Context, owner string) (string, error) {
	h := assistantActor(owner)
	var app struct {
		ID               string `json:"id"`
		CurrentSandboxID string `json:"current_sandbox_id"`
	}
	if a, err := s.Store.GetAssistant(ctx, owner); err == nil {
		if s.agentCall(ctx, h, http.MethodGet, "/v1/apps/"+a.AppID, nil, &app) != nil {
			app.ID = "" // its project was deleted: make a new one
		}
	} else if !errors.Is(err, store.ErrNotFound) {
		return "", err
	}
	if app.ID == "" {
		if err := s.agentCall(ctx, h, http.MethodPost, "/v1/apps", map[string]any{
			"name": "sandboxd", "description": "Your sandboxd agent: it builds and installs apps, adds agents and gives them tasks.",
			"tags": []string{assistantTag},
		}, &app); err != nil {
			return "", err
		}
	}
	if app.CurrentSandboxID == "" {
		var sb struct {
			ID string `json:"id"`
		}
		if err := s.agentCall(ctx, h, http.MethodPost, "/v1/apps/"+app.ID+"/sandbox", map[string]any{"template": "blank"}, &sb); err != nil {
			return "", err
		}
		app.CurrentSandboxID = sb.ID
	}
	s.assistants.addSandbox(app.CurrentSandboxID, owner)
	mcpURL, err := s.assistantMCPURL(ctx)
	if err != nil {
		return "", err
	}
	secret, err := randomSecret()
	if err != nil {
		return "", err
	}
	for path, content := range assistantFiles(mcpURL, secret) {
		if err := s.agentPutFile(ctx, h, app.CurrentSandboxID, path, content); err != nil {
			return "", fmt.Errorf("write %s: %w", path, err)
		}
	}
	if err := s.Store.PutAssistant(ctx, &store.Assistant{OwnerToken: owner, AppID: app.ID,
		SecretHash: hashSecret(secret), CreatedAt: time.Now().UTC().Format(time.RFC3339)}); err != nil {
		return "", err
	}
	return app.ID, nil
}

// assistantMCPURL is the tools address as a sandbox reaches it: the agent
// proxy, by IP (OpenCode's runtime refuses a single-label host name).
func (s *Server) assistantMCPURL(ctx context.Context) (string, error) {
	u, err := url.Parse(s.AgentProxyURL)
	if err != nil || u.Host == "" {
		return "", errors.New("sandboxd's agent address is not set")
	}
	ip, err := s.agentProxyIP(ctx)
	if err != nil {
		return "", err
	}
	if port := u.Port(); port != "" {
		u.Host = ip + ":" + port
	} else {
		u.Host = ip
	}
	return strings.TrimRight(u.String(), "/") + "/assistant/mcp", nil
}

// assistantFiles are the hidden project's settings: where its tools are (each
// coding agent reads its own file) and how sandboxd works.
func assistantFiles(mcpURL, secret string) map[string]string {
	headers := map[string]string{"Authorization": "Bearer " + secret}
	opencode, _ := json.MarshalIndent(map[string]any{
		"$schema": "https://opencode.ai/config.json",
		"mcp": map[string]any{"sandboxd": map[string]any{
			"type": "remote", "url": mcpURL, "headers": headers, "enabled": true}},
	}, "", "  ")
	claude, _ := json.MarshalIndent(map[string]any{
		"mcpServers": map[string]any{"sandboxd": map[string]any{"type": "http", "url": mcpURL, "headers": headers}},
	}, "", "  ")
	return map[string]string{
		"opencode.json":         string(opencode) + "\n",
		".mcp.json":             string(claude) + "\n",
		".claude/settings.json": "{\n  \"enableAllProjectMcpServers\": true\n}\n",
		"AGENTS.md":             assistantGuide,
		"CLAUDE.md":             "@AGENTS.md\n",
		".gitignore":            "opencode.json\n.mcp.json\n",
	}
}

// agentPutFile writes one file into a workspace through /v1, as the owner.
func (s *Server) agentPutFile(ctx context.Context, h *store.AgentInstance, sandboxID, path, content string) error {
	if s.inner == nil {
		return errors.New("sandboxd is not ready")
	}
	req := httptest.NewRequest(http.MethodPut, "/v1/sandboxes/"+sandboxID+"/files?path="+url.QueryEscape(path),
		bytes.NewReader([]byte(content))).WithContext(auth.WithActor(ctx, auth.Actor{Kind: "service", Name: h.OwnerToken}))
	rec := httptest.NewRecorder()
	s.inner.ServeHTTP(rec, req)
	if rec.Code >= 300 {
		return errors.New(strings.TrimSpace(rec.Body.String()))
	}
	return nil
}

// assistantProxy authenticates the assistant's tool calls by its secret.
func (s *Server) assistantProxy(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		w.Header().Set("Allow", http.MethodPost)
		w.WriteHeader(http.StatusMethodNotAllowed)
		return
	}
	token := strings.TrimPrefix(r.Header.Get("Authorization"), "Bearer ")
	a, err := s.Store.GetAssistantBySecretHash(r.Context(), hashSecret(token))
	if token == "" || err != nil {
		http.Error(w, "invalid assistant credentials", http.StatusUnauthorized)
		return
	}
	h := assistantActor(a.OwnerToken)
	s.serveMCP(w, r, assistantTools, "You are the user's sandboxd agent. These tools act on their sandboxd: "+
		"apps, open-source projects, agents and their memories. Builds and installs take minutes: check app_status.",
		func(ctx context.Context, name string, args map[string]string) (string, error) {
			return s.assistantTool(ctx, h, name, args)
		})
}

var assistantTools = append(agentToolsNamed("list_apps", "create_app", "ask_app", "app_status"),
	mcpTool{"install_project", "Install an open-source project from GitHub as a new app on sandboxd. A coding agent " +
		"copies the repository, makes it run on port 3000 and reports. Check progress with app_status.",
		obj(map[string]any{
			"repo_url": map[string]any{"type": "string", "description": "https://github.com/owner/name"},
			"ref":      map[string]any{"type": "string", "description": "Release tag or branch to install; the latest release when omitted"},
			"name":     map[string]any{"type": "string", "description": "Optional app name"},
		}, "repo_url")},
	mcpTool{"remove_app", "Remove an app that you installed or created yourself and that does not work (an install " +
		"that failed or was refused). You cannot remove the user's other apps.",
		obj(map[string]any{"app_id": map[string]any{"type": "string"}}, "app_id")},
	mcpTool{"read_memory", "Read an app's project memory (BRAIN.md and its notes).",
		obj(map[string]any{"app_id": map[string]any{"type": "string"}}, "app_id")},
	mcpTool{"list_agents", "List the user's agents (Hermes, OpenClaw...) with their status.", obj(map[string]any{})},
	mcpTool{"message_agent", "Give one of the user's agents a task or a question and wait for its answer " +
		"(up to a few minutes).",
		obj(map[string]any{
			"agent_id": map[string]any{"type": "string"},
			"text":     map[string]any{"type": "string", "description": "The task or question, in plain words"},
		}, "agent_id", "text")},
	mcpTool{"read_agent_conversation", "Read the latest messages between the user and one of their agents: " +
		"what it was asked and what it did.",
		obj(map[string]any{"agent_id": map[string]any{"type": "string"}}, "agent_id")},
)

func agentToolsNamed(names ...string) []mcpTool {
	var out []mcpTool
	for _, n := range names {
		for _, t := range agentTools {
			if t.Name == n {
				out = append(out, t)
			}
		}
	}
	return out
}

func (s *Server) assistantTool(ctx context.Context, h *store.AgentInstance, name string, args map[string]string) (string, error) {
	switch name {
	case "list_apps", "create_app", "ask_app", "app_status":
		return s.agentTool(ctx, h, name, args)

	case "install_project":
		return s.assistantInstall(ctx, h, args)

	case "remove_app":
		var app struct {
			Name string   `json:"name"`
			Tags []string `json:"tags"`
		}
		id := strings.TrimSpace(args["app_id"])
		if id == "" || s.agentCall(ctx, h, http.MethodGet, "/v1/apps/"+id, nil, &app) != nil {
			return "", errors.New("no such app (see list_apps)")
		}
		if !slices.Contains(app.Tags, "created-by:"+h.Name) {
			return "", errors.New("you can only remove apps you installed or created yourself; ask the user to remove this one")
		}
		if err := s.agentCall(ctx, h, http.MethodDelete, "/v1/apps/"+id, nil, nil); err != nil {
			return "", err
		}
		return "Removed " + app.Name + ".", nil

	case "read_memory":
		sbID, err := s.agentAppSandbox(ctx, h, args["app_id"])
		if err != nil {
			return "", err
		}
		var b strings.Builder
		if text, err := s.agentReadFile(ctx, h, sbID, "BRAIN.md"); err == nil {
			b.WriteString(truncate(text, 12000))
		}
		var notes struct {
			Entries []struct {
				Path string `json:"path"`
				Type string `json:"type"`
			} `json:"entries"`
		}
		if s.agentCall(ctx, h, http.MethodGet, "/v1/sandboxes/"+sbID+"/files?path=brain", nil, &notes) == nil {
			for _, e := range notes.Entries {
				if e.Type == "file" && strings.HasSuffix(e.Path, ".md") {
					if text, err := s.agentReadFile(ctx, h, sbID, e.Path); err == nil {
						fmt.Fprintf(&b, "\n\n--- %s ---\n%s", e.Path, truncate(text, 4000))
					}
				}
			}
		}
		if b.Len() == 0 {
			return "This app has no project memory yet.", nil
		}
		return b.String(), nil

	case "list_agents":
		var out struct {
			Agents []struct {
				ID      string `json:"id"`
				Name    string `json:"name"`
				Package string `json:"package"`
				Status  string `json:"status"`
				Chat    bool   `json:"chat"`
			} `json:"agents"`
		}
		if err := s.agentCall(ctx, h, http.MethodGet, "/v1/agent-instances", nil, &out); err != nil {
			return "", err
		}
		if len(out.Agents) == 0 {
			return "The user has no agents yet. They can add one (Hermes, OpenClaw...) under Agents in sandboxd.", nil
		}
		var b strings.Builder
		for _, a := range out.Agents {
			note := ""
			if !a.Chat {
				note = " (works in its own app; message_agent can't reach it)"
			}
			fmt.Fprintf(&b, "- %s (id %s, %s): %s%s\n", a.Name, a.ID, a.Package, a.Status, note)
		}
		return b.String(), nil

	case "message_agent":
		return s.assistantMessageAgent(ctx, h, args["agent_id"], args["text"])

	case "read_agent_conversation":
		msgs, _, err := s.agentConversation(ctx, h, args["agent_id"])
		if err != nil {
			return "", err
		}
		if len(msgs) == 0 {
			return "No messages yet.", nil
		}
		var b strings.Builder
		for _, m := range msgs[max(0, len(msgs)-20):] {
			fmt.Fprintf(&b, "[%s %s] %s\n", m.CreatedAt, m.Role, truncate(m.Content, 1500))
		}
		return b.String(), nil
	}
	return "", fmt.Errorf("unknown tool %q", name)
}

// assistantInstall creates an app from a GitHub repository and asks its
// coding agent to make it run, following the same rules as the Free apps
// catalog.
func (s *Server) assistantInstall(ctx context.Context, h *store.AgentInstance, args map[string]string) (string, error) {
	repo := strings.TrimSuffix(strings.TrimSpace(args["repo_url"]), ".git")
	u, err := url.Parse(repo)
	if err != nil || u.Scheme != "https" || u.Host != "github.com" || len(strings.Split(strings.Trim(u.Path, "/"), "/")) != 2 {
		return "", errors.New("give the project's GitHub address, like https://github.com/owner/name")
	}
	name := strings.TrimSpace(args["name"])
	if name == "" {
		name = strings.Split(strings.Trim(u.Path, "/"), "/")[1]
	}
	ref := strings.TrimSpace(args["ref"])
	var app struct {
		ID string `json:"id"`
	}
	body := map[string]any{"name": truncate(name, 80), "description": "Installed from " + repo,
		"tags": []string{"created-by:sandboxd"}}
	if ref != "" {
		body["git"] = map[string]any{"repo_url": repo, "branch": ref}
	}
	if err := s.agentCall(ctx, h, http.MethodPost, "/v1/apps", body, &app); err != nil {
		return "", err
	}
	var sb struct {
		ID string `json:"id"`
	}
	sbBody := map[string]any{}
	if ref == "" {
		sbBody["template"] = "blank"
	}
	if err := s.agentCall(ctx, h, http.MethodPost, "/v1/apps/"+app.ID+"/sandbox", sbBody, &sb); err != nil {
		return "", err
	}
	s.agentRecord(ctx, h, app.ID, "agent.app_created", "Your sandboxd agent installed this project")
	if err := s.agentBuild(ctx, h, sb.ID, installProjectPrompt(repo, ref)); err != nil {
		return "", fmt.Errorf("the app %s (id %s) was created, but its install could not start: %w", name, app.ID, err)
	}
	_, preview := s.agentSandboxInfo(ctx, h, sb.ID)
	return fmt.Sprintf("Installing %s as the app %q (id %s). Its coding agent is setting it up; this takes a few "+
		"minutes. Check with app_status: it reports whether the app runs, or why it can't. Preview: %s",
		repo, name, app.ID, preview), nil
}

func installProjectPrompt(repo, ref string) string {
	where := "The repository is already in this workspace (" + repo + " at " + ref + ")."
	if ref == "" {
		where = "Get " + repo + " at its latest release (a tagged version, never a moving branch): download the " +
			"release's prebuilt files when it has them, otherwise its source at that tag."
	}
	return "Install the open-source project " + repo + " so it runs in this workspace. " + where + "\n\n" +
		"First read what it needs (README, package.json engines, pyproject.toml, Dockerfile). If it needs " +
		"something this workspace lacks (see the rules below; for example Node newer than 22, Docker or a " +
		"database server), stop at once and say why in one plain sentence. Don't spend more than a few " +
		"minutes trying.\n\nWrite sandbox.yaml so `web.command` installs on the first start only (guard it with a file check, so later " +
		"starts are instant) and serves the app on 0.0.0.0 port 3000; set health_path to a page that answers 200. " +
		"Rules of this workspace: Linux without root, Node 22, Python 3.13 with uv, no Docker, no Postgres, MySQL, " +
		"Redis or MongoDB (SQLite and files are fine), no GPU, no headless browser, about 4 GB of memory. Pin " +
		"exact versions. The app is shown in a frame: no X-Frame-Options DENY, no absolute redirects built from " +
		"the Host header.\n\nIf it cannot run here, don't force it: say why in one plain sentence. Otherwise reply " +
		"in one plain sentence with what the person sees first (for example: create your account)."
}

// assistantMessageAgent sends a task to an agent and waits for its answer.
func (s *Server) assistantMessageAgent(ctx context.Context, h *store.AgentInstance, agentID, text string) (string, error) {
	text = strings.TrimSpace(text)
	if agentID == "" || text == "" {
		return "", errors.New("give agent_id and text")
	}
	before, _, err := s.agentConversation(ctx, h, agentID)
	if err != nil {
		return "", err
	}
	if err := s.agentCall(ctx, h, http.MethodPost, "/v1/agent-instances/"+agentID+"/messages",
		map[string]any{"content": "From the user's sandboxd agent: " + text}, nil); err != nil {
		return "", err
	}
	deadline := time.Now().Add(assistantWait)
	for {
		select {
		case <-ctx.Done():
			return "", ctx.Err()
		case <-time.After(assistantPoll):
		}
		msgs, working, err := s.agentConversation(ctx, h, agentID)
		if err != nil {
			return "", err
		}
		if !working && len(msgs) > len(before) {
			last := msgs[len(msgs)-1]
			if last.Role == "assistant" {
				if last.Error != "" {
					return "", errors.New(last.Error)
				}
				return truncate(last.Content, 8000), nil
			}
		}
		if time.Now().After(deadline) {
			return "The agent is still working on it. Its answer will be in its conversation: read it later " +
				"with read_agent_conversation.", nil
		}
	}
}

// How long message_agent waits for an answer, and how often it looks.
var (
	assistantWait = 4 * time.Minute
	assistantPoll = 2 * time.Second
)

func (s *Server) agentConversation(ctx context.Context, h *store.AgentInstance, agentID string) ([]v1AgentMessage, bool, error) {
	if agentID == "" {
		return nil, false, errors.New("give agent_id (see list_agents)")
	}
	var out struct {
		Messages []v1AgentMessage `json:"messages"`
		Working  bool             `json:"working"`
	}
	if err := s.agentCall(ctx, h, http.MethodGet, "/v1/agent-instances/"+agentID+"/messages", nil, &out); err != nil {
		return nil, false, err
	}
	return out.Messages, out.Working, nil
}

// agentReadFile reads a workspace file (app-relative) as the owner.
func (s *Server) agentReadFile(ctx context.Context, h *store.AgentInstance, sandboxID, path string) (string, error) {
	if s.inner == nil {
		return "", errors.New("sandboxd is not ready")
	}
	req := httptest.NewRequest(http.MethodGet, "/v1/sandboxes/"+sandboxID+"/files/content?path="+url.QueryEscape(path), nil).
		WithContext(auth.WithActor(ctx, auth.Actor{Kind: "service", Name: h.OwnerToken}))
	rec := httptest.NewRecorder()
	s.inner.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		return "", fmt.Errorf("%s: %d", path, rec.Code)
	}
	return rec.Body.String(), nil
}

const assistantGuide = `# You are the user's sandboxd agent

This workspace is not an app. It is where you, the user's main assistant on
sandboxd, work. The user talks to you in plain words; they are usually not
technical. You act on their sandboxd through the **sandboxd** tools (MCP):

- **Apps**: list_apps, create_app (build a new app from an idea), ask_app (ask an
  app's coding agent for a change), app_status, read_memory (an app's notes).
- **Open-source projects**: install_project installs a GitHub project as an app.
- **Agents** (Hermes, OpenClaw...): list_agents, message_agent (give one a task
  and get its answer), read_agent_conversation (what it was asked and did).

Use the tools rather than building things inside this workspace.

## How sandboxd works

- Each app is a project with its own Linux workspace and a coding agent. Its
  sandbox.yaml says how it starts; the app serves port 3000 and appears in a
  preview the user opens in sandboxd.
- A workspace has Node 22, Python 3.13 with uv, git and curl. It has no root,
  no Docker, no GPU, no headless browser, no Postgres/MySQL/Redis/MongoDB
  (SQLite and files are fine), and about 4 GB of memory.
- Agents are separate assistants with their own memory, tools, schedules and
  channels (Telegram...). Give them work that suits them: research, watching
  something, recurring jobs.

## Installing a project the user names

1. Check it can run here before installing: read its README and
   package.json (engines) or pyproject.toml with curl from
   raw.githubusercontent.com. It needs an OSI open-source license; Node 22 or
   older, Python, or a prebuilt Linux build; none of the blockers above; and
   to be useful without an account or key. If it can't run, say why in one
   sentence and suggest an alternative. Don't install it.
2. install_project with its GitHub address (and a release tag when you know
   one). Check app_status every 30 seconds for at most 3 minutes.
3. When it runs, tell the user the app's name and what they will see first.
   If it is still installing after 3 minutes, say so: it will appear in their
   projects when ready. If it failed or was refused, say why in plain words
   and remove that app with remove_app, so no broken project is left behind.

## Talking with the user

- Short, plain sentences. No jargon (no "MCP", "sandbox.yaml", "port").
- Say what you did and what happens next ("I asked Hermes; it will answer in a
  minute").
- Ask before deleting anything or doing something that costs money.
`
