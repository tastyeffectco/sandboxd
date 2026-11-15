// Package agentcatalog defines the operator agents sandboxd can run and
// renders their configuration. Core runs an agent as a hardened container from
// a pinned image; everything agent-specific lives in its Package, as data.
package agentcatalog

import (
	"encoding/json"
	"fmt"
	"net/url"
	"sort"
	"strconv"
	"strings"
)

// Package describes one kind of agent.
type Package struct {
	ID          string `json:"id"`
	Label       string `json:"label"`
	Description string `json:"description"`
	// Image is pinned by digest: the build its authors released and we tested.
	Image string `json:"-"`
	// Cmd is the foreground command the image's own supervisor runs.
	Cmd []string `json:"-"`
	// User is the non-root uid:gid the image runs as; state is owned by it.
	User string `json:"-"`
	// StatePath is where the agent keeps memory, skills and sessions.
	StatePath string `json:"-"`
	// APIPort serves the agent's OpenAI-compatible chat API (console chat).
	APIPort int `json:"-"`
	// ChatModel is the model name the agent's chat API expects.
	ChatModel string `json:"-"`
	// APIKeyEnv names the variable that sets the key for the agent's chat API.
	APIKeyEnv string `json:"-"`
	// UIPort serves the agent's own web app (its native interface), shown in
	// the person's agent window next to sandboxd's chat. 0: it has none.
	UIPort int `json:"-"`
	// UILabel names that app for people, e.g. "Control panel".
	UILabel string `json:"ui_label,omitempty"`
	// UILogin says how sandboxd signs the owner in to an app that has its
	// own login: "password" — sandboxd sets a password only it knows and
	// keeps the session itself (Hermes's dashboard).
	UILogin string `json:"-"`
	// ProxyAuth: the agent trusts sandboxd as its sign-in proxy (identity
	// headers from sandboxd's address only), so its own app needs no token
	// or device pairing. Its config is refreshed when sandboxd's address moves.
	ProxyAuth bool `json:"-"`
	// Env is fixed environment for the agent's container.
	Env []string `json:"-"`
	// ExecRun mounts an executable /run (supervisors such as s6 run from it).
	ExecRun bool `json:"-"`
	// UploadsDir, under StatePath, holds files the person attaches in chat;
	// the agent can open them with its own tools.
	UploadsDir string `json:"-"`
	// ConfigFile is written into StatePath by Render before every start.
	ConfigFile string                       `json:"-"`
	Render     func(Config) (string, error) `json:"-"`
	// RenderEnv adds per-agent environment (agents configured by variables).
	RenderEnv func(Config) []string `json:"-"`
	// GuideFile, under StatePath, is the instructions file the agent reads
	// on every turn (AGENTS.md). sandboxd keeps its guide in a marked section
	// of it (how scheduled work reaches the person); the rest is the
	// person's or the agent's own.
	GuideFile string `json:"-"`
	// GuideSeed, a path in the agent's image: the agent writes GuideFile
	// from this template only on its first conversation, so sandboxd starts
	// the file from it (its own text first) and adds its section from the
	// first message on.
	GuideSeed string `json:"-"`
	// Deliver tells the agent how a scheduled job sends its result to the
	// person's sandboxd chat (see Skill).
	Deliver func(Config) string `json:"-"`
	// Browser: the agent has a browser tool but its image has no browser.
	// sandboxd runs a headless Chromium next to it (BrowserImage, its own
	// locked-down container) and the agent reaches it on 127.0.0.1:BrowserPort
	// through a relay in its start command (BrowserUpstreamEnv says where).
	Browser bool `json:"-"`
	// Init runs a minimal init that reaps child processes (browsers).
	Init bool `json:"-"`
	// StrongModel: it leans on many tools at once, so the strongest model
	// connected is suggested when adding it (small models garble its calls).
	StrongModel bool `json:"strong_model"`
	// Revision changes when its container must be made again (new
	// command or environment); the agent's data stays.
	Revision string `json:"-"`
	// AppFirst: its own app is a full chat (tool steps, files, its own
	// automations), so it opens there; sandboxd's chat is the other tab.
	AppFirst bool `json:"app_first"`
	// Chat: sandboxd's own chat window can talk to it. Agents without it
	// open on their own app.
	Chat bool `json:"chat"`
	// Telegram: the agent can talk on Telegram with an allow-list of users.
	Telegram bool   `json:"telegram"`
	Memory   string `json:"-"`
	ShmSize  string `json:"-"`
	DiskNote string `json:"download"`
	// MemoryNote tells the user how much memory the agent can use.
	MemoryNote string `json:"memory"`
}

