// v1_agents.go — Phase 10B A0: read-only AI Agents status. Lists the static
// provider registry with a best-effort "installed" probe and an opaque
// "connected" check (auth dir non-empty). No tokens are ever read or returned,
// no Connect flow, no task behavior change.
package api

import (
	"context"
	"net/http"
	"os/exec"
	"strings"
	"time"

	"github.com/sandboxd/control-plane/internal/agentauth"
)

type v1Agent struct {
	ID             string `json:"id"`
	Label          string `json:"label"`
	InstalledState string `json:"installed_state"` // installed | not_installed | unknown
	Status         string `json:"status"`          // connected | needs_login
}

// v1ListAgents — GET /v1/agents.
func (s *Server) v1ListAgents(w http.ResponseWriter, _ *http.Request) {
	installed := s.installedAgents()
	out := make([]v1Agent, 0, len(agentauth.Providers()))
	for _, p := range agentauth.Providers() {
		state := "unknown"
		if installed != nil {
			if v, ok := installed[p.Binary]; ok {
				state = v
			}
		}
		status := "needs_login"
		if s.AgentAuth != nil && s.AgentAuth.Connected(p.ID) {
			status = "connected"
		}
		out = append(out, v1Agent{ID: p.ID, Label: p.Label, InstalledState: state, Status: status})
	}
	writeJSON(w, http.StatusOK, map[string]any{"providers": out})
}

// installedAgents lazily probes the base image ONCE for which agent binaries
// exist, caching the result. Best-effort and non-blocking: on any error it
// caches nil and the handler reports installed_state "unknown" — it never
// blocks startup or fails the request. Overridable in tests via agentProbeFn.
func (s *Server) installedAgents() map[string]string {
	s.agentProbeOnce.Do(func() {
		probe := probeInstalledAgents
		if s.agentProbeFn != nil {
			probe = s.agentProbeFn
		}
		s.agentProbe = probe(s.Image)
	})
	return s.agentProbe
}

// probeInstalledAgents runs one throwaway container that reports, per provider
// binary, whether it's on PATH. Returns nil on any failure (=> "unknown").
func probeInstalledAgents(image string) map[string]string {
	if image == "" {
		return nil
	}
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	const script = `for b in opencode claude codex; do if command -v "$b" >/dev/null 2>&1; then echo "$b=1"; else echo "$b=0"; fi; done`
	out, err := exec.CommandContext(ctx, "docker", "run", "--rm", "--entrypoint", "sh", image, "-lc", script).Output()
	if err != nil {
		return nil
	}
	res := map[string]string{}
	for _, line := range strings.Split(strings.TrimSpace(string(out)), "\n") {
		bin, val, ok := strings.Cut(strings.TrimSpace(line), "=")
		if !ok {
			continue
		}
		if val == "1" {
			res[bin] = "installed"
		} else {
			res[bin] = "not_installed"
		}
	}
	return res
}
