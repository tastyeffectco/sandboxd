// v1_settings.go — Phase 8A: a READ-ONLY instance/settings summary for the
// console's operability ("Settings") view. It returns only safe, static
// metadata about how this sandboxd is configured. It MUST NEVER include
// secrets, tokens, encryption keys, or raw env values — only booleans, modes,
// names, and counts that are safe to show any operator.
package api

import (
	"net/http"

	"github.com/sandboxd/control-plane/internal/preset"
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
	Version      string            `json:"version"`
	GitCommit    string            `json:"git_commit,omitempty"`
	Networking   v1SettingsNet     `json:"networking"`
	Auth         v1SettingsAuth    `json:"auth"`
	Runtime      v1SettingsRuntime `json:"runtime"`
	Lifecycle    v1SettingsLife    `json:"lifecycle"`
	Egress       v1SettingsEgress  `json:"egress"`
	Agents       v1SettingsAgents  `json:"agents"`
	Presets      []v1Preset        `json:"presets"`
	Capabilities map[string]bool   `json:"capabilities"`
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
		Auth:    v1SettingsAuth{Enabled: s.Instance.AuthEnabled},
		Runtime: v1SettingsRuntime{StorageMode: storageMode, BaseImage: s.Image},
		Lifecycle: v1SettingsLife{
			IdleReapEnabled:      s.Instance.IdleReapEnabled,
			IdleThresholdSeconds: s.Instance.IdleThresholdSeconds,
			KeepaliveMaxSeconds:  int(s.KeepaliveMax.Seconds()),
		},
		Egress:  v1SettingsEgress{Mode: egressMode},
		Agents:  v1SettingsAgents{Providers: s.Instance.AgentProviders},
		Presets: presets,
		Capabilities: map[string]bool{
			"snapshots":      s.Snapshot != nil,
			"config_secrets": s.Secrets != nil,
			"templates":      s.TemplatesDir != "",
			"forward_auth":   s.Auth != nil,
		},
	}
	writeJSON(w, http.StatusOK, out)
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
