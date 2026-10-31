// Package agentcatalog defines the operator agents sandboxd can run and
// renders their configuration. Core runs an agent as a hardened container from
// a pinned image; everything agent-specific lives in its Package, as data.
package agentcatalog

import (
	"encoding/json"
	"fmt"
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
	// Env is fixed environment for the agent's container.
	Env []string `json:"-"`
	// ExecRun mounts an executable /run (supervisors such as s6 run from it).
	ExecRun bool `json:"-"`
	// ConfigFile is written into StatePath by Render before every start.
	ConfigFile string                       `json:"-"`
	Render     func(Config) (string, error) `json:"-"`
	// Telegram: the agent can talk on Telegram with an allow-list of users.
	Telegram bool `json:"telegram"`
	// WhatsApp: the agent can be linked to the owner's WhatsApp by QR code;
	// WhatsAppCreds is where its WhatsApp login lands, relative to StatePath.
	WhatsApp      bool   `json:"whatsapp"`
	WhatsAppCreds string `json:"-"`
	// ChannelEnv returns the environment that turns the configured channels on,
	// for agents configured through variables rather than their config file.
	ChannelEnv func(Config) []string `json:"-"`
	Memory     string                `json:"-"`
	ShmSize    string                `json:"-"`
	DiskNote   string                `json:"download"`
	// MemoryNote tells the user how much memory the agent can use.
	MemoryNote string `json:"memory"`
}

// Hermes is Nous Research's Hermes Agent. Tested in its official image under
// sandboxd's profile: non-root, all capabilities dropped, read-only root,
// no-new-privileges (its s6 supervisor needs an executable /run).
var Hermes = Package{
	ID:          "hermes",
	Label:       "Hermes",
	Description: "A personal assistant that remembers you and learns new skills: research, reminders, writing, and building apps. Talk to it on WhatsApp or Telegram.",
	Image:       "nousresearch/hermes-agent@sha256:9c5561579f3ffdf333401797094ac3a743738d6c25d40f4331c7bf77b694238d",
	Cmd:         []string{"gateway", "run"},
	User:        "10000:10000",
	StatePath:   "/opt/data",
	APIPort:     8642,
	ChatModel:   "hermes-agent",
	APIKeyEnv:   "API_SERVER_KEY",
	Env:         []string{"API_SERVER_ENABLED=true", "API_SERVER_HOST=0.0.0.0", "API_SERVER_PORT=8642"},
	ExecRun:     true,
	ConfigFile:  "config.yaml",
	Render:      RenderHermesConfig,
	Telegram:    true,
	WhatsApp:    true,
	// Its WhatsApp bridge's own session; "self-chat" answers the owner in
	// their "Message yourself" chat.
	WhatsAppCreds: "platforms/whatsapp/session/creds.json",
	ChannelEnv:    hermesChannelEnv,
	Memory:        "2g",
	ShmSize:       "1g",
	DiskNote:      "about 3 GB",
	MemoryNote:    "up to 2 GB",
}

// OpenClaw is the OpenClaw personal assistant. Tested in its official image
// under sandboxd's profile (non-root, no capabilities, read-only root,
// no-new-privileges); its state lives under /state.
var OpenClaw = Package{
	ID:          "openclaw",
	Label:       "OpenClaw",
	Description: "An open-source personal assistant with skills and a memory of its own: it searches, plans, writes and builds apps for you. Talk to it on WhatsApp or Telegram.",
	Image:       "ghcr.io/openclaw/openclaw@sha256:1a2a3ce113e435039dbeac5ae261759bb4759908563d91c81059e896b38c5d98",
	Cmd:         []string{"node", "openclaw.mjs", "gateway"},
	User:        "1000:1000",
	StatePath:   "/state",
	APIPort:     18789,
	ChatModel:   "openclaw",
	APIKeyEnv:   "OPENCLAW_GATEWAY_TOKEN",
	// sandboxd owns openclaw.json (read-only to OpenClaw): OpenClaw restarts
	// its gateway whenever it saves its config, which cut off chats.
	Env: []string{"HOME=/state", "OPENCLAW_STATE_DIR=/state",
		"OPENCLAW_CONFIG_PATH=/state/openclaw.json", "OPENCLAW_CONFIG_READONLY=1",
		"OPENCLAW_NO_RESPAWN=1", "NODE_COMPILE_CACHE=/tmp/openclaw-compile-cache"},
	ConfigFile: "openclaw.json",
	Render:     RenderOpenClawConfig,
	Telegram:   true,
	// WhatsApp is a plugin OpenClaw installs into its state on first link.
	WhatsApp:      true,
	WhatsAppCreds: "credentials/whatsapp/default/creds.json",
	// It peaks at about 2.4 GB on a chat: with 2 GB and no swap it was killed.
	Memory:     "3g",
	ShmSize:    "1g",
	DiskNote:   "about 2 GB",
	MemoryNote: "up to 3 GB",
}

