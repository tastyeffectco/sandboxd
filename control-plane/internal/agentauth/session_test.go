package agentauth

import (
	"os"
	"os/exec"
	"strings"
	"testing"
	"time"
)

// fakeRunner runs a fake `claude` (a shell script) directly with HOME=staging —
// no Docker, no real subscription. It mimics the setup-token interaction:
// print a URL, read a pasted code from stdin, and on the right code write an
// opaque credential file under HOME.
type fakeRunner struct{ script string }

func (f fakeRunner) start(_ loginFlow, staging string) (*authProc, error) {
	cmd := exec.Command("sh", "-c", f.script)
	cmd.Env = append(os.Environ(), "HOME="+staging)
	return startProc(cmd, nil)
}

const fakeClaude = `
printf 'Welcome to Claude Code\n'
printf 'Use the url below to sign in (c to copy):\n'
printf 'https://claude.ai/oauth/authorize?response_type=code&state=abc&code_challenge=xyz\n'
printf 'Paste code here > '
read code
if [ "$code" = "GOODCODE" ]; then
  mkdir -p "$HOME/.claude"
  printf 'opaque-token-material' > "$HOME/.claude/.credentials.json"
  printf '\nLogin successful\n'
  exit 0
fi
printf '\nInvalid code\n'
exit 1
`

func newMgr(t *testing.T, script string) (*SessionManager, *Store) {
	t.Helper()
	st := NewStore(t.TempDir())
	if err := st.EnsureRoot(); err != nil {
		t.Fatal(err)
	}
	m := newSessionManager(st, fakeRunner{script: script})
	m.timeout = 5 * time.Second
	return m, st
}

func waitState(t *testing.T, m *SessionManager, id string, want SessionState) (url, errMsg string) {
	t.Helper()
	deadline := time.Now().Add(4 * time.Second)
	for time.Now().Before(deadline) {
		st, u, e, ok := m.Get(id)
		if ok && st == want {
			return u, e
		}
		if ok && (st == StateFailed || st == StateConnected) && st != want {
			t.Fatalf("session reached %q while waiting for %q (err=%q)", st, want, e)
		}
		time.Sleep(15 * time.Millisecond)
	}
	st, _, e, _ := m.Get(id)
	t.Fatalf("timed out waiting for %q; got %q (err=%q)", want, st, e)
	return "", ""
}

func TestConnectSuccessPromotesAndConnects(t *testing.T) {
	m, st := newMgr(t, fakeClaude)
	s, err := m.Connect("claude-code")
	if err != nil {
		t.Fatal(err)
	}
	url, _ := waitState(t, m, s.ID, StateAwaitCode)
	if !strings.HasPrefix(url, "https://") || !strings.Contains(url, "response_type=code") {
		t.Fatalf("bad login url surfaced: %q", url)
	}
	if err := m.SubmitCode(s.ID, "GOODCODE"); err != nil {
		t.Fatal(err)
	}
	waitState(t, m, s.ID, StateConnected)
	if !st.Connected("claude-code") {
		t.Error("claude-code should be connected after success")
	}
	// Disconnect deletes the dir.
	if err := m.Disconnect("claude-code"); err != nil {
		t.Fatal(err)
	}
	if st.Connected("claude-code") {
		t.Error("claude-code should be disconnected after Disconnect")
	}
}

func TestConnectBadCodeFailsAndCleansStaging(t *testing.T) {
	m, st := newMgr(t, fakeClaude)
	s, err := m.Connect("claude-code")
	if err != nil {
		t.Fatal(err)
	}
	waitState(t, m, s.ID, StateAwaitCode)
	if err := m.SubmitCode(s.ID, "WRONG"); err != nil {
		t.Fatal(err)
	}
	waitState(t, m, s.ID, StateFailed)
	if st.Connected("claude-code") {
		t.Error("must not be connected after a bad code")
	}
	// No leftover staging dirs.
	entries, _ := os.ReadDir(st.Root())
	for _, e := range entries {
		if strings.HasPrefix(e.Name(), ".staging-") {
			t.Errorf("staging dir not cleaned: %s", e.Name())
		}
	}
}

// Success must require the credential FILE, not a zero exit code.
func TestConnectExitZeroWithoutCredentialFails(t *testing.T) {
	// Script prints a URL, reads a code, then exits 0 WITHOUT writing creds.
	script := `printf 'sign in: https://claude.ai/oauth/authorize?response_type=code&state=x\n'; printf '> '; read c; exit 0`
	m, st := newMgr(t, script)
	s, _ := m.Connect("claude-code")
	waitState(t, m, s.ID, StateAwaitCode)
	_ = m.SubmitCode(s.ID, "whatever")
	waitState(t, m, s.ID, StateFailed)
	if st.Connected("claude-code") {
		t.Error("exit 0 without a credential file must NOT be connected")
	}
}

func TestConnectUnsupportedProvider(t *testing.T) {
	m, _ := newMgr(t, fakeClaude)
	if _, err := m.Connect("opencode"); err == nil {
		t.Error("opencode has no login flow in A2; Connect should error")
	}
}

func TestExtractAuthURLFromAnsiOutput(t *testing.T) {
	raw := "\x1b[6G\x1b[?25lWelcome to Claude Code\r\n" +
		"\x1b[2GUse the url below to sign in (c to copy):\r\n" +
		"\x1b[2Ghttps://claude.ai/oauth/authorize?response_type=code&state=abc&code_challenge=xyz\r\n" +
		"\x1b[2GPaste code here > "
	got := extractAuthURL(raw)
	want := "https://claude.ai/oauth/authorize?response_type=code&state=abc&code_challenge=xyz"
	if got != want {
		t.Errorf("extractAuthURL = %q; want %q", got, want)
	}
	if extractAuthURL("no url here") != "" {
		t.Error("expected empty when no auth URL present")
	}
}