// Hermes is Nous Research's Hermes Agent. Tested in its official image under
// sandboxd's profile: non-root, all capabilities dropped, read-only root,
// no-new-privileges (its s6 supervisor needs an executable /run).
var Hermes = Package{
	ID:          "hermes",
	Label:       "Hermes",
	Description: "A personal assistant that remembers, learns new skills and can build apps for you. Chat with it here or on Telegram.",
	Image:       "nousresearch/hermes-agent@sha256:9c5561579f3ffdf333401797094ac3a743738d6c25d40f4331c7bf77b694238d",
	Cmd:         []string{"gateway", "run"},
	User:        "10000:10000",
	StatePath:   "/opt/data",
	APIPort:     8642,
	// Its web dashboard (settings, sessions, skills) runs next to the
	// gateway, behind its own password login.
	UIPort:    9119,
	UILabel:   "Dashboard",
	UILogin:   "password",
	Chat:      true,
	ChatModel: "hermes-agent",
	// Its scheduled jobs garbled tool calls on a small model (DeepSeek flash).
	StrongModel: true,
	APIKeyEnv:   "API_SERVER_KEY",
	Env: []string{"API_SERVER_ENABLED=true", "API_SERVER_HOST=0.0.0.0", "API_SERVER_PORT=8642",
		"HERMES_DASHBOARD=1", "HERMES_DASHBOARD_BASIC_AUTH_USERNAME=" + User},
	// Its terminal works in its workspace (see RenderHermesConfig), where it
	// reads AGENTS.md on every turn; it skipped the guide as a skill.
	GuideFile: "workspace/AGENTS.md", // = hermesWorkspace
	Deliver: func(Config) string {
		return "End its instructions with: \"Send the result to the user with the sandboxd message_user tool.\" " +
			"Keep its own delivery local."
	},
	ExecRun:    true,
	UploadsDir: "uploads",
	ConfigFile: "config.yaml",
	Render:     RenderHermesConfig,
	Telegram:   true,
	Memory:     "2g",
	ShmSize:    "1g",
	DiskNote:   "about 3 GB",
	MemoryNote: "up to 2 GB",
}

