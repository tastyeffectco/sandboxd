package main

import (
	"encoding/json"
	"os"
	"strings"
	"testing"
)

// The opencode proxy config defines ONE custom openai-compatible provider that
// routes through the credential-injecting proxy with a DUMMY key, and the
// requested model is rewritten to `proxy/<id>` — so no credential is needed in
// the sandbox and the model runs on that provider.
func TestWriteOpencodeProxyConfig(t *testing.T) {
	t.Setenv("RUNTIMED_ANTHROPIC_PROXY", "http://127.0.0.1:9100")
	t.Setenv("SANDBOXD_OPENCODE_ZEN_PATH", "zengo")

	path, model, err := writeOpencodeProxyConfig("opencode/kimi-k2.6")
	if err != nil {
		t.Fatalf("err: %v", err)
	}
	defer os.Remove(path)
	if model != "proxy/kimi-k2.6" {
		t.Errorf("model = %q; want proxy/kimi-k2.6 (prefix stripped, routed via proxy provider)", model)
	}

	var cfg struct {
		Provider map[string]struct {
			Npm     string `json:"npm"`
			Options struct {
				BaseURL string `json:"baseURL"`
				APIKey  string `json:"apiKey"`
			} `json:"options"`
			Models map[string]any `json:"models"`
		} `json:"provider"`
	}
	b, _ := os.ReadFile(path)
	if err := json.Unmarshal(b, &cfg); err != nil {
		t.Fatalf("config not valid json: %v", err)
	}
	p, ok := cfg.Provider["proxy"]
	if !ok {
		t.Fatal("missing custom 'proxy' provider")
	}
	if p.Npm != "@ai-sdk/openai-compatible" {
		t.Errorf("npm = %q", p.Npm)
	}
	if !strings.HasSuffix(p.Options.BaseURL, "/opencode/zengo") {
		t.Errorf("baseURL = %q; want the proxy's opencode/zengo endpoint", p.Options.BaseURL)
	}
	if p.Options.APIKey != dummyKey {
		t.Errorf("apiKey = %q; the config must carry only the dummy (proxy injects the real one)", p.Options.APIKey)
	}
	if _, ok := p.Models["kimi-k2.6"]; !ok {
		t.Errorf("requested model not registered on the provider: %+v", p.Models)
	}
}

// No proxy configured → no config, no model rewrite (fall back to opencode's own
// auth/model handling).
func TestWriteOpencodeProxyConfigNoProxy(t *testing.T) {
	t.Setenv("RUNTIMED_ANTHROPIC_PROXY", "")
	path, model, err := writeOpencodeProxyConfig("opencode/glm-5")
	if err != nil || path != "" || model != "" {
		t.Errorf("no-proxy = (%q,%q,%v); want empty", path, model, err)
	}
}

// An empty model falls back to a catalog-safe default so the proxy provider
// always has a model to route.
func TestWriteOpencodeProxyConfigDefaultModel(t *testing.T) {
	t.Setenv("RUNTIMED_ANTHROPIC_PROXY", "http://127.0.0.1:9100")
	path, model, err := writeOpencodeProxyConfig("")
	if err != nil {
		t.Fatal(err)
	}
	defer os.Remove(path)
	if model != "proxy/"+defaultProxyModel {
		t.Errorf("model = %q; want the default %q", model, defaultProxyModel)
	}
}

func TestZenUpstream(t *testing.T) {
	t.Setenv("SANDBOXD_OPENCODE_ZEN_PATH", "zengo")
	if zenUpstream() != "zengo" {
		t.Error("zengo not honored")
	}
	t.Setenv("SANDBOXD_OPENCODE_ZEN_PATH", "bogus")
	if zenUpstream() != "zen" {
		t.Error("unknown value should fall back to zen")
	}
	t.Setenv("SANDBOXD_OPENCODE_ZEN_PATH", "")
	if zenUpstream() != "zen" {
		t.Error("default should be zen")
	}
}
