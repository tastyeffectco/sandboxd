<h1 align="center">sandboxd</h1>


<p align="center">
  <b>Self-hosted control plane for AI-built apps.</b><br/>
  Give every user an isolated cloud dev environment, a built-in coding agent,
  and a live preview URL — with a web console to drive it all. Self-hosted, on
  one machine, in one command.
</p>

<p align="center">
  <a href="LICENSE"><img alt="License: MIT" src="https://img.shields.io/badge/license-MIT-green.svg"></a>
  <img alt="Runs on Docker" src="https://img.shields.io/badge/runs%20on-Docker-2496ED.svg">
  <img alt="Single binary control plane" src="https://img.shields.io/badge/control%20plane-single%20Go%20binary-00ADD8.svg">
  <img alt="Status: beta" src="https://img.shields.io/badge/status-beta-yellow.svg">
</p>

<p align="center">
  <a href="https://github.com/sponsors/tastyeffectco"><img alt="Sponsor sandboxd" src="https://img.shields.io/badge/%E2%9D%A4%20Sponsor-tastyeffectco-db61a2?logo=githubsponsors&logoColor=white"></a>
</p>

> **sandboxd is MIT-licensed and free forever** — no paid tiers, no rug pulls.
> If it's useful to you, [**sponsoring**](https://github.com/sponsors/tastyeffectco)
> directly funds ongoing maintenance and the **deploy-feature roadmap** (one-click
> deploys of the apps your users build). Thank you 🙏

---

<img width="1100" height="816" alt="sandboxd-demo" src="https://github.com/user-attachments/assets/f794ff9b-8ffe-47e8-bd30-22541f870f09" />

