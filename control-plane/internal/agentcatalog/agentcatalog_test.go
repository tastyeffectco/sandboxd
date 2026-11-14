package agentcatalog

import (
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
