package api

import (
	"context"
	"encoding/json"
	"io"
	"os"
	"path/filepath"
	"time"

	"github.com/tastyeffectco/sandboxd/control-plane/internal/events"
	"github.com/tastyeffectco/sandboxd/control-plane/internal/runtime"
)

const (
	// watchMargin is the slack the watcher keeps beyond the task's own
	// timeout, so it is still streaming when runtimed emits the terminal
	// `done` event — which, on a timeout, fires right at the deadline.
	watchMargin       = 5 * time.Minute
	taskWatchAttempts = 3
)

// watchWindowFor is how long watchTask streams events before giving up.
// It MUST outlive the runtimed task timeout (the 10m default, or the
// caller's timeout_s): a shorter window aborts the stream and marks a
// still-running task failed. Earlier this was a fixed 15m, which
// quietly capped any task whose timeout_s exceeded ~15m.
func watchWindowFor(timeoutS int) time.Duration {
	eff := runtime.DefaultTaskTimeout
	if timeoutS > 0 {
		eff = time.Duration(timeoutS) * time.Second
	}
	return eff + watchMargin
}

// failedResult builds a clean terminal result for a task that could
// not complete normally. failure_reason is always an existing model
// value (sandbox_unavailable / internal) — no new terminal semantics
// are introduced.
func failedResult(taskID, reason, msg string) *runtime.TaskResult {
	return &runtime.TaskResult{
		ID:            taskID,
		Status:        runtime.TaskFailed,
		FailureReason: reason,
		ErrorMessage:  msg,
		FilesChanged:  []string{},
	}
}

// watchTask runs in the background for the lifetime of a coding task:
// it streams runtimed's event log and persists the canonical result
// to SQLite when the terminal `done` event arrives — independent of
// whether any client ever connects to the public events stream. This
// is what makes a task's result durable past the sandbox's lifetime.
// watchTask streams a task's events under a window derived from its
// timeout (taskTimeoutS; 0 = the runtimed default). See watchWindowFor.
func (s *Server) watchTask(sandboxID, taskID string, taskTimeoutS int) {
	s.watchTaskWindow(sandboxID, taskID, watchWindowFor(taskTimeoutS))
}

// watchTaskWindow is watchTask with an explicit streaming window. Split
// out so tests can inject a short window instead of waiting minutes.
func (s *Server) watchTaskWindow(sandboxID, taskID string, window time.Duration) {
	log := s.Log.With("component", "taskwatch", "task", taskID)
	ctx, cancel := context.WithTimeout(context.Background(), window)
	defer cancel()

	rc := s.runtimeClientFor(sandboxID)
	var body io.ReadCloser
	var err error
	for attempt := 1; ; attempt++ {
		if body, err = rc.TaskEvents(ctx, taskID, 0); err == nil {
			break
		}
		if attempt >= taskWatchAttempts {
			log.Warn("task watcher: cannot reach runtimed; marking failed", "err", err.Error())
			s.finishWatchedTask(sandboxID, taskID, failedResult(taskID, "sandbox_unavailable",
				"task watcher could not reach runtimed"))
			return
		}
		select {
		case <-time.After(2 * time.Second):
		case <-ctx.Done():
			return
		}
	}
	defer body.Close()

	var result *runtime.TaskResult
	_ = runtime.DecodeEvents(body, func(ev runtime.Event) bool {
		if ev.Type == runtime.EventDone {
			var tr runtime.TaskResult
			if json.Unmarshal(ev.Data, &tr) == nil {
				result = &tr
			}
			return false // terminal
		}
		return true
	})
	if result == nil {
		log.Warn("task watcher: event stream ended without a terminal event")
		s.finishWatchedTask(sandboxID, taskID, failedResult(taskID, "internal",
			"task event stream ended without a terminal event"))
		return
	}
	s.finishWatchedTask(sandboxID, taskID, result)
	log.Info("task watcher: result persisted", "status", result.Status)

}

func (s *Server) finishWatchedTask(sandboxID, taskID string, result *runtime.TaskResult) {
	raw, _ := json.Marshal(result)
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := s.Store.FinishTask(ctx, taskID, string(result.Status), string(raw)); err != nil {
		s.Log.Warn("task watcher: FinishTask failed", "task", taskID, "err", err.Error())
	}
	s.recordTaskEvents(sandboxID, taskID, result)
}

