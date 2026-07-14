package telemetry

import (
	"context"
	"os"
	"path/filepath"
	"regexp"
	"sync"
	"testing"
	"time"
)

func TestEnabledFromEnv(t *testing.T) {
	cases := []struct {
		name string
		env  map[string]string
		want bool
	}{
		{"default on (nothing set)", map[string]string{}, true},
		{"SANDBOXD_TELEMETRY=off", map[string]string{"SANDBOXD_TELEMETRY": "off"}, false},
		{"SANDBOXD_TELEMETRY=OFF (case)", map[string]string{"SANDBOXD_TELEMETRY": "OFF"}, false},
		{"SANDBOXD_TELEMETRY=0", map[string]string{"SANDBOXD_TELEMETRY": "0"}, false},
		{"SANDBOXD_TELEMETRY=false", map[string]string{"SANDBOXD_TELEMETRY": "false"}, false},
		{"SANDBOXD_TELEMETRY=no", map[string]string{"SANDBOXD_TELEMETRY": "no"}, false},
		{"SANDBOXD_TELEMETRY=on stays on", map[string]string{"SANDBOXD_TELEMETRY": "on"}, true},
		{"SANDBOXD_TELEMETRY=1 stays on", map[string]string{"SANDBOXD_TELEMETRY": "1"}, true},
		{"DO_NOT_TRACK=1", map[string]string{"DO_NOT_TRACK": "1"}, false},
		{"DO_NOT_TRACK=true", map[string]string{"DO_NOT_TRACK": "true"}, false},
		{"DO_NOT_TRACK=YES (case)", map[string]string{"DO_NOT_TRACK": "YES"}, false},
		{"DO_NOT_TRACK=0 stays on", map[string]string{"DO_NOT_TRACK": "0"}, true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			get := func(k string) string { return tc.env[k] }
			if got := EnabledFromEnv(get); got != tc.want {
				t.Fatalf("EnabledFromEnv() = %v, want %v", got, tc.want)
			}
		})
	}
}

var uuidRE = regexp.MustCompile(`^[0-9a-f]{8}-[0-9a-f]{4}-4[0-9a-f]{3}-[89ab][0-9a-f]{3}-[0-9a-f]{12}$`)

func TestInstanceIDGeneratePersistReread(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "instance-id")

	id, isNew, err := InstanceID(path)
	if err != nil {
		t.Fatalf("first InstanceID: %v", err)
	}
	if !isNew {
		t.Fatal("first call should report isNew=true")
	}
	if !uuidRE.MatchString(id) {
		t.Fatalf("generated id %q is not a v4-shaped UUID", id)
	}

	// File perms must be 0600.
	fi, err := os.Stat(path)
	if err != nil {
		t.Fatalf("stat: %v", err)
	}
	if perm := fi.Mode().Perm(); perm != 0o600 {
		t.Fatalf("instance-id perms = %o, want 600", perm)
	}

	// Second call is idempotent: same id, isNew=false.
	id2, isNew2, err := InstanceID(path)
	if err != nil {
		t.Fatalf("second InstanceID: %v", err)
	}
	if id2 != id {
		t.Fatalf("second id %q != first %q", id2, id)
	}
	if isNew2 {
		t.Fatal("second call should report isNew=false")
	}
}

func TestHeartbeatPropsBucketing(t *testing.T) {
	cases := []struct {
		count int
		want  string
	}{
		{-1, "0"},
		{0, "0"},
		{1, "1-3"},
		{3, "1-3"},
		{4, "4-10"},
		{10, "4-10"},
		{11, "10+"},
		{500, "10+"},
	}
	for _, tc := range cases {
		props := heartbeatProps("v0.3.0", "arm64", "linux", tc.count, true, false)
		if got := props["sandbox_bucket"]; got != tc.want {
			t.Errorf("count=%d bucket=%v want %v", tc.count, got, tc.want)
		}
	}
}

func TestHeartbeatPropsNoIPAndFields(t *testing.T) {
	props := heartbeatProps("v0.3.0", "amd64", "darwin", 2, true, true)

	// $ip must be present and EMPTY so PostHog does not geolocate/store IP.
	ip, ok := props["$ip"]
	if !ok {
		t.Fatal("props missing $ip")
	}
	if ip != "" {
		t.Fatalf("$ip = %q, want empty string", ip)
	}

	if props["version"] != "v0.3.0" {
		t.Errorf("version = %v", props["version"])
	}
	if props["arch"] != "amd64" {
		t.Errorf("arch = %v", props["arch"])
	}
	if props["os"] != "darwin" {
		t.Errorf("os = %v", props["os"])
	}
	if props["auth_enabled"] != true {
		t.Errorf("auth_enabled = %v", props["auth_enabled"])
	}
	if props["console_enabled"] != true {
		t.Errorf("console_enabled = %v", props["console_enabled"])
	}

	// No PII fields must ever leak in.
	for _, bad := range []string{"hostname", "host", "path", "ip", "user", "email"} {
		if _, present := props[bad]; present {
			t.Errorf("props leaked PII-ish key %q", bad)
		}
	}
}

func TestReporterRunSendsInstallAndHeartbeat(t *testing.T) {
	var mu sync.Mutex
	events := []string{}
	sent := make(chan string, 16)
	send := func(_ context.Context, event string, props map[string]any) error {
		mu.Lock()
		events = append(events, event)
		mu.Unlock()
		sent <- event
		return nil
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	r := &Reporter{
		InstanceID: "test-id",
		Version:    "v0.3.0",
		Arch:       "arm64",
		OS:         "linux",
		NewInstall: true,
		Interval:   5 * time.Millisecond, // fast tick for the test
		Send:       send,
		Snapshot:   func() (int, bool, bool) { return 2, true, false },
	}

	go r.Run(ctx)

	// Expect: install, then heartbeat, then at least one more heartbeat from the tick.
	got := []string{}
	timeout := time.After(2 * time.Second)
	for len(got) < 3 {
		select {
		case e := <-sent:
			got = append(got, e)
		case <-timeout:
			t.Fatalf("timed out; got events = %v", got)
		}
	}
	cancel()

	if got[0] != "install" {
		t.Errorf("first event = %q, want install", got[0])
	}
	if got[1] != "heartbeat" {
		t.Errorf("second event = %q, want heartbeat", got[1])
	}
	if got[2] != "heartbeat" {
		t.Errorf("third event = %q, want heartbeat", got[2])
	}
}

func TestReporterRunSurvivesSendError(t *testing.T) {
	sent := make(chan struct{}, 16)
	send := func(_ context.Context, event string, props map[string]any) error {
		sent <- struct{}{}
		return context.DeadlineExceeded // always fail
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	r := &Reporter{
		InstanceID: "x",
		NewInstall: false, // no install event: first send is a heartbeat
		Interval:   3 * time.Millisecond,
		Send:       send,
		Snapshot:   func() (int, bool, bool) { return 0, false, false },
	}
	go r.Run(ctx) // must not panic even though every send errors

	// See several sends despite the persistent error (loop keeps going).
	for i := 0; i < 3; i++ {
		select {
		case <-sent:
		case <-time.After(2 * time.Second):
			t.Fatalf("only saw %d sends; a send error must not stop Run", i)
		}
	}
}
