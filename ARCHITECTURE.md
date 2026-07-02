# Architecture

sandboxd is a small Go **control plane** (`sandboxd`) that drives the Docker
daemon, fronted by Traefik, with an optional **web console** (a pure `/v1` API
client) on top. It's a self-hosted engine for AI app-builder products: isolated
sandboxes, live preview URLs, coding agents, app-scoped config/secrets,
snapshots/fork/restore, runtime presets, and process logs/events. Everything
runs as containers on one host.

High level:

- **Control plane** (`sandboxd`, Go) — sandbox/app lifecycle, the `/v1` API.
- **SQLite** (WAL) — the single source of truth for apps, sandboxes, config,
  events, snapshots.
- **Docker runtime provider** — the only backend today; sandboxd shells the
  `docker` CLI. (A second provider is a future concern.)
- **Traefik** — edge router; publishes a preview URL per running sandbox.
- **runtimed** — in-sandbox supervisor + task runner, baked into the base image.
- **sandbox.yaml** — the per-app runtime manifest runtimed reads (web/workers/
  build/health/restart_after_task).
- **Presets / templates / base image** — the create-time layering (see
  [Runtime model](#runtime-model)).
- **Console** — a React SPA that talks **only** to `/v1`; never touches the DB or
  workspaces.
- **Agent auth store** — opaque per-provider credential dirs under the data root,
  **outside** any workspace (post-v0.4; see `docs/agent-auth.md`).
- **App config/secrets**, an **event timeline**, and **snapshots/fork/restore**
  are first-class app subsystems (below).

```
                         ┌── host (Docker daemon) ─────────────────────────┐
   browser  ──HTTP──▶ :80│  traefik ──┬─▶ s-<id>-3000  (running sandbox)   │
                         │            │      ▲  dev server :3000            │
   API/CLI ──HTTP──▶ :9090  sandboxd ─┼──────┘  (docker run/stop/exec)      │
                         │     │      └─▶ /forward-auth, /wake (catch-all)  │
                         │  SQLite (source of truth)                        │
                         │  reapers: idle (stop) + pressure (mem)           │
                         │  workspaces/  <id>/ … (bind-mounted, persist)    │
                         └──────────────────────────────────────────────────┘
```

## Components

### sandboxd (control plane)
A single Go binary, running in a container with the host Docker socket and the
data directory mounted. It:

- **Owns sandbox lifecycle** — create / list / get / exec / stop / destroy. It
  shells out to the `docker` CLI (`internal/docker`); no SDK.
- **Provisions workspaces** (`internal/loopback`) — one directory per sandbox
  under `SANDBOXD_DATA_DIR/workspaces/<id>`, seeded once from the image's
  `/opt/sandbox-skel`, then bind-mounted into the container at `/home/sandbox`.
- **Emits Traefik labels** (`internal/traefik`) so each sandbox self-registers
  its preview route(s) when it starts.
- **Runs two reapers** (`internal/reaper`): an *idle* reaper that `docker
  stop`s sandboxes idle past a threshold (freeing RAM), and a *pressure* reaper
  that stops sandboxes when host memory runs low.
- **Serves the wake path** (`internal/wake`): the first request to a stopped
  sandbox's preview URL is routed (by a low-priority Traefik catch-all) to
  sandboxd, which `docker start`s the container, waits for the port to come up,
  and serves a styled "warming up" page that auto-refreshes into the app.
- **Reconciles on boot** (`internal/reconcile`): lists Docker containers, diffs
  against SQLite, and converges Docker to the DB. SQLite is always the truth.
- **Stores state** in SQLite (WAL) via `internal/store`; migrations are numbered
  files baked into the image.

### runtimed (in-sandbox supervisor)
Built into the base image as the container's main process (`cmd/runtimed`). It
supervises the user's processes and runs coding tasks submitted through the
API. It's compiled in the base image's build stage, so the host needs no Go.

On boot runtimed reads an optional **`sandbox.yaml`** (the runtime manifest) from
the app dir and runs a generalized process model: one optional `web` process
(previewed) plus N background `workers`, each supervised with restart-on-exit
backoff. No manifest = the default Vite app (backward compatible). It also runs a
post-task `build` check and, for runtimes without live reload, a per-process
`restart_after_task` that bounces the process after each task so agent edits go
live. **Runtime presets** (`internal/preset`) are the create-time bundles —
template + generated `sandbox.yaml` + capabilities — behind `GET /v1/presets` and
the `runtime_preset` create field. Process state and per-process logs are exposed
over the API. Schema + rules: `docs/sandbox-manifest.md`.

### Traefik (edge)
Docker label provider, scoped by a `sandboxd.managed=true` constraint so it
only routes containers this stack owns. Running sandboxes win on a
priority-100 router; the priority-1 file-provider catch-all (`traefik/dynamic/
wake.yml`) forwards anything else to sandboxd's wake path. Plain HTTP by
default; TLS is a config switch (see README → Production / TLS).

### Console (optional UI)
A small React SPA served behind the same Traefik (`console.<domain>`). It is a
**pure `/v1` API client** — it never opens the DB or a workspace. It creates
apps, picks presets, drives sandbox lifecycle + preview, submits tasks and
streams logs, manages config/secrets, shows the events timeline and per-process
logs, does snapshot/fork/restore, and reads settings (editing only the lifecycle
tunables). Contract: `docs/openapi.yaml`.

### App subsystems
- **App config & secrets** — per-app key/values. Sensitive values are write-only:
  encrypted at rest (`internal/secrets`), never returned by the API (only
  `value_set`). Delivery to agents/runtime is policy-scoped.
- **Event timeline** — a durable, newest-first per-app activity log
  (`internal/events`): app/sandbox/config/task/snapshot events, surfaced at
  `GET /v1/apps/{id}/events`.
- **Snapshots / fork / restore** — capture an app's **workspace state**
  (`internal/snapshot`), excluding dependency/build artifacts; fork into a new
  app or restore in place. Ownership is normalized to the sandbox user.
- **Agent auth store** (post-v0.4, `feat/phase-10b-agent-auth`) — opaque
  per-provider credential dirs under `SANDBOXD_DATA_DIR/agent-auth/<provider>/`,
  **outside** any workspace, mounted read-only-to-the-task at
  `/run/agent-auth/<provider>` so the selected agent's `HOME` finds its creds.
  Credentials never enter workspaces, snapshots, Docker env, logs, events, or
  task results. See `docs/agent-auth.md`.

## Runtime model

How an app actually runs, layered create-time → run-time:

1. **Base image** — tools + `runtimed` (the container's main process) + optional
   per-preset `/opt/templates/<preset>` starters. Contract: `docs/base-image.md`.
2. **Runtime preset** — the create-time bundle (template + a generated
   `sandbox.yaml` + capabilities) chosen via `runtime_preset` / `GET /v1/presets`.
3. **`sandbox.yaml`** — the manifest runtimed reads: the `web` process (previewed),
   N background `workers`, the post-task `build` check, `health`, and
   `restart_after_task`. Full schema: `docs/sandbox-manifest.md`.
4. **Starter / import** — the app's source code (seeded from the preset's
   template; Git/private-repo import is not implemented yet).
5. **Snapshot** — a capture of the app **workspace state**, deliberately
   **excluding** installed dependencies and build output
   (`node_modules`, `.next`, `out`, `.venv`, `__pycache__`, `.cache`).

## Request flow: first hit to a stopped sandbox

1. Browser → `http://s-<id>-3000.preview.localhost`
2. Container is stopped, so no priority-100 router exists → Traefik's catch-all
   matches and forwards to `sandboxd:9000`.
3. sandboxd checks wake admission (memory headroom), `docker start`s the
   container, polls the port, returns the warming page.
4. The started container's labels make Traefik publish its priority-100 router.
5. The next refresh matches that router and proxies straight to the dev server.

## Isolation model

Each sandbox runs under hardened `runc`: `--cap-drop=ALL`,
`--security-opt=no-new-privileges`, `--read-only` rootfs with `tmpfs` for
`/tmp`, a hard `--memory` ceiling, `--pids-limit`, and file-descriptor ulimits.
The threat model is **authenticated, accountable users running their own code**
— not anonymous hostile multi-tenancy. Kernel-CVE container escape is mitigated
by patching, not by a VM boundary; if you need stronger isolation, run sandboxd
on a dedicated VM per trust domain.

## Storage & persistence

| Class | Where | Survives stop? | Survives reboot? |
|---|---|---|---|
| Workspace | `SANDBOXD_DATA_DIR/workspaces/<id>/` (bind mount) | yes | yes |
| Control-plane state | `SANDBOXD_DATA_DIR/state/sandboxd.db` (SQLite) | yes | yes |
| Container writable layer | none (`--read-only`) | no | no |
| `/tmp`, `/var/tmp` | tmpfs | no | no |

The only writable disk location inside a sandbox is `/home/sandbox`. Back up a
workspace by copying its directory; back up state by copying the SQLite file.

## Optional workflows on top of core

The core engine is just sandboxes + previews + the `/v1` API. Everything else —
durable **apps**, coding **agents**, and the **Git** workflow — is an optional
layer a client opts into; none of it is required to run a sandbox, and the console
is just one such `/v1` client.

**Git** (optional; see [`docs/git-workflow.md`](docs/git-workflow.md)) follows the
same boundary the rest of the system uses, split by who needs credentials and the
network:

- **Network git (clone on import, push) runs host-side in the control plane** —
  it holds the encrypted, owner-scoped PAT (decrypted only in these paths and fed
  to git only via `GIT_ASKPASS` + a `0600` file) and reaches the network. The
  **sandbox never sees the token** and has no network.
- **Local git (status, diff, commit) runs in-sandbox** (`docker exec`,
  credential-free, offline), so an agent-controlled `.git` stays inside the
  existing trust boundary instead of executing on the host.

Push targets the app's stored repo URL (never the repo's `.git/config` origin),
pushes only to a new branch, never force-pushes, and creates no PRs. PAT + HTTPS
only — no SSH, App/OAuth, or provider APIs.

## Design choices & current limitations (v1)

sandboxd v1 optimizes for "runs anywhere with just Docker, one command." A few
mechanisms are deliberately simple so there's nothing host-specific to install
or configure. Each is a conscious trade-off you can tighten later:

| Area | v1 choice | Trade-off / how to harden |
|---|---|---|
| Workspace storage | plain **directory** per sandbox | no hard per-workspace disk quota (host fs is shared); add quotas at the fs/volume layer if needed |
| Memory | hard `--memory` ceiling per sandbox | the softer cgroup `memory.high` throttle is opt-in (`SANDBOXD_SET_MEMORY_HIGH`, needs host cgroup access) |
| Egress | default-allow, no logging | add host firewall rules / a proxy if you need egress control |
| Package installs | public npm/PyPI registries | run your own caching proxy and point the image at it for speed/airgap |
| TLS / domain | HTTP on `*.localhost` out of the box | switch to a real wildcard domain + cert resolver (see README → Production / TLS) |
| Snapshots/templates | API present, **experimental** on directory storage | use plain workspace copies, or contribute a directory-tar snapshot backend |
| Preview ports | **one** public preview endpoint per sandbox | see *Single-port runtime model* below |
| Base image | **one** image instance-wide (`SANDBOXD_IMAGE`) | no per-app image selection (roadmap); native langs need a custom image — see [`docs/base-image.md`](docs/base-image.md) |
| Databases | **embedded SQLite** (app-bundled engine) | external Postgres/MySQL/Redis are out-of-sandbox/future — see *Embedded databases* below |

### Single-port runtime model

A sandbox exposes **exactly one public preview endpoint** (the manifest's
`web.port`, routed by Host). On that single port, **HTTP, WebSocket upgrades, and
SSE all work** (verified: Streamlit/Jupyter kernel WS → `101`, Reflex + NiceGUI +
Chainlit socket.io → `101` (Chainlit at `/ws/socket.io/`), Sanic native WS → `101` +
echo, Gradio SSE queue streamed) — every WS framework tested works with zero per-app
config. A naive multi-port app (separate frontend +
backend ports) is **not** reachable on the second port — run it in single-port mode
(e.g. Reflex `--single-port`, a fullstack server that mounts the API + socket on one
port) or stand up an **in-sandbox reverse proxy** that fans `/` and `/api` off the
one port. Multi-port exposure is roadmap, not current behaviour. (Apps shouldn't
hardcode the sandbox id; use the preview URL handed back by the API.)

### Embedded databases

**App-bundled SQLite works** and is part of the sandbox's workspace state (verified:
n8n → `~/.n8n/database.sqlite`, Express + Prisma → `prisma/dev.db`, Directus →
`data.db`; the engines — better-sqlite3, Prisma's bundled `libquery_engine` — ship
with the app, so no system `sqlite3` CLI is needed). The DB file lives in the
workspace, so **snapshots include it and can grow large**. Postgres/MySQL/Redis
need a custom image or an external service — there's no in-sandbox service layer,
DB provisioning, or Docker Compose (by design).

On **Node 22**, SQLite is a non-issue across the board: `better-sqlite3` 11.x and
`@libsql` ship arm64 **prebuilts** (no compile); Node's built-in `node:sqlite` works
with `NODE_OPTIONS=--experimental-sqlite`; and Python's `apsw`/`python-fasthtml`
bundle their own engine. The base image's Python `setuptools`/`distutils` fix is the
safety net for the cases that *do* compile from source (e.g. Ghost's `sqlite3` +
`sharp`). Recipes stay advisory — the DB file is app state.

`--userns=host` is set on the infra containers (and, by default, on sandboxes)
so workspace ownership is deterministic whether or not the host daemon uses
userns-remap. Set `SANDBOXD_USERNS=` empty to opt sandboxes back into the
daemon default.
