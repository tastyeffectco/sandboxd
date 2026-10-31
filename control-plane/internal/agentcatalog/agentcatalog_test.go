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
	got, err := RenderOpenClawConfig(Config{ProxyURL: "http://sandboxd:9100", AgentID: "a1", Secret: "s3cret", Model: "gpt-4.1-mini"})
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
	} {
		if !strings.Contains(got, want) {
			t.Errorf("openclaw.json is missing %s:\n%s", want, got)
		}
	}
}

func TestEveryPackageCanStart(t *testing.T) {
	for _, p := range List() {
		if p.Render == nil || p.ConfigFile == "" || p.ChatModel == "" || p.APIKeyEnv == "" || p.APIPort == 0 || p.StatePath == "" {
			t.Errorf("%s is missing what sandboxd needs to start it: %+v", p.ID, p)
		}
	}
}

func TestChannelsAreOffUntilLinked(t *testing.T) {
	base := Config{ProxyURL: "http://sandboxd:9100", AgentID: "a1", Secret: "s", Model: "m"}
	got, _ := RenderOpenClawConfig(base)
	if strings.Contains(got, `"channels"`) || strings.Contains(got, `"plugins"`) {
		t.Errorf("an agent with no channels linked has channel settings:\n%s", got)
	}
	if env := hermesChannelEnv(base); len(env) != 0 {
		t.Errorf("hermes channel env without channels: %v", env)
	}
}

func TestChannelsAnswerOnlyTheOwner(t *testing.T) {
	c := Config{ProxyURL: "http://sandboxd:9100", AgentID: "a1", Secret: "s", Model: "m",
		TelegramUsers: []string{"42"}, WhatsApp: true, WhatsAppNumber: "33612345678"}
	got, _ := RenderOpenClawConfig(c)
	for _, want := range []string{
		`"whatsapp": {
        "enabled": true`, // the plugin is loaded
		`"selfChatMode": true`,
		`"+33612345678"`,
		`"groupAllowFrom"`,
		`"wacli": {
        "enabled": false`, // the plugin's skill this image cannot run
		`"dmPolicy": "allowlist"`,
		`"42"`,
	} {
		if !strings.Contains(got, want) {
			t.Errorf("openclaw.json is missing %s:\n%s", want, got)
		}
	}
	env := strings.Join(hermesChannelEnv(c), " ")
	for _, want := range []string{"TELEGRAM_ALLOWED_USERS=42", "WHATSAPP_ENABLED=true", "WHATSAPP_MODE=self-chat",
		"WHATSAPP_ALLOWED_USERS=33612345678"} {
		if !strings.Contains(env, want) {
			t.Errorf("hermes env is missing %s: %s", want, env)
		}
	}
	// While linking, only the plugin is loaded: nobody is answered yet.
	linking, _ := RenderOpenClawConfig(Config{ProxyURL: "x", AgentID: "a1", Model: "m", WhatsApp: true})
	if !strings.Contains(linking, `"plugins"`) || strings.Contains(linking, `"selfChatMode"`) {
		t.Errorf("linking config:\n%s", linking)
	}
}
