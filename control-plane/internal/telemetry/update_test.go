package telemetry

import (
	"context"
	"testing"
)

func TestCompareSemver(t *testing.T) {
	cases := []struct {
		a, b string
		want int
	}{
		{"0.3.0", "0.4.0", -1},
		{"0.4.0", "0.3.0", 1},
		{"0.3.0", "0.3.1", -1},
		{"0.3.1", "0.3.0", 1},
		{"0.3.0", "0.3.0", 0},
		{"v0.3.0", "0.3.0", 0},     // v-prefix ignored
		{"v1.2.3", "v1.2.3", 0},    // both v-prefixed
		{"1.0.0", "0.9.9", 1},      // major dominates
		{"0.10.0", "0.9.0", 1},     // numeric, not lexical
		{"", "0.0.0", 0},           // malformed → 0
		{"garbage", "0.0.0", 0},    // malformed → 0
		{"1.2", "1.2.0", 0},        // missing patch → 0
		{"v0.4.0-rc1", "0.4.0", 0}, // pre-release stripped
	}
	for _, tc := range cases {
		if got := CompareSemver(tc.a, tc.b); got != tc.want {
			t.Errorf("CompareSemver(%q,%q) = %d, want %d", tc.a, tc.b, got, tc.want)
		}
	}
}

func TestCheckerUpdateAvailable(t *testing.T) {
	c := &Checker{
		Fetch: func(context.Context) (string, string, error) {
			return "v0.4.0", "https://github.com/tastyeffectco/sandboxd/releases/tag/v0.4.0", nil
		},
	}
	if _, _, err := c.Latest(context.Background()); err != nil {
		t.Fatalf("Latest: %v", err)
	}

	avail, latest, url := c.UpdateAvailable("0.3.0")
	if !avail {
		t.Error("expected update available for current=0.3.0 vs latest=0.4.0")
	}
	if latest != "v0.4.0" {
		t.Errorf("latest = %q", latest)
	}
	if url == "" {
		t.Error("changelog url should be populated")
	}

	// Current == latest: no update.
	avail2, _, _ := c.UpdateAvailable("0.4.0")
	if avail2 {
		t.Error("no update should be available when current == latest")
	}

	// Current newer than latest (dev build ahead of release): no update.
	avail3, _, _ := c.UpdateAvailable("0.5.0")
	if avail3 {
		t.Error("no update should be available when current > latest")
	}
}

func TestCheckerUpdateAvailableEmptyCacheIsSafe(t *testing.T) {
	c := &Checker{
		Fetch: func(context.Context) (string, string, error) {
			return "", "", context.DeadlineExceeded
		},
	}
	// Latest returns the error; UpdateAvailable must be best-effort false.
	if _, _, err := c.Latest(context.Background()); err == nil {
		t.Fatal("expected fetch error")
	}
	avail, _, _ := c.UpdateAvailable("0.3.0")
	if avail {
		t.Error("empty/failed cache must yield update_available=false")
	}
}

func TestCheckerUpdateAvailableNonSemverCurrent(t *testing.T) {
	c := &Checker{
		Fetch: func(context.Context) (string, string, error) {
			return "v0.3.0", "https://github.com/tastyeffectco/sandboxd/releases/tag/v0.3.0", nil
		},
	}
	if _, _, err := c.Latest(context.Background()); err != nil {
		t.Fatalf("Latest: %v", err)
	}

	// A bare commit hash or "dev" (a build tracking main) parses as 0.0.0 and
	// must NOT make the latest release look like an update — but the latest
	// release info is still surfaced for display.
	for _, cur := range []string{"e2ca6f6", "dev", "unknown"} {
		avail, latest, url := c.UpdateAvailable(cur)
		if avail {
			t.Errorf("current=%q: no update should be reported for a non-semver build", cur)
		}
		if latest != "v0.3.0" || url == "" {
			t.Errorf("current=%q: latest info should still be surfaced, got %q %q", cur, latest, url)
		}
	}

	// Tag-anchored describe (ahead of the release) compares by its tag: no update.
	if avail, _, _ := c.UpdateAvailable("v0.3.0-54-g83f2f7"); avail {
		t.Error("v0.3.0-54-g… (ahead of v0.3.0) should not report an update")
	}
	// And a genuinely older tagged build still does.
	if avail, _, _ := c.UpdateAvailable("v0.2.9"); !avail {
		t.Error("v0.2.9 should report an update vs v0.3.0")
	}
}
