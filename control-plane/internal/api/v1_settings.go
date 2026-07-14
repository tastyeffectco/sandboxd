// v1_settings.go — Phase 8A: a READ-ONLY instance/settings summary for the
// console's operability ("Settings") view. It returns only safe, static
// metadata about how this sandboxd is configured. It MUST NEVER include
// secrets, tokens, encryption keys, or raw env values — only booleans, modes,
// names, and counts that are safe to show any operator.
package api

import (
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/tastyeffectco/sandboxd/control-plane/internal/agentauth"
	"github.com/tastyeffectco/sandboxd/control-plane/internal/agentprompt"
	"github.com/tastyeffectco/sandboxd/control-plane/internal/audit"
	"github.com/tastyeffectco/sandboxd/control-plane/internal/preset"
	"github.com/tastyeffectco/sandboxd/control-plane/internal/store"
)

// InstanceInfo is the static, safe instance metadata, populated once in main
// from config. Keep it free of secrets — anything here is world-readable.
type InstanceInfo struct {
	Version              string
	GitCommit            string
	AuthEnabled          bool
	StorageMode          string   // "directory" (OSS) | "loopback"
	EgressMode           string   // "disabled" | "enabled"
	AgentProviders       []string // e.g. ["opencode"]
	IdleReapEnabled      bool
	IdleThresholdSeconds int
}

type v1Settings struct {
	Version   string `json:"version"`
	GitCommit string `json:"git_commit,omitempty"`
	// Update info from the release checker (best-effort). update_available is
	// always present; latest_version/changelog_url are omitted until a release
	// has been fetched.
	UpdateAvailable bool              `json:"update_available"`
	LatestVersion   string            `json:"latest_version,omitempty"`
	ChangelogURL    string            `json:"changelog_url,omitempty"`
	Networking      v1SettingsNet     `json:"networking"`
	Auth            v1SettingsAuth    `json:"auth"`
	Runtime         v1SettingsRuntime `json:"runtime"`
	Lifecycle       v1SettingsLife    `json:"lifecycle"`
	Egress          v1SettingsEgress  `json:"egress"`
	Agents          v1SettingsAgents  `json:"agents"`
	Presets         []v1Preset        `json:"presets"`
	Capabilities    map[string]bool   `json:"capabilities"`
	// Editable lists the field paths a client may change via PATCH /v1/settings.
	// Everything else is read-only (env/file-managed or restart-required).
	Editable []string `json:"editable"`
}

type v1SettingsNet struct {
	PreviewDomain     string `json:"preview_domain"`
	PublicHTTPPort    string `json:"public_http_port,omitempty"`
	PreviewBase       string `json:"preview_base"`
	PreviewTLS        bool   `json:"preview_tls"`
	PreviewEntrypoint string `json:"preview_entrypoint,omitempty"`
}
type v1SettingsAuth struct {
	Enabled bool `json:"enabled"`
}
type v1SettingsRuntime struct {
	StorageMode string `json:"storage_mode"`
	BaseImage   string `json:"base_image"`
}
type v1SettingsLife struct {
	IdleReapEnabled      bool `json:"idle_reap_enabled"`
	IdleThresholdSeconds int  `json:"idle_threshold_seconds"`
	KeepaliveMaxSeconds  int  `json:"keepalive_max_seconds"`
}
type v1SettingsEgress struct {
	Mode string `json:"mode"`
}
type v1SettingsAgents struct {
	Providers []string `json:"providers"`
	// SystemPrompt is the platform briefing appended to every agent run (read
	// only). Rendered with default port/health for display; runtimed renders it
	// with each sandbox's real values at task time. Single source: internal/agentprompt.
	SystemPrompt string `json:"system_prompt,omitempty"`
	// DefaultModels maps an agent id to its operator-set default model id, used
	// when a task doesn't specify one. Editable via PATCH. Empty map when unset.
	DefaultModels map[string]string `json:"default_models"`
}

