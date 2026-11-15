package agentcatalog

import (
	"encoding/json"
	"path"
	"strings"
	"testing"
)

func TestRenderHermesConfig(t *testing.T) {
	got, err := RenderHermesConfig(Config{ProxyURL: "http://sandboxd:9100/", AgentID: "h1",
		Secret: `s"ecret`, Model: "gpt-4.1-mini"})
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{
		"provider: custom",
		`base_url: "http://sandboxd:9100/agents/h1/v1"`,
		`url: "http://sandboxd:9100/agents/h1/mcp"`,
		`api_key: "s\"ecret"`, // quoted, so a secret can never break the YAML
		"backend: local",      // tools run inside the agent's own container
		`default: "gpt-4.1-mini"`,
	} {
		if !strings.Contains(got, want) {
			t.Errorf("config is missing %q:\n%s", want, got)
		}
	}
	if _, err := RenderHermesConfig(Config{ProxyURL: "x", AgentID: "h", Model: "m"}); err == nil {
		t.Error("a config without a secret was rendered")
	}
}

func TestPackagesArePinned(t *testing.T) {
	for _, p := range List() {
		if !strings.Contains(p.Image, "@sha256:") {
			t.Errorf("%s image %q is not pinned by digest", p.ID, p.Image)
		}
		if p.User == "" || strings.HasPrefix(p.User, "0:") {
			t.Errorf("%s must run as a non-root user, got %q", p.ID, p.User)
		}
	}
}

func TestRenderOpenClawConfig(t *testing.T) {
	got, err := RenderOpenClawConfig(Config{ProxyURL: "http://sandboxd:9100", AgentID: "a1", Secret: "s3cret", Model: "gpt-4.1-mini",
		ProxyIP: "172.18.0.4"})
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(got, "s3cret") {
		t.Error("the secret must stay in the environment, not in openclaw.json")
	}
	for _, want := range []string{
		`"mode": "local"`,
		`"chatCompletions": {`,
		`"baseUrl": "http://sandboxd:9100/agents/a1/v1"`,
		`"api": "openai-completions"`,
		`"primary": "sandboxd/gpt-4.1-mini"`,
		`"url": "http://sandboxd:9100/agents/a1/mcp"`,
		`"transport": "streamable-http"`,
		`"Authorization": "Bearer ${SANDBOXD_AGENT_SECRET}"`,
		// sandboxd's app tools are visible up front; its own app builders are off.
		`"portal",`,
		`"canvas"`,
		`"toolSearch": {`,
		`"enabled": false`,
		// sandboxd alone signs people in, so the Control panel needs no pairing.
		`"trustedProxies": [
      "172.18.0.4"
    ]`,
		`"mode": "trusted-proxy"`,
		`"userHeader": "x-sandboxd-user"`,
		`"deviceAutoApprove": {`,
		`"http://a-a1:18789"`,
	} {
		if !strings.Contains(got, want) {
			t.Errorf("openclaw.json is missing %s:\n%s", want, got)
		}
	}
}

func TestEveryPackageCanStart(t *testing.T) {
	for _, p := range List() {
		chatOK := !p.Chat || (p.ChatModel != "" && (p.APIKeyEnv != "" || p.ProxyAuth))
		configOK := (p.Render != nil && p.ConfigFile != "") || p.RenderEnv != nil
		if !chatOK || !configOK || p.APIPort == 0 || p.StatePath == "" || (!p.Chat && p.UIPort == 0) {
			t.Errorf("%s is missing what sandboxd needs to start it: %+v", p.ID, p)
		}
	}
}

func TestOpenClawConfigNeedsSandboxdAddress(t *testing.T) {
	if _, err := RenderOpenClawConfig(Config{ProxyURL: "http://sandboxd:9100", AgentID: "a1", Model: "m"}); err == nil {
		t.Fatal("without sandboxd's address OpenClaw would trust no one, or everyone")
	}
}

func TestProxySignInAgentsGetNoSharedToken(t *testing.T) {
	// OpenClaw will not start with a shared token next to trusted-proxy auth.
	if OpenClaw.APIKeyEnv != "" {
		t.Errorf("OpenClaw gets %s", OpenClaw.APIKeyEnv)
	}
}

func TestOpenMausBotUsesSandboxdModels(t *testing.T) {
	env := strings.Join(RenderOpenMausBotEnv(Config{ProxyURL: "http://sandboxd:9100/", AgentID: "m1", Secret: "s", Model: "deepseek/x"}), "\n")
	// Loopback: it allows plain HTTP for images and computer tools only there.
	for _, want := range []string{"SANDBOXD_MODEL_UPSTREAM=sandboxd:9100", "OPENAI_COMPAT_URL=http://127.0.0.1:9101/agents/m1/v1",
		"OPENAI_COMPAT_API_KEY=s", "OPENAI_COMPAT_MODEL=deepseek/x"} {
		if !strings.Contains(env, want) {
			t.Errorf("missing %s in\n%s", want, env)
		}
	}
	if !strings.Contains(strings.Join(OpenMausBot.Env, " "), "OMB_BOT_BOX=0") {
		t.Error("its per-bot sandbox needs user namespaces, which sandboxd's profile does not grant")
	}
}

