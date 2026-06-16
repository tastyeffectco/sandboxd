package store

import (
	"context"
	"testing"
)

// TestTaskTimeoutRoundTrip guards the timeout_s column wiring (migration
// 0012 + the CreateTask insert and the GetTask / ListRunningTasks
// scans): a persisted timeout must survive a read back, so the boot-time
// reconciler can re-attach a watcher with the right streaming window.
func TestTaskTimeoutRoundTrip(t *testing.T) {
	st := openTestStore(t)
	ctx := context.Background()

	if err := st.CreateTask(ctx, &Task{
		TaskID: "01TASKTIMEOUT0000000000001", SandboxID: "01SBX00000000000000000001",
		Agent: "opencode", Prompt: "p", TimeoutS: 3600,
	}); err != nil {
		t.Fatalf("create task: %v", err)
	}

	got, err := st.GetTask(ctx, "01TASKTIMEOUT0000000000001")
	if err != nil {
		t.Fatalf("get task: %v", err)
	}
	if got.TimeoutS != 3600 {
		t.Errorf("GetTask timeout_s = %d; want 3600", got.TimeoutS)
	}

	running, err := st.ListRunningTasks(ctx)
	if err != nil {
		t.Fatalf("list running: %v", err)
	}
	if len(running) != 1 || running[0].TimeoutS != 3600 {
		t.Errorf("ListRunningTasks timeout_s = %+v; want one row with 3600", running)
	}

	// Default stays 0 when unset (omitted timeout_s → runtimed default).
	if err := st.CreateTask(ctx, &Task{
		TaskID: "01TASKNOTIMEOUT00000000001", SandboxID: "01SBX00000000000000000001",
		Agent: "opencode", Prompt: "p",
	}); err != nil {
		t.Fatalf("create task 2: %v", err)
	}
	got2, _ := st.GetTask(ctx, "01TASKNOTIMEOUT00000000001")
	if got2.TimeoutS != 0 {
		t.Errorf("default timeout_s = %d; want 0", got2.TimeoutS)
	}
}
