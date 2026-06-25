package agentauth

import (
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"io"
	"os"
	"regexp"
	"strings"
	"sync"
	"time"
)

// SessionState is the lifecycle of a provider login session.
type SessionState string

const (
	StateStarting   SessionState = "starting"      // process launched, no URL yet
	StateAwaitCode  SessionState = "awaiting_code" // URL surfaced; waiting for the pasted code
	StateFinalizing SessionState = "finalizing"    // code submitted; CLI exchanging it
	StateConnected  SessionState = "connected"     // credential material present + promoted
	StateFailed     SessionState = "failed"        // killed/timed-out/no credential produced
)

// loginFlow describes a provider's CLI login. Provider-shaped even though A2
// exposes claude-code only.
type loginFlow struct {
	provider string
	// innerCmd runs inside a PTY (via `script`) in the ephemeral auth container.
	innerCmd string
	// credentialFile (relative to HOME) must exist non-empty for success. It is
	// only ever stat'd — never opened or parsed.
	credentialFile string
}

var loginFlows = map[string]loginFlow{
	"claude-code": {
		provider:       "claude-code",
		innerCmd:       "stty cols 1000 2>/dev/null; exec claude setup-token",
		credentialFile: ".claude/.credentials.json",
	},
}

// SupportsConnect reports whether a provider has a login flow.
func SupportsConnect(provider string) bool { _, ok := loginFlows[provider]; return ok }

// Session is one in-progress (or finished) login. The URL is sensitive-ish
// runtime data: it is held in memory and returned to the console, but never
// logged, persisted, or emitted in events.
type Session struct {
	ID       string
	Provider string

	mu      sync.Mutex
	state   SessionState
	url     string
	errMsg  string
	stdin   io.WriteCloser
	staging string
	kill    func()
}

func (s *Session) snapshot() (SessionState, string, string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.state, s.url, s.errMsg
}

func (s *Session) set(state SessionState, url, errMsg string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.state = state
	if url != "" {
		s.url = url
	}
	if errMsg != "" {
		s.errMsg = errMsg
	}
}

// SessionManager runs provider login sessions and promotes their opaque output
// into the auth store. nil-safe at the API layer (handlers check for nil).
type SessionManager struct {
	store   *Store
	runner  authRunner
	timeout time.Duration

	mu     sync.Mutex
	byID   map[string]*Session
	active map[string]string // provider -> current session id
}

// NewSessionManager builds a manager whose login sessions run in ephemeral
// containers from the given base image (used only because it has the CLIs).
func NewSessionManager(store *Store, image, dockerBin, userns string) *SessionManager {
	return newSessionManager(store, dockerAuthRunner{image: image, dockerBin: dockerBin, userns: userns})
}

func newSessionManager(store *Store, runner authRunner) *SessionManager {
	return &SessionManager{
		store:   store,
		runner:  runner,
		timeout: 5 * time.Minute,
		byID:    map[string]*Session{},
		active:  map[string]string{},
	}
}

// Connect starts a login for the provider. Returns immediately; the URL is
// populated asynchronously (poll Get). Supersedes any prior active session.
func (m *SessionManager) Connect(provider string) (*Session, error) {
	flow, ok := loginFlows[provider]
	if !ok {
		return nil, fmt.Errorf("provider %q does not support connect", provider)
	}
	if m.store == nil || m.runner == nil {
		return nil, fmt.Errorf("agent auth not configured")
	}
	staging, err := m.store.NewStaging()
	if err != nil {
		return nil, err
	}
	proc, err := m.runner.start(flow, staging)
	if err != nil {
		_ = os.RemoveAll(staging)
		return nil, err
	}
	s := &Session{ID: newSessionID(), Provider: provider, state: StateStarting, staging: staging, stdin: proc.stdin, kill: proc.kill}

	m.mu.Lock()
	if old, ok := m.active[provider]; ok {
		if prev := m.byID[old]; prev != nil && prev.kill != nil {
			prev.kill()
		}
	}
	m.byID[s.ID] = s
	m.active[provider] = s.ID
	m.mu.Unlock()

	go m.drive(s, flow, proc)
	return s, nil
}

