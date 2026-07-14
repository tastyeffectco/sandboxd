// Package telemetry implements sandboxd's anonymous, opt-out usage
// heartbeat and its GitHub-backed update check.
//
// What it sends is deliberately minimal and non-identifying: a random
// instance UUID (generated locally, never derived from any host detail),
// the build version, GOOS/GOARCH, a coarse sandbox-count bucket, and two
// feature booleans. It never sends hostnames, IP addresses, file paths,
// tokens, or any user content. See docs/telemetry.md for the exact list.
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
	"strings"
	"time"
)

const (
	// DefaultPostHogHost / DefaultPostHogKey are the project's public capture
	// endpoint. The phc_ key is a write-only client key (safe to embed — it can
	// only append events, never read them). Both are overridable via
	// SANDBOXD_POSTHOG_HOST / SANDBOXD_POSTHOG_KEY.
	DefaultPostHogHost = "https://us.i.posthog.com"
	DefaultPostHogKey  = "phc_vyQtLTZPBHwEBcY8mcfneP43xAFGLzFVic9DhQ7VGrqV"

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

// heartbeatProps builds the event properties. sandbox count is bucketed (never
// sent as an exact number) and "$ip" is forced to "" so PostHog neither stores
// nor geolocates the caller's IP. No hostnames, paths, or user content ever
// appear here.
func heartbeatProps(version, arch, osName string, sandboxCount int, authEnabled, consoleEnabled bool) map[string]any {
	return map[string]any{
		"version":         version,
		"arch":            arch,
		"os":              osName,
		"sandbox_bucket":  bucketCount(sandboxCount),
		"auth_enabled":    authEnabled,
		"console_enabled": consoleEnabled,
		// Empty $ip tells PostHog to drop the request IP (no geo, no storage).
		"$ip": "",
	}
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
	// Snapshot supplies the live counters at send time. nil → zero/false.
	Snapshot func() (sandboxCount int, authEnabled, consoleEnabled bool)
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
	if r.Send == nil {
		return
	}
	count, authEnabled, consoleEnabled := 0, false, false
	if r.Snapshot != nil {
		count, authEnabled, consoleEnabled = r.Snapshot()
	}
	props := heartbeatProps(r.Version, r.Arch, r.OS, count, authEnabled, consoleEnabled)
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
