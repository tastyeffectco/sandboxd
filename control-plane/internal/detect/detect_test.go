package detect

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func osWrite(dir, rel, content string) error {
	return os.WriteFile(filepath.Join(dir, rel), []byte(content), 0o644)
}

// mapFiles is an in-memory Files for fixture tests.
type mapFiles map[string]string

func (m mapFiles) Read(rel string) ([]byte, bool) { v, ok := m[rel]; return []byte(v), ok }
func (m mapFiles) Exists(rel string) bool         { _, ok := m[rel]; return ok }

func pkg(deps string) string { return `{"dependencies":{` + deps + `}}` }

func topSuggestion(r Result) string {
	if len(r.Suggestions) == 0 {
		return ""
	}
	return r.Suggestions[0].Preset
}

func TestDetectFrameworks(t *testing.T) {
	cases := []struct {
		name        string
		files       mapFiles
		wantTop     string
		wantDefault string // "" = no default expected
		runnable    bool
	}{
		{"nextjs", mapFiles{"package.json": pkg(`"next":"14"`), "next.config.mjs": "x"}, "nextjs", "nextjs", true},
		{"react-vite", mapFiles{"package.json": pkg(`"vite":"5","react":"18","react-dom":"18"`)}, "react-vite", "react-vite", true},
		{"node-express", mapFiles{"package.json": pkg(`"express":"4"`), "server.js": "x"}, "node-express", "node-express", true},
		{"fastapi", mapFiles{"requirements.txt": "fastapi==0.110\nuvicorn"}, "fastapi", "fastapi", true},
		{"worker", mapFiles{"worker.sh": "while true; do :; done"}, "worker", "worker", true},
		// astro is DETECT-ONLY: suggested, but not runnable and not a default.
		{"astro detect-only", mapFiles{"package.json": pkg(`"astro":"4"`), "astro.config.mjs": "x"}, "astro", "", false},
		{"docusaurus detect-only", mapFiles{"package.json": pkg(`"@docusaurus/core":"3"`)}, "docusaurus", "", false},
	}
	for _, c := range cases {
		r := Inspect(c.files)
		if topSuggestion(r) != c.wantTop {
			t.Errorf("%s: top = %q; want %q (%+v)", c.name, topSuggestion(r), c.wantTop, r.Suggestions)
		}
		if r.DefaultSuggestion != c.wantDefault {
			t.Errorf("%s: default = %q; want %q", c.name, r.DefaultSuggestion, c.wantDefault)
		}
		if len(r.Suggestions) > 0 && r.Suggestions[0].Runnable != c.runnable {
			t.Errorf("%s: runnable = %v; want %v", c.name, r.Suggestions[0].Runnable, c.runnable)
		}
		if len(r.Suggestions) > 0 && len(r.Suggestions[0].Reasons) == 0 {
			t.Errorf("%s: suggestion has no reasons", c.name)
		}
	}
}

func TestAstroSuggestedManifest(t *testing.T) {
	r := Inspect(mapFiles{"package.json": pkg(`"astro":"4"`), "astro.config.mjs": "x"})
	var astro *Suggestion
	for i := range r.Suggestions {
		if r.Suggestions[i].Preset == "astro" {
			astro = &r.Suggestions[i]
		}
	}
	if astro == nil {
		t.Fatal("no astro suggestion")
	}
	if astro.Runnable {
		t.Error("astro must be detect-only (not runnable)")
	}
	for _, want := range []string{"version: 1", "astro dev", "--host 0.0.0.0", "--port 3000", "health_path"} {
		if !strings.Contains(astro.SuggestedManifest, want) {
			t.Errorf("astro suggested_manifest missing %q: %q", want, astro.SuggestedManifest)
		}
	}
	// allowedHosts guidance is a config-file snippet (not a CLI flag)
	var snip string
	for _, c := range astro.ConfigSnippets {
		snip += c.File + " " + c.Note + " "
	}
	if !strings.Contains(snip, "allowedHosts") || !strings.Contains(snip, "astro.config") {
		t.Errorf("astro should carry an allowedHosts config snippet: %v", astro.ConfigSnippets)
	}
	if !strings.Contains(strings.Join(astro.Notes, " "), "4321") {
		t.Errorf("astro should note the 4321 default: %v", astro.Notes)
	}
}

func TestAstroDetectOnlyNoDefault(t *testing.T) {
	r := Inspect(mapFiles{"package.json": pkg(`"astro":"4"`)})
	if r.DefaultSuggestion != "" {
		t.Error("astro must not be a default (detect-only)")
	}
}

func TestAmbiguousRepoNoDefault(t *testing.T) {
	// Nothing recognizable -> no suggestions, no default, a guidance warning.
	r := Inspect(mapFiles{"README.md": "hello"})
	if len(r.Suggestions) != 0 || r.DefaultSuggestion != "" {
		t.Errorf("expected no suggestions/default; got %+v", r)
	}
	if len(r.Warnings) == 0 {
		t.Error("expected a guidance warning for an undetectable repo")
	}
}

