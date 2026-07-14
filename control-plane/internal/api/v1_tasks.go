package api

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"path/filepath"
	"strconv"
	"time"

	"github.com/tastyeffectco/sandboxd/control-plane/internal/agentauth"
	"github.com/tastyeffectco/sandboxd/control-plane/internal/audit"
	"github.com/tastyeffectco/sandboxd/control-plane/internal/events"
	"github.com/tastyeffectco/sandboxd/control-plane/internal/runtime"
	"github.com/tastyeffectco/sandboxd/control-plane/internal/store"
)

// runtimeClientFor builds a runtime.Client for a sandbox's runtimed.
func (s *Server) runtimeClientFor(id string) *runtime.Client {
	_, mnt := s.Loopback.Paths(id)
	return runtime.NewClient(filepath.Join(mnt, ".runtimed", "sock"))
}

// --- POST /v1/sandboxes/{id}/tasks ----------------------------------

type v1TaskSubmitReq struct {
	Prompt string `json:"prompt"`
	Agent  string `json:"agent,omitempty"`
	// Model, when set, runs this task on the given model (passed to the agent
	// CLI's --model). Agent-namespaced: opencode wants "provider/model" (e.g.
	// "opencode/claude-sonnet-4-5"), claude an alias/id ("sonnet"). Empty = the
	// agent's default. Unknown/unavailable models fail the task at the agent.
	Model string `json:"model,omitempty"`
	// TimeoutS sets the maximum task runtime in seconds.
	// 0 or omitted means use the runtimed default (10m).
	TimeoutS int `json:"timeout_s,omitempty"`
	// Continue resumes the sandbox's most recent agent session instead of a fresh
	// one (works for all agents: claude/opencode --continue, codex resume --last).
	// Tri-state: omitted = default (continue when a prior session exists, else
	// fresh); true/false force the choice.
	Continue *bool `json:"continue,omitempty"`
}

func (s *Server) v1SubmitTask(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	sb, err := s.Store.Get(r.Context(), id)
	if errors.Is(err, store.ErrNotFound) {
		writeV1Err(w, http.StatusNotFound, "not_found", "no such sandbox")
		return
	}
	if err != nil {
		writeV1Err(w, http.StatusInternalServerError, "internal", err.Error())
		return
	}

	// B1 — wake-on-task-submit: a stopped sandbox is woken first by
	// delegating to the proven internal wake path. (A private sandbox
	// whose wake path expects a preview-token cookie is not covered —
	// see the runtimed README "NOT implemented yet".)
	if sb.Status == "stopped" {
		code, body := s.delegate(r, s.handleWakeJSON, http.MethodPost, "/wake/"+id,
			map[string]string{"id": id}, nil)
		if code != http.StatusOK {
			relayV1Error(w, code, body) // 503 -> sandbox_capacity, etc.
			return
		}
		if sb, err = s.Store.Get(r.Context(), id); err != nil {
			writeV1Err(w, http.StatusInternalServerError, "internal", err.Error())
			return
		}
	}
	if sb.Status != "running" {
		writeV1Err(w, http.StatusConflict, "conflict",
			"sandbox is "+sb.Status+" — cannot run a task")
		return
	}

	var req v1TaskSubmitReq
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeV1Err(w, http.StatusBadRequest, "invalid_request", "invalid json: "+err.Error())
		return
	}
	if req.Prompt == "" {
		writeV1Err(w, http.StatusBadRequest, "invalid_request", "prompt is required")
		return
	}
	agent := req.Agent
	if agent == "" {
		agent = s.DefaultAgent // operator default (SANDBOXD_DEFAULT_AGENT)
	}
	if agent == "" {
		agent = "opencode"
	}
	// Only providers runtimed can actually run as a task agent are accepted.
	// An explicit agent is honored regardless of the default; the provider's
	// creds are mounted at sandbox create if connected. See docs/agent-auth.md.
	if !agentauth.Runnable(agent) {
		writeV1Err(w, http.StatusBadRequest, "invalid_request",
			"unsupported agent (supported: opencode, claude-code)")
		return
	}
	if req.TimeoutS < 0 {
		writeV1Err(w, http.StatusBadRequest, "invalid_request",
			"timeout_s must be >= 0")
		return
	}
	const maxTimeoutS = 86400 // 24h
	if req.TimeoutS > maxTimeoutS {
		writeV1Err(w, http.StatusBadRequest, "invalid_request",
			fmt.Sprintf("timeout_s must be <= %d", maxTimeoutS))
		return
	}
	// Model is opaque + agent-namespaced; sandboxd only length-bounds it and
	// leaves validity to the agent (which fails the task on an unknown model).
	if len(req.Model) > 200 {
		writeV1Err(w, http.StatusBadRequest, "invalid_request", "model too long")
		return
	}

	taskID := newULID()
	if err := s.runtimeClientFor(id).StartTask(r.Context(), runtime.StartTaskRequest{
		TaskID: taskID, Prompt: req.Prompt, Agent: agent, Model: req.Model, TimeoutS: req.TimeoutS, Continue: req.Continue,
	}); err != nil {
		if errors.Is(err, runtime.ErrTaskInProgress) {
			writeV1Err(w, http.StatusConflict, "task_in_progress", "a task is already in progress")
			return
		}
		writeV1Err(w, http.StatusBadGateway, "sandbox_unavailable", "runtimed: "+err.Error())
		return
	}

	// B2 — persist the durable task row; B3 — start the result watcher.
	if err := s.Store.CreateTask(r.Context(), &store.Task{
		TaskID: taskID, SandboxID: id, Agent: agent, Prompt: req.Prompt,
		Status:         "running",
		TimeoutS:       req.TimeoutS,
		ExternalUserID: sb.ExternalUserID, ExternalProjectID: sb.ExternalProjectID,
	}); err != nil {
		// The task is running in runtimed but the row failed to write.
		// The task still proceeds; GET would 404 until reconciled.
		s.loggerFor(r, id).Error("v1 task: CreateTask failed", "task", taskID, "err", err.Error())
	} else {
		go s.watchTask(id, taskID, req.TimeoutS)
	}

	s.auditAction(r, audit.Entry{
		Action: "task.create", Target: id,
		Detail: map[string]any{"task_id": taskID, "agent": agent},
	})
	s.recordEvent(r, events.Event{Type: events.TaskStarted, Severity: events.SeverityInfo,
		Message: "Task started", AppID: sb.AppID.String, SandboxID: id, TaskID: taskID,
		Payload: map[string]any{"agent": agent}})
	writeJSON(w, http.StatusAccepted, map[string]any{
		"id":         taskID,
		"sandbox_id": id,
		"status":     "running",
		"agent":      agent,
		"events_url": fmt.Sprintf("/v1/sandboxes/%s/tasks/%s/events", id, taskID),
	})
}

