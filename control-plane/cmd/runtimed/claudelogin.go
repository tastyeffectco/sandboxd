package main

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"os"
	"os/exec"
	"regexp"
	"strings"
	"sync"
	"time"

	"github.com/creack/pty"
)

// Claude subscriptions sign in to the unmodified Claude Code binary inside the
// workspace, through Anthropic's own flow. runtimed only runs `claude auth
// login` in a terminal, shows its sign-in link and types the code the user
// pastes, exactly as a terminal would. Claude Code stores the login in this
// workspace; sandboxd never reads, stores or relays the token.

// claudeBinary is the Claude Code CLI; tests substitute a fake.
var claudeBinary = "claude"

const (
	claudeLoginTimeout = 10 * time.Minute
	claudeOutputLimit  = 64 << 10
)

var (
	terminalEscapes = regexp.MustCompile(`\x1b\][^\x07\x1b]*(?:\x07|\x1b\\)|\x1b\[[0-9;?]*[ -/]*[@-~]|\x1b[@-_]`)
	authorizeURL    = regexp.MustCompile(`https://[^\s"'<>]+/oauth/authorize\?[^\s"'<>]+`)
)

// claudeAccountEnv is the environment for Claude Code using the workspace's own
// login: secret-shaped and runtimed variables are scrubbed and no proxy
// variables are set, so Claude Code talks to Anthropic itself.
func claudeAccountEnv(specEnv map[string]string) []string {
	return buildAgentEnv(os.Environ(), specEnv)
}

type claudeLoginSession struct {
	cmd      *exec.Cmd
	terminal *os.File
	output   strings.Builder
	url      string
	urlReady chan struct{}
	done     chan struct{}
}

type claudeLogins struct {
	mu      sync.Mutex
	session *claudeLoginSession
}

func (l *claudeLogins) stopLocked() {
	if s := l.session; s != nil {
		_ = s.cmd.Process.Kill()
		_ = s.terminal.Close()
		l.session = nil
	}
}

// start launches `claude auth login` and returns the sign-in link it prints.
func (l *claudeLogins) start(ctx context.Context, dir string) (string, error) {
	cmd := exec.Command(claudeBinary, "auth", "login", "--claudeai")
	cmd.Dir = dir
	cmd.Env = claudeAccountEnv(nil)
	l.mu.Lock()
	l.stopLocked()
	// A wide terminal keeps the long sign-in link on one line.
	terminal, err := pty.StartWithSize(cmd, &pty.Winsize{Cols: 4000, Rows: 50})
	if err != nil {
		l.mu.Unlock()
		return "", err
	}
	s := &claudeLoginSession{cmd: cmd, terminal: terminal, urlReady: make(chan struct{}), done: make(chan struct{})}
	l.session = s
	l.mu.Unlock()
	go func() {
		buf := make([]byte, 4096)
		for {
			n, err := terminal.Read(buf)
			if n > 0 {
				l.mu.Lock()
				if s.output.Len() < claudeOutputLimit {
					s.output.Write(buf[:n])
				}
				if s.url == "" {
					clean := terminalEscapes.ReplaceAllString(s.output.String(), "")
					if u := authorizeURL.FindString(clean); u != "" {
						// The link text can follow the link target directly.
						if i := strings.Index(u[len("https://"):], "https://"); i >= 0 {
							u = u[:len("https://")+i]
						}
						s.url = u
						close(s.urlReady)
					}
				}
				l.mu.Unlock()
			}
			if err != nil {
				break
			}
		}
		_ = cmd.Wait()
		close(s.done)
	}()
	time.AfterFunc(claudeLoginTimeout, func() {
		l.mu.Lock()
		if l.session == s {
			l.stopLocked()
		}
		l.mu.Unlock()
	})
	select {
	case <-s.urlReady:
		return s.url, nil
	case <-s.done:
		return "", errors.New("Claude Code ended before showing a sign-in link")
	case <-time.After(30 * time.Second):
		return "", errors.New("Claude Code did not show a sign-in link")
	case <-ctx.Done():
		return "", ctx.Err()
	}
}