// v1GetSettings — GET /v1/settings. Read-only, tokenless-safe instance summary.
func (s *Server) v1GetSettings(w http.ResponseWriter, _ *http.Request) {
	ps := preset.List()
	presets := make([]v1Preset, 0, len(ps))
	for _, p := range ps {
		presets = append(presets, v1Preset{
			ID: p.ID, Label: p.Label, Description: p.Description,
			Template: p.Template, Capabilities: p.Capabilities,
		})
	}
	egressMode := s.Instance.EgressMode
	if egressMode == "" {
		egressMode = "disabled"
	}
	storageMode := s.Instance.StorageMode
	if storageMode == "" {
		storageMode = "directory"
	}
	out := v1Settings{
		Version:   s.Instance.Version,
		GitCommit: s.Instance.GitCommit,
		Networking: v1SettingsNet{
			PreviewDomain:     s.PreviewDomain,
			PublicHTTPPort:    s.PublicHTTPPort,
			PreviewBase:       s.previewBase(),
			PreviewTLS:        s.PreviewTLS,
			PreviewEntrypoint: s.PreviewEntrypoint,
		},
		Auth:      v1SettingsAuth{Enabled: s.Instance.AuthEnabled},
		Runtime:   v1SettingsRuntime{StorageMode: storageMode, BaseImage: s.Image},
		Lifecycle: s.lifecycleView(),
		Egress:    v1SettingsEgress{Mode: egressMode},
		Agents:    v1SettingsAgents{Providers: s.Instance.AgentProviders, SystemPrompt: agentprompt.Render(agentprompt.Vars{}), DefaultModels: s.agentDefaultModels()},
		Presets:   presets,
		Capabilities: map[string]bool{
			"snapshots":      s.Snapshot != nil,
			"config_secrets": s.Secrets != nil,
			"templates":      s.TemplatesDir != "",
			"forward_auth":   s.Auth != nil,
		},
	}
	if s.Live != nil {
		out.Editable = []string{
			"lifecycle.idle_reap_enabled",
			"lifecycle.idle_threshold_seconds",
			"lifecycle.keepalive_max_seconds",
			"agents.default_models",
		}
	}
	if s.Update != nil {
		out.UpdateAvailable, out.LatestVersion, out.ChangelogURL = s.Update.UpdateAvailable(s.Instance.Version)
	}
	writeJSON(w, http.StatusOK, out)
}

// lifecycleView reads the live (runtime-editable) lifecycle settings when wired,
// else the static startup values.
func (s *Server) lifecycleView() v1SettingsLife {
	if s.Live != nil {
		sn := s.Live.Snapshot()
		return v1SettingsLife{
			IdleReapEnabled:      sn.IdleEnabled,
			IdleThresholdSeconds: sn.IdleThresholdSeconds,
			KeepaliveMaxSeconds:  sn.KeepaliveMaxSeconds,
		}
	}
	return v1SettingsLife{
		IdleReapEnabled:      s.Instance.IdleReapEnabled,
		IdleThresholdSeconds: s.Instance.IdleThresholdSeconds,
		KeepaliveMaxSeconds:  int(s.KeepaliveMax.Seconds()),
	}
}

// keepaliveMax returns the live max keepalive window (Phase 8B), falling back to
// the static value when no live config is wired (e.g. in tests).
func (s *Server) keepaliveMax() time.Duration {
	if s.Live != nil {
		return s.Live.KeepaliveMax()
	}
	return s.KeepaliveMax
}

// Editable-tunable bounds. Deliberately conservative.
const (
	minIdleThresholdSec = 60
	maxIdleThresholdSec = 86400     // 1 day
	maxKeepaliveSec     = 7 * 86400 // 7 days (0 = keepalive disabled)
)

// v1SettingsPatch is the STRICT allowlist of editable fields. Decoding with
// DisallowUnknownFields means any other key — including protected ones (auth,
// egress, networking, secrets, version) — is rejected with 400.
type v1SettingsPatch struct {
	Lifecycle *struct {
		IdleReapEnabled      *bool `json:"idle_reap_enabled"`
		IdleThresholdSeconds *int  `json:"idle_threshold_seconds"`
		KeepaliveMaxSeconds  *int  `json:"keepalive_max_seconds"`
	} `json:"lifecycle"`
	Agents *struct {
		// DefaultModels merges into the stored map: a non-empty value sets that
		// agent's default model, an empty value clears it. Other agents untouched.
		DefaultModels map[string]string `json:"default_models"`
	} `json:"agents"`
}

// maxAgentDefaultModels bounds how many per-agent defaults can be stored.
const maxAgentDefaultModels = 32

