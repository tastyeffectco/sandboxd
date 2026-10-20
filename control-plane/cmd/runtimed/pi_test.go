package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/tastyeffectco/sandboxd/control-plane/internal/modelconfig"
)

func TestPiStreamAccountingAndError(t *testing.T) {
	stream := `{"type":"message_update","assistantMessageEvent":{"type":"text_delta","delta":"hello"}}
{"type":"message_end","message":{"role":"assistant","content":[{"type":"text","text":"hello"}],"stopReason":"stop","usage":{"input":3,"output":2,"cacheRead":1,"cacheWrite":4,"totalTokens":10,"cost":{"total":0.02}}}}
{"type":"agent_settled","aborted":false}
`
	var events int
	final, usage, err := parsePiStream(strings.NewReader(stream), func(string, any) { events++ })
	if err != nil || final != "hello" || events != 1 || usage.Total != 10 || usage.CacheWrite != 4 || usage.Cost != 0.02 {
		t.Fatalf("incorrect Pi result: %s %+v events=%d %v", final, usage, events, err)
	}
	for _, bad := range []string{
		`{"type":"message_end","message":{"role":"assistant","content":[],"stopReason":"error","errorMessage":"invalid model"}}
{"type":"agent_settled","aborted":false}`,
		`{"type":"agent_settled","aborted":true}`,
		`{"type":"session"}`,
	} {
		if _, _, err := parsePiStream(strings.NewReader(bad), func(string, any) {}); err == nil {
			t.Fatalf("silent success: %s", bad)
		}
	}
}

func TestProviderAgentConfigsPreserveAliases(t *testing.T) {
	for _, protocol := range []string{"openai-completions", "anthropic-messages"} {
		t.Run(protocol, func(t *testing.T) {
			conn := &modelconfig.Connection{ID: "connection", Protocol: protocol, BaseURL: "http://127.0.0.1:9100/providers/connection/v1",
				Token: "temporary-task-capability", Model: modelconfig.Model{ID: "team/model:latest", ContextWindow: 8192, MaxTokens: 2048}}
			path, model, err := writeOpencodeConnection(conn)
			if err != nil {
				t.Fatal(err)
			}
			defer os.Remove(path)
			if model != "sandboxd/team/model:latest" {
				t.Fatal("lost model namespace", model)
			}
			var cfg map[string]json.RawMessage
			raw, err := os.ReadFile(path)
			if err != nil || json.Unmarshal(raw, &cfg) != nil {
				t.Fatal("bad OpenCode config", err)
			}
			if !strings.Contains(string(raw), `"team/model:latest"`) {
				t.Fatal("lost exact model ID")
			}
			info, _ := os.Stat(path)
			if info.Mode().Perm() != 0600 {
				t.Fatal("publicly readable config")
			}
			dir, err := writePiConnection(conn)
			if err != nil {
				t.Fatal(err)
			}
			defer os.RemoveAll(dir)
			raw, err = os.ReadFile(filepath.Join(dir, "models.json"))
			if err != nil {
				t.Fatal(err)
			}
			wantBase := conn.BaseURL
			if protocol == "anthropic-messages" {
				wantBase = strings.TrimSuffix(wantBase, "/v1")
			}
			if !strings.Contains(string(raw), wantBase) || !strings.Contains(string(raw), `"team/model:latest"`) {
				t.Fatal("incorrect Pi model/base URL")
			}
		})
	}
}