func TestExistingManifestSummaryAndWarnings(t *testing.T) {
	// Manifest present, port missing, command binds localhost only, missing entry.
	yaml := "version: 1\nweb:\n  command: \"node server.js --host localhost\"\n"
	r := Inspect(mapFiles{"sandbox.yaml": yaml})
	em := r.ExistingManifest
	if em == nil || !em.Present || !em.Authoritative {
		t.Fatalf("existing manifest not summarized: %+v", em)
	}
	if em.WebCommand == "" {
		t.Error("web_command not summarized")
	}
	joined := strings.Join(r.Warnings, " | ")
	for _, want := range []string{"web.port is not set", "localhost", "server.js"} {
		if !strings.Contains(joined, want) {
			t.Errorf("missing warning %q in %q", want, joined)
		}
	}
}

func TestExistingManifestHealthyNoFalseWarnings(t *testing.T) {
	yaml := "version: 1\nweb:\n  command: \"pnpm dev --host 0.0.0.0\"\n  port: 3000\n  health_path: /\n"
	r := Inspect(mapFiles{"sandbox.yaml": yaml})
	if len(r.Warnings) != 0 {
		t.Errorf("healthy manifest should have no warnings; got %v", r.Warnings)
	}
	if r.ExistingManifest.WebPort != 3000 || r.ExistingManifest.HealthPath != "/" {
		t.Errorf("summary wrong: %+v", r.ExistingManifest)
	}
}

// OSFiles reads real workspace files (the control-plane host-side path).
func TestOSFilesDetectsRealDir(t *testing.T) {
	dir := t.TempDir()
	if err := osWrite(dir, "package.json", pkg(`"next":"14"`)); err != nil {
		t.Fatal(err)
	}
	r := Inspect(OSFiles(dir))
	if r.DefaultSuggestion != "nextjs" {
		t.Errorf("OSFiles detect default = %q; want nextjs", r.DefaultSuggestion)
	}
}

func TestSafeJoinRejectsTraversal(t *testing.T) {
	if _, ok := safeJoin("/data/ws", "../../etc/passwd"); ok {
		t.Error("safeJoin must reject path traversal")
	}
	if _, ok := safeJoin("/data/ws", "package.json"); !ok {
		t.Error("safeJoin should allow a normal relative file")
	}
}

// runtime-inspect surfaces recipe suggestions for frameworks with no built-in
// preset (Gatsby/Vue/Nuxt/SvelteKit/Eleventy), each advisory with a manifest.
func TestInspectRecipeFrameworks(t *testing.T) {
	cases := []struct {
		id    string
		files mapFiles
	}{
		{"gatsby", mapFiles{"package.json": pkg(`"gatsby":"5"`)}},
		{"vite-vue", mapFiles{"package.json": pkg(`"vite":"5","vue":"3"`)}},
		{"nuxt", mapFiles{"package.json": pkg(`"nuxt":"3"`)}},
		{"sveltekit", mapFiles{"package.json": pkg(`"@sveltejs/kit":"2"`)}},
		{"eleventy", mapFiles{".eleventy.js": "x"}},
	}
	for _, c := range cases {
		r := Inspect(c.files)
		var found *Suggestion
		for i := range r.Suggestions {
			if r.Suggestions[i].Preset == c.id {
				found = &r.Suggestions[i]
			}
		}
		if found == nil {
			t.Errorf("%s: no suggestion; got %+v", c.id, r.Suggestions)
			continue
		}
		if found.Runnable {
			t.Errorf("%s: should be advisory (not runnable)", c.id)
		}
		if !strings.Contains(found.SuggestedManifest, "port: 3000") {
			t.Errorf("%s: suggested_manifest should pin 3000: %q", c.id, found.SuggestedManifest)
		}
	}
}

// Config-snippet recipe (Django) surfaces via runtime-inspect: detected by
// manage.py, advisory, with an ALLOWED_HOSTS config snippet (not a manifest field).
func TestInspectDjangoConfigSnippet(t *testing.T) {
	r := Inspect(mapFiles{"manage.py": "x", "requirements.txt": "Django==5.1\n"})
	var dj *Suggestion
	for i := range r.Suggestions {
		if r.Suggestions[i].Preset == "django" {
			dj = &r.Suggestions[i]
		}
	}
	if dj == nil {
		t.Fatalf("no django suggestion; got %+v", r.Suggestions)
	}
	if dj.Runnable {
		t.Error("django is advisory (not a runnable preset)")
	}
	if !strings.Contains(dj.SuggestedManifest, "runserver 0.0.0.0:3000") {
		t.Errorf("django manifest wrong: %q", dj.SuggestedManifest)
	}
	var snip string
	for _, c := range dj.ConfigSnippets {
		snip += c.File + " " + c.Note + " "
	}
	if !strings.Contains(snip, "ALLOWED_HOSTS") || !strings.Contains(snip, "settings.py") {
		t.Errorf("django should carry an ALLOWED_HOSTS settings.py snippet: %v", dj.ConfigSnippets)
	}
}