// recordTaskEvents appends the durable timeline entries for a finished task.
// It runs in a background path (no request), so it resolves the owning tenant
// + app from the sandbox itself. Payloads carry reasons/counts only — never
// agent output or secrets.
func (s *Server) recordTaskEvents(sandboxID, taskID string, result *runtime.TaskResult) {
	if s.Events == nil {
		return
	}
	ctx := context.Background()
	ownerToken, appID := "", ""
	if sb, err := s.Store.Get(ctx, sandboxID); err == nil && sb.AppID.Valid {
		appID = sb.AppID.String
		if app, aerr := s.Store.GetApp(ctx, appID); aerr == nil {
			ownerToken = app.OwnerToken
		}
	}
	base := events.Event{OwnerToken: ownerToken, AppID: appID, SandboxID: sandboxID, TaskID: taskID}

	rec := func(typ, sev, msg string, payload map[string]any) {
		e := base
		e.Type, e.Severity, e.Message, e.Payload = typ, sev, msg, payload
		s.Events.Record(ctx, e)
	}

	// Payloads carry safe STRUCTURED flags/reasons only — never the raw
	// build/dev-server/agent output (which can echo secrets the user's app
	// printed). The full text stays in the task's result.json.
	if result.Status == runtime.TaskSucceeded {
		rec(events.TaskCompleted, events.SeverityInfo, "Task completed",
			map[string]any{"files_changed": len(result.FilesChanged), "duration_ms": result.DurationMS, "build_ok": result.BuildOK})
	} else {
		rec(events.TaskFailed, events.SeverityError, "Task failed: "+string(result.Status),
			map[string]any{"failure_reason": result.FailureReason, "has_error": result.ErrorMessage != ""})
	}
	// A real build failure (distinct from infra failures, which leave
	// BuildErrorMessage empty).
	if result.BuildErrorMessage != "" {
		rec(events.TaskBuildFailed, events.SeverityError, "Task build failed",
			map[string]any{"reason": "build_failed", "has_build_error": true})
	}
	// Preview health after the task: report only a clear signal. preview_status
	// is the structured enum (down/starting/ready/error), not raw output.
	switch {
	case result.PreviewErrorMessage != "" || result.PreviewStatusAfter == runtime.PreviewError:
		rec(events.PreviewHealthFailed, events.SeverityWarning, "Preview unhealthy after task",
			map[string]any{"preview_status": string(result.PreviewStatusAfter), "has_preview_error": result.PreviewErrorMessage != ""})
	case result.PreviewStatusAfter == runtime.PreviewReady:
		rec(events.PreviewHealthOK, events.SeverityInfo, "Preview healthy after task", nil)
	}
}

// ReconcileTasks finalizes tasks left `running` by a previous sandboxd
// run (whose watcher goroutines did not survive the restart). Run once
// at boot, before the idle reaper — which trusts the task table —
// begins ticking. Per running row:
//   - runtimed already wrote result.json -> finalize from it;
//   - else the sandbox is still running -> re-attach a watcher;
//   - else -> finalize as a clean sandbox_unavailable failure.
//
// An interrupted task therefore lands on the existing failure model
// (status=failed, failure_reason=sandbox_unavailable) — no new
// terminal state is introduced.
func (s *Server) ReconcileTasks(ctx context.Context) {
	tasks, err := s.Store.ListRunningTasks(ctx)
	if err != nil {
		s.Log.Warn("task reconcile: list running tasks failed", "err", err.Error())
		return
	}
	for _, t := range tasks {
		_, mnt := s.Loopback.Paths(t.SandboxID)
		resultPath := filepath.Join(mnt, ".runtimed", "tasks", t.TaskID, "result.json")
		if raw, rerr := os.ReadFile(resultPath); rerr == nil {
			var tr runtime.TaskResult
			if json.Unmarshal(raw, &tr) == nil && tr.Status != "" {
				s.finishWatchedTask(t.SandboxID, t.TaskID, &tr)
				s.Log.Info("task reconcile: finalized from runtimed result.json",
					"task", t.TaskID, "status", tr.Status)
				continue
			}
		}
		if sb, gerr := s.Store.Get(ctx, t.SandboxID); gerr == nil && sb.Status == "running" {
			s.Log.Info("task reconcile: sandbox running — re-attaching watcher", "task", t.TaskID)
			go s.watchTask(t.SandboxID, t.TaskID, t.TimeoutS)
			continue
		}
		s.finishWatchedTask(t.SandboxID, t.TaskID, failedResult(t.TaskID, "sandbox_unavailable",
			"task interrupted by a sandboxd restart; the sandbox was unavailable to resume or report it"))
		s.Log.Info("task reconcile: finalized interrupted task", "task", t.TaskID)
	}
}
