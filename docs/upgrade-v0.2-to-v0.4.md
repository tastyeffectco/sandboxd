> **Historical note.** This document predates the public **0.3.0** launch numbering (the pre-launch line was internally versioned v0.4.x). It is kept for provenance; for the current release see [`CHANGELOG.md`](../CHANGELOG.md).

# Upgrading sandboxd v0.2.0 → v0.4

**TL;DR — v0.4 is a backward-compatible, additive upgrade from v0.2.0.** No API
routes were removed, the legacy `/sandbox/*` endpoints are unchanged, the
on-disk workspace layout and container hardening are identical, and the database
upgrades forward by applying new migrations. There are **no true breaking
changes** in v0.4-core. A small number of behaviors are worth knowing about
before you upgrade (below).

> The Claude Code subscription / managed agent-auth work is **post-v0.4** (the
> `feat/phase-10b-agent-auth` branch, not part of the v0.4 release). It carries
> the **one behavior change** that affects an existing workflow — see
> [§ Post-v0.4](#post-v04-agent-auth-branch-not-in-v04).

## What does NOT change (safe to upgrade)

- **Public API.** Every v0.2.0 route still exists with the same path/verb. The
  legacy `/sandbox`, `/sandboxes`, `/sandbox/{id}/exec|keepalive|claim|purge|…`
  endpoints are untouched. v0.4 only **adds** routes (apps config/secrets,
  events, presets, process logs, settings, fork/restore, sandbox start).
  Existing clients keep working unchanged.
- **Response shapes are additive.** New fields appear on existing responses
  (e.g. `processes[]`, `runtime_preset`, a `preview` object). Clients that
  ignore unknown fields are unaffected. No field was removed or retyped.
- **Workspace layout.** Still one directory per sandbox bind-mounted at
  `/home/sandbox`, app at `/home/sandbox/workspace/app`. Existing workspaces
  are compatible as-is.
- **Container hardening & Traefik routing.** The `docker run` flags
  (`--cap-drop=ALL`, `--security-opt=no-new-privileges`, `--read-only`, tmpfs,
  `--memory`, `--pids-limit`, ulimits, `--userns`) and the Traefik label
  generator are **byte-for-byte identical**. Preview routing is unchanged.
- **Base image tag.** Default is still `sandboxd-base:1.0.0`
  (`SANDBOXD_IMAGE`). No forced rebuild for the control plane to start, though
  you should rebuild the base image to pick up new preset templates if you use
  the new presets.
- **Idle/wake/keepalive defaults.** Idle threshold default unchanged (2100s);
  wake and keepalive semantics unchanged.
- **No env vars removed or renamed.**

## Database upgrade path

Migrations are **forward-only and additive**. A v0.2.0 database is at migration
`0013_apps`. v0.4 applies, on first boot, in order:

| Migration | Adds |
|---|---|
| `0014_app_config` | `app_config` table (config/secrets) |
| `0015_snapshot_app_id` | nullable `app_id` column on snapshots |
| `0016_app_events` | `app_events` table (activity timeline) |
| `0017_app_runtime_preset` | nullable `runtime_preset` column on apps |
| `0018_instance_settings` | `instance_settings` table (lifecycle tunables) |

All are `CREATE TABLE` or nullable `ADD COLUMN` — **no `DROP`, no destructive
`ALTER`, no data rewrite.** Back up `SANDBOXD_DATA_DIR/state/sandboxd.db` before
upgrading (standard practice); the upgrade is otherwise automatic.

## New environment variables (all optional)

| Var | Default | Notes |
|---|---|---|
| `SANDBOXD_PUBLIC_HTTP_PORT` | `80` | Host port appended to preview URLs **only when ≠ 80/443**. On a default `:80` host the preview URL shape is **unchanged**; on a non-80 host it now returns a correct, reachable URL (a fix). |
| `SANDBOXD_SECRETS_KEY` | *(auto)* | Encrypts sensitive app config at rest. **If unset, a key is auto-generated** at `SANDBOXD_DATA_DIR/secrets.key` (0600). **Back up the data dir** — if that key is lost, previously-stored encrypted secrets are unrecoverable. Setting the env var is optional; a malformed value (not base64-of-32-bytes) is a startup error. |

Nothing new is **required** — an existing v0.2.0 env upgrades unchanged.

## Behaviors worth knowing (not breaks)

- **`DELETE /v1/sandboxes/{id}` purges the workspace** (container + workspace +
  row). This is the **same** behavior as v0.2.0 — it is not a v0.4 change — but
  it's easy to confuse with the legacy `DELETE /sandbox/{id}`, which **stops and
  keeps** the workspace. Use the legacy verb if you want a soft stop.
- **Task `agent` validation is more permissive, not less.** v0.2.0 accepted only
  `agent:"opencode"`; v0.4 accepts the same plus (post-v0.4) `claude-code`. An
  omitted `agent` still defaults to `opencode`. The 400 error **message** for an
  unknown agent changed wording (not a contract change).
- **Optional web console.** v0.4 adds an optional first-party console
  (`docker compose --profile console up`). Plain `docker compose up` runs the
  **core only**, exactly as before. The console is a pure `/v1` client.
- **Warming interstitial returns HTTP `200`**, and `keepalive_until` is honored
  but not surfaced in `GET /v1/sandboxes/{id}` — both pre-existing, documented.

## How to upgrade

1. Back up `SANDBOXD_DATA_DIR` (workspaces + `state/sandboxd.db` + any
   `secrets.key`).
2. Pull v0.4, rebuild images (`docker compose build`, plus the base image if you
   want the new presets), and restart. Migrations apply automatically.
3. (Optional) `docker compose --profile console up -d` to add the console.
4. Verify: `GET /healthz` → `ok`; existing apps/sandboxes still listed
   (`GET /sandboxes`, `GET /v1/apps`); a stopped preview still wakes.

## Post-v0.4 (agent-auth branch — NOT in v0.4)

These ship in a future release (`feat/phase-10b-agent-auth`), not v0.4. The one
item that changes an existing workflow:

- **⚠️ Agent credentials via the sandbox `env` no longer reach the agent.** The
  agent process env is now **scrubbed** of secret-shaped variables
  (`*_KEY`, `*_TOKEN`, `*_SECRET`, …). On v0.2.0 **and v0.4-core**, passing
  `env:{"ANTHROPIC_API_KEY":"…"}` at sandbox create made the key available to the
  coding agent. On the agent-auth branch it does not — credentials are delivered
  via the **managed agent-auth store** (import `~/.claude/.credentials.json` for
  `claude-code`). If/when that branch is released, this is a **migration step**
  for anyone relying on env-based agent keys. Additive on the same branch:
  `GET /v1/agents`, `POST /v1/agents/claude-code/import|disconnect`,
  `SANDBOXD_DEFAULT_AGENT` (default `opencode`), and `agent:"claude-code"` tasks.
  See [`docs/agent-auth.md`](agent-auth.md).
