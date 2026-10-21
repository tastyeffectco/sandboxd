package main

import (
	"github.com/tastyeffectco/sandboxd/control-plane/internal/modelconfig"
	"testing"
)

func TestContinuationRequiresThisAgentConnectionAndModel(t *testing.T) {
	root := t.TempDir()
	first := &task{agentName: "opencode", model: "account-coder", cont: true}
	if canContinue(root, first) {
		t.Fatal("first task must start fresh")
	}
	if err := rememberSession(root, first); err != nil {
		t.Fatal(err)
	}
	if !canContinue(root, first) {
		t.Fatal("same conversation should resume")
	}
	for _, different := range []*task{
		{agentName: "claude-code", model: "account-coder", cont: true},
		{agentName: "opencode", model: "other-model", cont: true},
		{agentName: "opencode", model: "account-coder", provider: &modelconfig.Connection{ID: "different-account"}, cont: true},
		{agentName: "opencode", model: "account-coder", cont: false},
	} {
		if canContinue(root, different) {
			t.Fatalf("must start fresh: %+v", different)
		}
	}
}