func (m *SessionManager) drive(s *Session, flow loginFlow, proc *authProc) {
	// Hard timeout: kill an unfinished session so it can't linger.
	done := make(chan struct{})
	go func() {
		select {
		case <-done:
		case <-time.After(m.timeout):
			if st, _, _ := s.snapshot(); st != StateConnected && st != StateFailed {
				s.kill()
			}
		}
	}()

	// Stream combined output; surface the login URL as soon as it appears.
	buf := make([]byte, 4096)
	var acc strings.Builder
	for {
		n, rerr := proc.stdout.Read(buf)
		if n > 0 {
			acc.Write(buf[:n])
			if st, _, _ := s.snapshot(); st == StateStarting {
				if u := extractAuthURL(acc.String()); u != "" {
					s.set(StateAwaitCode, u, "")
				}
			}
		}
		if rerr != nil {
			break
		}
	}
	_ = proc.wait()
	close(done)

	// Success requires the credential FILE to be present and non-empty — NOT a
	// zero exit code (the CLI exits 0 even when not logged in).
	if m.store.CredentialPresent(s.staging, flow.credentialFile) {
		if err := m.store.Promote(s.staging, s.Provider); err != nil {
			_ = os.RemoveAll(s.staging)
			s.set(StateFailed, "", "could not store credentials")
			return
		}
		s.set(StateConnected, "", "")
		return
	}
	_ = os.RemoveAll(s.staging)
	s.set(StateFailed, "", "login did not complete")
}

// SubmitCode feeds the pasted authorization code to the CLI's stdin.
func (m *SessionManager) SubmitCode(id, code string) error {
	m.mu.Lock()
	s := m.byID[id]
	m.mu.Unlock()
	if s == nil {
		return fmt.Errorf("no such session")
	}
	if st, _, _ := s.snapshot(); st != StateAwaitCode {
		return fmt.Errorf("session is not awaiting a code")
	}
	s.mu.Lock()
	w := s.stdin
	s.mu.Unlock()
	if w == nil {
		return fmt.Errorf("session closed")
	}
	if _, err := io.WriteString(w, strings.TrimSpace(code)+"\n"); err != nil {
		return err
	}
	s.set(StateFinalizing, "", "")
	return nil
}

// Get returns a snapshot of a session's public state.
func (m *SessionManager) Get(id string) (state SessionState, url, errMsg string, ok bool) {
	m.mu.Lock()
	s := m.byID[id]
	m.mu.Unlock()
	if s == nil {
		return "", "", "", false
	}
	st, u, e := s.snapshot()
	return st, u, e, true
}

// Disconnect kills any active session and deletes the provider's auth dir.
func (m *SessionManager) Disconnect(provider string) error {
	if m.store == nil {
		return fmt.Errorf("agent auth not configured")
	}
	m.mu.Lock()
	if id, ok := m.active[provider]; ok {
		if s := m.byID[id]; s != nil && s.kill != nil {
			s.kill()
		}
		delete(m.active, provider)
	}
	m.mu.Unlock()
	return m.store.Delete(provider)
}

func newSessionID() string {
	b := make([]byte, 12)
	_, _ = rand.Read(b)
	return hex.EncodeToString(b)
}

// --- URL extraction from PTY output ---------------------------------------

var (
	ansiRe = regexp.MustCompile("\x1b\\[[0-9;?]*[ -/]*[@-~]|\x1b[()][AB0]|[\x00-\x08\x0b\x0c\x0e-\x1f]")
	urlRe  = regexp.MustCompile(`https://[^\s'"]+`)
)

func stripANSI(s string) string {
	return ansiRe.ReplaceAllString(strings.ReplaceAll(s, "\r", "\n"), "")
}

// extractAuthURL pulls the OAuth login URL out of the CLI's (PTY, ANSI-laden)
// output. The auth container sets a wide terminal (stty cols 1000) so the URL
// is not wrapped across lines.
func extractAuthURL(raw string) string {
	for _, line := range strings.Split(stripANSI(raw), "\n") {
		for _, u := range urlRe.FindAllString(line, -1) {
			if strings.Contains(u, "response_type=code") || strings.Contains(u, "/oauth/") || strings.Contains(u, "/authorize") {
				return u
			}
		}
	}
	return ""
}
