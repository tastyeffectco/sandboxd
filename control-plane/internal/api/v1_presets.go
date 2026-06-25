// v1_presets.go — Phase 7C-1: list the available runtime presets so the
// console can populate its "New App / Create Sandbox" picker from the registry
// (the single source of truth) rather than hardcoding the list.
package api

import (
	"net/http"

	"github.com/sandboxd/control-plane/internal/preset"
)

type v1Preset struct {
	ID           string   `json:"id"`
	Label        string   `json:"label"`
	Description  string   `json:"description"`
	Template     string   `json:"template,omitempty"`
	Capabilities []string `json:"capabilities,omitempty"`
}

// v1ListPresets — GET /v1/presets. Public, tokenless-safe metadata (no secrets).
func (s *Server) v1ListPresets(w http.ResponseWriter, _ *http.Request) {
	ps := preset.List()
	out := make([]v1Preset, 0, len(ps))
	for _, p := range ps {
		out = append(out, v1Preset{
			ID: p.ID, Label: p.Label, Description: p.Description,
			Template: p.Template, Capabilities: p.Capabilities,
		})
	}
	writeJSON(w, http.StatusOK, map[string]any{"presets": out})
}
