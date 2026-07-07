package runtime

import (
	"encoding/json"
	"time"
)

// DefaultTaskTimeout is the runtimed task timeout applied when a task
// submits no explicit timeout_s. It is the single source of truth for
// the default, shared by runtimed (which enforces it) and the
// control-plane task watcher (which must outlive it).
const DefaultTaskTimeout = 10 * time.Minute

// TaskStatus is the lifecycle state of a coding task.
type TaskStatus string

const (
	TaskRunning   TaskStatus = "running"
	TaskSucceeded TaskStatus = "succeeded"
	TaskFailed    TaskStatus = "failed"
	TaskCancelled TaskStatus = "cancelled"
)

// Task event types. status/build/done are runtimed-generated and
// reliable; message/tool are provider-derived and best-effort
// (ops/design/v1-external-api.md §4.4). `done` is the single terminal
// event and carries the TaskResult.
const (
	EventStatus  = "status"
	EventMessage = "message"
	EventTool    = "tool"
	EventBuild   = "build"
	EventDone    = "done"
)

// StartTaskRequest is the POST /tasks body.
type StartTaskRequest struct {
	TaskID string            `json:"task_id"`
	Prompt string            `json:"prompt"`
	Agent  string            `json:"agent,omitempty"` // opencode | claude-code
	Env    map[string]string `json:"env,omitempty"`   // passed to the agent process (credentials)
	// Model, when set, is the model this task runs on — passed to the agent CLI's
	// model flag (opencode -m, claude --model). Agent-namespaced (opencode wants
	// provider/model). Empty = the agent's configured/global default.
	Model    string `json:"model,omitempty"`
	TimeoutS int    `json:"timeout_s,omitempty"`
	// Continue resumes the sandbox's most recent agent session instead of starting
	// fresh (claude/opencode --continue, codex `exec resume --last`). Tri-state:
	// nil (omitted) = the default, which is to continue when the sandbox already
	// has a prior session and start fresh otherwise; true/false force the choice.
	Continue *bool `json:"continue,omitempty"`
}

// Event is one task progress event. Data is type-specific JSON.
type Event struct {
	ID   int             `json:"id"`
	Type string          `json:"type"`
	Time time.Time       `json:"ts"`
	Data json.RawMessage `json:"data,omitempty"`
}

// Build outcomes for TaskResult.BuildStatus.
const (
	BuildPassed  = "passed"
	BuildFailed  = "failed"
	BuildSkipped = "skipped"
)

// TaskResult is the canonical task outcome — carried by the terminal
// `done` event and persisted to result.json. The upstream renders the
// final outcome from this alone, with no event replay.
type TaskResult struct {
	ID                string     `json:"id"`
	Status            TaskStatus `json:"status"`
	FailureReason     string     `json:"failure_reason,omitempty"`
	ErrorMessage      string     `json:"error_message,omitempty"`
	FilesChanged      []string   `json:"files_changed"`
	AgentMessageFinal string     `json:"agent_message_final,omitempty"`
	// BuildOK is kept for backward compatibility; it is true ONLY when
	// BuildStatus is "passed". A skipped build is never build_ok=true.
	BuildOK bool `json:"build_ok"`
	// BuildStatus is the honest build outcome: "passed", "failed", or
	// "skipped" (no build command declared — e.g. the Next.js preset).
	BuildStatus        string        `json:"build_status,omitempty"`
	BuildErrorMessage  string        `json:"build_error_message,omitempty"`
	PreviewStatusAfter PreviewStatus `json:"preview_status_after,omitempty"`
	// PreviewOK is whether the public endpoint is serving after the task.
	// nil/omitted for worker-only apps (no public endpoint).
	PreviewOK *bool `json:"preview_ok,omitempty"`
	// AppHealthy is overall post-task health: build not failed AND
	// (web: preview serving) / (worker-only: a worker process running).
	AppHealthy bool `json:"app_healthy"`
	// PreviewErrorMessage is the live dev-server error when the app
	// fails to render despite a clean build (e.g. a stale-config 500 on
	// the CSS/TS entry). Set by the post-task health pipeline; empty
	// when the preview renders. Distinct from BuildErrorMessage, which
	// is the `pnpm build` output — build_ok can be true while this is
	// set. See cmd/runtimed/health.go.
	PreviewErrorMessage string     `json:"preview_error_message,omitempty"`
	CheckpointID        string     `json:"checkpoint_id,omitempty"`
	DurationMS          int64      `json:"duration_ms"`
	Tokens              TokenUsage `json:"tokens"`
	CreatedAt           time.Time  `json:"created_at"`
	StartedAt           time.Time  `json:"started_at"`
	FinishedAt          time.Time  `json:"finished_at"`
}

// TokenUsage is the model-token accounting for one task — one
// coding-agent session — summed across the agent's steps. Counts are
// always populated; Cost is provider-dependent and is 0 on a
// flat-rate subscription (e.g. the z.ai GLM Coding Plan).
type TokenUsage struct {
	Input      int     `json:"input"`
	Output     int     `json:"output"`
	Reasoning  int     `json:"reasoning"`
	CacheRead  int     `json:"cache_read"`
	CacheWrite int     `json:"cache_write"`
	Total      int     `json:"total"`
	Cost       float64 `json:"cost"`
}

// ActiveTask is the active-task summary embedded in GET /status.
type ActiveTask struct {
	ID     string     `json:"id"`
	Status TaskStatus `json:"status"`
	Phase  string     `json:"phase"`
}
