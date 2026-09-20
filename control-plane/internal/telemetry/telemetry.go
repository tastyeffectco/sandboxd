// Package telemetry implements sandboxd's anonymous, opt-out usage
// heartbeat and its GitHub-backed update check.
//
// What it sends is deliberately non-identifying: a random instance UUID
// (generated locally, never derived from any host detail), the build
// version, GOOS/GOARCH, and a handful of bucketed counts and enumerated
// settings (see Props). It never sends hostnames, IP addresses, file paths,
// names, tokens, or any user content. docs/telemetry.md lists every field.
//
// Telemetry is ON by default and can be disabled with SANDBOXD_TELEMETRY=off
// (or the cross-tool DO_NOT_TRACK=1). Every network send is best-effort with
// a short timeout: a failing or slow endpoint can never block or crash the
// daemon.
package telemetry

import (
	"bytes"
	"context"
	"crypto/rand"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"time"
)

const (
	// DefaultPostHogHost / DefaultPostHogKey are the project's public capture
	// endpoint. The phc_ key is a write-only client key (safe to embed — it can
	// only append events, never read them). Both are overridable via
	// SANDBOXD_POSTHOG_HOST / SANDBOXD_POSTHOG_KEY.
	DefaultPostHogHost = "https://us.i.posthog.com"
	DefaultPostHogKey  = "phc_pma2C4Wg9EKf4KARJbnU5ZdcNDJGnA5oDtsypKofF2YY"

	// defaultInterval is the heartbeat cadence once running.
	defaultInterval = 24 * time.Hour

	// sendTimeout caps a single capture POST so a hung endpoint never wedges
	// the reporter goroutine.
	sendTimeout = 15 * time.Second
)

// EnabledFromEnv reports whether telemetry should run. It is ON by default and
// returns false only when the operator has explicitly opted out: SANDBOXD_TELEMETRY
// set to off/0/false/no (case-insensitive), or the cross-tool DO_NOT_TRACK set to
// 1/true/yes. `get` is an env accessor (os.Getenv in production; a map in tests).
func EnabledFromEnv(get func(string) string) bool {
	switch strings.ToLower(strings.TrimSpace(get("SANDBOXD_TELEMETRY"))) {
	case "off", "0", "false", "no":
		return false
	}
	switch strings.ToLower(strings.TrimSpace(get("DO_NOT_TRACK"))) {
	case "1", "true", "yes":
		return false
	}
	return true
}

// InstanceID reads (or, on first run, generates and persists) the anonymous
// instance UUID stored at path. The id is random (crypto/rand) — it is NOT
// derived from any machine attribute, so it cannot be correlated back to a host.
// isNew is true only when the id was just generated, which the caller uses to
// emit a one-time "install" event. The file is written 0600.
func InstanceID(path string) (id string, isNew bool, err error) {
	if b, rerr := os.ReadFile(path); rerr == nil {
		if s := strings.TrimSpace(string(b)); s != "" {
			return s, false, nil
		}
	}
	id, err = newUUIDv4()
	if err != nil {
		return "", false, err
	}
	if err = os.MkdirAll(filepath.Dir(path), 0o750); err != nil {
		return "", false, err
	}
	if err = os.WriteFile(path, []byte(id+"\n"), 0o600); err != nil {
		return "", false, err
	}
	return id, true, nil
}

// newUUIDv4 returns a random RFC-4122 version-4 UUID string (8-4-4-4-12 hex).
func newUUIDv4() (string, error) {
	var b [16]byte
	if _, err := rand.Read(b[:]); err != nil {
		return "", err
	}
	b[6] = (b[6] & 0x0f) | 0x40 // version 4
	b[8] = (b[8] & 0x3f) | 0x80 // RFC-4122 variant
	return fmt.Sprintf("%x-%x-%x-%x-%x", b[0:4], b[4:6], b[6:8], b[8:10], b[10:16]), nil
}

// Snapshot is the live, instance-level state the heartbeat is built from. Every
// numeric field is bucketed and every string is an enumerated label before it
// leaves the host; nothing here is free text from the user.
type Snapshot struct {
	CloudOrg       string // set only by the opt-in sandboxd Cloud installer
	SandboxCount   int
	AppCount       int
	Tasks7d        int
	AuthEnabled    bool
	ConsoleEnabled bool
	PreviewDomain  string // classified by PreviewKind; the domain itself is never sent
	PreviewTLS     bool
	AgentDefault   string // provider name only (e.g. "opencode")
	Runtime        string // "runc" | "gvisor" | "other"
	EgressMode     string
	StorageMode    string
	InstallMethod  string // "install.sh" | "bootstrap" | "cloud-init" | "unknown"
	DockerVersion  string // reduced to its major by DockerMajor
	CPUs           int
	MemBytes       uint64
}