// --- GET /v1/sandboxes/{id}/tasks/{taskId} --------------------------

// v1Task is the canonical task result: runtime.TaskResult plus the
// owning sandbox id (the embedded struct's json tags are promoted).
type v1Task struct {
	SandboxID string `json:"sandbox_id"`
	runtime.TaskResult
}

// v1GetTask reads the canonical result from sandboxd's durable task
// store — so it works whether or not the sandbox is still running, and
// after the sandbox has been destroyed.
func (s *Server) v1GetTask(w http.ResponseWriter, r *http.Request) {
	id, taskID := r.PathValue("id"), r.PathValue("taskId")
	t, err := s.Store.GetTask(r.Context(), taskID)
	if errors.Is(err, store.ErrNotFound) {
		writeV1Err(w, http.StatusNotFound, "not_found", "no such task")
		return
	}
	if err != nil {
		writeV1Err(w, http.StatusInternalServerError, "internal", err.Error())
		return
	}
	if t.SandboxID != id {
		writeV1Err(w, http.StatusNotFound, "not_found", "no such task for that sandbox")
		return
	}
	if t.Status == "running" || !t.ResultJSON.Valid {
		writeJSON(w, http.StatusOK, map[string]any{
			"id": taskID, "sandbox_id": id, "status": "running",
		})
		return
	}
	var tr runtime.TaskResult
	if err := json.Unmarshal([]byte(t.ResultJSON.String), &tr); err != nil {
		writeV1Err(w, http.StatusInternalServerError, "internal",
			"decode stored task result: "+err.Error())
		return
	}
	writeJSON(w, http.StatusOK, v1Task{SandboxID: id, TaskResult: tr})
}

// --- GET /v1/sandboxes/{id}/tasks -----------------------------------

// v1TaskSummary is one row of the task-history list.
type v1TaskSummary struct {
	ID           string   `json:"id"`
	Prompt       string   `json:"prompt,omitempty"`
	Agent        string   `json:"agent,omitempty"`
	Status       string   `json:"status"`
	AgentMessage string   `json:"agent_message,omitempty"` // the agent's final reply, for the chat history
	ErrorMessage string   `json:"error_message,omitempty"` // why a task failed (e.g. agent not connected) — surfaced to the user
	FilesChanged []string `json:"files_changed,omitempty"`
	CheckpointID string   `json:"checkpoint_id,omitempty"`
	CanRevert    bool     `json:"can_revert"` // a checkpoint exists to go back to
	CreatedAt    string   `json:"created_at,omitempty"`
}