// OpenClaw is the OpenClaw personal assistant. Tested in its official image
// under sandboxd's profile (non-root, no capabilities, read-only root,
// no-new-privileges); its state lives under /state.
var OpenClaw = Package{
	ID:          "openclaw",
	Label:       "OpenClaw",
	Description: "An open-source personal assistant with skills and a memory of its own. Chat with it here and ask it to build apps for you.",
	Image:       "ghcr.io/openclaw/openclaw@sha256:1a2a3ce113e435039dbeac5ae261759bb4759908563d91c81059e896b38c5d98",
	Cmd:         []string{"sh", "-c", loopbackRelay(BrowserUpstreamEnv, BrowserPort) + "exec node openclaw.mjs gateway"},
	Browser:     true,
	Init:        true,
	Revision:    "1", // a browser of its own
	User:        "1000:1000",
	StatePath:   "/state",
	APIPort:     18789,
	UIPort:      18789,
	UILabel:     "Control panel",
	ProxyAuth:   true,
	Chat:        true,
	AppFirst:    true,
	StrongModel: true,
	ChatModel:   "openclaw",
	// No shared token: OpenClaw refuses one next to proxy sign-in.
	APIKeyEnv: "",
	// sandboxd owns openclaw.json (read-only to OpenClaw): OpenClaw restarts
	// its gateway whenever it saves its config, which cut off chats.
	Env: []string{"HOME=/state", "OPENCLAW_STATE_DIR=/state",
		"OPENCLAW_CONFIG_PATH=/state/openclaw.json", "OPENCLAW_CONFIG_READONLY=1",
		"OPENCLAW_NO_RESPAWN=1", "NODE_COMPILE_CACHE=/tmp/openclaw-compile-cache"},
	UploadsDir: "workspace/uploads", // its file tools work in its workspace
	// Its scheduled jobs cannot use MCP tools, so their result comes back
	// through its own webhook delivery, to the agent's sandboxd inbox. The
	// guide sits in its own AGENTS.md: as a skill, or as an extra file, it
	// was skipped.
	GuideFile: "workspace/AGENTS.md",
	GuideSeed: "/app/docs/reference/templates/AGENTS.md",
	Deliver: func(c Config) string {
		return "Give every automation, one-time or recurring, this delivery: `{\"mode\": \"webhook\", \"to\": \"" +
			InboxURL(c) + "\"}`. Its final reply is posted in the user's chat, so make that reply the message " +
			"itself, and do not also send it another way. Announce delivery and the current session never reach " +
			"the user here, so never use them; check the delivery when you edit a job. Automations here have no " +
			"run limit, and a counter kept in a trigger script was lost between runs: for a limited number of " +
			"runs, create one one-time automation per run."
	},
	ConfigFile: "openclaw.json",
	Render:     RenderOpenClawConfig,
	// It peaks at about 2.4 GB on a chat: with 2 GB and no swap it was killed.
	Memory:     "3g",
	ShmSize:    "1g",
	DiskNote:   "about 2.2 GB, with its browser",
	MemoryNote: "up to 4 GB, with its browser",
}

// OpenMausBot is OpenMausBot's bot workspace, used through its own app.
// Tested in its official image under sandboxd's profile (non-root, no
// capabilities, read-only root, no-new-privileges); its per-bot sandbox is
// off (it needs user namespaces) and Chrome runs without its own sandbox,
// as OpenMausBot does in any container.
var OpenMausBot = Package{
	ID:          "openmausbot",
	Label:       "OpenMausBot",
	Description: "Bots that work on a computer of their own: they browse, use files and run tasks for you. Use it in its own app here.",
	Image:       "ghcr.io/milind-soni/openmausbot@sha256:597574f2ee587ff9a1c0a960dc8395714103f228d709ba5353f09a59f4ec9415",
	User:        "1001:1001",
	StatePath:   "/data",
	APIPort:     8799,
	UIPort:      8799,
	UILabel:     "Workspace",
	UILogin:     "pairing",
	Env: []string{"HOME=/data", "OMB_LAN=1", "OMB_BOT_BOX=0", "OMB_SERVER_NAME=sandboxd",
		"OPENAI_COMPAT_PROVIDER=sandboxd"},
	// It accepts plain HTTP for images and computer tools only on loopback,
	// so a small relay on its loopback carries model calls to sandboxd.
	Cmd:         []string{"sh", "-c", ombStart},
	Revision:    "2",
	StrongModel: true,
	RenderEnv:   RenderOpenMausBotEnv,
	Init:        true,
	UploadsDir:  "uploads",
	Memory:      "3g",
	ShmSize:     "512m",
	DiskNote:    "about 1.5 GB",
	MemoryNote:  "up to 3 GB",
}

var packages = map[string]Package{Hermes.ID: Hermes, OpenClaw.ID: OpenClaw, OpenMausBot.ID: OpenMausBot}

// ombModelPort is the loopback port of the relay to sandboxd's model proxy.
const ombModelPort = "9101"

// ombStart starts the relay, then OpenMausBot itself.
var ombStart = loopbackRelay("SANDBOXD_MODEL_UPSTREAM", ombModelPort) + "exec node dist-server/server-launcher.js"

// The browser sandboxd runs for agents with Browser: headless Chromium,
// open source, pinned by digest (multi-arch).
const (
	BrowserImage       = "chromedp/headless-shell@sha256:830fa77e0602cb42e5cb2cbd9237ba27b649ad69d5fb82b54713aac8b7ab4f29"
	BrowserPort        = "9222"
	BrowserUpstreamEnv = "SANDBOXD_BROWSER_UPSTREAM"
)

