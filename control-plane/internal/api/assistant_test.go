package api

import (
	"context"
	"encoding/json"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/tastyeffectco/sandboxd/control-plane/internal/docker"
	"github.com/tastyeffectco/sandboxd/control-plane/internal/store"
)

func assistantTestServer(t *testing.T) (*Server, *store.Store, string) {
	t.Helper()
	s, st, _ := agentTestServer(t)
	const secret = "assistant-secret-for-tests"
	if err := st.PutAssistant(context.Background(), &store.Assistant{OwnerToken: "alice", AppID: "a1",
		SecretHash: hashSecret(secret), CreatedAt: "now"}); err != nil {
		t.Fatal(err)
	}
	return s, st, secret
}

func assistantToolText(t *testing.T, out map[string]any) (string, bool) {
	t.Helper()
	res, _ := out["result"].(map[string]any)
	content, _ := res["content"].([]any)
	if len(content) == 0 {
		t.Fatalf("no tool result: %v", out)
	}
	isErr, _ := res["isError"].(bool)
	return content[0].(map[string]any)["text"].(string), isErr
}

func TestAssistantToolsNeedItsOwnSecret(t *testing.T) {
	s, _, secret := assistantTestServer(t)
	p := s.AgentProxy()
	for _, bad := range []string{"", "wrong", "agent-secret-for-tests"} { // an agent's secret is not the assistant's
		if code, _ := mcpCall(t, p, "/assistant/mcp", bad, "ping", nil); code != http.StatusUnauthorized {
			t.Errorf("secret %q: HTTP %d; want 401", bad, code)
		}
	}
	code, out := mcpCall(t, p, "/assistant/mcp", secret, "tools/list", nil)
	if code != http.StatusOK {
		t.Fatalf("own secret: HTTP %d", code)
	}
	var names []string
	for _, tool := range out["result"].(map[string]any)["tools"].([]any) {
		names = append(names, tool.(map[string]any)["name"].(string))
	}
	got := strings.Join(names, ",")
	for _, want := range []string{"create_app", "install_project", "read_memory", "list_agents", "message_agent", "read_agent_conversation", "remove_agent"} {
		if !strings.Contains(got, want) {
			t.Errorf("tools %s: missing %s", got, want)
		}
	}
	// It talks to the person in its own chat: the agents' chat tools don't apply.
	if strings.Contains(got, "message_user") || strings.Contains(got, "share_file") {
		t.Errorf("tools %s: the assistant has no message_user/share_file", got)
	}
}

func TestAssistantSeesTheOwnersAgentsAndTheirConversations(t *testing.T) {
	s, st, secret := assistantTestServer(t)
	ctx := context.Background()
	for _, m := range []*store.AgentMessage{
		{ID: "01A", AgentID: "h1", Role: "user", Content: "Watch the price of flights to Lisbon", Steps: "[]", State: "done", Channel: "app", CreatedAt: "t1", UpdatedAt: "t1"},
		{ID: "01B", AgentID: "h1", Role: "assistant", Content: "Done: I check every morning at 8.", Steps: "[]", State: "done", Channel: "app", CreatedAt: "t2", UpdatedAt: "t2"},
	} {
		if err := st.AddAgentMessage(ctx, m); err != nil {
			t.Fatal(err)
		}
	}
	p := s.AgentProxy()
	_, out := mcpCall(t, p, "/assistant/mcp", secret, "tools/call", map[string]any{"name": "list_agents", "arguments": map[string]string{}})
	if text, isErr := assistantToolText(t, out); isErr || !strings.Contains(text, "Hermes (id h1") {
		t.Errorf("list_agents: %q", text)
	}
	_, out = mcpCall(t, p, "/assistant/mcp", secret, "tools/call", map[string]any{"name": "read_agent_conversation",
		"arguments": map[string]string{"agent_id": "h1"}})
	if text, isErr := assistantToolText(t, out); isErr || !strings.Contains(text, "flights to Lisbon") || !strings.Contains(text, "every morning at 8") {
		t.Errorf("read_agent_conversation: %q", text)
	}
	// Another person's agent is out of reach.
	if err := st.CreateAgentInstance(ctx, &store.AgentInstance{ID: "h2", OwnerToken: "bob", Package: "hermes",
		Name: "Bob's", ConnectionID: "c1", Model: "m1", SecretHash: hashSecret("x"), CreatedAt: "now"}); err != nil {
		t.Fatal(err)
	}
	_, out = mcpCall(t, p, "/assistant/mcp", secret, "tools/call", map[string]any{"name": "read_agent_conversation",
		"arguments": map[string]string{"agent_id": "h2"}})
	if _, isErr := assistantToolText(t, out); !isErr {
		t.Error("read another owner's agent: want an error")
	}
}