// Props builds the heartbeat properties from a Snapshot. Counts are bucketed
// (never exact), the preview domain is reduced to a kind, and "$ip" is forced
// to "" so PostHog neither stores nor geolocates the caller's IP.
func Props(version, arch, osName string, s Snapshot) map[string]any {
	props := map[string]any{
		"version":         version,
		"arch":            arch,
		"os":              osName,
		"sandbox_bucket":  bucketCount(s.SandboxCount),
		"apps_bucket":     bucketCount(s.AppCount),
		"tasks_7d_bucket": bucketCount(s.Tasks7d),
		"auth_enabled":    s.AuthEnabled,
		"console_enabled": s.ConsoleEnabled,
		"preview_kind":    PreviewKind(s.PreviewDomain),
		"preview_tls":     s.PreviewTLS,
		"agent_default":   label(s.AgentDefault, "unknown"),
		"runtime":         label(s.Runtime, "runc"),
		"egress_mode":     label(s.EgressMode, "disabled"),
		"storage_mode":    label(s.StorageMode, "directory"),
		"install_method":  label(s.InstallMethod, "unknown"),
		"docker_major":    DockerMajor(s.DockerVersion),
		"cpu_bucket":      bucketCPUs(s.CPUs),
		"mem_bucket":      bucketMem(s.MemBytes),
		// Empty $ip tells PostHog to drop the request IP (no geo, no storage).
		"$ip": "",
	}
	if s.CloudOrg != "" {
		props["cloud_org"] = label(s.CloudOrg, "unknown")
		props["customer_type"] = "cloud"
	}
	return props
}

// UpgradeProps describes one finished upgrade attempt. Versions are release
// tags (public information); result is the terminal phase.
func UpgradeProps(from, to, result, source string) map[string]any {
	return map[string]any{
		"from":   label(from, "unknown"),
		"to":     label(to, "unknown"),
		"result": label(result, "unknown"),
		"source": label(source, "unknown"),
		"$ip":    "",
	}
}

func label(v, def string) string {
	v = strings.ToLower(strings.TrimSpace(v))
	if v == "" {
		return def
	}
	if len(v) > 32 {
		v = v[:32]
	}
	return v
}

var (
	privateIP = regexp.MustCompile(`^(10\.|127\.|192\.168\.|172\.(1[6-9]|2\d|3[01])\.|100\.(6[4-9]|[7-9]\d|1[01]\d|12[0-7])\.|169\.254\.)`)
	bareIP    = regexp.MustCompile(`^\d{1,3}(\.\d{1,3}){3}$`)
	magicDNS  = regexp.MustCompile(`^(\d{1,3})[.-](\d{1,3})[.-](\d{1,3})[.-](\d{1,3})\.(sslip\.io|nip\.io)$`)
)

// PreviewKind reduces the preview domain to one of four labels so the domain
// itself never leaves the host:
//
//	lan     localhost, *.local/*.lan/*.internal, private IPs (bare or via sslip/nip)
//	ip      a public IP, bare or via sslip.io/nip.io
//	tunnel  a *.sandboxd.io hosted URL
//	domain  anything else (a real domain the operator configured)
func PreviewKind(domain string) string {
	d := strings.ToLower(strings.TrimSpace(domain))
	switch {
	case d == "" || d == "localhost" || strings.HasSuffix(d, ".localhost"):
		return "lan"
	case strings.HasSuffix(d, ".local") || strings.HasSuffix(d, ".lan") || strings.HasSuffix(d, ".internal") || strings.HasSuffix(d, ".home.arpa"):
		return "lan"
	case strings.HasSuffix(d, ".sandboxd.io"):
		return "tunnel"
	case bareIP.MatchString(d):
		if privateIP.MatchString(d) {
			return "lan"
		}
		return "ip"
	}
	if m := magicDNS.FindStringSubmatch(d); m != nil {
		if privateIP.MatchString(m[1] + "." + m[2] + "." + m[3] + "." + m[4]) {
			return "lan"
		}
		return "ip"
	}
	return "domain"
}

// DockerMajor keeps only the major version ("27.3.1" → "27"); "" → "unknown".
func DockerMajor(v string) string {
	v = strings.TrimSpace(v)
	if i := strings.IndexByte(v, '.'); i > 0 {
		v = v[:i]
	}
	if v == "" {
		return "unknown"
	}
	return v
}

func bucketCount(n int) string {
	switch {
	case n <= 0:
		return "0"
	case n <= 3:
		return "1-3"
	case n <= 10:
		return "4-10"
	default:
		return "10+"
	}
}

func bucketCPUs(n int) string {
	switch {
	case n <= 0:
		return "unknown"
	case n <= 2:
		return "1-2"
	case n <= 4:
		return "3-4"
	case n <= 8:
		return "5-8"
	default:
		return "9+"
	}
}

