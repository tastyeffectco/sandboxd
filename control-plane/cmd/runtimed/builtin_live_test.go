package main

import (
	"bytes"
	"context"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/tastyeffectco/sandboxd/control-plane/internal/agentauth"
	"github.com/tastyeffectco/sandboxd/control-plane/internal/authproxy"
	"github.com/tastyeffectco/sandboxd/control-plane/internal/modelconfig"
)

// Explicitly opt-in: real subscriptions through the actual sandboxd proxy.
// Credentials stay in a private control-plane store, never in the agent's HOME.
func TestLiveBuiltinAgents(t *testing.T) {
	for _, name := range []string{"codex", "claude-code", "opencode", "pi"} {
		t.Run(name, func(t *testing.T) {
			variable := "SANDBOXD_TEST_" + strings.ToUpper(strings.ReplaceAll(name, "-", "_"))
			credentialPath := os.Getenv(variable + "_AUTH_FILE")
			if credentialPath == "" && !(name == "opencode" && os.Getenv("SANDBOXD_TEST_OPENCODE_FREE") == "1") {
				t.Skip("explicit live credentials or free-tier opt-in required")
			}
			root := t.TempDir()
			work := filepath.Join(root, "work")
			home := filepath.Join(root, "home")
			for _, dir := range []string{work, home, filepath.Join(home, ".codex")} {
				if err := os.MkdirAll(dir, 0700); err != nil {
					t.Fatal(err)
				}
			}
			st := agentauth.NewStore(filepath.Join(root, "control-plane"))
			if credentialPath != "" {
				b, err := os.ReadFile(credentialPath)
				if err != nil {
					t.Fatal(err)
				}
				rel, _ := agentauth.CredentialFile(name)
				if err := st.ImportCredential(name, rel, b); err != nil {
					t.Fatal(err)
				}
			}
			handler := authproxy.New(st, nil)
			proxy := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				started := time.Now()
				t.Logf("provider request: %s %s", r.Method, r.URL.Path)
				handler.ServeHTTP(w, r)
				t.Logf("provider request finished after %s", time.Since(started).Round(time.Millisecond))
			}))
			defer proxy.Close()
			t.Setenv("RUNTIMED_ANTHROPIC_PROXY", proxy.URL)
			t.Setenv("HOME", home)
			t.Setenv("CODEX_HOME", filepath.Join(home, ".codex"))
			t.Setenv("CLAUDE_CONFIG_DIR", filepath.Join(home, ".claude"))
			t.Setenv("XDG_CONFIG_HOME", filepath.Join(home, "config"))
			t.Setenv("XDG_DATA_HOME", filepath.Join(home, "data"))
			t.Setenv("XDG_CACHE_HOME", filepath.Join(home, "cache"))
			t.Setenv("XDG_STATE_HOME", filepath.Join(home, "state"))
			t.Setenv("OPENCODE_DISABLE_AUTOUPDATE", "true")
			model := os.Getenv(variable + "_MODEL")
			if name == "opencode" && model == "" {
				model = "big-pickle"
			}
			if name == "codex" || (name == "opencode" && credentialPath == "") {
				catalog, err := authproxy.New(st, nil).Models(context.Background(), name)
				if err != nil {
					t.Fatal(err)
				}
				found := false
				for _, entry := range catalog.Models {
					if entry.ID == model {
						found = true
					}
				}
				if !found {
					t.Fatalf("selected model %q was not returned by this account", model)
				}
				t.Logf("authenticated account catalog returned %d selectable models", len(catalog.Models))
			}
			var connection *modelconfig.Connection
			if name == "pi" || (name == "opencode" && credentialPath != "") {
				var err error
				connection, err = handler.BuiltinConnection(context.Background(), name, model, proxy.URL)
				if err != nil {
					t.Fatal(err)
				}
				model = connection.Model.ID
			}
			a, err := selectAgent(name, slog.New(slog.NewTextHandler(io.Discard, nil)))
			if err != nil {
				t.Fatal(err)
			}
			var diagnostics bytes.Buffer
			for turn, prompt := range []string{
				"Create greeting.txt containing exactly hello from sandboxd. Do not read anything outside this workspace. Then reply Done.",
				"Update greeting.txt to contain exactly hello again from sandboxd. Then reply Done.",
			} {
				ctx, cancel := context.WithTimeout(context.Background(), 3*time.Minute)
				final, usage, err := a.run(ctx, agentSpec{workDir: work, stateDir: filepath.Join(root, "runtime"), provider: connection, model: model, prompt: prompt, cont: turn > 0, rawLog: &diagnostics}, func(string, any) {})
				contextErr := ctx.Err()
				cancel()
				if err != nil || contextErr != nil {
					t.Fatalf("turn %d: %v (context %v); diagnostics: %s", turn, err, contextErr, diagnostics.String())
				}
				expected := "hello from sandboxd"
				if turn > 0 {
					expected = "hello again from sandboxd"
				}
				content, err := os.ReadFile(filepath.Join(work, "greeting.txt"))
				if err != nil || strings.TrimSpace(string(content)) != expected {
					t.Fatalf("turn %d did not perform requested edit; err=%v final=%q", turn, err, final)
				}
				t.Logf("turn %d wrote expected file; input=%d output=%d", turn, usage.Input, usage.Output)
			}
		})
	}
}