func TestAssistantInstallNeedsAGitHubProject(t *testing.T) {
	s, _, secret := assistantTestServer(t)
	for _, bad := range []string{"", "immich", "http://github.com/immich-app/immich", "https://gitlab.com/a/b", "https://github.com/immich-app"} {
		_, out := mcpCall(t, s.AgentProxy(), "/assistant/mcp", secret, "tools/call",
			map[string]any{"name": "install_project", "arguments": map[string]string{"repo_url": bad}})
		if text, isErr := assistantToolText(t, out); !isErr || !strings.Contains(text, "github.com/owner/name") {
			t.Errorf("repo %q: %q (error %v)", bad, text, isErr)
		}
	}
}

func TestInstallProjectPromptCarriesTheRules(t *testing.T) {
	latest := installProjectPrompt("https://github.com/a/b", "")
	pinned := installProjectPrompt("https://github.com/a/b", "v1.2.0")
	for _, p := range []string{latest, pinned} {
		for _, want := range []string{"port 3000", "no Docker", "one plain sentence", "Pin", "stop at once"} {
			if !strings.Contains(p, want) {
				t.Errorf("prompt lacks %q:\n%s", want, p)
			}
		}
	}
	if !strings.Contains(latest, "latest release") || !strings.Contains(latest, "no releases") || !strings.Contains(pinned, "already in this workspace") {
		t.Error("the prompt should say where the code comes from")
	}
}

func TestAssistantFilesPointEachCodingAgentAtTheTools(t *testing.T) {
	files := assistantFiles("http://172.18.0.2:7681/assistant/mcp", "s3cret")
	var oc struct {
		MCP map[string]struct {
			Type    string            `json:"type"`
			URL     string            `json:"url"`
			Headers map[string]string `json:"headers"`
		} `json:"mcp"`
	}
	if err := json.Unmarshal([]byte(files["opencode.json"]), &oc); err != nil {
		t.Fatal(err)
	}
	if m := oc.MCP["sandboxd"]; m.Type != "remote" || m.URL != "http://172.18.0.2:7681/assistant/mcp" || m.Headers["Authorization"] != "Bearer s3cret" {
		t.Errorf("opencode.json: %+v", oc)
	}
	var cc struct {
		MCPServers map[string]struct {
			Type, URL string
		} `json:"mcpServers"`
	}
	if err := json.Unmarshal([]byte(files[".mcp.json"]), &cc); err != nil || cc.MCPServers["sandboxd"].Type != "http" {
		t.Errorf(".mcp.json: %s", files[".mcp.json"])
	}
	if !strings.Contains(files[".claude/settings.json"], "enableAllProjectMcpServers") || files["CLAUDE.md"] != "@AGENTS.md\n" {
		t.Error("Claude Code must load the project's MCP server and guide")
	}
	if files[".codex/config.toml"] != "[mcp_servers.sandboxd]\nurl = \"http://172.18.0.2:7681/assistant/mcp\"\n"+
		"http_headers = { Authorization = \"Bearer s3cret\" }\n" || !strings.Contains(files[".gitignore"], ".codex/") {
		t.Errorf("Codex config: %q", files[".codex/config.toml"])
	}
	if !strings.Contains(files["AGENTS.md"], "install_project") || !strings.Contains(files[".gitignore"], "opencode.json") {
		t.Error("the guide names the tools, and the secret stays out of git")
	}
}

