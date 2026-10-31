package helpers

import (
	"strings"
	"testing"
)

func TestRenderHermesConfig(t *testing.T) {
	got, err := RenderHermesConfig(HermesConfig{ProxyURL: "http://sandboxd:9100/", HelperID: "h1",
		Secret: `s"ecret`, Model: "gpt-4.1-mini"})
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{
		"provider: custom",
		`base_url: "http://sandboxd:9100/helpers/h1/v1"`,
		`url: "http://sandboxd:9100/helpers/h1/mcp"`,
		`api_key: "s\"ecret"`, // quoted, so a secret can never break the YAML
		"backend: local",      // tools run inside the helper's own container
		`default: "gpt-4.1-mini"`,
	} {
		if !strings.Contains(got, want) {
			t.Errorf("config is missing %q:\n%s", want, got)
		}
	}
	if _, err := RenderHermesConfig(HermesConfig{ProxyURL: "x", HelperID: "h", Model: "m"}); err == nil {
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