// v1ListTasks returns a sandbox's task history (newest first) from the durable
// store — the "go back" list. Each row carries its files_changed and whether it
// can be reverted to (a pre-task checkpoint exists).
func (s *Server) v1ListTasks(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	tasks, err := s.Store.ListTasksForSandbox(r.Context(), id, 100)
	if err != nil {
		writeV1Err(w, http.StatusInternalServerError, "internal", err.Error())
		return
	}
	out := []v1TaskSummary{}
	for _, t := range tasks {
		sum := v1TaskSummary{ID: t.TaskID, Prompt: t.Prompt, Agent: t.Agent, Status: t.Status, CreatedAt: t.CreatedAt.Format(time.RFC3339)}
		if t.ResultJSON.Valid {
			var tr runtime.TaskResult
			if json.Unmarshal([]byte(t.ResultJSON.String), &tr) == nil {
				sum.AgentMessage = tr.AgentMessageFinal
				sum.ErrorMessage = tr.ErrorMessage
				// A failed task (esp. opencode) can finish with no agent reply —
				// fall back to the error so the chat never shows a blank "(failed)".
				if sum.AgentMessage == "" {
					sum.AgentMessage = tr.ErrorMessage
				}
				sum.FilesChanged = tr.FilesChanged
				sum.CheckpointID = tr.CheckpointID
				sum.CanRevert = tr.CheckpointID != ""
			}
		}
		out = append(out, sum)
	}
	writeJSON(w, http.StatusOK, map[string]any{"tasks": out})
}

// --- POST /v1/sandboxes/{id}/tasks/{taskId}/revert ------------------

// v1RevertTask restores the workspace to a task's pre-task checkpoint — undoing
// that task and everything after it. The git restore runs in the workspace via
// runtimed, so the sandbox must be running.
func (s *Server) v1RevertTask(w http.ResponseWriter, r *http.Request) {
	id, taskID := r.PathValue("id"), r.PathValue("taskId")
	sb, err := s.Store.Get(r.Context(), id)
	if errors.Is(err, store.ErrNotFound) {
		writeV1Err(w, http.StatusNotFound, "not_found", "no such sandbox")
		return
	}
	if err != nil {
		writeV1Err(w, http.StatusInternalServerError, "internal", err.Error())
		return
	}
	t, err := s.Store.GetTask(r.Context(), taskID)
	if errors.Is(err, store.ErrNotFound) {
		writeV1Err(w, http.StatusNotFound, "not_found", "no such task")
		return
	}
	if err != nil {
		writeV1Err(w, http.StatusInternalServerError, "internal", err.Error())
		return
	}
	if t.SandboxID != id {
		writeV1Err(w, http.StatusNotFound, "not_found", "no such task for that sandbox")
		return
	}
	if sb.Status != "running" {
		writeV1Err(w, http.StatusConflict, "conflict", "start the sandbox to revert (the restore runs in the workspace)")
		return
	}
	if err := s.runtimeClientFor(id).RevertTask(r.Context(), taskID); err != nil {
		writeV1Err(w, http.StatusBadRequest, "revert_failed", err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"task_id": taskID, "status": "reverted"})
}

// --- GET /v1/sandboxes/{id}/tasks/{taskId}/events (SSE) -------------

func (s *Server) v1TaskEvents(w http.ResponseWriter, r *http.Request) {
	id, taskID := r.PathValue("id"), r.PathValue("taskId")
	since := 0
	if leid := r.Header.Get("Last-Event-ID"); leid != "" {
		if n, err := strconv.Atoi(leid); err == nil {
			since = n + 1 // resume after the last delivered event
		}
	}
	if q := r.URL.Query().Get("since"); q != "" {
		if n, err := strconv.Atoi(q); err == nil && n >= 0 {
			since = n
		}
	}
	body, err := s.runtimeClientFor(id).TaskEvents(r.Context(), taskID, since)
	if err != nil {
		writeV1Err(w, http.StatusBadGateway, "sandbox_unavailable", "runtimed: "+err.Error())
		return
	}
	defer body.Close()

	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	flusher, _ := w.(http.Flusher)
	_ = runtime.DecodeEvents(body, func(ev runtime.Event) bool {
		data := ev.Data
		if len(data) == 0 {
			data = json.RawMessage("{}")
		}
		fmt.Fprintf(w, "id: %d\nevent: %s\ndata: %s\n\n", ev.ID, ev.Type, data)
		if flusher != nil {
			flusher.Flush()
		}
		return r.Context().Err() == nil
	})
}

// --- POST /v1/sandboxes/{id}/tasks/{taskId}/cancel ------------------

func (s *Server) v1CancelTask(w http.ResponseWriter, r *http.Request) {
	id, taskID := r.PathValue("id"), r.PathValue("taskId")
	if err := s.runtimeClientFor(id).CancelTask(r.Context(), taskID); err != nil {
		writeV1Err(w, http.StatusBadGateway, "sandbox_unavailable", "runtimed: "+err.Error())
		return
	}
	s.auditAction(r, audit.Entry{
		Action: "task.cancel", Target: id,
		Detail: map[string]any{"task_id": taskID},
	})
	writeJSON(w, http.StatusOK, map[string]string{"id": taskID, "status": "cancelling"})
}