// loopbackRelay is a shell line that relays 127.0.0.1:port to the host:port
// in the environment variable env, in the background. Agents that only trust
// loopback (OpenMausBot's model calls, Chromium's DevTools, which refuses a
// container name as Host) reach sandboxd's services through it.
func loopbackRelay(env, port string) string {
	return `node -e '
const net = require("net"), [host, port] = process.env.` + env + `.split(":");
net.createServer((c) => {
  const u = net.connect(Number(port), host);
  c.on("error", () => u.destroy()); u.on("error", () => c.destroy());
  c.pipe(u).pipe(c);
}).listen(` + port + `, "127.0.0.1");
' &
`
}

// RenderOpenMausBotEnv points its built-in OpenAI-compatible engine at
// sandboxd's model proxy through the loopback relay (the real key never
// enters the container).
func RenderOpenMausBotEnv(c Config) []string {
	upstream := strings.TrimPrefix(strings.TrimPrefix(strings.TrimRight(c.ProxyURL, "/"), "http://"), "https://")
	if !strings.Contains(upstream, ":") {
		upstream += ":80"
	}
	base := "http://127.0.0.1:" + ombModelPort + "/agents/" + c.AgentID + "/v1"
	return []string{"SANDBOXD_MODEL_UPSTREAM=" + upstream, "OPENAI_COMPAT_URL=" + base,
		"OPENAI_COMPAT_API_KEY=" + c.Secret, "OPENAI_COMPAT_MODEL=" + c.Model}
}

// Get returns a package by id.
func Get(id string) (Package, bool) {
	p, ok := packages[id]
	return p, ok
}

// List returns every package, sorted by id.
func List() []Package {
	out := make([]Package, 0, len(packages))
	for _, p := range packages {
		out = append(out, p)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].ID < out[j].ID })
	return out
}

// SecretEnv carries the agent secret in the container's environment, for
// agents whose config references it instead of holding it.
const SecretEnv = "SANDBOXD_AGENT_SECRET"

// Config is what sandboxd writes into an agent's config file.
type Config struct {
	ProxyURL string // sandboxd's in-network proxy, e.g. http://sandboxd:9100
	AgentID  string
	Secret   string // the agent secret: model access and app tools
	Model    string // the model id of the chosen connection
	// ProxyIP is sandboxd's address on the agents' network: the only
	// source trusted to sign people in (ProxyAuth packages).
	ProxyIP string
	// UIOrigin is the Origin sandboxd presents to the agent's own app.
	UIOrigin string
}

// Identity headers sandboxd sends to ProxyAuth agents. The client address is
// a fixed documentation address: requests through sandboxd are never local.
const (
	UserHeader = "X-Sandboxd-User"
	User       = "owner"
	ClientIP   = "192.0.2.1"
)

func uiOrigin(c Config) string {
	if c.UIOrigin != "" {
		return c.UIOrigin
	}
	return "http://a-" + c.AgentID + ":18789"
}

// RenderHermesConfig returns config.yaml: the model goes through sandboxd's
// proxy (the real key never enters the container), sandboxd's app tools are an
// MCP server, and commands run inside the agent's own container.
func RenderHermesConfig(c Config) (string, error) {
	base := strings.TrimRight(c.ProxyURL, "/") + "/agents/" + c.AgentID
	for name, v := range map[string]string{"proxy URL": c.ProxyURL, "agent id": c.AgentID, "secret": c.Secret, "model": c.Model} {
		if v == "" {
			return "", fmt.Errorf("hermes config: %s is required", name)
		}
	}
	q := strconv.Quote // YAML accepts JSON-style double-quoted strings
	return "# Written by sandboxd. Changes to model, terminal and mcp_servers are\n" +
		"# replaced when the agent is re-created.\n" +
		"model:\n" +
		"  provider: custom\n" +
		"  default: " + q(c.Model) + "\n" +
		"  base_url: " + q(base+"/v1") + "\n" +
		"  api_key: " + q(c.Secret) + "\n" +
		"terminal:\n" +
		"  backend: local\n" +
		"  cwd: " + q(hermesWorkspace) + "\n" +
		"mcp_servers:\n" +
		"  sandboxd:\n" +
		"    url: " + q(base+"/mcp") + "\n" +
		"    headers:\n" +
		"      Authorization: " + q("Bearer "+c.Secret) + "\n", nil
}

