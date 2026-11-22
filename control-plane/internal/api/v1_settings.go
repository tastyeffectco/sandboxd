// v1_settings.go — Phase 8A: a READ-ONLY instance/settings summary for the
// console's operability ("Settings") view. It returns only safe, static
// metadata about how this sandboxd is configured. It MUST NEVER include
// secrets, tokens, encryption keys, or raw env values — only booleans, modes,
// names, and counts that are safe to show any operator.
package api

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"time"
	_ "time/tzdata" // validates agents.time_zone on any base image

	"github.com/tastyeffectco/sandboxd/control-plane/internal/agentauth"
	"github.com/tastyeffectco/sandboxd/control-plane/internal/agentprompt"
	"github.com/tastyeffectco/sandboxd/control-plane/internal/audit"
	"github.com/tastyeffectco/sandboxd/control-plane/internal/instancecfg"
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
	// always present; the latest_* fields are omitted until a release has been
	// fetched. update_kind is "release" (an older tagged build) or "untagged"
	// (a build with no comparable version) when an update is reported.
	UpdateAvailable   bool              `json:"update_available"`
	UpdateKind        string            `json:"update_kind,omitempty"`
	LatestVersion     string            `json:"latest_version,omitempty"`
	ChangelogURL      string            `json:"changelog_url,omitempty"`
	LatestNotes       string            `json:"latest_notes,omitempty"`
	LatestBreaking    string            `json:"latest_breaking,omitempty"`
	LatestPublishedAt string            `json:"latest_published_at,omitempty"`
	Networking        v1SettingsNet     `json:"networking"`
	Auth              v1SettingsAuth    `json:"auth"`
	Runtime           v1SettingsRuntime `json:"runtime"`
	Lifecycle         v1SettingsLife    `json:"lifecycle"`
	Egress            v1SettingsEgress  `json:"egress"`
	Agents            v1SettingsAgents  `json:"agents"`
	Presets           []v1Preset        `json:"presets"`
	Capabilities      map[string]bool   `json:"capabilities"`
	// Editable lists the field paths a client may change via PATCH /v1/settings.
	// Everything else is read-only (env/file-managed or restart-required).
	Editable []string `json:"editable"`
}

