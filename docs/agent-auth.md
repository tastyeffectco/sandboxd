# Managed agent auth (Phase 10B)

How sandboxd gives a sandbox's coding agent its credentials **without** putting
them in the workspace, snapshots, container env, logs, events, or task results.

> **Status: A2.** The store, the runtime delivery mechanism (A1), and the
> console-driven **Connect Claude Code** flow (A2) exist. OpenCode/Codex connect
> and per-task hardening are later slices.

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

## Connecting Claude Code (A2, console-driven)

Fully console-driven — owners never run the login inside an app sandbox:

1. Console **"Use your Claude subscription"** → `POST /v1/agents/claude-code/connect`.
2. sandboxd starts an **ephemeral auth container** from the base image (used only
   because it carries the `claude` binary): no workspace mounted, `HOME` → a
   staging dir, a PTY via `script`, runs `claude setup-token`.
3. sandboxd captures the **login URL** and returns it; the console shows it.
4. The owner opens it in their **own** browser, signs in with their Claude
   subscription, and copies the code (`redirect_uri` is a hosted page, not
   localhost — so this works for a remote server).
5. Console submits the code → `POST …/connect/{id}/code` → sandboxd writes it to
   the CLI's stdin.
6. **Success = the credential file is present and non-empty** (`claude` exits 0
   even when not logged in, so the exit code is not trusted). On success the
   staging dir is atomically **promoted** to `<DataDir>/agent-auth/claude-code`;
   on failure/timeout it is deleted.
7. `GET /v1/agents` then reports `claude-code: connected`; the next sandbox
   created with `SANDBOXD_DEFAULT_AGENT=claude-code` mounts it (A1).

**Disconnect** (`POST /v1/agents/claude-code/disconnect`) deletes the auth dir.
The login **URL/code/state are sensitive-ish session data**: returned to the
console, but never logged, persisted, or emitted in events. No token is parsed.

> The exact credential file (`~/.claude/.credentials.json`) is the success
> signal; confirm it against the installed `claude` version with the live
> checklist below.

## Live verification checklist (real Claude subscription)

A2 is tested with a **fake claude** (no subscription). Validate the real flow:

- [ ] Console → AI Agents → **Use your Claude subscription** → URL appears.
- [ ] Open URL, sign in, paste the code → status becomes **connected**.
- [ ] `GET /v1/agents` shows `claude-code: connected`; the credential file exists
      under `<DataDir>/agent-auth/claude-code/` (e.g. `.claude/.credentials.json`).
- [ ] Create a sandbox with `SANDBOXD_DEFAULT_AGENT=claude-code`, run a task →
      the agent uses your subscription.
- [ ] Token is **absent** from: the workspace, a snapshot, `docker inspect` env,
      logs, events, and task results.
- [ ] **Disconnect** → the dir is gone; `GET /v1/agents` shows `needs_login`.
