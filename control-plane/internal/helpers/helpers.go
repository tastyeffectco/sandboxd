// Package helpers defines the operator agents ("helpers") sandboxd can run and
// renders their configuration. Core runs a helper as a hardened container from
// a pinned image; everything agent-specific lives in its Package, as data.
package helpers

import (
	"fmt"
	"sort"
	"strconv"
	"strings"
)

// Package describes one kind of helper.
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
	// StatePath is where the helper keeps memory, skills and sessions.
	StatePath string `json:"-"`
	// APIPort serves the helper's OpenAI-compatible chat API (console chat).
	APIPort  int    `json:"-"`
	Memory   string `json:"-"`
	ShmSize  string `json:"-"`
	DiskNote string `json:"download"`
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
	Memory:      "2g",
	ShmSize:     "1g",
	DiskNote:    "about 3 GB",
}

var packages = map[string]Package{Hermes.ID: Hermes}

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

// HermesConfig is what sandboxd writes into Hermes's config.yaml.
type HermesConfig struct {
	ProxyURL string // sandboxd's in-network proxy, e.g. http://sandboxd:9100
	HelperID string
	Secret   string // the helper secret: model access and app tools
	Model    string // the model id of the chosen connection
}

// RenderHermesConfig returns config.yaml: the model goes through sandboxd's
// proxy (the real key never enters the container), sandboxd's app tools are an
// MCP server, and commands run inside the helper's own container.
func RenderHermesConfig(c HermesConfig) (string, error) {
	base := strings.TrimRight(c.ProxyURL, "/") + "/helpers/" + c.HelperID
	for name, v := range map[string]string{"proxy URL": c.ProxyURL, "helper id": c.HelperID, "secret": c.Secret, "model": c.Model} {
		if v == "" {
			return "", fmt.Errorf("hermes config: %s is required", name)
		}
	}
	q := strconv.Quote // YAML accepts JSON-style double-quoted strings
	return "# Written by sandboxd. Changes to model, terminal and mcp_servers are\n" +
		"# replaced when the helper is re-created.\n" +
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
