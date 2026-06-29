// Package detect is ADVISORY runtime detection: it reads a workspace's files and
// suggests a runtime stack/preset with confidence + reasons + warnings. It is
// read-only — it NEVER runs app code, installs dependencies, touches Git
// credentials, or applies anything. The control plane surfaces the result via
// GET /v1/apps/{id}/runtime-inspect; the user always overrides manually.
package detect

import (
	"encoding/json"
	"os"
	"path/filepath"
	"regexp"
	"strings"

	"github.com/sandboxd/control-plane/internal/manifest"
	"github.com/sandboxd/control-plane/internal/recipes"
)

// osReadFile/osExists read a workspace-relative file under root, refusing any
// path that escapes root (defense in depth; detection only uses fixed names).
func safeJoin(root, rel string) (string, bool) {
	p := filepath.Join(root, rel)
	if p != root && !strings.HasPrefix(p, root+string(os.PathSeparator)) {
		return "", false
	}
	return p, true
}
func osReadFile(root, rel string) ([]byte, error) {
	p, ok := safeJoin(root, rel)
	if !ok {
		return nil, os.ErrNotExist
	}
	return os.ReadFile(p)
}
func osExists(root, rel string) bool {
	p, ok := safeJoin(root, rel)
	if !ok {
		return false
	}
	_, err := os.Stat(p)
	return err == nil
}

// Files is the read-only workspace view the detector inspects (injected so it's
// testable and can never execute anything).
type Files interface {
	Read(rel string) ([]byte, bool)
	Exists(rel string) bool
}

// OSFiles reads workspace files from a host directory (control-plane side). It
// only ever READS regular files under root — no execution, no writes.
func OSFiles(root string) Files { return osFiles{root: root} }

type osFiles struct{ root string }

func (o osFiles) Read(rel string) ([]byte, bool) {
	b, err := osReadFile(o.root, rel)
	return b, err == nil
}
func (o osFiles) Exists(rel string) bool { return osExists(o.root, rel) }

// Suggestion is one advisory stack match.
type Suggestion struct {
	Preset     string   `json:"preset"`     // stack id; may be detect-only (Runnable=false)
	Runnable   bool     `json:"runnable"`   // a built-in preset exists to run it
	Confidence string   `json:"confidence"` // high | medium | low
	Reasons    []string `json:"reasons"`
	Warnings   []string `json:"warnings,omitempty"`
	// SuggestedManifest is ADVISORY sandbox.yaml text for a detect-only stack that
	// has no built-in preset yet. It is never written or applied — guidance only.
	SuggestedManifest string                  `json:"suggested_manifest,omitempty"`
	Notes             []string                `json:"notes,omitempty"`
	ConfigSnippets    []recipes.ConfigSnippet `json:"config_snippets,omitempty"`
	Tags              []string                `json:"tags,omitempty"`
}

// ManifestSummary describes an existing sandbox.yaml. Authoritative = a manifest is
// present and takes precedence (sandboxd won't substitute a runtime). Valid/Errors
// come from the SAME core validator as POST /v1/runtime/manifest/validate and
// GET /v1/apps/{id}/runtime/manifest, so all three agree (e.g. a versionless
// manifest is present+authoritative but valid:false). The web_* fields are
// as-declared (parsed, non-authoritative when valid is false).
type ManifestSummary struct {
	Present       bool     `json:"present"`
	Authoritative bool     `json:"authoritative"`
	Valid         bool     `json:"valid"`
	Errors        []string `json:"errors,omitempty"`
	WebCommand    string   `json:"web_command,omitempty"`
	WebPort       int      `json:"web_port,omitempty"`
	HealthPath    string   `json:"health_path,omitempty"`
}

// Result is the runtime-inspect payload.
type Result struct {
	ExistingManifest  *ManifestSummary `json:"existing_manifest"`
	Suggestions       []Suggestion     `json:"suggestions"`
	Alternatives      []string         `json:"alternatives,omitempty"`
	DefaultSuggestion string           `json:"default_suggestion,omitempty"`
	Warnings          []string         `json:"warnings,omitempty"`
}