// Each chat agent gets a skill that says how its scheduled jobs reach the
// person; OpenClaw's go to its sandboxd inbox, signed with its secret.
func TestScheduledResultsGuide(t *testing.T) {
	c := Config{ProxyURL: "http://sandboxd:9100", AgentID: "a1", Model: "m", ProxyIP: "172.18.0.2"}
	if s := Guide(Hermes, c); !strings.Contains(s, "message_user") || !strings.HasPrefix(s, GuideBegin+"\n") || !strings.HasSuffix(s, GuideEnd+"\n") {
		t.Fatalf("hermes instructions:\n%s", s)
	}
	// Hermes reads AGENTS.md where its terminal works.
	if path.Join(Hermes.StatePath, Hermes.GuideFile) != hermesWorkspace+"/AGENTS.md" {
		t.Fatal("Hermes's guide is not in its workspace")
	}
	if cfg, _ := RenderHermesConfig(Config{ProxyURL: "http://sandboxd:9100", AgentID: "a1", Secret: "s", Model: "m"}); !strings.Contains(cfg, "  cwd: \"/opt/data/workspace\"\n") {
		t.Fatalf("hermes config:\n%s", cfg)
	}
	if s := Guide(OpenClaw, c); !strings.Contains(s, `"to": "http://sandboxd:9100/agents/a1/inbox"`) {
		t.Fatalf("openclaw instructions:\n%s", s)
	}
	if Guide(OpenMausBot, c) != "" {
		t.Fatal("an agent without a sandboxd chat got the guide")
	}
	cfg, err := RenderOpenClawConfig(c)
	if err != nil {
		t.Fatal(err)
	}
	var out struct {
		Cron struct {
			WebhookToken string `json:"webhookToken"`
			Policy       struct {
				Hosts []string `json:"allowedHostnames"`
			} `json:"webhookSsrfPolicy"`
		} `json:"cron"`
	}
	if err := json.Unmarshal([]byte(cfg), &out); err != nil {
		t.Fatal(err)
	}
	if out.Cron.WebhookToken != "${"+SecretEnv+"}" || len(out.Cron.Policy.Hosts) != 1 || out.Cron.Policy.Hosts[0] != "sandboxd" {
		t.Fatalf("cron config: %+v", out.Cron)
	}
}

// sandboxd's section is added once and then replaced in place; the person's
// own text around it stays as it is.
func TestWithGuide(t *testing.T) {
	g1 := GuideBegin + "\nold\n" + GuideEnd + "\n"
	g2 := GuideBegin + "\nnew\n" + GuideEnd + "\n"
	if got := WithGuide("", g1); got != g1 {
		t.Fatalf("empty file: %q", got)
	}
	mine := "# My rules\nAnswer in French.\n"
	once := WithGuide(mine, g1)
	if once != mine+"\n"+g1 {
		t.Fatalf("added: %q", once)
	}
	twice := WithGuide(once+"## After\n", g2)
	if twice != mine+"\n"+g2+"## After\n" || WithGuide(twice, g2) != twice {
		t.Fatalf("replaced: %q", twice)
	}
}

// OpenClaw's image has no browser: it attaches to sandboxd's headless
// Chromium through a loopback relay (Chromium refuses a container name).
func TestOpenClawUsesSandboxdsBrowser(t *testing.T) {
	if !OpenClaw.Browser || len(OpenClaw.Cmd) != 3 || !strings.Contains(OpenClaw.Cmd[2], "process.env."+BrowserUpstreamEnv) ||
		!strings.Contains(OpenClaw.Cmd[2], `.listen(9222, "127.0.0.1")`) || !strings.HasSuffix(OpenClaw.Cmd[2], "exec node openclaw.mjs gateway") {
		t.Fatalf("cmd: %q", OpenClaw.Cmd)
	}
	if !strings.Contains(ombStart, "process.env.SANDBOXD_MODEL_UPSTREAM") || !strings.Contains(ombStart, `.listen(9101, "127.0.0.1")`) {
		t.Fatalf("openmausbot relay: %q", ombStart)
	}
	cfg, err := RenderOpenClawConfig(Config{ProxyURL: "http://sandboxd:9100", AgentID: "a1", Model: "m", ProxyIP: "172.18.0.2"})
	if err != nil {
		t.Fatal(err)
	}
	var out struct {
		Browser struct {
			Enabled  bool   `json:"enabled"`
			Default  string `json:"defaultProfile"`
			Profiles map[string]struct {
				CDP    string `json:"cdpUrl"`
				Attach bool   `json:"attachOnly"`
			} `json:"profiles"`
		} `json:"browser"`
	}
	_ = json.Unmarshal([]byte(cfg), &out)
	p := out.Browser.Profiles[out.Browser.Default]
	if !out.Browser.Enabled || p.CDP != "http://127.0.0.1:9222" || !p.Attach {
		t.Fatalf("browser config: %+v", out.Browser)
	}
	if !strings.HasPrefix(BrowserImage, "chromedp/headless-shell@sha256:") {
		t.Fatal("the browser image is not pinned")
	}
}