> **Two ways to use it.** sandboxd is an **API-first engine** — everything is a
> call to the public `/v1` API, so you can build it into your own product. It
> also ships an **optional web console**: the fastest, no-code way to use
> sandboxd hands-on — create apps, chat to a coding agent, watch the live
> preview, edit files, and push with Git, all from a browser. The console is a
> **pure `/v1` client** (it touches nothing the API doesn't expose), so the
> engine runs perfectly headless — the console is a convenience, not a
> dependency. New here? Start with the console; building a product? Drive the API.

## What you get

- **Web console** — create and open apps, watch previews, run agent tasks, manage everything from a browser (or drive the same `/v1` API directly).
- **Runtime presets** — one-step React/Vite, Next.js, Node/Express, FastAPI, or Worker apps (`GET /v1/presets`); each boots and reloads after agent edits.
- **Live preview URLs** — each app is reachable at its own shareable link; sleeps when idle, wakes on request.
- **Agent tasks** — submit a prompt, stream the agent's progress, get the build/health result. **No credential ever enters the sandbox**: agents reach their model provider through a credential-injecting proxy, so no API key or OAuth token lands in the workspace.
- **Undo a task** — every task is checkpointed first, so you can **revert** the workspace to before any task from the history (kept off your git branch — nothing to clean up).
- **Browse & edit files, review & push Git** — an in-console file tree + code editor with inline git diff, and a Git tab to commit and push.
- **App config & secrets** — per-app key/values; sensitive values are write-only (set once, encrypted at rest, never returned).
- **Snapshots / fork / restore** — capture a workspace, fork it into a new app, or restore in place.
- **Activity / events** — a durable per-app timeline of what happened.
- **Process logs** — per-process status (web + workers) and tail-able logs.
- **Settings / lifecycle controls** — a read-only instance overview, with editable idle-reap / keepalive tuning, applied live.

## What is sandboxd? (start here)

Think of the apps where you type *"build me a todo app"* and seconds later a
working website appears at its own link — like Lovable, Bolt, v0, or Replit.
**sandboxd is the open-source backend that makes that possible**, running on
your own server.

Here's what it does, in plain terms. You send it one HTTP request, and it:

1. **Creates a sandbox** — a private, isolated Linux container (its own
   filesystem, its own memory limits), so one user's code can never see or
   break another's.
2. **Runs an AI coding agent inside it** — you give it a prompt, and it writes
   the code into that sandbox. (The OpenCode and Claude Code CLIs come
   pre-installed.)
3. **Gives the app a live URL** — the dev server running inside the sandbox is
   instantly reachable at a shareable preview link.

```
POST /sandbox          → a private, isolated container spins up
POST .../tasks         → an AI agent writes an app inside it
http://<id>.preview... → that app is live at its own URL
```

It's also cheap to run: a sandbox **goes to sleep when nobody's using it**
(freeing memory) and **wakes up the instant someone opens its link again** —
files are saved on disk the whole time. So one ordinary server can hold many
users instead of needing one virtual machine each.

Under the hood it's deliberately small and easy to understand: **one Go program
that tells Docker what to do**, with **Traefik** handling the URLs and
**SQLite** as the database. No Kubernetes, no separate database server, no
message queue — you could read the whole thing in an afternoon.

```
            ┌──────────────── your host (just needs Docker) ────────────────┐
 browser ──▶│  Traefik  ──▶  sandbox  (coding agent + dev server :3000)      │
            │     ▲              ▲   ▲                                        │
 API/CLI ──▶│  sandboxd ─────────┘   └─ workspace dir (persists)             │
            │     │  SQLite (source of truth) · idle→stop · request→wake      │
            └─────┴────────────────────────────────────────────────────────-─┘
```

### Who's it for?

**✅ Use it if** you're running **many sandboxes for other people** — an AI
app-builder ("describe an app → see it live"), an agent platform, a coding
playground, per-user or per-branch preview environments, or multi-app hosting
for a team.

**❌ Skip it if** you just need one or two containers for yourself — a shell
script, `docker run`, or [lxd](https://canonical.com/lxd) is simpler. (More on
that [below](#why-not-just-a-shell-script).)

## Why sandboxd?

If you're building an **AI app-builder, an agent platform, a coding playground,
or a per-user preview product**, the hard part isn't the prompt — it's the
infrastructure underneath it:

- **Multi-tenant isolation** so one user's code can't touch another's.
- **Per-user preview URLs** with automatic routing and TLS.
- **Cost control** — idle environments must release memory, or your bill explodes.
- **Agent orchestration** — run a coding agent against a workspace, stream its
  progress, capture the result.
- **Persistence, wake-on-demand, reconciliation after a crash or reboot.**

That's months of platform work. sandboxd is that platform, distilled to one
command:

- ⚡ **One-line install.** `curl -fsSL …/install.sh | bash` and you have a working API + previews.
- 🧠 **Agents included.** The OpenCode and Claude Code CLIs ship in every sandbox;
  hand a sandbox a prompt and it builds.
- 💸 **Dense by design.** Stop-on-idle + wake-on-request means dozens of sandboxes
  share one box instead of one VM each — the difference between a $20 server and
  a $2,000 cluster.
- 🔓 **Yours.** Self-hosted, MIT-licensed, no vendor lock-in. Own your data, your
  margins, and your roadmap.
- 🪶 **Boring on purpose.** SQLite + the `docker` CLI + Traefik. A reconciler
  converges Docker back to the database on every boot. You can read the whole
  control plane in an afternoon.

## "Why not just a shell script?"

Fair question — and honestly: **if you need one or two long-lived containers for
yourself, a shell script (or `docker run`, or [lxd](https://canonical.com/lxd))
is simpler. Use that.** We mean it. sandboxd is overkill for one-off projects.

It earns its keep the moment you're running **many** sandboxes for **other
people** — a team, or a product — because that's when the tidy little `docker
run` script quietly grows into all of this:

- **URLs, not ports.** Every sandbox gets a clean preview URL with automatic
  routing + TLS — no port bookkeeping, no collisions to manage.
- **It sleeps and wakes itself.** Idle sandboxes stop to free RAM and restart
  transparently on the next request (warming-up page, readiness probe, request
  hold). That part alone is well past 100 lines — and it's the difference
  between one cheap box and a rack of always-on VMs.
- **It survives reboots.** SQLite is the source of truth; a reconciler
  re-converges Docker to it on boot. A script forgets everything when the host
  restarts.
- **It's an API, not a CLI you shell into.** create / exec / stop / destroy /
  write-files / run-agent-task are real HTTP endpoints with auth — you call them
  from your app backend, per user, at scale.
- **One user can't take down the rest.** Per-sandbox memory/PID limits + a
  host-memory pressure reaper.
- **Agents with a lifecycle.** Submit a prompt, stream progress (SSE), capture a
  durable result — not just `opencode` fired inline.

Rebuild those as your script grows and you've rebuilt sandboxd. So: skip it for
one-offs; reach for it when "just a script" has started keeping you up at night.

> **Prefer Kubernetes?** The control plane talks to the container runtime through
> a thin `docker` CLI boundary, so a k8s Job/Pod backend is an interface swap,
> not a rewrite — a great first contribution. Today it targets a single Docker
> host (no k8s required), which is the sweet spot for teams who don't want to run
> a cluster just for sandboxes.

## Quick start

Requirements: **Docker + the Compose plugin** (Docker Engine on Linux, or Docker
Desktop on macOS) and **git**. That's it.

### 1. Install

One line — it fetches the repo, builds the images, and starts the stack:

```bash
curl -fsSL https://raw.githubusercontent.com/tastyeffectco/sandboxd/main/install.sh | bash
```

<details>
<summary>Prefer to clone first? (or pin a version)</summary>

```bash
git clone https://github.com/tastyeffectco/sandboxd.git
cd sandboxd
./install.sh
```

The installer is idempotent and safe to re-run. Override the source with
`SANDBOXD_REPO_URL` / `SANDBOXD_REF` (branch or tag) when piping.
</details>

`install.sh` checks Docker, writes a `.env`, builds the sandbox base image + the
control plane, and starts the stack. The API is then live at
`http://127.0.0.1:9090` (verify: `curl http://127.0.0.1:9090/healthz` → `ok`).

> **Linux** is the primary target. **macOS** works via Docker Desktop (the
> installer points the data dir at `~/.sandboxd`, which Docker Desktop shares by
> default) — treat it as best-effort for local development.

### 2. Have an agent build an app

The base image already includes the **OpenCode** and **Claude Code** CLIs, but
an agent needs a credential to reach its model. **Connect an agent first**
(OpenCode or Claude Code — see **connect a provider** just below), then hand a
sandbox a prompt and watch it build:

```bash
API=http://127.0.0.1:9090

# create a sandbox that will serve on port 3000
ID=$(curl -s -XPOST $API/sandbox -H 'content-type: application/json' \
       -d '{"ports":[3000]}' | sed -E 's/.*"id":"([^"]+)".*/\1/')
echo "sandbox: $ID"

# spin a coding agent with a request — it works in ~/workspace/app
curl -s -XPOST $API/v1/sandboxes/$ID/tasks -H 'content-type: application/json' -d '{
        "prompt":"create a Vite app that shows a todo list and run it on port 3000",
        "agent":"opencode"
     }'
# -> {"id":"<taskId>","status":"running","events_url":"/v1/sandboxes/<id>/tasks/<taskId>/events"}

# stream the agent's progress (Server-Sent Events)
curl -N $API/v1/sandboxes/$ID/tasks/<taskId>/events
```

To use your own model account, **connect a provider** — the control plane stores
the credential encrypted server-side and supplies it to the agent, so it never
lives in the sandbox. (Credential-shaped env vars like `ANTHROPIC_API_KEY` passed
via `env` are deliberately **scrubbed** from the agent process; they won't work.)

```bash
# API key:
curl -s -XPOST $API/v1/agents/claude-code/api-key -d '{"api_key":"sk-ant-..."}'
# or a subscription via guided OAuth:
curl -s -XPOST $API/v1/agents/claude-code/oauth/start   # → open the URL, then /oauth/finish
```

For a subscription, the real token stays behind a control-plane **auth proxy** —
the sandbox only ever sees a dummy key and the proxy URL. See
[`docs/agent-auth.md`](docs/agent-auth.md).

### 3. Open the live preview

Once the app serves on port 3000, it's reachable at its preview URL — the
sandbox self-registered the route, nothing else to wire:

```
http://s-<id>-3000.preview.localhost
```

`*.localhost` resolves to `127.0.0.1` in every modern browser, so it works
locally with zero DNS and zero certificates (add `:$HTTP_PORT` if you changed it
from 80). The first request to a stopped sandbox **wakes it** automatically. On a
real domain you get `https://s-<id>-3000.preview.yourdomain.com`
(see [Production / TLS](#production--tls)).

> **Rancher Desktop / Docker Desktop + k3s:** these bundle a k3s cluster whose
> klipper-lb grabs port 80 before sandboxd's Traefik can, so preview URLs return
> 404. Fix: set `HTTP_PORT=8080` in `.env` and run `docker compose up -d traefik`.
> Preview URLs become `http://s-<id>-<port>.preview.localhost:8080`.

> **Just want a shell, no agent?** Skip step 2 and run anything via the exec API:
> `curl -XPOST $API/sandbox/$ID/exec -d '{"cmd":["bash","-lc","cd ~/workspace/app && python3 -m http.server 3000"]}'`
> then open the same preview URL.

## Web console (optional UI)

Prefer a UI to curl? sandboxd ships an optional web console — a small React SPA
that talks **only** to the public `/v1` API. From it you can:

- **create apps** and **choose a runtime preset**;
- **create / start / stop / delete** a sandbox and **open its live preview**;
- **chat with the agent** — submit tasks, watch streamed output, and see the
  **full task history** (it persists across reloads), with a **↩ revert** on any
  turn to roll the workspace back to before that task (see [Undo a task](#undo-a-task--checkpoints));
- **browse & edit files** — a workspace file tree with an in-browser code editor
  (syntax highlighting, `Cmd/Ctrl+S` to save), inline git status badges, and a
  **download-as-zip**;
- **review & ship with Git** — a Git tab with a per-file **diff viewer**, staged
  **commit**, and **push** (ahead/behind); credentials live in **Settings → Git
  credentials**;
- **manage per-app config & secrets** (sensitive values are write-only: set
  once, never shown again);
- **view the activity/events timeline** and **runtime processes + their logs**;
- **snapshot / fork / restore** an app's workspace;
- **connect coding agents** (OpenCode, Claude Code — OAuth or API key) and
  **read instance settings** / edit the lifecycle tunables (see [Settings](#settings)).

```bash
# the console is gated by basic auth and ships FAIL-CLOSED — set a login first:
export CONSOLE_BASIC_AUTH="admin:$(openssl passwd -apr1 'choose-a-password')"
docker compose --profile console up -d        # core stack + console
```

Then open **http://console.localhost** (or `console.<PREVIEW_DOMAIN>:<HTTP_PORT>`
if you changed them). It's routed through the same Traefik as the previews, by
Host header — `console.<domain>` → console, `*.preview.<domain>` → previews — so
it shares one entrypoint, no extra port. Plain `docker compose up -d` (no
profile) runs sandboxd without the console.

> **Console auth.** The console is protected by Traefik HTTP basic auth and is
> **fail-closed**: without `CONSOLE_BASIC_AUTH` it returns `401` (the default is
> a locked, unknown password) — it is never exposed open. Set
> `CONSOLE_BASIC_AUTH="user:HASH"` (`HASH` from `htpasswd -nbB` or
> `openssl passwd -apr1`; in a `.env` file double every `$` → `$$`). The dev
> installer (`scripts/dev/install-v04-ubuntu.sh`) generates and prints one for
> you; with the plain `./install.sh` you set it yourself. This is **separate
> from API auth** — see [Authentication](#authentication).

The console never touches the database or workspaces — it's a pure `/v1` client
(contract in [`docs/openapi.yaml`](docs/openapi.yaml)). More detail:
[`console/README.md`](console/README.md).

## Undo a task — checkpoints

Before **every** agent task, sandboxd snapshots the workspace to a private git
ref (`refs/sandboxd/checkpoints/<task>`) — off to the side, so it never shows up
in your branch history, `git log`, or a push. That snapshot is the **revert
seam**.

- **List history:** `GET /v1/sandboxes/{id}/tasks` returns every task (newest
  first) with its `files_changed` and a `can_revert` flag. In the console it's
  the task history in the agent chat.
- **Revert:** `POST /v1/sandboxes/{id}/tasks/{taskId}/revert` (or the **↩ revert**
  link on a turn) restores the working files to **before that task ran** —
  undoing its changes and everything after. It keeps installed packages and
  build caches, and it does **not** move your branch/HEAD. Requires a running
  sandbox (the restore runs in the workspace).

Checkpoints are distinct from **snapshots** (full workspace freezes, including
installed deps) and from your own **git commits** (what you ship).

## Settings

Most settings are **env/install-managed** and read-only at runtime
(`GET /v1/settings` exposes a safe, secret-free summary). The **only**
values editable from the console/API (`PATCH /v1/settings`, hot-applied to the
running reaper) are the lifecycle tunables:

| Editable (console / `PATCH /v1/settings`) | Read-only (env / install-managed) |
|---|---|
| `idle_reap_enabled` | API auth, egress mode |
| `idle_threshold_seconds` | networking, preview domain/port |
| `keepalive_max_seconds` | base image, agent providers, secrets key |

Auth tokens, the secrets-encryption key, and egress are **env/file-only** and
are never shown or editable through the API.

## Runtime presets & `sandbox.yaml`

Create a working app of a common type in one step. `GET /v1/presets` lists the
built-in presets and you pass `runtime_preset` when creating an app/sandbox (the
console has a New-App picker):

| Preset | Serves | Post-task reload |
|---|---|---|
| `react-vite` | Vite SPA on :3000 | Vite HMR |
| `nextjs` | Next.js on :3000 | restart (also heals an agent `next build`) |
| `node-express` | Express API on :3000 (`/health`) | restart |
| `fastapi` | FastAPI on :3000 (`/health`) | `uvicorn --reload` |
| `worker` | no public endpoint | restart (editable `worker.sh`) |

A preset seeds starter files and writes a **`sandbox.yaml`** describing how the
app runs — its `web` process (command/port/health_path), background `workers`, a
post-task `build` check, and `restart_after_task`. Presets standardize on port
`3000`, but a manifest can declare any `web.port` and **sandboxd routes the
declared preview port**. Advanced users edit `sandbox.yaml` directly; it lives in
the workspace so snapshots preserve it. **When you change `sandbox.yaml`, restart
the sandbox so runtimed re-reads it.** Process status is on
`GET /v1/sandboxes/{id}` (`processes[]`) and per-process logs at
`GET /v1/sandboxes/{id}/processes/{name}/logs`. Full schema:
[`docs/sandbox-manifest.md`](docs/sandbox-manifest.md). For importing existing
repos of any stack, see [`docs/web-framework-recipes.md`](docs/web-framework-recipes.md).

## Git import & push (optional)

An optional workflow on top of core: import a private repo into an app, let an
agent work on it, then commit and push the changes back — all through `/v1` (the
console adds a Git panel). Store an encrypted **personal access token** once
(`POST /v1/git-credentials`), create an app from a repo URL (`POST /v1/apps` with
a `git` block), then `git/status` · `git/diff` · `git/commit` · `git/push` under
`/v1/apps/{id}`.

The token stays **control-plane-only**: network git (clone, push) runs host-side
and feeds the token to git via `GIT_ASKPASS` + a `0600` file — never into the
sandbox, argv, env, `.git/config`, snapshots, or logs — while local git
(status/diff/commit) runs credential-free in the sandbox. Push goes to a **new
branch only** (no force, no PR), HTTPS + PAT only. Full guide + security
boundaries + limitations: [`docs/git-workflow.md`](docs/git-workflow.md).

## API

Base URL = `http://127.0.0.1:9090` (set by `SANDBOXD_API_BIND`). Auth is **off
by default** for local use; with `SANDBOXD_API_AUTH_DISABLED=false` +
`SANDBOXD_API_TOKENS`, send `-H "Authorization: Bearer <secret>"`.

| Method & path | Body | Purpose |
|---|---|---|
| `POST /sandbox` | `{"ports":[3000],"env":{...}}` | **create** — `id` optional (ULID auto); `env` injects vars (e.g. API keys) |
| `GET /sandboxes` | — | list all sandboxes |
| `GET /sandbox/{id}` | — | get one (status, ports, container id…) |
| `POST /sandbox/{id}/exec` | `{"cmd":["bash","-lc","…"]}` | run a command (non-interactive) |
| `POST /sandbox/{id}/keepalive` | — | postpone the idle reaper |
| `POST /v1/sandboxes/{id}/stop` | — | stop now to free RAM (wakes on next preview hit) |
| `DELETE /sandbox/{id}` | — | destroy the container, **keep** the workspace |
| `POST /sandbox/{id}/purge` | — | destroy **and delete** the workspace |
| `POST /v1/sandboxes/{id}/tasks` | `{"prompt":"…","agent":"opencode","timeout_s":600}` | run a coding agent headlessly. `agent`/`model` optional (default agent = `opencode`); `continue` is a tri-state that defaults to resuming the last session |
| `GET /v1/sandboxes/{id}/tasks` | — | **task history** (newest first) — each row has `files_changed` + `can_revert` |
| `GET /v1/sandboxes/{id}/tasks/{taskId}` | — | task result |
| `GET /v1/sandboxes/{id}/tasks/{taskId}/events` | — | live task event stream (SSE) |
| `POST /v1/sandboxes/{id}/tasks/{taskId}/revert` | — | **undo a task** — restore the workspace to its pre-task checkpoint |
| `GET /v1/sandboxes/{id}/files?path=&recursive=` | — | list the workspace file tree |
| `GET /v1/sandboxes/{id}/files/content?path=` | — | read a file (`text/plain`, 2 MiB cap) |
| `PUT /v1/sandboxes/{id}/files?path=` | *(raw body = file contents)* | write a file |
| `GET /v1/sandboxes/{id}/export` | — | download the whole workspace as a `.zip` |
| `GET /healthz`, `GET /readyz` | — | liveness / readiness |

The full, contract-tested reference (git, agents, apps, snapshots, settings…) is
[`docs/openapi.yaml`](docs/openapi.yaml).

A complete, copy-pasteable runbook (including driving it from your own agent) is
in **[`AGENTS.md`](AGENTS.md)**.

## How it works

| Concern | Choice |
|---|---|
| Container runtime | Docker + hardened `runc` (cap-drop ALL, `no-new-privileges`, read-only rootfs) |
| Workspace storage | one bind-mounted directory per sandbox under the data dir (persists) |
| Edge / preview | Traefik v3 Docker provider — sandboxes self-register their routes |
| Idle management | stop-on-idle (`docker stop`) + wake-on-request; no warm pool |
| State | SQLite (WAL); a reconciler converges Docker to the DB on boot |
| Control plane | one Go binary, shells out to the `docker` CLI over the mounted socket |

The control plane runs in a container with the host Docker socket mounted and
launches each sandbox as a sibling container on a shared network so Traefik can
route to it. Full design: [`ARCHITECTURE.md`](ARCHITECTURE.md).

## Configuration

Everything is in `.env` (created from [`.env.example`](.env.example) on install).
The defaults run a complete local stack. The knobs you'll touch most:

| Variable | Default | What it does |
|---|---|---|
| `PREVIEW_DOMAIN` | `localhost` | domain preview URLs hang off |
| `HTTP_PORT` | `80` | host port Traefik listens on |
| `SANDBOXD_DATA_DIR` | `/var/lib/sandboxed` | where workspaces + state live |
| `SANDBOXD_API_BIND` | `127.0.0.1:9090` | where the control-plane API is published |
| `SANDBOXD_API_AUTH_DISABLED` | `true` | open API for local use; set `false` + tokens for prod |
| `CONSOLE_BASIC_AUTH` | *(locked)* | `user:htpasswd-hash` gating the optional console; fail-closed default |
| `SANDBOXD_DEFAULT_AGENT` | `opencode` | agent used for tasks that don't specify one |
| `SANDBOXD_OPENCODE_ZEN_PATH` | `zen` | OpenCode Zen endpoint: `zen` (pay-as-you-go) or `zengo` (the "go" subscription). See [agent-auth](docs/agent-auth.md#opencode-zen-subscription-vs-pay-as-you-go) |
| `SANDBOXD_AGENT_PROXY_URL` | `http://sandboxd:9100` | in-network URL of the credential-injecting auth proxy (empty disables it) |

Agent auth, the proxy, and the full env reference live in
[`docs/agent-auth.md`](docs/agent-auth.md).

## Authentication

sandboxd has **two independent** auth layers. They are separate knobs — neither
is required for core use, and there is **no token-management UI in v0.3**.

**API auth — protects the sandboxd `/v1` API (application layer).**
- **Off by default** (`SANDBOXD_API_AUTH_DISABLED=true`). To enable:
  ```bash
  SANDBOXD_API_AUTH_DISABLED=false
  SANDBOXD_API_TOKENS=admin=your-token,ci=another-token   # NAME=VALUE, comma-separated
  ```
  then call the API with `Authorization: Bearer your-token`.
- Tokens are **env/file-only** — never stored in the database, **never returned
  by any endpoint, and never shown in the console** (Settings shows only whether
  auth is on/off). There is no create/list/revoke API and no `last_used`.

**Console auth — protects the optional web UI at the edge (Traefik basic auth).**
- The console (`--profile console`) is **fail-closed**: the default
  `CONSOLE_BASIC_AUTH` is a locked entry with an unknown password, so an
  un-configured console returns `401` — it is **never exposed open**.
- Set your own: `CONSOLE_BASIC_AUTH="user:HASH"` (`HASH` from `htpasswd -nbB user 'pass'`
  or `openssl passwd -apr1 'pass'`). In a `.env` file, **double every `$` → `$$`**.
- The dev installer (`scripts/dev/install-v04-ubuntu.sh`) generates a random
  password, wires it, and prints it once (override with `CONSOLE_USER` /
  `CONSOLE_PASS` before running it). The plain `./install.sh` does not — set
  `CONSOLE_BASIC_AUTH` yourself before `docker compose --profile console up`.
- This is a **separate gate** from API auth; the console reaches `/v1` over an
  internal proxy, so the basic-auth login is what stands in front of the browser.

> Core-only users running `docker compose up` (no console) are unaffected by
> either setting; both default to a safe state.

## Production / TLS

For a public deployment on a real wildcard domain:

1. Point `*.preview.yourdomain.com` at the host.
2. In `traefik/traefik.yml`, enable the `websecure` entrypoint and add a
   certificate resolver (Let's Encrypt DNS-01 is ideal — one wildcard cert covers
   every preview host, so you never hit per-host ACME limits).
3. In `.env`: `PREVIEW_DOMAIN=yourdomain.com`, `PREVIEW_ENTRYPOINT=websecure`,
   `PREVIEW_TLS=true`, and **enable auth** — `SANDBOXD_API_AUTH_DISABLED=false`
   with `SANDBOXD_API_TOKENS=name:secret`.
4. `docker compose up -d`.

## Uninstall

```bash
./uninstall.sh            # stop the stack + remove all sandboxes + network (keeps your data)
./uninstall.sh --images   # also remove the built Docker images
./uninstall.sh --data     # also DELETE all workspaces + state (asks to confirm)
./uninstall.sh --all      # full removal: images + data
```

Safe by default — it removes only what sandboxd created (containers labelled
`sandboxd.managed=true`, the compose stack, the network) and **keeps your
workspaces** unless you pass `--data`/`--all`.

## Is this a good foundation for a startup?

Yes — that's exactly the point. If you want to ship an **AI app-builder or agent
SaaS** without first spending months building multi-tenant isolation, preview
routing, idle/wake cost control, and agent orchestration, sandboxd gives you
that core on day one, on a single inexpensive server, with margins you control.
It's a **strong, honest starting point** — beta-quality, MIT-licensed, and built
to be read and extended. Launch lean on it; harden as you grow (next section).

## Before you scale hard: what's simple on purpose, and what to harden

sandboxd is tuned for "**works anywhere with just Docker, in one command**."
To keep it that simple, a few things were left basic **on purpose**. None of
them affect the core loop (create → build → preview → sleep → wake → persist) —
they're the knobs to tighten once you have real users and real money on the line.
Plain version:

| Kept simple on purpose | Fine for | Do this when you're scaling / serious |
|---|---|---|
| **Container isolation** (hardened Docker), not full VMs | your own users running their own code | running **untrusted strangers' code** → put each tenant on its own VM, or use gVisor / Kata / Firecracker |
| **API auth is OFF by default** | local development | **turn it on** (`SANDBOXD_API_AUTH_DISABLED=false` + tokens) and never expose the API port unauthenticated |
| **Preview links are public** (anyone with the URL) | demos, sharing | gate sensitive previews (the private-sandbox forward-auth hook) |
| **Open, unlogged network egress** | most apps | add firewall / egress rules + logging |
| **Plain-directory workspaces**, no disk quota | a single server | add filesystem/volume quotas; plan multi-host sharding |
| **One server, one Docker socket** (the control plane is root-equivalent on the host) | starting out | treat the host as a trust boundary, keep it patched, isolate it, and don't co-locate unrelated secrets |

**The short version for a fast-scaling company:** the three that matter most are
(1) **stronger isolation** (VM-per-tenant) if you ever run untrusted code,
(2) **turn on API auth** and lock down the host, and (3) **plan for more than one
machine**. Everything else above is a config change, not a rewrite. Start lean,
revisit these as you grow — and PRs are very welcome ([`CONTRIBUTING.md`](CONTRIBUTING.md)).

### Known limitations (v0.3.0)

Tracked, non-blocking — details in [`docs/sandbox-manifest.md`](docs/sandbox-manifest.md):

- `DELETE /v1/sandboxes/{id}` **purges** the workspace, while the legacy internal
  `DELETE /sandbox/{id}` **stops and keeps** it.
- `keepalive_until` is honored but not surfaced in `GET /v1/sandboxes/{id}`.
- The wake/warming interstitial returns HTTP `200` (callers can't distinguish
  "warming" from "ready" by status code alone).
- Per-task `agent.log` can be empty on task timeout (transcript persistence WIP).
- **Docker is the only runtime provider today** (OCI/containerd/Kata are a future
  provider; see [`docs/sandbox-manifest.md`](docs/sandbox-manifest.md)).
- **Compose / local service stacks are not first-class yet**, and there is no
  built-in database/sidecar story — run **Postgres/Supabase/Neon etc. as remote
  services** for now.
- **No in-sandbox terminal yet** (no PTY endpoint).
- **Package managers**: the default install path is `pnpm`. A repo that pins
  `bun`/`yarn` via `packageManager`, or needs an external DB, won't boot
  zero-config — set an explicit `web.command` in `sandbox.yaml`.

## Community & roadmap

- **Roadmap** — what's shipped, what's next, and how versioning works before 1.0:
  [`ROADMAP.md`](ROADMAP.md).
- **Discussions** — questions, self-hosting help, and feature ideas:
  [github.com/tastyeffectco/sandboxd/discussions](https://github.com/tastyeffectco/sandboxd/discussions).
- **Contribute** — good first PRs include adding a runtime preset or an App Store
  recipe; see [`CONTRIBUTING.md`](CONTRIBUTING.md).
- **Security** — report vulnerabilities privately per [`SECURITY.md`](SECURITY.md).

## License

[MIT](LICENSE). Use it, ship it, sell what you build on it.

## Sponsors

sandboxd is free and MIT-licensed. **Sponsors keep it maintained and fund the
[deploy roadmap](https://github.com/sponsors/tastyeffectco)** — thank you to
everyone who chips in.

<!--
  SPONSOR LOGO WALL — how to add a sponsor:
  Drop a cell into the <p align="center"> grid below (highest tiers first):
      <a href="https://SPONSOR_SITE"><img src="LOGO_URL" width="130" alt="Sponsor Name"></a>
  Guidelines: logo ~130px wide, PNG or SVG, hosted on a stable URL (the
  sponsor's site or this repo's assets). Remove the placeholder <em> line once
  the first real logo lands. Ordering is by tier, then join date.
-->
<p align="center">
  <!-- sponsor logos go here -->
  <em>No sponsors yet — <a href="https://github.com/sponsors/tastyeffectco">be the first</a>.</em>
</p>