// runnablePresets are the stack ids that have a built-in runtime preset today
// (must match internal/preset). astro/docusaurus are detect-only for now.
var runnablePresets = map[string]bool{
	"nextjs": true, "react-vite": true, "node-express": true, "fastapi": true, "worker": true,
}

type pkgJSON struct {
	Dependencies    map[string]string `json:"dependencies"`
	DevDependencies map[string]string `json:"devDependencies"`
	Scripts         map[string]string `json:"scripts"`
}

func (p *pkgJSON) has(dep string) bool {
	if p == nil {
		return false
	}
	if _, ok := p.Dependencies[dep]; ok {
		return true
	}
	_, ok := p.DevDependencies[dep]
	return ok
}

func anyExists(f Files, names ...string) (string, bool) {
	for _, n := range names {
		if f.Exists(n) {
			return n, true
		}
	}
	return "", false
}

// Inspect runs advisory detection over a workspace.
func Inspect(f Files) Result {
	res := Result{ExistingManifest: existingManifest(f), Suggestions: []Suggestion{}}
	if res.ExistingManifest.Present {
		res.Warnings = append(res.Warnings, manifestWarnings(f, res.ExistingManifest)...)
	}

	var pkg *pkgJSON
	if raw, ok := f.Read("package.json"); ok {
		var p pkgJSON
		if json.Unmarshal(raw, &p) == nil {
			pkg = &p
		}
	}

	add := func(s Suggestion) { res.Suggestions = append(res.Suggestions, s) }

	// JS web frameworks come from the advisory recipe registry (data, not code) so
	// contributors add a framework by adding a YAML file — no detection code here.
	deps := map[string]bool{}
	if pkg != nil {
		for d := range pkg.Dependencies {
			deps[d] = true
		}
		for d := range pkg.DevDependencies {
			deps[d] = true
		}
	}
	// Python deps live in requirements.txt / pyproject.toml, not package.json — pass
	// their text so recipes can match via requirements_contains (generic, data).
	var reqText string
	if raw, ok := f.Read("requirements.txt"); ok {
		reqText += string(raw) + "\n"
	}
	if raw, ok := f.Read("pyproject.toml"); ok {
		reqText += string(raw)
	}
	if matched, err := recipes.Match(deps, f.Exists, reqText); err == nil {
		for _, mm := range matched {
			conf := "medium"
			if mm.StrongDep {
				conf = "high"
			}
			add(Suggestion{
				Preset:            mm.Recipe.ID,
				Runnable:          mm.Recipe.Preset != "", // a built-in preset backs it
				Confidence:        conf,
				Reasons:           mm.Reasons,
				SuggestedManifest: mm.Recipe.SuggestedManifest,
				Notes:             mm.Recipe.Notes,
				ConfigSnippets:    mm.Recipe.ConfigSnippets,
				Tags:              mm.Recipe.Tags,
			})
		}
	}

	// Node / Express — kept as code (entry-file detection, not a simple dep match).
	if pkg.has("express") {
		if entry, ok := anyExists(f, "server.js", "index.js", "app.js", "src/index.js", "src/server.js"); ok {
			add(framework("node-express", true, true, "express is a dependency", "entry file "+entry+" present"))
		} else {
			s := framework("node-express", true, true, "express is a dependency", "")
			s.Confidence = "medium"
			s.Warnings = append(s.Warnings, "no obvious server entry file found (server.js/index.js/app.js)")
			add(s)
		}
	}
	// FastAPI (python)
	if fastapiDetected(f) {
		add(framework("fastapi", true, true, "fastapi found in requirements/pyproject or a main.py import", ""))
	}
	// Worker
	if f.Exists("worker.sh") {
		add(framework("worker", true, true, "worker.sh present", ""))
	}

	rank(&res)
	return res
}

// framework builds a base suggestion. strongDep=true => high confidence.
func framework(id string, runnable, strongDep bool, reasons ...string) Suggestion {
	conf := "medium"
	if strongDep {
		conf = "high"
	}
	s := Suggestion{Preset: id, Runnable: runnable && runnablePresets[id], Confidence: conf}
	for _, rr := range reasons {
		if rr != "" {
			s.Reasons = append(s.Reasons, rr)
		}
	}
	return s
}

