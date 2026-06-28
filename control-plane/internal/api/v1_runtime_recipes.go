// v1_runtime_recipes.go — read-only listing of the embedded advisory recipe
// registry (for the console + docs). Recipes are data; nothing is applied.
package api

import (
	"net/http"

	"github.com/sandboxd/control-plane/internal/recipes"
)

// GET /v1/runtime/recipes
func (s *Server) v1RuntimeRecipes(w http.ResponseWriter, r *http.Request) {
	all, err := recipes.All()
	if err != nil {
		writeV1Err(w, http.StatusInternalServerError, "internal", "recipe registry unavailable")
		return
	}
	if all == nil {
		all = []recipes.Recipe{}
	}
	writeJSON(w, http.StatusOK, map[string]any{"recipes": all})
}
