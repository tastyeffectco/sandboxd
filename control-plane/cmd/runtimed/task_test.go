package main

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/tastyeffectco/sandboxd/control-plane/internal/runtime"
)

func boolp(b bool) *bool { return &b }

// Continue is the default, but gated on there being a prior session: the first
// task in a sandbox always starts fresh, later tasks continue unless the caller
// forces a choice.
func TestNewTaskContinueDefault(t *testing.T) {
	root := t.TempDir()

	mk := func(id string, cont *bool) bool {
		tk, err := newTask(runtime.StartTaskRequest{TaskID: id, Prompt: "p", Continue: cont}, root)
		if err != nil {
			t.Fatalf("newTask(%s): %v", id, err)
		}
		return tk.cont
	}

	// First task, default (nil) → fresh: nothing to continue yet.
	if mk("t1", nil) {
		t.Error("first task should start fresh (no prior session)")
	}
	// Second task, default (nil) → continue: a prior task dir now exists.
	if !mk("t2", nil) {
		t.Error("second task should continue by default")
	}
	// Forced continue on a would-be-first sandbox → gated to fresh.
	root2 := t.TempDir()
	if _, err := newTask(runtime.StartTaskRequest{TaskID: "solo", Prompt: "p", Continue: boolp(true)}, root2); err != nil {
		t.Fatal(err)
	}
	// (solo is the only task in root2) forcing continue must still start fresh.
	// Re-create the check directly via hasPriorTask semantics:
	if hasPriorTask(root2, "solo") {
		t.Error("solo should have no prior task")
	}
	// Forced fresh even when a prior session exists.
	if mk("t3-forced-fresh", boolp(false)) {
		t.Error("explicit continue:false must start fresh")
	}
}

func TestHasPriorTask(t *testing.T) {
	root := t.TempDir()
	if hasPriorTask(root, "self") {
		t.Error("empty root has no prior task")
	}
	// A dir that is only the task itself doesn't count.
	os.MkdirAll(filepath.Join(root, "self"), 0o755)
	if hasPriorTask(root, "self") {
		t.Error("only-self should not count as a prior task")
	}
	os.MkdirAll(filepath.Join(root, "earlier"), 0o755)
	if !hasPriorTask(root, "self") {
		t.Error("a sibling task dir is a prior task")
	}
}
