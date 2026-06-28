package recipes

import (
	"strings"
	"testing"

	"github.com/sandboxd/control-plane/internal/manifest"
)

func TestAllLoadAndValidate(t *testing.T) {
	all, err := All()
	if err != nil {
		t.Fatalf("registry failed to load/validate: %v", err)
	}
	if len(all) < 10 {
		t.Fatalf("expected >=10 recipes, got %d", len(all))
	}
	for _, r := range all {
		if r.ID == "" || r.DisplayName == "" || r.SuggestedManifest == "" {
			t.Errorf("recipe %q missing required fields", r.ID)
		}
		// every suggested_manifest must validate (the contribution gate)
		if res := manifest.Validate([]byte(r.SuggestedManifest)); !res.Valid {
			t.Errorf("recipe %q manifest invalid: %v", r.ID, res.Errors)
		}
		// QA gotcha: install guard must key on the framework binary, not the dir
		if strings.Contains(r.SuggestedManifest, "[ -d node_modules ]") {
			t.Errorf("recipe %q uses the fragile `[ -d node_modules ]` guard", r.ID)
		}
		// QA gotcha: pin port 3000 + bind 0.0.0.0
		if !strings.Contains(r.SuggestedManifest, "--port 3000") &&
			!strings.Contains(r.SuggestedManifest, "-p 3000") {
			t.Errorf("recipe %q does not pin port 3000", r.ID)
		}
	}
}

func TestMatch(t *testing.T) {
	none := func(string) bool { return false }
	cases := []struct {
		name  string
		deps  []string
		files []string
		want  string // recipe id expected to match
	}{
		{"nextjs", []string{"next", "react"}, nil, "nextjs"},
		{"react-vite", []string{"vite", "react"}, nil, "react-vite"},
		{"vite-vue", []string{"vite", "vue"}, nil, "vite-vue"},
		{"astro", []string{"astro"}, nil, "astro"},
		{"docusaurus", []string{"@docusaurus/core"}, nil, "docusaurus"},
		{"gatsby", []string{"gatsby"}, nil, "gatsby"},
		{"nuxt", []string{"nuxt"}, nil, "nuxt"},
		{"sveltekit", []string{"@sveltejs/kit"}, nil, "sveltekit"},
		{"remix", []string{"@remix-run/dev", "react", "vite"}, nil, "remix-vite"},
		{"eleventy by config", nil, []string{".eleventy.js"}, "eleventy"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			deps := map[string]bool{}
			for _, d := range c.deps {
				deps[d] = true
			}
			exists := none
			if len(c.files) > 0 {
				fs := map[string]bool{}
				for _, f := range c.files {
					fs[f] = true
				}
				exists = func(p string) bool { return fs[p] }
			}
			matched, err := Match(deps, exists)
			if err != nil {
				t.Fatal(err)
			}
			var ids []string
			for _, m := range matched {
				ids = append(ids, m.Recipe.ID)
			}
			found := false
			for _, id := range ids {
				if id == c.want {
					found = true
				}
			}
			if !found {
				t.Errorf("expected %q in matches, got %v", c.want, ids)
			}
		})
	}
}

// react-vite must NOT match a Next.js / Remix / Astro app (exclude_deps).
func TestMatchExcludes(t *testing.T) {
	deps := map[string]bool{"vite": true, "react": true, "next": true}
	matched, _ := Match(deps, func(string) bool { return false })
	for _, m := range matched {
		if m.Recipe.ID == "react-vite" {
			t.Error("react-vite must be excluded when next is present")
		}
	}
}
