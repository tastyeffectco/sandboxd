// Package sandboxspec builds the docker RunSpec for a sandbox from its stored
// row, so a container can be (re)created from the database alone.
//
// Why this exists: the sandbox base image carries `runtimed` (the in-sandbox
// supervisor). Upgrading sandboxd rebuilds that image, but a container created
// from the OLD image keeps running the OLD runtimed until it is recreated —
// so new agent/model support silently never reaches long-lived sandboxes
// ("Model X is not supported" on an otherwise up-to-date install). Recreating
// is safe because ALL durable state lives in the workspace bind mount, never
// in the container's writable layer (the rootfs is read-only).
package sandboxspec

import (
	"github.com/tastyeffectco/sandboxd/control-plane/internal/docker"
	"github.com/tastyeffectco/sandboxd/control-plane/internal/store"
	"github.com/tastyeffectco/sandboxd/control-plane/internal/traefik"
)

// Env carries the instance-level settings a sandbox container needs. These
// mirror the values the create path passes; keeping them in one struct lets
// the recreate path produce an identical container.
type Env struct {
	Image             string // current base image (the whole point: may be newer)
	Network           string
	Userns            string
	Runtime           string // "" = docker default, "runsc" = gVisor
	DNSResolvConf     string // gVisor: host resolv.conf to bind-mount
	PreviewDomain     string
	PreviewEntrypoint string
	PreviewTLS        bool
	AgentProxyURL     string
	OpencodeModel     string
	OpencodeZenPath   string
	RuntimePreset     string   // from the owning app, when known
	AgentAuthMounts   []string // only when the auth proxy is disabled
}

// Build returns the RunSpec for `sb`. It reproduces the create path's flags:
// read-only rootfs, all capabilities dropped, no-new-privileges, tmpfs /tmp,
// the workspace bind mount, and the Traefik preview labels.
func Build(sb *store.Sandbox, e Env) docker.RunSpec {
	name := "s-" + sb.ID

	env := []string{}
	if e.RuntimePreset != "" {
		env = append(env, "RUNTIMED_RUNTIME_PRESET="+e.RuntimePreset)
	}
	if e.AgentProxyURL != "" {
		env = append(env, "RUNTIMED_ANTHROPIC_PROXY="+e.AgentProxyURL)
	}
	if e.OpencodeModel != "" {
		env = append(env, "RUNTIMED_OPENCODE_MODEL="+e.OpencodeModel)
	}
	if e.OpencodeZenPath != "" {
		env = append(env, "SANDBOXD_OPENCODE_ZEN_PATH="+e.OpencodeZenPath)
	}

	volumes := append([]string{sb.WorkspaceMnt + ":/home/sandbox"}, e.AgentAuthMounts...)
	if e.DNSResolvConf != "" {
		volumes = append(volumes, e.DNSResolvConf+":/etc/resolv.conf:ro")
	}

	// Preview routers: the stored ports, ensuring the resolved web port is
	// present (mirrors ensurePort in the create path). A port-less sandbox
	// stays preview-less.
	ports := append([]int(nil), sb.Ports...)
	if len(ports) > 0 {
		web := 3000
		if sb.WebPort.Valid && sb.WebPort.Int64 > 0 {
			web = int(sb.WebPort.Int64)
		}
		found := false
		for _, p := range ports {
			if p == web {
				found = true
				break
			}
		}
		if !found {
			ports = append(ports, web)
		}
	}
	visibility := sb.Visibility
	if visibility == "" {
		visibility = "private"
	}
	labels := traefik.Labels(sb.ID, ports, e.PreviewDomain, visibility, e.PreviewEntrypoint, e.PreviewTLS)

	return docker.RunSpec{
		Name:        name,
		Hostname:    name,
		Network:     e.Network,
		Userns:      e.Userns,
		Runtime:     e.Runtime,
		ReadOnly:    true,
		CapDrop:     []string{"ALL"},
		SecurityOpt: []string{"no-new-privileges"},
		CPUShares:   100,
		Memory:      "10g",
		MemorySwap:  "10g",
		PidsLimit:   1024,
		Ulimits:     []string{"nofile=65536:65536"},
		Tmpfs:       []string{"/tmp:size=512m", "/var/tmp:size=128m"},
		Env:         env,
		Volumes:     volumes,
		Labels:      labels,
		Image:       e.Image,
	}
}

// NeedsRecreate reports whether a container should be replaced before start:
// it was built from a different image than the one the instance now runs.
// After an upgrade this is what carries a new runtimed into old sandboxes.
func NeedsRecreate(containerImage, currentImage string) bool {
	return containerImage != "" && currentImage != "" && containerImage != currentImage
}