type v1SettingsNet struct {
	PreviewDomain  string `json:"preview_domain"`
	PublicHTTPPort string `json:"public_http_port,omitempty"`
	// PreviewBase is scheme://<pattern>[:port] covering every preview host.
	// Nested: a real DNS wildcard (*.preview.<domain>). Flat: previews share
	// their parent with the console and API, so the pattern is written as
	// s-*-*[--tag].<domain>, mirroring the preview host shape.
	PreviewBase       string `json:"preview_base"`
	PreviewHostStyle  string `json:"preview_host_style"`
	PreviewHostTag    string `json:"preview_host_tag,omitempty"`
	PreviewTLS        bool   `json:"preview_tls"`
	PreviewURLScheme  string `json:"preview_url_scheme"`
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
	// TimeZone is the IANA time zone agents run in ("" is UTC). Editable.
	TimeZone string `json:"time_zone"`
	// DefaultAI is the AI new chats start with (Settings → AI). Agent ""
	// means the built-in free AI. Editable.
	DefaultAI instancecfg.DefaultAI `json:"default_ai"`
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
			PreviewHostStyle:  s.hosts().Style,
			PreviewHostTag:    s.hosts().Tag,
			PublicHTTPPort:    s.PublicHTTPPort,
			PreviewBase:       s.previewBase(),
			PreviewTLS:        s.PreviewTLS,
			PreviewURLScheme:  func() string { sc, _ := s.previewScheme(); return sc }(),
			PreviewEntrypoint: s.PreviewEntrypoint,
		},
		Auth:      v1SettingsAuth{Enabled: s.Instance.AuthEnabled},
		Runtime:   v1SettingsRuntime{StorageMode: storageMode, BaseImage: s.Image},
		Lifecycle: s.lifecycleView(),
		Egress:    v1SettingsEgress{Mode: egressMode},
		Agents:    v1SettingsAgents{Providers: s.Instance.AgentProviders, SystemPrompt: agentprompt.Render(agentprompt.Vars{}), DefaultModels: s.agentDefaultModels(), TimeZone: s.agentTimeZone(), DefaultAI: s.agentDefaultAI()},
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
			"agents.time_zone",
			"agents.default_ai",
		}
	}
	if s.Update != nil {
		u := s.Update.Status(s.Instance.Version)
		out.UpdateAvailable, out.UpdateKind = u.Available, u.Kind
		out.LatestVersion, out.ChangelogURL = u.Latest, u.ChangelogURL
		out.LatestNotes, out.LatestBreaking, out.LatestPublishedAt = u.Notes, u.Breaking, u.PublishedAt
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
		// TimeZone is an IANA name ("Europe/Paris"); "" means UTC. Agents
		// are made again to pick it up.
		TimeZone *string `json:"time_zone"`
		// DefaultAI replaces the AI new chats start with. An empty agent
		// resets it to the built-in free AI.
		DefaultAI *instancecfg.DefaultAI `json:"default_ai"`
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
			"editable: lifecycle tunables (idle_reap_enabled, idle_threshold_seconds, keepalive_max_seconds) and agents.default_models / agents.time_zone / agents.default_ai: "+err.Error())
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

	zoneChanged := false
	if req.Agents != nil && req.Agents.TimeZone != nil {
		target = "agents.time_zone"
		zone := strings.TrimSpace(*req.Agents.TimeZone)
		if zone != "" {
			if _, err := time.LoadLocation(zone); err != nil || !validZone(zone) {
				writeV1Err(w, http.StatusBadRequest, "invalid_request", "unknown time zone: "+zone)
				return
			}
		}
		zoneChanged = zone != next.TimeZone
		next.TimeZone = zone
	}

	if req.Agents != nil && req.Agents.DefaultAI != nil {
		target = "agents.default_ai"
		ai := instancecfg.DefaultAI{
			Agent:    strings.TrimSpace(req.Agents.DefaultAI.Agent),
			Provider: strings.TrimSpace(req.Agents.DefaultAI.Provider),
			Model:    strings.TrimSpace(req.Agents.DefaultAI.Model),
		}
		if ai.Agent == "" {
			ai = instancecfg.DefaultAI{} // the built-in free AI
		} else if !agentauth.Runnable(ai.Agent) {
			writeV1Err(w, http.StatusBadRequest, "invalid_request", "unknown agent in default_ai: "+ai.Agent)
			return
		}
		if len(ai.Provider) > 100 || len(ai.Model) > 200 {
			writeV1Err(w, http.StatusBadRequest, "invalid_request", "default_ai provider or model too long")
			return
		}
		next.DefaultAI = ai
	}

	if err := s.Store.SaveInstanceSettings(r.Context(), store.InstanceSettings{
		IdleReapEnabled:      next.IdleEnabled,
		IdleThresholdSeconds: next.IdleThresholdSeconds,
		KeepaliveMaxSeconds:  next.KeepaliveMaxSeconds,
		AgentDefaultModels:   next.DefaultModels,
		AgentTimeZone:        next.TimeZone,
		AgentDefaultAI: store.DefaultAI{
			Agent: next.DefaultAI.Agent, Provider: next.DefaultAI.Provider, Model: next.DefaultAI.Model,
		},
	}); err != nil {
		writeV1Err(w, http.StatusInternalServerError, "internal", err.Error())
		return
	}
	s.Live.Set(next) // hot-apply (reaper + keepalive + task-submit read this live)
	if zoneChanged {
		// Agents read TZ when they start: make them again, in the background.
		go s.RefreshAgents(context.WithoutCancel(r.Context()))
	}
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

// agentDefaultAI is the AI new chats start with (zero value: the free AI).
func (s *Server) agentDefaultAI() instancecfg.DefaultAI {
	if s.Live == nil {
		return instancecfg.DefaultAI{}
	}
	return s.Live.Snapshot().DefaultAI
}

// agentTimeZone is the IANA time zone agents run in ("" is UTC).
func (s *Server) agentTimeZone() string {
	if s.Live == nil {
		return ""
	}
	return s.Live.TimeZone()
}

// validZone accepts IANA names only ("Europe/Paris", "UTC"), never "Local"
// or a path, since the value becomes the agents' TZ.
func validZone(zone string) bool {
	if zone == "Local" || len(zone) > 64 || strings.HasPrefix(zone, "/") || strings.Contains(zone, "..") {
		return false
	}
	for _, r := range zone {
		if !(r >= 'a' && r <= 'z' || r >= 'A' && r <= 'Z' || r >= '0' && r <= '9' || strings.ContainsRune("/_+-", r)) {
			return false
		}
	}
	return true
}

// previewBase is the scheme://host[:port] previews are reached under, with the
// host-facing port appended unless it's the scheme default (mirrors previewURL).
func (s *Server) previewBase() string {
	scheme, defaultPort := s.previewScheme()
	if s.PreviewURLScheme != "" {
		defaultPort = s.PublicHTTPPort // the terminator in front owns the public port
	}
	host := s.hosts().Wildcard()
	if p := s.PublicHTTPPort; p != "" && p != defaultPort {
		host += ":" + p
	}
	return scheme + "://" + host
}
