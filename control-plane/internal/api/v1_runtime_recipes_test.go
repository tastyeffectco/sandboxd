package api

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/sandboxd/control-plane/internal/recipes"
)

func TestRuntimeRecipesEndpoint(t *testing.T) {
	s := &Server{Store: memStore(t)}
	r := reqAs("GET", "/v1/runtime/recipes", "", "t")
	w := httptest.NewRecorder()
	s.v1RuntimeRecipes(w, r)
	if w.Code != http.StatusOK {
		t.Fatalf("got %d", w.Code)
	}
	var resp struct {
		Recipes []recipes.Recipe `json:"recipes"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatal(err)
	}
	if len(resp.Recipes) < 10 {
		t.Fatalf("expected >=10 recipes, got %d", len(resp.Recipes))
	}
	byID := map[string]recipes.Recipe{}
	for _, rc := range resp.Recipes {
		byID[rc.ID] = rc
	}
	for _, want := range []string{"nextjs", "react-vite", "astro", "gatsby", "sveltekit", "eleventy", "n8n", "strapi", "payload", "ghost", "tanstack-start", "astro-node-server", "python-asgi", "nicegui", "sanic", "react-router", "fasthtml", "slidev"} {
		if _, ok := byID[want]; !ok {
			t.Errorf("registry missing %q", want)
		}
	}
	// shape: each carries display_name + a suggested_manifest
	if byID["astro"].SuggestedManifest == "" || byID["astro"].DisplayName == "" {
		t.Errorf("astro recipe shape wrong: %+v", byID["astro"])
	}
}