// runtime-inspect agrees with the core validator: a present-but-versionless
// manifest is authoritative (it exists) yet valid:false with the version error —
// consistent with POST /v1/runtime/manifest/validate.
func TestExistingManifestValidityConsistent(t *testing.T) {
	bad := Inspect(mapFiles{"sandbox.yaml": "web:\n  command: \"node server.js\"\n  port: 3000\n"}).ExistingManifest
	if bad == nil || !bad.Present || !bad.Authoritative {
		t.Fatalf("a present manifest stays present+authoritative: %+v", bad)
	}
	if bad.Valid {
		t.Errorf("versionless manifest must be valid:false: %+v", bad)
	}
	found := false
	for _, e := range bad.Errors {
		if strings.Contains(e, "version") {
			found = true
		}
	}
	if !found {
		t.Errorf("expected a version error in existing_manifest.errors: %v", bad.Errors)
	}

	good := Inspect(mapFiles{"sandbox.yaml": "version: 1\nweb:\n  command: \"node server.js\"\n  port: 3000\n"}).ExistingManifest
	if !good.Valid || len(good.Errors) != 0 {
		t.Errorf("version:1 manifest must be valid with no errors: %+v", good)
	}
}

// The base image ships pnpm/npm/bun + corepack, but the built-in presets assume
// pnpm — so a repo declaring another manager fails on its first command. These
// cover the generic detection that makes such repos runnable.
func TestDetectPackageManagerPrefersThePin(t *testing.T) {
	f := mapFiles{"yarn.lock": "", "package.json": `{"packageManager":"yarn@1.22.22"}`}
	var p pkgJSON
	_ = json.Unmarshal([]byte(f["package.json"]), &p)
	pm, ok := DetectPackageManager(f, &p)
	if !ok || pm.Name != "yarn" {
		t.Fatalf("pm = %+v, ok = %v; want yarn", pm, ok)
	}
	if !strings.HasPrefix(pm.Install, "corepack ") {
		t.Errorf("a pinned manager must run through corepack, got %q", pm.Install)
	}
}

func TestDetectPackageManagerFallsBackToLockfiles(t *testing.T) {
	cases := []struct{ lock, want string }{
		{"yarn.lock", "yarn"},
		{"bun.lockb", "bun"},
		{"package-lock.json", "npm"},
		{"pnpm-lock.yaml", "pnpm"},
	}
	for _, tc := range cases {
		pm, ok := DetectPackageManager(mapFiles{tc.lock: ""}, nil)
		if !ok || pm.Name != tc.want {
			t.Errorf("%s -> %+v (ok=%v), want %s", tc.lock, pm, ok, tc.want)
		}
	}
	if pm, ok := DetectPackageManager(mapFiles{}, nil); ok || pm.Name != "pnpm" {
		t.Errorf("no signal must keep the pnpm default, got %+v ok=%v", pm, ok)
	}
}

func TestPackageManagerSuggestionOnlyForNonPnpm(t *testing.T) {
	// pnpm is what the presets already do — no advisory suggestion.
	if _, ok := packageManagerSuggestion(mapFiles{"pnpm-lock.yaml": ""}, nil); ok {
		t.Error("pnpm must not produce a package-manager suggestion")
	}
	// A yarn monorepo gets a ready-to-apply manifest that uses corepack yarn.
	raw := `{"packageManager":"yarn@1.22.22","scripts":{"start":"vite"}}`
	var p pkgJSON
	_ = json.Unmarshal([]byte(raw), &p)
	s, ok := packageManagerSuggestion(mapFiles{"package.json": raw, "yarn.lock": ""}, &p)
	if !ok {
		t.Fatal("a yarn repo must produce a suggestion")
	}
	for _, want := range []string{"corepack yarn install", "corepack yarn run start", "--host 0.0.0.0", "port: 3000"} {
		if !strings.Contains(s.SuggestedManifest, want) {
			t.Errorf("manifest missing %q:\n%s", want, s.SuggestedManifest)
		}
	}
	if s.Runnable {
		t.Error("the suggestion is advisory — no built-in preset runs yarn")
	}
}

func TestInspectSurfacesThePackageManagerForAYarnRepo(t *testing.T) {
	res := Inspect(mapFiles{
		"package.json": `{"packageManager":"yarn@1.22.22","scripts":{"dev":"vite"}}`,
		"yarn.lock":    "",
	})
	for _, s := range res.Suggestions {
		if strings.Contains(s.SuggestedManifest, "corepack yarn") {
			return
		}
	}
	t.Errorf("Inspect must surface a yarn manifest; got %d suggestions", len(res.Suggestions))
}
