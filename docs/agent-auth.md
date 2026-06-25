# Managed agent auth (Phase 10B)

How sandboxd gives a sandbox's coding agent its credentials **without** putting
them in the workspace, snapshots, container env, logs, events, or task results.

> **Status: A1.** The store + the runtime delivery mechanism exist. There is no
> in-console **Connect** flow yet (A2) — populate the auth dir out-of-band for
> now (see "Populating" below).

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

## Populating the auth dir (until Connect lands in A2)

Place the provider's HOME-relative credential files under
`<DataDir>/agent-auth/<provider>/`, owned by the sandbox uid (`1000:1000`).
`GET /v1/agents` then reports the provider as `connected` and the next sandbox
created will mount it. A2 will do this for Claude Code via `claude setup-token`.
