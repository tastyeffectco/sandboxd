package authproxy

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"strings"

	"github.com/tastyeffectco/sandboxd/control-plane/internal/agentauth"
	"github.com/tastyeffectco/sandboxd/control-plane/internal/modelconfig"
)

type importedCredential struct {
	Type      string `json:"type"`
	Key       string `json:"key"`
	Access    string `json:"access"`
	Refresh   string `json:"refresh"`
	AccountID string `json:"accountId"`
	Expires   int64  `json:"expires"`
}

func (p *Proxy) importedCredentials(agent string) map[string]importedCredential {
	rel, ok := agentauth.CredentialFile(agent)
	if !ok {
		return nil
	}
	raw, err := os.ReadFile(filepath.Join(p.store.Dir(agent), rel))
	if err != nil {
		return nil
	}
	var values map[string]importedCredential
	if json.Unmarshal(raw, &values) != nil {
		return nil
	}
	return values
}

func importedUpstream(agent, account string, c importedCredential) string {
	switch account {
	case "openai-codex":
		if agent == "pi" && c.Type == "oauth" {
			return "codex"
		}
	case "openai":
		// OpenCode 1.19.3 stores its ChatGPT/Codex OAuth login under openai.
		// Pi uses openai-codex for that backend; its openai login is distinct.
		if agent == "opencode" && c.Type == "oauth" {
			return "codex"
		}
		return "openai"
	case "anthropic":
		// A Claude subscription login is only for Claude Code itself; Anthropic
		// API keys are fine.
		if c.Type == "oauth" {
			return ""
		}
		return "anthropic"
	case "opencode":
		return "zen"
	case "opencode-go":
		return "zengo"
	}
	return ""
}

func injectImported(c importedCredential, up string) (func(http.Header), bool) {
	token := c.Key
	if c.Type == "oauth" {
		token = c.Access
	}
	if token == "" || (c.Type != "oauth" && c.Type != "api" && c.Type != "api_key") {
		return nil, false
	}
	return func(h http.Header) {
		h.Del("Authorization")
		h.Del("X-Api-Key")
		h.Del("ChatGPT-Account-Id")
		if up == "anthropic" && c.Type != "oauth" {
			h.Set("X-Api-Key", token)
		} else {
			h.Set("Authorization", "Bearer "+token)
		}
		if up == "anthropic" && c.Type == "oauth" {
			h.Set("anthropic-beta", mergeBeta(h.Get("anthropic-beta")))
		}
		if up == "codex" && c.AccountID != "" {
			h.Set("ChatGPT-Account-Id", c.AccountID)
		}
	}, true
}

func (p *Proxy) importedModels(ctx context.Context, agent string) (Catalog, error) {
	credentials := p.importedCredentials(agent)
	out := Catalog{Models: []CatalogModel{}, Source: "provider"}
	for _, account := range []string{"openai-codex", "openai", "anthropic", "opencode", "opencode-go"} {
		c, ok := credentials[account]
		if !ok {
			continue
		}
		up := importedUpstream(agent, account, c)
		if up == "" {
			continue
		}
		catalog, err := p.modelsFrom(ctx, agent, up)
		if err != nil {
			return Catalog{}, fmt.Errorf("%s: %w", account, err)
		}
		for _, m := range catalog.Models {
			m.Provider = account
			m.ID = account + "/" + m.ID
			out.Models = append(out.Models, m)
		}
	}
	if len(out.Models) == 0 {
		return Catalog{}, fmt.Errorf("no supported account login found; import this agent's login or configure a model connection")
	}
	return out, nil
}

// BuiltinConnection uses the provider's returned model ID; it never substitutes
// a guessed model. The returned credentials are dummy proxy values only.
func (p *Proxy) BuiltinConnection(ctx context.Context, agent, model, proxyURL string) (*modelconfig.Connection, error) {
	catalog, err := p.Models(ctx, agent)
	if err != nil {
		return nil, err
	}
	var selected *CatalogModel
	for i := range catalog.Models {
		if catalog.Models[i].ID == model || (model == "" && i == 0) {
			selected = &catalog.Models[i]
			break
		}
	}
	if selected == nil {
		return nil, fmt.Errorf("selected model is not in the connected account's catalog")
	}
	credential := p.importedCredentials(agent)[selected.Provider]
	upstream := importedUpstream(agent, selected.Provider, credential)
	protocol := "openai-completions"
	if upstream == "anthropic" {
		protocol = "anthropic-messages"
	}
	if upstream == "openai" {
		protocol = "openai-responses"
	}
	if upstream == "codex" {
		protocol = "openai-codex-responses"
	}
	contextWindow := selected.ContextWindow
	if contextWindow == 0 {
		contextWindow = 32768
	}
	base := strings.TrimRight(proxyURL, "/") + "/" + agent + "/" + upstream
	if upstream == "anthropic" {
		base += "/v1"
	}
	return &modelconfig.Connection{ID: "account/" + selected.Provider, Protocol: protocol, BaseURL: base, Token: "sandboxd-proxy-injected", Model: modelconfig.Model{ID: strings.TrimPrefix(selected.ID, selected.Provider+"/"), ContextWindow: contextWindow, MaxTokens: 4096}}, nil
}
