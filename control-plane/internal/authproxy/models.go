package authproxy

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"strings"
	"time"

	"github.com/tastyeffectco/sandboxd/control-plane/internal/agentauth"
)

// Catalog contains only provider-returned model metadata, never credentials.
// Being listed is not an inference entitlement check: a real task can still
// fail because of quota, organization policy, or a provider-side change.
type Catalog struct {
	Models []CatalogModel `json:"models"`
	Source string         `json:"source"`
}

type CatalogModel struct {
	ID            string `json:"id"`
	Name          string `json:"name"`
	ContextWindow int    `json:"context_window,omitempty"`
	Provider      string `json:"provider,omitempty"`
}

// Models queries the connected account. A failed discovery is an error, never
// a reason to manufacture a list from model-name guesses or bundled defaults.
func (p *Proxy) Models(ctx context.Context, agent string) (Catalog, error) {
	ctx, cancel := context.WithTimeout(ctx, 45*time.Second)
	defer cancel()
	if agent == "pi" || (agent == "opencode" && p.store.Method(agent) == "oauth") {
		if err := p.store.RefreshImported(ctx, agent); err != nil {
			return Catalog{}, err
		}
		return p.importedModels(ctx, agent)
	}
	up := ""
	switch agent {
	case "codex":
		if err := p.store.RefreshCodex(ctx); err != nil {
			return Catalog{}, err
		}
		up = "openai"
		if p.store.Method(agent) == "oauth" {
			up = "codex"
		}
	case "claude-code":
		// A Claude subscription is signed in inside each project, so there is no
		// account here to query; offer Claude Code's own model aliases.
		if p.store.Method(agent) != "api_key" {
			return Catalog{Models: []CatalogModel{
				{ID: "sonnet", Name: "Sonnet"}, {ID: "opus", Name: "Opus"}, {ID: "haiku", Name: "Haiku"},
			}, Source: "claude-code"}, nil
		}
		up = "anthropic"
	case "opencode":
		// With no account, OpenCode runs its built-in free model; Zen's paid
		// catalog would only offer models that fail without a key.
		if !p.store.Connected(agent) {
			free := agentauth.OpencodeFreeModel()
			return Catalog{Models: []CatalogModel{{ID: free, Name: "Free model"}}, Source: "free"}, nil
		}
		up = "zen"
		if p.store.Method(agent) != "" && os.Getenv("SANDBOXD_OPENCODE_ZEN_PATH") == "zengo" {
			up = "zengo"
		}
	default:
		return Catalog{}, fmt.Errorf("model discovery is not configured for this agent")
	}
	return p.modelsFrom(ctx, agent, up)
}

func (p *Proxy) modelsFrom(ctx context.Context, agent, up string) (Catalog, error) {
	suffix := "/models"
	if up == "codex" {
		suffix += "?client_version=0.162.1"
	}
	if up == "anthropic" {
		suffix = "/v1/models?limit=1000"
	}
	inject, ok := p.credFor(agent, up)
	if !ok {
		return Catalog{}, fmt.Errorf("connect %s before loading its models", agent)
	}
	client := &http.Client{Timeout: 20 * time.Second, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}
	out := Catalog{Models: []CatalogModel{}, Source: "provider"}
	seen := map[string]bool{}
	cursor := ""
	for page := 0; page < 20; page++ {
		pageSuffix := suffix
		if cursor != "" {
			pageSuffix += "&after_id=" + url.QueryEscape(cursor)
		}
		req, err := http.NewRequestWithContext(ctx, http.MethodGet, strings.TrimRight(upstreams[up], "/")+pageSuffix, nil)
		if err != nil {
			return Catalog{}, err
		}
		req.Header.Set("Accept", "application/json")
		req.Header.Set("Anthropic-Version", "2023-06-01")
		req.Header.Set("User-Agent", "sandboxd-model-discovery")
		inject(req.Header)
		response, err := client.Do(req)
		if err != nil {
			return Catalog{}, fmt.Errorf("could not reach the account's model catalog")
		}
		defer response.Body.Close()
		if response.StatusCode != http.StatusOK {
			if response.StatusCode == 401 || response.StatusCode == 403 {
				return Catalog{}, fmt.Errorf("the provider denied model discovery (HTTP %d); check or reconnect this account", response.StatusCode)
			}
			return Catalog{}, fmt.Errorf("model catalog returned HTTP %d", response.StatusCode)
		}
		raw, err := io.ReadAll(io.LimitReader(response.Body, (2<<20)+1))
		_ = response.Body.Close()
		if err != nil || len(raw) > 2<<20 {
			return Catalog{}, fmt.Errorf("invalid model catalog response")
		}
		type row struct {
			ID            string `json:"id"`
			Slug          string `json:"slug"`
			Name          string `json:"display_name"`
			Visibility    string `json:"visibility"`
			ContextWindow int    `json:"context_window"`
		}
		var body struct {
			Data    []row  `json:"data"`
			Models  []row  `json:"models"`
			HasMore bool   `json:"has_more"`
			LastID  string `json:"last_id"`
		}
		if json.Unmarshal(raw, &body) != nil {
			return Catalog{}, fmt.Errorf("invalid model catalog response")
		}
		rows := body.Data
		if up == "codex" {
			rows = body.Models
		}
		for _, m := range rows {
			id := m.ID
			if up == "codex" {
				id = m.Slug
			}
			if id == "" || len(id) > 200 || m.Visibility == "hide" || seen[id] {
				continue
			}
			seen[id] = true
			name := m.Name
			if name == "" {
				name = id
			}
			out.Models = append(out.Models, CatalogModel{ID: id, Name: name, ContextWindow: m.ContextWindow})
		}

		if !body.HasMore {
			break
		}
		if up != "anthropic" || body.LastID == "" || body.LastID == cursor || page == 19 {
			return Catalog{}, fmt.Errorf("provider returned an incomplete model catalog")
		}
		cursor = body.LastID
	}
	if len(out.Models) == 0 {
		return Catalog{}, fmt.Errorf("the provider returned no selectable models for this account")
	}
	return out, nil
}

// ResolveModel validates explicit selections and resolves an omitted default
// from the connected account's catalog. It never picks a CLI's bundled model.
func (p *Proxy) ResolveModel(ctx context.Context, agent, selected string) (string, error) {
	catalog, err := p.Models(ctx, agent)
	if err != nil {
		return "", err
	}
	for i, model := range catalog.Models {
		if model.ID == selected || (selected == "" && i == 0) {
			return model.ID, nil
		}
	}
	return "", fmt.Errorf("selected model is not in the connected account's catalog; refresh models and select again")
}
