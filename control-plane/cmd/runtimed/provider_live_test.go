package main

import (
	"bytes"
	"context"
	"io"
	"log/slog"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/tastyeffectco/sandboxd/control-plane/internal/modelprovider"
	"github.com/tastyeffectco/sandboxd/control-plane/internal/secrets"
	"github.com/tastyeffectco/sandboxd/control-plane/internal/store"
)

// Opt-in semantic smoke test against an operator-supplied live gateway exposing
// BOTH API formats. Unlike the deterministic CI fixtures, a real model chooses
// and executes the tool here. Never enabled implicitly: remote calls may cost.
func TestLiveCompatibleProviders(t *testing.T) {
	base := os.Getenv("SANDBOXD_TEST_LIVE_PROVIDER_URL")
	model := os.Getenv("SANDBOXD_TEST_LIVE_MODEL")
	if base == "" || model == "" {
		t.Skip("set SANDBOXD_TEST_LIVE_PROVIDER_URL and SANDBOXD_TEST_LIVE_MODEL explicitly")
	}
	key := os.Getenv("SANDBOXD_TEST_LIVE_API_KEY")
	for _, agentName := range []string{"opencode", "pi"} {
		for _, protocol := range []string{modelprovider.OpenAI, modelprovider.Anthropic} {
			t.Run(agentName+"/"+protocol, func(t *testing.T) {
				root := t.TempDir()
				work := filepath.Join(root, "app")
				if err := os.Mkdir(work, 0o700); err != nil {
					t.Fatal(err)
				}
				t.Setenv("XDG_CONFIG_HOME", filepath.Join(root, "config"))
				t.Setenv("XDG_CACHE_HOME", filepath.Join(root, "cache"))
				t.Setenv("XDG_STATE_HOME", filepath.Join(root, "state"))
				t.Setenv("OPENCODE_DISABLE_MODELS_FETCH", "true")
				t.Setenv("OPENCODE_DISABLE_AUTOUPDATE", "true")
				st, err := store.Open(context.Background(), filepath.Join(root, "db.sqlite"), "../../migrations")
				if err != nil {
					t.Fatal(err)
				}
				defer st.Close()
				cipher, err := secrets.Load("", filepath.Join(root, "key"))
				if err != nil {
					t.Fatal(err)
				}
				service, err := modelprovider.New(st, cipher)
				if err != nil {
					t.Fatal(err)
				}
				config, err := service.Save(context.Background(), "", modelprovider.Input{
					Name: "Explicit live test", Protocol: protocol, BaseURL: base, APIKey: &key,
					Models: []modelprovider.Model{{ID: model, ContextWindow: 16384, MaxTokens: 512}},
				})
				if err != nil {
					t.Fatal(err)
				}
				proxy := httptest.NewServer(service)
				defer proxy.Close()
				connection, err := service.Connection(context.Background(), config.ID, model, proxy.URL, 11*time.Minute)
				if err != nil {
					t.Fatal(err)
				}
				agent, err := selectAgent(agentName, slog.New(slog.NewTextHandler(io.Discard, nil)))
				if err != nil {
					t.Fatal(err)
				}
				ctx, cancel := context.WithTimeout(context.Background(), 10*time.Minute)
				defer cancel()
				var diagnostics bytes.Buffer
				final, usage, err := agent.run(ctx, agentSpec{
					workDir: work, stateDir: filepath.Join(root, "runtime"), provider: connection, model: model,
					prompt: "Use the write tool to create greeting.txt containing exactly the text: hello from sandboxd\nDo not inspect any other files. After creating it, reply Done.",
					rawLog: &diagnostics,
				}, func(string, any) {})
				if ctx.Err() != nil || err != nil {
					t.Fatalf("live task failed: context=%v error=%v stderr=%s", ctx.Err(), err, diagnostics.String())
				}
				content, err := os.ReadFile(filepath.Join(work, "greeting.txt"))
				if err != nil || strings.TrimSpace(string(content)) != "hello from sandboxd" {
					t.Fatalf("live model did not perform the requested edit: content=%q err=%v final=%q", content, err, final)
				}
				t.Logf("real model wrote greeting.txt; input=%d output=%d final=%q", usage.Input, usage.Output, final)
			})
		}
	}
}