func TestAssistantRemovesTheUsersAppsOnlyAfterTheySayYes(t *testing.T) {
	s, _, secret := assistantTestServer(t)
	ctx := context.Background()
	h := assistantActor("alice")
	var mine, theirs struct {
		ID string `json:"id"`
	}
	if err := s.agentCall(ctx, h, http.MethodPost, "/v1/apps", map[string]any{"name": "JSON Crack", "tags": []string{"created-by:sandboxd"}}, &mine); err != nil {
		t.Fatal(err)
	}
	if err := s.agentCall(ctx, h, http.MethodPost, "/v1/apps", map[string]any{"name": "My shop"}, &theirs); err != nil {
		t.Fatal(err)
	}
	call := func(id string) (string, bool) {
		_, out := mcpCall(t, s.AgentProxy(), "/assistant/mcp", secret, "tools/call",
			map[string]any{"name": "remove_app", "arguments": map[string]string{"app_id": id}})
		return assistantToolText(t, out)
	}
	s.assistants.addSandbox("sb-assistant", "alice")
	s.assistants.note("sb-assistant", assistantChoice{}) // the user's message
	if text, isErr := call(mine.ID); isErr || text != "Removed JSON Crack." {
		t.Errorf("its own app: %q (error %v)", text, isErr)
	}
	if err := s.agentCall(ctx, h, http.MethodGet, "/v1/apps/"+mine.ID, nil, nil); err == nil {
		t.Error("the app is still there")
	}
	theirsKept := func(when string) {
		if err := s.agentCall(ctx, h, http.MethodGet, "/v1/apps/"+theirs.ID, nil, nil); err != nil {
			t.Errorf("%s: the person's app is gone: %v", when, err)
		}
	}
	if text, isErr := call(theirs.ID); isErr || !strings.Contains(text, "Ask the user") || !strings.Contains(text, "My shop") {
		t.Errorf("the person's app, first call: %q (error %v)", text, isErr)
	}
	theirsKept("first call")
	// Calling again in the same turn does not count as the user's yes.
	if text, _ := call(theirs.ID); !strings.Contains(text, "Ask the user") {
		t.Errorf("same turn: %q", text)
	}
	theirsKept("same turn")
	s.assistants.note("sb-assistant", assistantChoice{}) // "yes"
	if text, isErr := call(theirs.ID); isErr || text != "Removed My shop." {
		t.Errorf("after the user's yes: %q (error %v)", text, isErr)
	}
	if err := s.agentCall(ctx, h, http.MethodGet, "/v1/apps/"+theirs.ID, nil, nil); err == nil {
		t.Error("the person's app is still there after they said yes")
	}
}

func TestAssistantRemovesAnAgentOnlyAfterTheUserSaysYes(t *testing.T) {
	s, _, secret := assistantTestServer(t)
	ctx := context.Background()
	s.Docker = &docker.Client{Bin: "true"} // removing the agent's container always works
	s.assistants.addSandbox("sb-assistant", "alice")
	call := func(id string) (string, bool) {
		_, out := mcpCall(t, s.AgentProxy(), "/assistant/mcp", secret, "tools/call",
			map[string]any{"name": "remove_agent", "arguments": map[string]string{"agent_id": id}})
		return assistantToolText(t, out)
	}
	s.assistants.note("sb-assistant", assistantChoice{}) // "remove Hermes"
	if text, isErr := call("h1"); isErr || !strings.Contains(text, "Remove the agent Hermes?") {
		t.Errorf("first call: %q (error %v)", text, isErr)
	}
	s.assistants.note("sb-assistant", assistantChoice{}) // "no, wait, something else"
	s.assistants.note("sb-assistant", assistantChoice{}) // a later message is not the answer
	if text, _ := call("h1"); !strings.Contains(text, "Ask the user") {
		t.Errorf("stale confirmation accepted: %q", text)
	}
	if _, err := s.Store.GetAgentInstanceForOwner(ctx, "h1", "alice"); err != nil {
		t.Fatalf("the agent is gone too early: %v", err)
	}
	s.assistants.note("sb-assistant", assistantChoice{}) // "yes"
	if text, isErr := call("h1"); isErr || text != "Removed the agent Hermes." {
		t.Errorf("after yes: %q (error %v)", text, isErr)
	}
	if _, err := s.Store.GetAgentInstanceForOwner(ctx, "h1", "alice"); err == nil {
		t.Error("the agent is still there")
	}
	if _, isErr := call("nope"); !isErr {
		t.Error("unknown agent: want an error")
	}
}

