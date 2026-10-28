package main

import (
	"context"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// fakeClaude installs a stand-in for `claude auth …` that prints its sign-in
// link the way Claude Code does (an OSC 8 hyperlink whose text repeats the
// link), waits for a pasted code and records the login in a state file.
func fakeClaude(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	state := filepath.Join(dir, "logged-in")
	script := `#!/bin/sh
case "$1 $2" in
"auth login")
  u='https://claude.com/cai/oauth/authorize?code=true&state=abc'
  printf '\033]8;;%s\033\\%s\033]8;;\033\\\nPaste code here if prompted > ' "$u" "$u"
  read code
  [ "$code" = "good-code" ] && echo ok > "$FAKE_CLAUDE_STATE"
  ;;
"auth status")
  if [ -f "$FAKE_CLAUDE_STATE" ]; then echo '{"loggedIn": true, "authMethod": "claude.ai"}'; else echo '{"loggedIn": false, "authMethod": "none"}'; fi
  ;;
"auth logout") rm -f "$FAKE_CLAUDE_STATE" ;;
esac
`
	path := filepath.Join(dir, "claude")
	if err := os.WriteFile(path, []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("FAKE_CLAUDE_STATE", state)
	old := claudeBinary
	claudeBinary = path
	t.Cleanup(func() { claudeBinary = old })
	return state
}

func TestClaudeSignInRunsClaudeCodesOwnLogin(t *testing.T) {
	fakeClaude(t)
	a := &app{appDir: t.TempDir()}
	ctx := context.Background()
	u, err := a.claude.start(ctx, a.appDir)
	if err != nil || u != "https://claude.com/cai/oauth/authorize?code=true&state=abc" {
		t.Fatalf("start = %q, %v; want the single sign-in link", u, err)
	}
	if err := a.claude.submit(ctx, "good-code"); err != nil {
		t.Fatal(err)
	}
	status, err := claudeStatus(ctx, a.appDir)
	if err != nil || !status.LoggedIn || status.Method != "claude.ai" {
		t.Fatalf("status = %+v, %v; want signed in", status, err)
	}
	w := httptest.NewRecorder()
	a.handleClaudeLogout(w, httptest.NewRequest("DELETE", "/claude-login", nil))
	if status, _ := claudeStatus(ctx, a.appDir); w.Code != 200 || status.LoggedIn {
		t.Fatalf("logout: %d, still signed in = %v", w.Code, status.LoggedIn)
	}
}

func TestClaudeSignInRejectsWrongOrMalformedCodes(t *testing.T) {
	fakeClaude(t)
	a := &app{appDir: t.TempDir()}
	for _, body := range []string{`{"code":""}`, `{"code":"a\nb"}`, `not json`} {
		w := httptest.NewRecorder()
		a.handleClaudeLoginCode(w, httptest.NewRequest("POST", "/claude-login/code", strings.NewReader(body)))
		if w.Code != http.StatusBadRequest {
			t.Errorf("code %s: HTTP %d; want 400", body, w.Code)
		}
	}
	w := httptest.NewRecorder()
	a.handleClaudeLoginCode(w, httptest.NewRequest("POST", "/claude-login/code", strings.NewReader(`{"code":"x"}`)))
	if w.Code != http.StatusConflict {
		t.Errorf("code without a session: HTTP %d; want 409", w.Code)
	}
	if _, err := a.claude.start(context.Background(), a.appDir); err != nil {
		t.Fatal(err)
	}
	w = httptest.NewRecorder()
	a.handleClaudeLoginCode(w, httptest.NewRequest("POST", "/claude-login/code", strings.NewReader(`{"code":"wrong-code"}`)))
	if w.Code != http.StatusConflict || !strings.Contains(w.Body.String(), "did not accept") {
		t.Errorf("wrong code: HTTP %d %s; want a rejected sign-in", w.Code, w.Body.String())
	}
}

// Claude Code on the workspace's own login gets no proxy variables or dummy
// key, so it talks to Anthropic itself.
func TestClaudeAccountEnvHasNoProxy(t *testing.T) {
	t.Setenv("RUNTIMED_ANTHROPIC_PROXY", "http://127.0.0.1:9100")
	t.Setenv("ANTHROPIC_API_KEY", "inherited-secret")
	for _, kv := range claudeAccountEnv(nil) {
		for _, banned := range []string{"ANTHROPIC_BASE_URL=", "ANTHROPIC_API_KEY=", "RUNTIMED_ANTHROPIC_PROXY="} {
			if strings.HasPrefix(kv, banned) {
				t.Errorf("unexpected %s in the signed-in Claude environment", kv)
			}
		}
	}
}
