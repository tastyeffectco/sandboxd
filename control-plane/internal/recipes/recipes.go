// Package recipes is an embedded, in-repo registry of ADVISORY runtime recipes:
// per-framework sandbox.yaml guidance for Git-imported apps that have no matching
// built-in preset. Recipes are pure declarative DATA — never executed, never
// written, never auto-applied. Core only matches a recipe's detect rule and
// serves its suggested_manifest text; runtimed/the agent/user decide what to do.
//
// A recipe != a preset: a preset scaffolds a NEW app (starter files + image
// template + Go code); a recipe just configures an EXISTING repo. Adding a
// framework is therefore a data-only change (one YAML file) with no coupling to
// core. Every recipe's suggested_manifest is validated by internal/manifest at
// load time, so a bad contribution fails tests.
package recipes

import (
	"embed"
	"fmt"
	"sort"
	"strings"
	"sync"

	"gopkg.in/yaml.v3"

	"github.com/sandboxd/control-plane/internal/manifest"
	"github.com/sandboxd/control-plane/internal/preset"
)

//go:embed data/*.yaml
var dataFS embed.FS

// Recipe is one framework's advisory runtime guidance.
type Recipe struct {
	ID                string          `yaml:"id" json:"id"`
	DisplayName       string          `yaml:"display_name" json:"display_name"`
	Preset            string          `yaml:"preset" json:"preset,omitempty"` // runnable built-in preset, if any
	Detect            Detect          `yaml:"detect" json:"detect"`
	SuggestedManifest string          `yaml:"suggested_manifest" json:"suggested_manifest"`
	ConfigSnippets    []ConfigSnippet `yaml:"config_snippets" json:"config_snippets,omitempty"`
	Notes             []string        `yaml:"notes" json:"notes,omitempty"`
	// Tags are advisory capability hints for clients/docs: needs_config_snippet,
	// heavy_install, websocket, sse, sqlite_app, custom_image_recommended, …
	Tags     []string `yaml:"tags" json:"tags,omitempty"`
	Verified Verified `yaml:"verified" json:"verified"`
}

// Detect is how runtime-inspect recognizes the framework (all data, no code).
type Detect struct {
	Deps        []string `yaml:"deps" json:"deps"`                           // package.json deps — ALL must be present (AND)
	ConfigFiles []string `yaml:"config_files" json:"config_files"`           // ANY present matches
	ExcludeDeps []string `yaml:"exclude_deps" json:"exclude_deps,omitempty"` // if ANY present, no match
	// RequirementsContains matches a token (case-insensitive) in the project's
	// Python deps (requirements.txt / pyproject.toml). ANY token matches — a
	// GENERIC content match so Python frameworks stay data, not Go code.
	RequirementsContains []string `yaml:"requirements_contains" json:"requirements_contains,omitempty"`
}

// ConfigSnippet is a non-sandbox.yaml edit the user must make (advice only).
type ConfigSnippet struct {
	File string `yaml:"file" json:"file"`
	Note string `yaml:"note" json:"note"`
}

type Verified struct {
	Starter string `yaml:"starter" json:"starter"`
	Version string `yaml:"version" json:"version"`
}

// Matched is a recipe that fired against a workspace, with the reasons + whether
// it was a (high-confidence) dependency match.
type Matched struct {
	Recipe    Recipe
	Reasons   []string
	StrongDep bool
}

var (
	loadOnce sync.Once
	loaded   []Recipe
	loadErr  error
)

// All returns the embedded registry (loaded + validated once).
func All() ([]Recipe, error) {
	loadOnce.Do(func() { loaded, loadErr = load() })
	return loaded, loadErr
}

func load() ([]Recipe, error) {
	entries, err := dataFS.ReadDir("data")
	if err != nil {
		return nil, fmt.Errorf("read recipes dir: %w", err)
	}
	var out []Recipe
	seen := map[string]bool{}
	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".yaml") {
			continue
		}
		raw, err := dataFS.ReadFile("data/" + e.Name())
		if err != nil {
			return nil, fmt.Errorf("read %s: %w", e.Name(), err)
		}
		var r Recipe
		if err := yaml.Unmarshal(raw, &r); err != nil {
			return nil, fmt.Errorf("parse %s: %w", e.Name(), err)
		}
		if err := validate(r); err != nil {
			return nil, fmt.Errorf("%s: %w", e.Name(), err)
		}
		if seen[r.ID] {
			return nil, fmt.Errorf("duplicate recipe id %q", r.ID)
		}
		seen[r.ID] = true
		out = append(out, r)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].ID < out[j].ID })
	return out, nil
}

func validate(r Recipe) error {
	if r.ID == "" || r.DisplayName == "" {
		return fmt.Errorf("recipe needs id + display_name")
	}
	if len(r.Detect.Deps) == 0 && len(r.Detect.ConfigFiles) == 0 && len(r.Detect.RequirementsContains) == 0 {
		return fmt.Errorf("recipe %q has no detect signal (deps/config_files/requirements_contains)", r.ID)
	}
	if r.SuggestedManifest == "" {
		return fmt.Errorf("recipe %q has no suggested_manifest", r.ID)
	}
	// Core's own validator is the contribution quality gate.
	if res := manifest.Validate([]byte(r.SuggestedManifest)); !res.Valid {
		return fmt.Errorf("recipe %q suggested_manifest is invalid: %s", r.ID, strings.Join(res.Errors, "; "))
	}
	if r.Preset != "" && !preset.Valid(r.Preset) {
		return fmt.Errorf("recipe %q references unknown preset %q", r.ID, r.Preset)
	}
	return nil
}

// Match returns recipes whose detect rule fires for the given package.json
// dependency set, a file-existence check, and the lowercased Python requirements
// text (requirements.txt + pyproject.toml, "" if none). Pure: no side effects.
func Match(deps map[string]bool, exists func(string) bool, reqText string) ([]Matched, error) {
	all, err := All()
	if err != nil {
		return nil, err
	}
	req := strings.ToLower(reqText)
	var out []Matched
	for _, r := range all {
		if anyDep(deps, r.Detect.ExcludeDeps) {
			continue
		}
		var reasons []string
		strong := false
		if len(r.Detect.Deps) > 0 && allDeps(deps, r.Detect.Deps) {
			strong = true
			for _, d := range r.Detect.Deps {
				reasons = append(reasons, d+" is a dependency")
			}
		}
		for _, tok := range r.Detect.RequirementsContains {
			if tok != "" && strings.Contains(req, strings.ToLower(tok)) {
				strong = true
				reasons = append(reasons, tok+" in requirements")
				break
			}
		}
		var cfg string
		for _, c := range r.Detect.ConfigFiles {
			if exists(c) {
				cfg = c
				break
			}
		}
		if cfg != "" {
			reasons = append(reasons, cfg+" present")
		}
		if strong || cfg != "" {
			out = append(out, Matched{Recipe: r, Reasons: reasons, StrongDep: strong})
		}
	}
	return out, nil
}

func allDeps(have map[string]bool, want []string) bool {
	for _, d := range want {
		if !have[d] {
			return false
		}
	}
	return true
}

func anyDep(have map[string]bool, want []string) bool {
	for _, d := range want {
		if have[d] {
			return true
		}
	}
	return false
}