func TestAssistantBuildsRunOnTheAIThePersonChose(t *testing.T) {
	s, _, _ := assistantTestServer(t)
	agent := &store.AgentInstance{ID: "h1", OwnerToken: "alice", Name: "Hermes"}
	me := assistantActor("alice")
	// Nothing chosen yet (or a regular agent): OpenCode's default.
	if b := s.buildRequest(me, "go"); b["agent"] != "opencode" || b["provider"] != nil {
		t.Errorf("default build: %v", b)
	}
	s.assistants.addSandbox("sb-assistant", "alice")
	s.assistants.note("sb-other", assistantChoice{Agent: "claude-code"}) // not an assistant's workspace
	if b := s.buildRequest(me, "go"); b["agent"] != "opencode" {
		t.Errorf("a task elsewhere changed the choice: %v", b)
	}
	s.assistants.note("sb-assistant", assistantChoice{Agent: "opencode", Provider: "conn-openrouter", Model: "deepseek/deepseek-v4.1-flash"})
	b := s.buildRequest(me, "install it")
	if b["provider"] != "conn-openrouter" || b["model"] != "deepseek/deepseek-v4.1-flash" || b["prompt"] != "install it" {
		t.Errorf("assistant build: %v", b)
	}
	if b := s.buildRequest(agent, "go"); b["provider"] != nil {
		t.Errorf("an agent's build must not use the assistant's AI: %v", b)
	}
	if b := s.buildRequest(assistantActor("bob"), "go"); b["provider"] != nil {
		t.Errorf("another person's assistant: %v", b)
	}
}

func TestAssistantReadsAnAgentsMemoryNotes(t *testing.T) {
	s, st, secret := assistantTestServer(t)
	s.AgentRoot = t.TempDir()
	write := func(rel, text string) {
		p := filepath.Join(s.AgentRoot, "h1", filepath.FromSlash(rel))
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, []byte(text), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	write("memories/USER.md", "The user is Abdel; prefers short answers.")
	write("workspace/MEMORY.md", "Watching flight prices to Lisbon every morning.")
	write("sessions/memory/old.md", "a session transcript, not a note")
	write("notes.txt", "not memory")
	_, out := mcpCall(t, s.AgentProxy(), "/assistant/mcp", secret, "tools/call",
		map[string]any{"name": "read_agent_memory", "arguments": map[string]string{"agent_id": "h1"}})
	text, isErr := assistantToolText(t, out)
	if isErr || !strings.Contains(text, "prefers short answers") || !strings.Contains(text, "flight prices to Lisbon") {
		t.Errorf("memory: %q", text)
	}
	if strings.Contains(text, "session transcript") || strings.Contains(text, "not memory") {
		t.Errorf("memory has more than the notes: %q", text)
	}
	// Another person's agent stays private.
	if err := st.CreateAgentInstance(context.Background(), &store.AgentInstance{ID: "h2", OwnerToken: "bob", Package: "hermes",
		Name: "Bob's", ConnectionID: "c1", Model: "m1", SecretHash: hashSecret("y"), CreatedAt: "now"}); err != nil {
		t.Fatal(err)
	}
	_, out = mcpCall(t, s.AgentProxy(), "/assistant/mcp", secret, "tools/call",
		map[string]any{"name": "read_agent_memory", "arguments": map[string]string{"agent_id": "h2"}})
	if _, isErr := assistantToolText(t, out); !isErr {
		t.Error("another owner's agent memory: want an error")
	}
}

func TestAssistantListsTheAgentCatalog(t *testing.T) {
	s, _, secret := assistantTestServer(t)
	_, out := mcpCall(t, s.AgentProxy(), "/assistant/mcp", secret, "tools/call",
		map[string]any{"name": "add_agent", "arguments": map[string]string{}})
	if text, isErr := assistantToolText(t, out); isErr || !strings.Contains(text, "(package hermes)") {
		t.Errorf("catalog: %q", text)
	}
}

// The guide the assistant reads on every turn names each of its tools, so a
// new tool is not left unexplained (and a removed one is not still promised).
func TestAssistantGuideNamesEveryTool(t *testing.T) {
	for _, tool := range assistantTools {
		if !strings.Contains(assistantGuide, tool.Name) {
			t.Errorf("assistantGuide does not mention the %s tool", tool.Name)
		}
	}
}
