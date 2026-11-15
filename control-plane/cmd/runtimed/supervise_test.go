package main

import (
	"context"
	"io"
	"log/slog"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// A new start command that lands while the old one fails for the fifth time
// in a row must still run. The deliberate stop used to count as the fifth
// fast failure, so the supervisor gave up and the new command never ran
// (a one-click install sat "down" until the sandbox restarted).
func TestReplaceDuringLastFastFailureStillRuns(t *testing.T) {
	window, unit := fastFailWindow, backoffUnit
	fastFailWindow, backoffUnit = 2*time.Second, 5*time.Millisecond
	t.Cleanup(func() { fastFailWindow, backoffUnit = window, unit })

	dir := t.TempDir()
	log := slog.New(slog.NewTextHandler(io.Discard, nil))
	p := newProcess("web", "web", dir, "sleep 0.3; exit 1", filepath.Join(dir, "web.log"), log)
	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)
	go p.supervise(ctx)

	// Wait for the fifth run (four failures behind it) to be in flight.
	waitFor(t, func() bool {
		_, restarts, running := p.snapshot()
		return restarts == maxFastFails-1 && running
	}, "fifth fast-failing run")
	p.replace(ctx, "sleep 300", false)

	waitFor(t, func() bool {
		p.mu.Lock()
		defer p.mu.Unlock()
		return p.running && strings.HasPrefix(p.command, "sleep 300") && p.supervising
	}, "new command running")
	time.Sleep(500 * time.Millisecond) // past the old command's 0.3 s
	if _, _, running := p.snapshot(); !running {
		t.Fatal("the new command stopped")
	}
}

// A new command that arrives during a long backoff runs at once.
func TestReplaceDuringBackoffRunsAtOnce(t *testing.T) {
	window, unit := fastFailWindow, backoffUnit
	fastFailWindow, backoffUnit = 2*time.Second, 10*time.Second
	t.Cleanup(func() { fastFailWindow, backoffUnit = window, unit })

	dir := t.TempDir()
	log := slog.New(slog.NewTextHandler(io.Discard, nil))
	p := newProcess("web", "web", dir, "exit 1", filepath.Join(dir, "web.log"), log)
	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)
	go p.supervise(ctx)
	waitFor(t, func() bool { _, restarts, _ := p.snapshot(); return restarts == 1 }, "first failure")
	p.replace(ctx, "sleep 300", false)
	waitFor(t, func() bool { _, _, running := p.snapshot(); return running }, "new command before the 10 s backoff")
}

func waitFor(t *testing.T, ok func() bool, what string) {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for !ok() {
		if time.Now().After(deadline) {
			t.Fatalf("timed out waiting for %s", what)
		}
		time.Sleep(5 * time.Millisecond)
	}
}
