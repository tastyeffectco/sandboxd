# runtimed — in-sandbox supervisor

`runtimed` runs **inside every sandbox container** as its main process (under
`tini`). It supervises the app's dev server and executes coding tasks, so the
preview survives idle→wake and task execution has a stable owner. The control plane
(`sandboxd`) talks to it over a Unix domain socket on the workspace mount;
`runtimed` is never network-reachable.

## What it does

**Dev-server supervision.** Starts the app's dev server (`RUNTIMED_DEV_CMD`, default
`pnpm dev`) in its own process group, restarts it with exponential backoff on
unexpected exit, and after repeated fast failures stops restarting and reports the
preview `down` rather than crash-looping. It reads the app's `sandbox.yaml`
[manifest](../../../docs/sandbox-manifest.md) for the dev command + preview port.

**Health probing.** Polls the dev-server port and derives `preview.status`
(`down` / `starting` / `ready`).

**Coding tasks.** `POST /tasks`, `GET /tasks/{id}/events`, `POST /tasks/{id}/cancel`
— **one active task at a time** (a concurrent submit gets `409 task_in_progress`).
Per task: a pre-task git checkpoint (the app dir is `git init`-ed on first use) →
run the agent → authoritative `files_changed` from `git diff` against the checkpoint
→ a build check → the canonical `runtime.TaskResult`.

**Agents.** Adapters for **opencode** (default), **claude-code**, and **codex** —
each drives the CLI with `--dangerously-skip-permissions` and parses its stream into
canonical `message` events. Provider credentials never enter the sandbox; the
control-plane proxy injects them on the wire (see [agent-auth](../../../docs/agent-auth.md)).

**Events.** A monotonic stream — `status` / `message` / `build` / `done` — appended
to `.runtimed/tasks/<id>/events.jsonl` and streamed live (NDJSON, resumable via
`?since=`). `done` is the single terminal event and carries the result.

**Persistence + recovery.** Each task keeps `.runtimed/tasks/<id>/` with
`events.jsonl`, `result.json`, `agent.log`. On boot, a task with an event log but no
`result.json` (interrupted by a stop/crash) is finalized `failed` — never resumed.

**Cancellation & timeout.** Cancel kills the agent's process group and finalizes
`cancelled`; a timeout is a runtimed-initiated cancellation (`failed` /
`agent_timeout`).

## Control surface

`GET /status` (→ `runtime.Status`) and the `/tasks` endpoints are served over
HTTP/1.1 on a Unix socket at `/home/sandbox/.runtimed/sock`. The socket lives on the
durable workspace, so `sandboxd` reaches the same inode on the host. No network
port; no cross-tenant reachability. The shared protocol types and the sandboxd-side
`runtime.Client` live in `control-plane/internal/runtime`.

## Configuration (environment)

| Variable | Default |
|---|---|
| `RUNTIMED_APP_DIR` | `/home/sandbox/workspace/app` |
| `RUNTIMED_DIR` | `/home/sandbox/.runtimed` |
| `RUNTIMED_SOCKET` | `<RUNTIMED_DIR>/sock` |
| `RUNTIMED_DEV_CMD` | `pnpm dev` |
| `RUNTIMED_PREVIEW_PORT` | `3000` |
| `RUNTIMED_PROBE_INTERVAL_SECONDS` | `3` |

The control plane also passes the selected runtime preset and the agent-proxy URL
via env at container create.

## Build

```sh
CGO_ENABLED=0 go build -o runtimed ./cmd/runtimed
```

Pure Go, statically linked. It's compiled into the sandbox base image by
`image/build.sh` as the container's `CMD` under `tini`, so every sandbox boots it
automatically — there is no `docker exec`-started dev server.

## Not implemented (by design)

- **Full task event-log retention past destroy** — only the canonical *result* is
  kept (in the control plane's SQLite); the event *log* lives with the workspace and
  is gone once the sandbox is destroyed.
- **Provider-derived `tool` / `file_change` events** — only `message` events are
  surfaced; `files_changed` is always computed from git.
- **Dev-server restart on dependency changes** — a task that edits `package.json`
  does not yet trigger a dev-server restart.