// RenderOpenClawConfig returns openclaw.json: a local gateway with its
// OpenAI-compatible chat endpoint on, the model through sandboxd's proxy, and
// sandboxd's app tools as a streamable-HTTP MCP server. The secret stays in
// the environment (SecretEnv); the file only references it.
func RenderOpenClawConfig(c Config) (string, error) {
	for name, v := range map[string]string{"proxy URL": c.ProxyURL, "agent id": c.AgentID, "model": c.Model,
		"proxy address": c.ProxyIP} {
		if v == "" {
			return "", fmt.Errorf("openclaw config: %s is required", name)
		}
	}
	base := strings.TrimRight(c.ProxyURL, "/") + "/agents/" + c.AgentID
	ref := "${" + SecretEnv + "}"
	cfg := map[string]any{
		// Only sandboxd signs people in: its own chat and the person's
		// Control panel arrive through sandboxd with an identity header, from
		// sandboxd's address alone. Browsers enroll without pairing codes.
		"gateway": map[string]any{"mode": "local", "bind": "lan",
			"trustedProxies": []string{c.ProxyIP},
			"auth": map[string]any{"mode": "trusted-proxy",
				"identityScopes": map[string]any{User: []string{"operator.admin"}},
				"trustedProxy": map[string]any{"userHeader": strings.ToLower(UserHeader), "allowUsers": []string{User},
					"deviceAutoApprove": map[string]any{"enabled": true}}},
			"controlUi": map[string]any{"allowedOrigins": []string{uiOrigin(c)}},
			"http":      map[string]any{"endpoints": map[string]any{"chatCompletions": map[string]any{"enabled": true}}}},
		"models": map[string]any{"mode": "merge", "providers": map[string]any{"sandboxd": map[string]any{
			"baseUrl": base + "/v1", "apiKey": ref, "api": "openai-completions",
			"models": []map[string]any{{"id": c.Model, "name": c.Model, "input": []string{"text"},
				"contextWindow": 128000, "maxTokens": 8192}},
		}}},
		"agents": map[string]any{"defaults": map[string]any{"model": map[string]any{"primary": "sandboxd/" + c.Model}},
			"entries": map[string]any{"main": map[string]any{}}},
		// The skills OpenClaw's startup check turns off in this image (they need
		// tools or accounts a sandboxd agent does not have). Written here so
		// the read-only config needs no change; refresh when the pin moves.
		"skills": map[string]any{"entries": disabledSkills(openClawUnavailableSkills)},
		// Apps are built through sandboxd's tools so the person can open them.
		// OpenClaw otherwise hides MCP tools behind a search step and builds
		// apps itself (files, a local server, portals, canvas) inside its
		// private container, where nobody can open them. Its other tools stay.
		"tools": map[string]any{"deny": []string{"portal", "canvas"},
			"toolSearch": map[string]any{"enabled": false}},
		// Scheduled jobs post their result to the agent's sandboxd inbox
		// (see Deliver), signed with the agent's secret.
		"cron": map[string]any{"webhookToken": ref,
			"webhookSsrfPolicy": map[string]any{"allowedHostnames": []string{proxyHost(c.ProxyURL)}}},
		// Its browser is sandboxd's headless Chromium (see Browser), already
		// running: OpenClaw attaches to it and never starts one itself.
		"browser": map[string]any{"enabled": true, "defaultProfile": "sandboxd",
			"profiles": map[string]any{"sandboxd": map[string]any{
				"cdpUrl": "http://127.0.0.1:" + BrowserPort, "attachOnly": true}}},
		"mcp": map[string]any{"servers": map[string]any{"sandboxd": map[string]any{
			"url": base + "/mcp", "transport": "streamable-http",
			"headers": map[string]string{"Authorization": "Bearer " + ref},
		}}},
	}
	b, err := json.MarshalIndent(cfg, "", "  ")
	return string(b) + "\n", err
}

