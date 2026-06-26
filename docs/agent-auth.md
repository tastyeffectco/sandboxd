# Managed agent auth (Phase 10B)

> **⚠️ Post-v0.4 / next release — not in the v0.4 release.** Everything in this
> document lives on the **`feat/phase-10b-agent-auth`** branch, which is **not
> merged** into `release/v0.4-apps-console`. The **credential import** + the
> **`claude-code` task adapter** are **accepted and live-verified** on a real
> Claude subscription, but they ship in a **future release**, not v0.4. v0.4
> itself ships only the OpenCode/Claude Code CLIs in the base image and the
> OpenCode task agent.

How sandboxd gives a sandbox's coding agent its credentials **without** putting
them in the workspace, snapshots, container env, logs, events, or task results.

> **Status (accepted on the branch):** the store + runtime delivery, opaque
> **credential import** for claude-code, and the **Claude Code task adapter** are
> done and verified. There is **no custom OAuth and no `setup-token` browser
> flow** implemented — credentials are obtained by **importing** an existing
> `~/.claude/.credentials.json`. A guided in-console login (`setup-token`
> PTY/xterm), **Codex** support, and **stronger per-task auth isolation** are
> **deferred** (the old stdout-scrape `setup-token` automation was removed because
> claude v2's Ink/TUI flow can't be driven by piped stdin).
>
> **Product model:** the `opencode` provider runs OpenCode (still supported); the
> `claude-code` provider runs the real Claude Code CLI adapter. Claude
> credentials are only meaningful for `claude-code` — they are never fed to
> OpenCode.

## How credentials reach the agent

1. Each provider's credentials live in an **opaque** host directory under the
   sandboxd data root: `<DataDir>/agent-auth/<provider>/`. This is **outside**
   any sandbox workspace, so it can never be copied into a workspace or a
   snapshot. sandboxd never opens or parses these files.
2. When a sandbox is created, sandboxd bind-mounts **every connected provider's**
   auth dir at **`/run/agent-auth/<provider>`** (e.g.
   `/run/agent-auth/opencode`, `/run/agent-auth/claude-code`) — only for
   providers that are connected, and deliberately **not** under `/home/sandbox`
   (the workspace).
3. When runtimed spawns the agent for a task, it points the agent's **`HOME`** at
   **`/run/agent-auth/<the task's agent>`** if that dir is mounted. So a
   `claude-code` task gets the claude-code credentials even when the sandbox's
   default agent is opencode.

`SANDBOXD_DEFAULT_AGENT` (default `opencode`, settable in `docker-compose.yml`)
only chooses the agent for tasks that **don't** specify one. An explicit
`agent:"claude-code"` task always works (its creds are mounted at create).

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
workspace tree), the container env / `docker inspect` (no token and no auth path
in env at all), task results, events, or logs. (Verified live with the real
`claude` CLI reading from `/run/agent-auth/claude-code/.claude`.)

## Known limitations (owner-operated mode)

- **All connected providers' auth dirs are present in the sandbox while it
  runs.** For simplicity in this slice every connected provider is mounted at
  create (read-write), so during a task those credential files exist on the
  container's filesystem and a future same-container terminal/process could read
  them. Stronger isolation — a **per-task copy** that mounts only the selected
  agent's creds onto a tmpfs and removes them after — is **A3**.
- **Auth changes require a sandbox recreate.** The mounts are fixed at create
  time, so connecting/changing a provider's auth takes effect on the **next**
  sandbox (re)create. A3's per-task copy removes this too.

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

- Import the claude-code credential (above). It's then mounted into **every** new
  sandbox at `/run/agent-auth/claude-code` — no need to change the default agent.
- Submit a task with `agent: "claude-code"`. runtimed runs
  `claude -p <prompt> --output-format stream-json --verbose --dangerously-skip-permissions`
  with `HOME=/run/agent-auth/claude-code`, and maps the stream to task events + a
  final result.
- Optionally set `SANDBOXD_DEFAULT_AGENT=claude-code` (compose) to make
  claude-code the default for tasks that don't specify an agent.

## Live verification checklist (real Claude subscription)

Tested with a **fake claude** (no subscription). Validate the real flow:

- [ ] Import `~/.claude/.credentials.json` → `GET /v1/agents` shows
      `claude-code: connected, runnable: true`; the file exists under
      `<DataDir>/agent-auth/claude-code/.claude/.credentials.json`.
- [ ] Create a sandbox (claude-code is mounted automatically once connected);
      submit a task with `agent:"claude-code"` **even with the default left at
      opencode** → the Claude Code CLI runs on your subscription and the task
      produces a final result.
- [ ] Token is **absent** from: the workspace, a snapshot, `docker inspect` env,
      logs, events, and task results.
- [ ] **Disconnect** → the dir is gone; `GET /v1/agents` shows `needs_login`.
