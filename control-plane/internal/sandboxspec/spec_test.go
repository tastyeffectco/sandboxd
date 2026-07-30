package sandboxspec

import (
	"database/sql"
	"strings"
	"testing"

	"github.com/tastyeffectco/sandboxd/control-plane/internal/store"
)

func row() *store.Sandbox {
	return &store.Sandbox{
		ID:           "01TEST",
		Status:       "stopped",
		Image:        "sandboxd-base:0.3.0", // what it was CREATED from
		WorkspaceMnt: "/var/lib/sandboxed/workspaces/01TEST",
		Ports:        []int{3000},
		WebPort:      sql.NullInt64{Int64: 3000, Valid: true},
		Visibility:   "private",
	}
}

func TestBuildReproducesTheHardenedCreateFlags(t *testing.T) {
	s := Build(row(), Env{Image: "sandboxd-base:0.4.0", Network: "sandboxd_net", PreviewDomain: "example.test", PreviewEntrypoint: "web"})

	if s.Name != "s-01TEST" || s.Hostname != "s-01TEST" {
		t.Errorf("name/hostname = %q/%q", s.Name, s.Hostname)
	}
	// The point of recreation: the CURRENT image, not the row's old one.
	if s.Image != "sandboxd-base:0.4.0" {
		t.Errorf("image = %q, want the instance's current image", s.Image)
	}
	if !s.ReadOnly {
		t.Error("rootfs must stay read-only")
	}
	if len(s.CapDrop) != 1 || s.CapDrop[0] != "ALL" {
		t.Errorf("CapDrop = %v, want [ALL]", s.CapDrop)
	}
	if len(s.SecurityOpt) != 1 || s.SecurityOpt[0] != "no-new-privileges" {
		t.Errorf("SecurityOpt = %v", s.SecurityOpt)
	}
	if s.PidsLimit != 1024 {
		t.Errorf("PidsLimit = %d", s.PidsLimit)
	}
	// The workspace bind mount is what makes recreation safe.
	if len(s.Volumes) == 0 || !strings.HasSuffix(s.Volumes[0], ":/home/sandbox") {
		t.Errorf("volumes = %v, want the workspace mounted at /home/sandbox", s.Volumes)
	}
}

func TestBuildEmitsPreviewLabelsForStoredPorts(t *testing.T) {
	s := Build(row(), Env{Image: "img", PreviewDomain: "example.test", PreviewEntrypoint: "web"})
	joined := strings.Join(s.Labels, "\n")
	for _, want := range []string{"traefik.enable=true", "s-01TEST-3000", "example.test"} {
		if !strings.Contains(joined, want) {
			t.Errorf("labels missing %q\ngot: %s", want, joined)
		}
	}
}

func TestBuildAddsTheWebPortWhenMissingFromPorts(t *testing.T) {
	r := row()
	r.Ports = []int{8080}
	r.WebPort = sql.NullInt64{Int64: 3000, Valid: true}
	s := Build(r, Env{Image: "img", PreviewDomain: "d", PreviewEntrypoint: "web"})
	joined := strings.Join(s.Labels, "\n")
	if !strings.Contains(joined, "s-01TEST-3000") || !strings.Contains(joined, "s-01TEST-8080") {
		t.Errorf("both the requested and the web port need routers, got:\n%s", joined)
	}
}

func TestBuildPortlessSandboxGetsNoPreviewRouters(t *testing.T) {
	r := row()
	r.Ports = nil
	s := Build(r, Env{Image: "img", PreviewDomain: "d", PreviewEntrypoint: "web"})
	if strings.Contains(strings.Join(s.Labels, "\n"), "routers") {
		t.Errorf("a port-less sandbox must stay preview-less, got %v", s.Labels)
	}
}

func TestBuildPassesRuntimeAndDNSForGvisor(t *testing.T) {
	s := Build(row(), Env{Image: "img", Runtime: "runsc", DNSResolvConf: "/var/lib/sandboxed/gvisor-resolv.conf"})
	if s.Runtime != "runsc" {
		t.Errorf("Runtime = %q", s.Runtime)
	}
	if !strings.Contains(strings.Join(s.Volumes, "\n"), "/etc/resolv.conf:ro") {
		t.Errorf("gVisor needs the resolv.conf mount, got %v", s.Volumes)
	}
}

func TestBuildForwardsRuntimedEnv(t *testing.T) {
	s := Build(row(), Env{
		Image: "img", RuntimePreset: "node-express",
		AgentProxyURL: "http://sandboxd:9100", OpencodeZenPath: "zen",
	})
	joined := strings.Join(s.Env, "\n")
	for _, want := range []string{
		"RUNTIMED_RUNTIME_PRESET=node-express",
		"RUNTIMED_ANTHROPIC_PROXY=http://sandboxd:9100",
		"SANDBOXD_OPENCODE_ZEN_PATH=zen",
	} {
		if !strings.Contains(joined, want) {
			t.Errorf("env missing %q\ngot: %s", want, joined)
		}
	}
}

func TestNeedsRecreate(t *testing.T) {
	cases := []struct {
		name, container, current string
		want                     bool
	}{
		{"stale image after upgrade", "sandboxd-base:0.3.0", "sandboxd-base:0.4.0", true},
		{"already current", "sandboxd-base:0.4.0", "sandboxd-base:0.4.0", false},
		{"unknown container image", "", "sandboxd-base:0.4.0", false},
		{"unknown current image", "sandboxd-base:0.3.0", "", false},
	}
	for _, tc := range cases {
		if got := NeedsRecreate(tc.container, tc.current); got != tc.want {
			t.Errorf("%s: NeedsRecreate(%q,%q) = %v, want %v", tc.name, tc.container, tc.current, got, tc.want)
		}
	}
}
