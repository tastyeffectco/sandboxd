# Managed agent auth (Phase 10B)

How sandboxd gives a sandbox's coding agent its credentials **without** putting
them in the workspace, snapshots, container env, logs, events, or task results.

> **Status: import + adapter.** The store + runtime delivery (A1), opaque
> **credential import** for claude-code, and the **Claude Code task adapter**
> exist. The guided in-console login (`setup-token` PTY/xterm) is a later slice —
> the old stdout-scrape automation was removed because claude v2's Ink/TUI
> `setup-token` can't be driven by piped stdin.
>
> **Product model:** the `opencode` provider runs OpenCode; the `claude-code`
> provider runs the Claude Code CLI. Claude credentials are only meaningful for
> `claude-code` — they are never fed to OpenCode.

## How credentials reach the agent

1. Each provider's credentials live in an **opaque** host directory under the
   sandboxd data root: `<DataDir>/agent-auth/<provider>/`. This is **outside**
   any sandbox workspace, so it can never be copied into a workspace or a
   snapshot. sandboxd never opens or parses these files.
2. When a sandbox is created, sandboxd bind-mounts the **selected provider's**
   auth dir (only if it's connected — i.e. the dir is non-empty) to a fixed
   in-container path, **`/run/agent-home`**, and sets `RUNTIMED_AGENT_HOME` so
   runtimed knows where it is. The path is deliberately **not** under
   `/home/sandbox` (the workspace).
3. When runtimed spawns the agent, it points the agent's **`HOME`** at
   `/run/agent-home`, so the CLI finds its credential files there.

The selected provider is `SANDBOXD_DEFAULT_AGENT` (default `opencode`). It must
match the agent runtimed actually runs (opencode today).

## Env scrub

The agent process env is **scrubbed** of secret-shaped variables
(`*_KEY`, `*_TOKEN`, `*_SECRET`, `*_PASSWORD`, `*_CREDENTIALS`, and runtimed's
own `RUNTIMED_*`). Two consequences:

- An agent can never pick up a credential that happens to be in the container
  env. **Credentials are delivered only as files under `HOME`, never as env.**
- **Container-env API keys are no longer used by the agent.** Setting a key via
  the sandbox create `env` (e.g. `ANTHROPIC_API_KEY`) no longer reaches the
  agent — populate the managed auth dir instead.

Non-secret config (`PATH`, `HOME`, `LANG`, `*_MODEL`, `*_BASE_URL`, …) is kept.

## What's guaranteed (verified)

Credentials are **not** present in: the workspace, snapshots (which copy only the
workspace tree), the container env / `docker inspect` (only the *path*
`RUNTIMED_AGENT_HOME=/run/agent-home` appears), task results, events, or logs.

## Known limitations (owner-operated mode)

- **Same-container isolation is not guaranteed yet.** The agent runs inside the
  sandbox, so during a task the credential files exist on that container's
  filesystem. A future same-container terminal/process could read them. The
  terminal feature and a per-task ephemeral copy (tmpfs in/out) that closes this
  gap are deferred to **A3**.
- **Auth changes require a sandbox recreate.** The mount is fixed at create time,
  so connecting or changing a provider's auth takes effect on the **next**
  sandbox (re)create, not on an already-running one. A3's per-task copy removes
  this constraint.

## Connecting Claude Code: credential import

Until the guided login lands, claude-code is connected by **importing an existing
credential**:

1. On a machine where you've signed in to Claude Code (`claude setup-token` or a
   normal login), copy the contents of `~/.claude/.credentials.json`.
2. Console → AI Agents → **Import Claude credentials** → paste it. (Or
   `POST /v1/agents/claude-code/import` with `{"credentials":"<contents>"}`.)
3. sandboxd writes the bytes **verbatim** (opaque, never parsed) to a staging
   dir at `.claude/.credentials.json`, then atomically **promotes** it to
   `<DataDir>/agent-auth/claude-code/`.
4. `GET /v1/agents` reports `claude-code: connected, runnable: true`.

**Disconnect** (`POST /v1/agents/claude-code/disconnect`) deletes the auth dir.
The imported credential is never logged, echoed back, or emitted in events.

## Runnable vs connected

`GET /v1/agents` reports `runnable` per provider — whether runtimed has a task
adapter for it. A provider that is `connected` but **not** `runnable` shows
"credentials imported, runner not enabled yet". With the Claude Code adapter,
`claude-code` is runnable.

## Running tasks as Claude Code

- Create the sandbox with `SANDBOXD_DEFAULT_AGENT=claude-code` so A1 mounts the
  claude-code auth dir as the agent's `HOME` (`/run/agent-home`).
- Submit a task with `agent: "claude-code"`. runtimed runs
  `claude -p <prompt> --output-format stream-json --verbose --dangerously-skip-permissions`
  with `HOME` on the mounted creds, and maps the stream to task events + a final
  result.

## Live verification checklist (real Claude subscription)

Tested with a **fake claude** (no subscription). Validate the real flow:

- [ ] Import `~/.claude/.credentials.json` → `GET /v1/agents` shows
      `claude-code: connected, runnable: true`; the file exists under
      `<DataDir>/agent-auth/claude-code/.claude/.credentials.json`.
- [ ] Create a sandbox with `SANDBOXD_DEFAULT_AGENT=claude-code`; submit a task
      with `agent:"claude-code"` → the Claude Code CLI runs on your subscription
      and the task produces a final result.
- [ ] Token is **absent** from: the workspace, a snapshot, `docker inspect` env,
      logs, events, and task results.
- [ ] **Disconnect** → the dir is gone; `GET /v1/agents` shows `needs_login`.