func fastapiDetected(f Files) bool {
	if raw, ok := f.Read("requirements.txt"); ok && strings.Contains(strings.ToLower(string(raw)), "fastapi") {
		return true
	}
	if raw, ok := f.Read("pyproject.toml"); ok && strings.Contains(strings.ToLower(string(raw)), "fastapi") {
		return true
	}
	for _, e := range []string{"main.py", "app.py", "app/main.py"} {
		if raw, ok := f.Read(e); ok && strings.Contains(string(raw), "fastapi") {
			return true
		}
	}
	return false
}

// existingManifest summarizes sandbox.yaml if present.
func existingManifest(f Files) *ManifestSummary {
	raw, ok := f.Read(manifest.File)
	if !ok {
		return &ManifestSummary{Present: false}
	}
	// Use the authoritative core validator so runtime-inspect agrees with the
	// validate/manifest endpoints (notably: version is required).
	res := manifest.Validate(raw)
	sum := &ManifestSummary{Present: true, Authoritative: true, Valid: res.Valid, Errors: res.Errors}
	if res.Parsed != nil && res.Parsed.Web != nil { // as-declared (non-authoritative when invalid)
		sum.WebCommand, sum.WebPort, sum.HealthPath = res.Parsed.Web.Command, res.Parsed.Web.Port, res.Parsed.Web.HealthPath
	}
	return sum
}

var localhostHints = regexp.MustCompile(`localhost|127\.0\.0\.1`)
var nodeEntryRe = regexp.MustCompile(`\bnode\s+([\w./-]+\.js)\b`)
var bashEntryRe = regexp.MustCompile(`\bbash\s+([\w./-]+\.sh)\b`)

func manifestWarnings(f Files, sum *ManifestSummary) []string {
	var out []string
	if sum.WebCommand == "" && sum.WebPort == 0 {
		return out // worker-only or empty web; nothing to warn
	}
	if sum.WebPort == 0 {
		out = append(out, "sandbox.yaml web.port is not set")
	}
	cmd := sum.WebCommand
	// Only flag an EXPLICIT localhost/127.0.0.1 — don't guess "dev server without
	// --host" (false-positives on all-interface binders like node/Nest/Bun).
	if cmd != "" && localhostHints.MatchString(cmd) {
		out = append(out, "web.command may bind localhost only; the preview needs 0.0.0.0 (e.g. --host 0.0.0.0)")
	}
	if m := nodeEntryRe.FindStringSubmatch(cmd); m != nil && !f.Exists(m[1]) {
		out = append(out, "web.command references "+m[1]+" which is not in the workspace")
	}
	if m := bashEntryRe.FindStringSubmatch(cmd); m != nil && !f.Exists(m[1]) {
		out = append(out, "web.command references "+m[1]+" which is not in the workspace")
	}
	return out
}

// rank orders suggestions (high>medium>low) and sets a default only when there
// is exactly ONE high-confidence RUNNABLE suggestion (unambiguous).
func rank(res *Result) {
	order := map[string]int{"high": 0, "medium": 1, "low": 2}
	ss := res.Suggestions
	for i := 1; i < len(ss); i++ {
		for j := i; j > 0 && order[ss[j].Confidence] < order[ss[j-1].Confidence]; j-- {
			ss[j], ss[j-1] = ss[j-1], ss[j]
		}
	}
	var highRunnable []string
	for _, s := range ss {
		if s.Confidence == "high" && s.Runnable {
			highRunnable = append(highRunnable, s.Preset)
		}
	}
	if len(highRunnable) == 1 {
		res.DefaultSuggestion = highRunnable[0]
	}
	for i, s := range ss {
		if i == 0 {
			continue
		}
		res.Alternatives = append(res.Alternatives, s.Preset)
	}
	// Only nudge to configure a runtime when there's neither a detected stack nor
	// an authoritative sandbox.yaml.
	if len(ss) == 0 && (res.ExistingManifest == nil || !res.ExistingManifest.Present) {
		res.Warnings = append(res.Warnings,
			"could not confidently detect a runtime — pick a preset or add a sandbox.yaml")
	}
}