var packages = map[string]Package{Hermes.ID: Hermes, OpenClaw.ID: OpenClaw}

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
	// TelegramUsers are the Telegram user ids the agent answers (its bot
	// token is in the environment); empty leaves Telegram off.
	TelegramUsers []string
	// WhatsApp loads the agent's WhatsApp channel (also while linking);
	// WhatsAppNumber is the owner's linked number, the only one it answers.
	WhatsApp       bool
	WhatsAppNumber string
}

func hermesChannelEnv(c Config) []string {
	var env []string
	if len(c.TelegramUsers) > 0 {
		env = append(env, "TELEGRAM_ALLOWED_USERS="+strings.Join(c.TelegramUsers, ","))
	}
	if c.WhatsAppNumber != "" {
		env = append(env, "WHATSAPP_ENABLED=true", "WHATSAPP_MODE=self-chat",
			"WHATSAPP_ALLOWED_USERS="+c.WhatsAppNumber)
	}
	return env
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
	for name, v := range map[string]string{"proxy URL": c.ProxyURL, "agent id": c.AgentID, "model": c.Model} {
		if v == "" {
			return "", fmt.Errorf("openclaw config: %s is required", name)
		}
	}
	base := strings.TrimRight(c.ProxyURL, "/") + "/agents/" + c.AgentID
	ref := "${" + SecretEnv + "}"
	cfg := map[string]any{
		"gateway": map[string]any{"mode": "local", "bind": "lan",
			"controlUi": map[string]any{"allowedOrigins": []string{"http://localhost:18789", "http://127.0.0.1:18789"}},
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
		"mcp": map[string]any{"servers": map[string]any{"sandboxd": map[string]any{
			"url": base + "/mcp", "transport": "streamable-http",
			"headers": map[string]string{"Authorization": "Bearer " + ref},
		}}},
	}
	channels := map[string]any{}
	// Only the owner, in direct chats and groups alike (OpenClaw's startup
	// check asks for both lists).
	if len(c.TelegramUsers) > 0 { // the bot token is TELEGRAM_BOT_TOKEN
		channels["telegram"] = map[string]any{"enabled": true, "dmPolicy": "allowlist",
			"allowFrom": c.TelegramUsers, "groupAllowFrom": c.TelegramUsers}
	}
	if c.WhatsApp {
		cfg["plugins"] = map[string]any{"entries": map[string]any{"whatsapp": map[string]any{"enabled": true}}}
		// The plugin's WhatsApp CLI skill needs a tool the image does not have.
		cfg["skills"] = map[string]any{"entries": disabledSkills(append([]string{"wacli"}, openClawUnavailableSkills...))}
	}
	if c.WhatsAppNumber != "" { // the owner's own number, as in OpenClaw's "personal phone" setup
		owner := []string{"+" + c.WhatsAppNumber}
		channels["whatsapp"] = map[string]any{"selfChatMode": true, "dmPolicy": "allowlist",
			"allowFrom": owner, "groupAllowFrom": owner}
	}
	if len(channels) > 0 {
		cfg["channels"] = channels
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