// openClawUnavailableSkills are the skills OpenClaw's doctor disables in the
// pinned image under sandboxd's profile.
var openClawUnavailableSkills = []string{"1password", "blogwatcher", "blucli", "camsnap", "coding-agent",
	"eightctl", "gemini", "gh-issues", "gifgrep", "github", "gog", "goplaces", "himalaya", "mcporter",
	"model-usage", "nano-pdf", "obsidian", "openai-whisper", "openai-whisper-api", "openhue", "oracle",
	"ordercli", "sag", "sherpa-onnx-tts", "songsee", "sonoscli", "spotify-player", "summarize", "tmux",
	"trello", "xurl"}

func disabledSkills(names []string) map[string]any {
	out := make(map[string]any, len(names))
	for _, n := range names {
		out[n] = map[string]bool{"enabled": false}
	}
	return out
}

// InboxURL is where an agent posts what its scheduled jobs found; sandboxd
// adds it to the agent's conversation. It is short and the same for every
// agent (the agent's secret says which one): a model copied a longer one
// with a typo.
func InboxURL(c Config) string {
	return strings.TrimRight(c.ProxyURL, "/") + "/agents/inbox"
}

func proxyHost(proxyURL string) string {
	if u, err := url.Parse(proxyURL); err == nil && u.Hostname() != "" {
		return u.Hostname()
	}
	return "sandboxd"
}

// hermesWorkspace is where Hermes's terminal works (StatePath/workspace).
const hermesWorkspace = "/opt/data/workspace"

// The guide's section in GuideFile.
const (
	GuideBegin = "<!-- sandboxd:begin (written by sandboxd, replaced when the agent is updated) -->"
	GuideEnd   = "<!-- sandboxd:end -->"
)

// Guide is sandboxd's section of GuideFile: work that runs later has no one
// in the chat, so it must say how its result reaches the person.
func Guide(p Package, c Config) string {
	if p.Deliver == nil || p.GuideFile == "" {
		return ""
	}
	return GuideBegin + `
## Results of scheduled work

The user talks to you in sandboxd's chat. Work that runs later (a scheduled
job, a reminder, a background task) cannot answer in that chat by itself, and
no other channel reaches the user unless they connected one.

When you create such work:

1. ` + p.Deliver(c) + `
2. Do not deliver to Telegram, Slack or other channels unless the user asked.
3. When the user asks for a limited number of runs ("3 times", "until Friday"),
   make sure it stops: give the job its scheduler's own run limit or end date
   if it has one; otherwise schedule one-time jobs, one per run. Never leave a
   job running that the user expects to end.
4. Tell the user the result will appear in this chat when it runs.

## Files for the user

To give the user a file you made, save it in your own folders and send it
with the sandboxd share_file tool. Attaching it to your reply does not reach
the user here.
` + GuideEnd + "\n"
}

// WithGuide puts the guide in an instructions file: it replaces sandboxd's
// section, or adds one at the end, keeping everything else as it is.
func WithGuide(file, guide string) string {
	if b := strings.Index(file, GuideBegin); b >= 0 {
		if e := strings.Index(file[b:], GuideEnd); e >= 0 {
			rest := strings.TrimPrefix(file[b+e+len(GuideEnd):], "\n")
			return file[:b] + guide + rest
		}
	}
	if file == "" {
		return guide
	}
	return strings.TrimRight(file, "\n") + "\n\n" + guide
}

// SeedText is a template from an agent's image as the agent itself writes
// it into its workspace: without the YAML front matter.
func SeedText(template string) string {
	if rest, ok := strings.CutPrefix(template, "---\n"); ok {
		if i := strings.Index(rest, "\n---\n"); i >= 0 {
			template = rest[i+len("\n---\n"):]
		}
	}
	return strings.TrimLeft(template, "\n")
}
