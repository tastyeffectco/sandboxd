package recipes

import (
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
		res := manifest.Validate([]byte(r.SuggestedManifest))
		if !res.Valid {
			t.Errorf("recipe %q manifest invalid: %v", r.ID, res.Errors)
			continue
		}
		// The real contract is web.port == 3000 (the field) — the command may
		// declare the port many ways (:3000, --port=3000, PORT=3000, or in code).
		if res.Effective == nil || res.Effective.Web == nil || res.Effective.Web.Port != 3000 {
			t.Errorf("recipe %q must declare web.port 3000; got %+v", r.ID, res.Effective)
		}
		// A node recipe with a single framework binary should guard on it (the
		// fragile `[ -d node_modules ]` reinstalls forever after an interrupted
		// install). Stacks with no single bin (bun/hono) may use the dir guard.
	}
}

func TestMatch(t *testing.T) {
	cases := []struct {
		name string
		deps []string
		file string // single config file present ("" = none)
		req  string // requirements/pyproject text ("" = none)
		want string // recipe id expected to match
	}{
		{"nextjs", []string{"next", "react"}, "", "", "nextjs"},
		{"react-vite", []string{"vite", "react"}, "", "", "react-vite"},
		{"vite-vue", []string{"vite", "vue"}, "", "", "vite-vue"},
		{"astro", []string{"astro"}, "", "", "astro"},
		{"docusaurus", []string{"@docusaurus/core"}, "", "", "docusaurus"},
		{"gatsby", []string{"gatsby"}, "", "", "gatsby"},
		{"nuxt", []string{"nuxt"}, "", "", "nuxt"},
		{"sveltekit", []string{"@sveltejs/kit"}, "", "", "sveltekit"},
		{"remix", []string{"@remix-run/dev"}, "", "", "remix-vite"},
		{"eleventy by config", nil, ".eleventy.js", "", "eleventy"},
		// new extended stacks
		{"angular", []string{"@angular/core"}, "", "", "angular"},
		{"nestjs", []string{"@nestjs/core"}, "", "", "nestjs"},
		{"solidstart", []string{"@solidjs/start"}, "", "", "solidstart"},
		{"qwik", []string{"@builder.io/qwik-city"}, "", "", "qwik"},
		{"hono node", []string{"hono", "@hono/node-server"}, "", "", "hono"},
		{"directus", []string{"directus"}, "", "", "directus"},
		{"storybook by config", nil, ".storybook/main.ts", "", "storybook"},
		{"bun by lockfile", nil, "bun.lockb", "", "bun"},
		{"django by manage.py", nil, "manage.py", "", "django"},
		// Python recipes via requirements_contains
		{"flask", nil, "", "Flask==3.0\ngunicorn", "flask"},
		{"streamlit", nil, "", "streamlit==1.38", "streamlit"},
		{"gradio", nil, "", "gradio>=4", "gradio"},
		{"jupyter", nil, "", "jupyterlab", "jupyter"},
		{"dash", nil, "", "dash\nplotly", "dash"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			deps := map[string]bool{}
			for _, d := range c.deps {
				deps[d] = true
			}
			exists := func(p string) bool { return c.file != "" && p == c.file }
			matched, err := Match(deps, exists, c.req)
			if err != nil {
				t.Fatal(err)
			}
			found := false
			for _, m := range matched {
				if m.Recipe.ID == c.want {
					found = true
				}
			}
			if !found {
				var ids []string
				for _, m := range matched {
					ids = append(ids, m.Recipe.ID)
				}
				t.Errorf("expected %q in matches, got %v", c.want, ids)
			}
		})
	}
}

// react-vite must NOT match a Next.js / Remix / Astro app (exclude_deps).
func TestMatchExcludes(t *testing.T) {
	deps := map[string]bool{"vite": true, "react": true, "next": true}
	matched, _ := Match(deps, func(string) bool { return false }, "")
	for _, m := range matched {
		if m.Recipe.ID == "react-vite" {
			t.Error("react-vite must be excluded when next is present")
		}
	}
}
