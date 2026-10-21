package main

import (
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/tastyeffectco/sandboxd/control-plane/internal/modelconfig"
)

func connectionSessionDir(spec agentSpec, agent string) string {
	// Keep continuation within this workspace, agent, connection and model.
	providerID, model := "builtin", spec.model
	if spec.provider != nil {
		providerID, model = spec.provider.ID, spec.provider.Model.ID
	}
	sum := sha256.Sum256([]byte(providerID + "\x00" + model))
	root := spec.stateDir
	if root == "" {
		root = filepath.Join(spec.workDir, ".runtimed")
	}
	return filepath.Join(root, "agent-sessions", agent, hex.EncodeToString(sum[:16]))
}

func writeOpencodeConnection(c *modelconfig.Connection) (string, string, error) {
	base, err := ipBaseURL(c.BaseURL)
	if err != nil {
		return "", "", err
	}
	npm := "@ai-sdk/openai-compatible"
	switch c.Protocol {
	case "openai-completions":
	case "openai-responses", "openai-codex-responses":
		npm = "@ai-sdk/openai"
	case "anthropic-messages":
		npm = "@ai-sdk/anthropic"
	default:
		return "", "", fmt.Errorf("unsupported model protocol %q", c.Protocol)
	}
	id := c.Model.ID // preserve slashes, colons and gateway aliases verbatim
	cfg := map[string]any{
		"enabled_providers": []string{"sandboxd"},
		"provider": map[string]any{
			"sandboxd": map[string]any{
				"npm": npm, "name": "sandboxd",
				"options": map[string]any{"baseURL": base, "apiKey": c.Token},
				"models": map[string]any{id: map[string]any{
					"name": id, "options": map[string]any{"store": false}, "limit": map[string]int{"context": c.Model.ContextWindow, "output": c.Model.MaxTokens},
				}},
			},
		},
	}
	f, err := os.CreateTemp("", "sandboxd-opencode-*.json")
	if err != nil {
		return "", "", err
	}
	defer f.Close()
	if err := json.NewEncoder(f).Encode(cfg); err != nil {
		os.Remove(f.Name())
		return "", "", err
	}
	return f.Name(), "sandboxd/" + id, nil
}

func writePiConnection(c *modelconfig.Connection) (string, error) {
	base, err := ipBaseURL(c.BaseURL)
	if err != nil {
		return "", err
	}
	if c.Protocol != "openai-completions" && c.Protocol != "anthropic-messages" && c.Protocol != "openai-responses" && c.Protocol != "openai-codex-responses" {
		return "", fmt.Errorf("unsupported model protocol %q", c.Protocol)
	}
	dir, err := os.MkdirTemp("", "sandboxd-pi-")
	if err != nil {
		return "", err
	}
	// Pi's Anthropic adapter appends /v1/messages to its base URL, whereas its
	// OpenAI adapter appends /chat/completions. Verify both with real CLI tests.
	if c.Protocol == "anthropic-messages" {
		base = strings.TrimSuffix(base, "/v1")
	}
	token := c.Token
	if c.Protocol == "openai-codex-responses" {
		// Pi's transport requires an account-shaped placeholder locally. This is
		// deliberately unsigned and carries no real identity or authorization;
		// the proxy replaces both it and the account header on the wire.
		claims := `{"https://api.openai.com/auth":{"chatgpt_account_id":"sandboxd-proxy"}}`
		token = "e30." + base64.RawURLEncoding.EncodeToString([]byte(claims)) + ".sandboxd"
	}
	cfg := map[string]any{"providers": map[string]any{
		"sandboxd": map[string]any{
			"baseUrl": base, "api": c.Protocol, "apiKey": token,
			"models": []map[string]any{{
				"id": c.Model.ID, "name": c.Model.ID, "reasoning": false, "input": []string{"text"},
				"contextWindow": c.Model.ContextWindow, "maxTokens": c.Model.MaxTokens,
				"cost": map[string]int{"input": 0, "output": 0, "cacheRead": 0, "cacheWrite": 0},
			}},
		},
	}}
	raw, err := json.Marshal(cfg)
	if err == nil {
		err = os.WriteFile(filepath.Join(dir, "models.json"), raw, 0o600)
	}
	if err != nil {
		os.RemoveAll(dir)
		return "", err
	}
	if err := os.WriteFile(filepath.Join(dir, "settings.json"), []byte(`{"transport":"sse"}`), 0600); err != nil {
		os.RemoveAll(dir)
		return "", err
	}
	return dir, nil
}