func bucketMem(b uint64) string {
	const gb = 1 << 30
	switch {
	case b == 0:
		return "unknown"
	case b < 4*gb:
		return "<4g"
	case b < 8*gb:
		return "4-8g"
	case b < 16*gb:
		return "8-16g"
	default:
		return "16g+"
	}
}

// HostMemBytes reads MemTotal from /proc/meminfo (0 when unavailable). Inside
// a container this is the host's total, which is what sizing guidance needs.
func HostMemBytes() uint64 {
	b, err := os.ReadFile("/proc/meminfo")
	if err != nil {
		return 0
	}
	for _, line := range strings.Split(string(b), "\n") {
		if strings.HasPrefix(line, "MemTotal:") {
			f := strings.Fields(line)
			if len(f) >= 2 {
				var kb uint64
				fmt.Sscanf(f[1], "%d", &kb)
				return kb * 1024
			}
		}
	}
	return 0
}

// SendFunc delivers one event. It is injectable so tests never touch the
// network; PostHogSend is the production implementation.
type SendFunc func(ctx context.Context, event string, props map[string]any) error

// Reporter periodically emits the anonymous heartbeat. All fields are plain
// values so it is trivial to construct in main and in tests.
type Reporter struct {
	InstanceID string
	Version    string
	Arch       string
	OS         string
	// NewInstall, when true, makes Run emit a one-time "install" event before
	// the first heartbeat.
	NewInstall bool
	// Interval is the heartbeat cadence; zero means the 24h default. Tests set
	// it small.
	Interval time.Duration
	// Send delivers each event (best-effort). nil disables sending.
	Send SendFunc
	// Snapshot supplies the live state at send time. nil → an empty Snapshot.
	Snapshot func() Snapshot
	Log      *slog.Logger
}

// Run emits an optional install event, an immediate heartbeat, then a heartbeat
// every Interval until ctx is done. Every send is best-effort: errors are logged
// and swallowed so the loop — and the daemon — keep running.
func (r *Reporter) Run(ctx context.Context) {
	interval := r.Interval
	if interval <= 0 {
		interval = defaultInterval
	}
	if r.NewInstall {
		r.emit(ctx, "install")
	}
	r.emit(ctx, "heartbeat")

	t := time.NewTicker(interval)
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-t.C:
			r.emit(ctx, "heartbeat")
		}
	}
}

func (r *Reporter) emit(ctx context.Context, event string) {
	var s Snapshot
	if r.Snapshot != nil {
		s = r.Snapshot()
	}
	r.Emit(ctx, event, Props(r.Version, r.Arch, r.OS, s))
}

// Emit sends one event with the given properties (plus the instance id).
// Best-effort like everything else here; used for the one-time "upgrade" event.
func (r *Reporter) Emit(ctx context.Context, event string, props map[string]any) {
	if r.Send == nil {
		return
	}
	if props == nil {
		props = map[string]any{}
	}
	// The sender lifts distinct_id to the top level of the PostHog payload.
	props["distinct_id"] = r.InstanceID

	sctx, cancel := context.WithTimeout(ctx, sendTimeout)
	defer cancel()
	if err := r.Send(sctx, event, props); err != nil && r.Log != nil {
		r.Log.Debug("telemetry send failed (ignored)", "event", event, "err", err.Error())
	}
}

// PostHogSend returns a SendFunc that posts a single capture event to a PostHog
// instance. The distinct_id is read from props (set by Reporter.emit) and lifted
// to the top level; it is not duplicated inside properties.
func PostHogSend(host, apiKey string) SendFunc {
	client := &http.Client{Timeout: 10 * time.Second}
	endpoint := strings.TrimRight(host, "/") + "/i/v0/e/"
	return func(ctx context.Context, event string, props map[string]any) error {
		distinctID, _ := props["distinct_id"].(string)
		properties := make(map[string]any, len(props))
		for k, v := range props {
			if k == "distinct_id" {
				continue
			}
			properties[k] = v
		}
		body, err := json.Marshal(map[string]any{
			"api_key":     apiKey,
			"event":       event,
			"distinct_id": distinctID,
			"properties":  properties,
			"timestamp":   time.Now().UTC().Format(time.RFC3339),
		})
		if err != nil {
			return err
		}
		req, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, bytes.NewReader(body))
		if err != nil {
			return err
		}
		req.Header.Set("Content-Type", "application/json")
		resp, err := client.Do(req)
		if err != nil {
			return err
		}
		defer resp.Body.Close()
		_, _ = io.Copy(io.Discard, resp.Body)
		if resp.StatusCode >= 300 {
			return fmt.Errorf("posthog: unexpected status %d", resp.StatusCode)
		}
		return nil
	}
}