// submit types the code the user copied from Anthropic's sign-in page.
func (l *claudeLogins) submit(ctx context.Context, code string) error {
	l.mu.Lock()
	s := l.session
	l.mu.Unlock()
	if s == nil {
		return errors.New("start signing in with Claude first")
	}
	if _, err := s.terminal.Write([]byte(code + "\r")); err != nil {
		return errors.New("the sign-in session has ended; start again")
	}
	select {
	case <-s.done:
	case <-time.After(90 * time.Second):
		l.mu.Lock()
		if l.session == s {
			l.stopLocked()
		}
		l.mu.Unlock()
		return errors.New("Claude Code did not finish signing in; start again")
	case <-ctx.Done():
		return ctx.Err()
	}
	l.mu.Lock()
	if l.session == s {
		l.session = nil
	}
	l.mu.Unlock()
	return nil
}

type claudeAuthStatus struct {
	LoggedIn bool   `json:"logged_in"`
	Method   string `json:"method,omitempty"`
}

// claudeStatus asks Claude Code whether this workspace is signed in.
func claudeStatus(ctx context.Context, dir string) (claudeAuthStatus, error) {
	ctx, cancel := context.WithTimeout(ctx, 20*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, claudeBinary, "auth", "status", "--json")
	cmd.Dir = dir
	cmd.Env = claudeAccountEnv(nil)
	out, err := cmd.Output()
	var status struct {
		LoggedIn   bool   `json:"loggedIn"`
		AuthMethod string `json:"authMethod"`
	}
	if jerr := json.Unmarshal(out, &status); jerr != nil {
		if err == nil {
			err = jerr
		}
		return claudeAuthStatus{}, err
	}
	if !status.LoggedIn {
		return claudeAuthStatus{}, nil
	}
	return claudeAuthStatus{LoggedIn: true, Method: status.AuthMethod}, nil
}

func (a *app) handleClaudeLoginStart(w http.ResponseWriter, r *http.Request) {
	u, err := a.claude.start(r.Context(), a.appDir)
	if err != nil {
		writeJSON(w, http.StatusBadGateway, map[string]string{"error": err.Error()})
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"url": u})
}

func (a *app) handleClaudeLoginCode(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Code string `json:"code"`
	}
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 4096)).Decode(&body); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid request"})
		return
	}
	code := strings.TrimSpace(body.Code)
	if code == "" || len(code) > 1024 || strings.ContainsAny(code, "\r\n\x00\x1b") {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "paste the code from the Claude sign-in page"})
		return
	}
	if err := a.claude.submit(r.Context(), code); err != nil {
		writeJSON(w, http.StatusConflict, map[string]string{"error": err.Error()})
		return
	}
	status, err := claudeStatus(r.Context(), a.appDir)
	if err != nil || !status.LoggedIn {
		writeJSON(w, http.StatusConflict, map[string]string{"error": "Claude did not accept that code; start signing in again"})
		return
	}
	writeJSON(w, http.StatusOK, status)
}

func (a *app) handleClaudeLoginStatus(w http.ResponseWriter, r *http.Request) {
	status, err := claudeStatus(r.Context(), a.appDir)
	if err != nil {
		writeJSON(w, http.StatusBadGateway, map[string]string{"error": "could not check the Claude sign-in"})
		return
	}
	writeJSON(w, http.StatusOK, status)
}

func (a *app) handleClaudeLogout(w http.ResponseWriter, r *http.Request) {
	ctx, cancel := context.WithTimeout(r.Context(), 20*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, claudeBinary, "auth", "logout")
	cmd.Dir = a.appDir
	cmd.Env = claudeAccountEnv(nil)
	if err := cmd.Run(); err != nil {
		writeJSON(w, http.StatusBadGateway, map[string]string{"error": "could not sign out of Claude"})
		return
	}
	writeJSON(w, http.StatusOK, claudeAuthStatus{})
}