// v1PatchSettings — PATCH /v1/settings. Edits ONLY the lifecycle tunables; it
// persists them, hot-applies via the shared Live config, and audits the change.
// It never touches secrets/auth/egress/networking (those reject as unknown).
func (s *Server) v1PatchSettings(w http.ResponseWriter, r *http.Request) {
	if s.Live == nil || s.Store == nil {
		writeV1Err(w, http.StatusServiceUnavailable, "unavailable", "settings are not editable on this instance")
		return
	}
	dec := json.NewDecoder(r.Body)
	dec.DisallowUnknownFields()
	var req v1SettingsPatch
	if err := dec.Decode(&req); err != nil {
		writeV1Err(w, http.StatusBadRequest, "invalid_request",
			"editable: lifecycle tunables (idle_reap_enabled, idle_threshold_seconds, keepalive_max_seconds) and agents.default_models: "+err.Error())
		return
	}
	if req.Lifecycle == nil && req.Agents == nil {
		writeV1Err(w, http.StatusBadRequest, "invalid_request", "no editable fields provided")
		return
	}

	next := s.Live.Snapshot()
	target := "settings"
	if req.Lifecycle != nil {
		target = "lifecycle"
		if v := req.Lifecycle.IdleReapEnabled; v != nil {
			next.IdleEnabled = *v
		}
		if v := req.Lifecycle.IdleThresholdSeconds; v != nil {
			next.IdleThresholdSeconds = *v
		}
		if v := req.Lifecycle.KeepaliveMaxSeconds; v != nil {
			next.KeepaliveMaxSeconds = *v
		}
		if next.IdleThresholdSeconds < minIdleThresholdSec || next.IdleThresholdSeconds > maxIdleThresholdSec {
			writeV1Err(w, http.StatusBadRequest, "invalid_request",
				fmt.Sprintf("idle_threshold_seconds must be %d..%d", minIdleThresholdSec, maxIdleThresholdSec))
			return
		}
		if next.KeepaliveMaxSeconds < 0 || next.KeepaliveMaxSeconds > maxKeepaliveSec {
			writeV1Err(w, http.StatusBadRequest, "invalid_request",
				fmt.Sprintf("keepalive_max_seconds must be 0..%d", maxKeepaliveSec))
			return
		}
	}
	if req.Agents != nil && req.Agents.DefaultModels != nil {
		target = "agents.default_models"
		merged := map[string]string{}
		for k, v := range next.DefaultModels {
			merged[k] = v
		}
		for agent, model := range req.Agents.DefaultModels {
			if !agentauth.Runnable(agent) {
				writeV1Err(w, http.StatusBadRequest, "invalid_request",
					"unknown agent in default_models: "+agent)
				return
			}
			if len(model) > 200 {
				writeV1Err(w, http.StatusBadRequest, "invalid_request", "model too long for agent "+agent)
				return
			}
			if strings.TrimSpace(model) == "" {
				delete(merged, agent) // empty clears the default
			} else {
				merged[agent] = strings.TrimSpace(model)
			}
		}
		if len(merged) > maxAgentDefaultModels {
			writeV1Err(w, http.StatusBadRequest, "invalid_request",
				fmt.Sprintf("too many agent default models (max %d)", maxAgentDefaultModels))
			return
		}
		next.DefaultModels = merged
	}

	if err := s.Store.SaveInstanceSettings(r.Context(), store.InstanceSettings{
		IdleReapEnabled:      next.IdleEnabled,
		IdleThresholdSeconds: next.IdleThresholdSeconds,
		KeepaliveMaxSeconds:  next.KeepaliveMaxSeconds,
		AgentDefaultModels:   next.DefaultModels,
	}); err != nil {
		writeV1Err(w, http.StatusInternalServerError, "internal", err.Error())
		return
	}
	s.Live.Set(next) // hot-apply (reaper + keepalive + task-submit read this live)
	s.auditAction(r, audit.Entry{Action: "settings.update", Target: target})
	s.v1GetSettings(w, r) // echo the updated settings
}

// agentDefaultModels returns the live per-agent default model map (never nil).
func (s *Server) agentDefaultModels() map[string]string {
	if s.Live != nil {
		if m := s.Live.Snapshot().DefaultModels; m != nil {
			return m
		}
	}
	return map[string]string{}
}

// previewBase is the scheme://host[:port] previews are reached under, with the
// host-facing port appended unless it's the scheme default (mirrors previewURL).
func (s *Server) previewBase() string {
	scheme, defaultPort := "http", "80"
	if s.PreviewTLS {
		scheme, defaultPort = "https", "443"
	}
	host := "*.preview." + s.PreviewDomain
	if p := s.PublicHTTPPort; p != "" && p != defaultPort {
		host += ":" + p
	}
	return scheme + "://" + host
}
