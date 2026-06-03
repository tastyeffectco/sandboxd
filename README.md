<h1 align="center">sandboxed</h1>

<p align="center">
  <b>The open-source engine for AI app-builder products.</b><br/>
  Give every user an isolated cloud dev environment, a built-in coding agent,
  and a live preview URL — self-hosted, on one machine, in one command.
</p>

<p align="center">
  <a href="LICENSE"><img alt="License: MIT" src="https://img.shields.io/badge/license-MIT-green.svg"></a>
  <img alt="Runs on Docker" src="https://img.shields.io/badge/runs%20on-Docker-2496ED.svg">
  <img alt="Single binary control plane" src="https://img.shields.io/badge/control%20plane-single%20Go%20binary-00ADD8.svg">
  <img alt="Status: beta" src="https://img.shields.io/badge/status-beta-yellow.svg">
</p>

---

## What is sandboxed?

**sandboxed turns one Linux box into a fleet of isolated, on-demand dev
sandboxes — each with a real shell, the common toolchains, a coding agent, and
its own preview URL.** You drive it with a small HTTP API:

```
POST /sandbox          → an isolated container spins up
POST .../tasks         → an AI agent builds an app inside it
http://<id>.preview... → the running app, live, on a shareable URL
```

Sandboxes **stop when idle** to free memory and **wake on the next request**, so
a modest server can host many of them. Workspaces persist on disk across stops
and reboots. The whole thing is a single Go control plane that drives the Docker
daemon, fronted by Traefik — no Kubernetes, no database server, no message bus.

This is the infrastructure that sits behind "describe an app → watch it get
built → see it running" products. sandboxed gives you that core, open source,
on your own hardware.

```
            ┌──────────────── your host (just needs Docker) ────────────────┐
 browser ──▶│  Traefik  ──▶  sandbox  (coding agent + dev server :3000)      │
            │     ▲              ▲   ▲                                        │
 API/CLI ──▶│  sandboxd ─────────┘   └─ workspace dir (persists)             │
            │     │  SQLite (source of truth) · idle→stop · request→wake      │
            └─────┴────────────────────────────────────────────────────────-─┘
```

## Why sandboxed?

If you're building an **AI app-builder, an agent platform, a coding playground,
or a per-user preview product**, the hard part isn't the prompt — it's the
infrastructure underneath it:

- **Multi-tenant isolation** so one user's code can't touch another's.
- **Per-user preview URLs** with automatic routing and TLS.
- **Cost control** — idle environments must release memory, or your bill explodes.
- **Agent orchestration** — run a coding agent against a workspace, stream its
  progress, capture the result.
- **Persistence, wake-on-demand, reconciliation after a crash or reboot.**

That's months of platform work. sandboxed is that platform, distilled to one
command:

- ⚡ **One-command install.** `./install.sh` and you have a working API + previews.
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

## Quick start

Requirements: **Docker Engine + the Compose plugin**, on Linux. That's it.

### 1. Install

```bash
git clone https://github.com/tastyeffectco/sandboxes.git
cd sandboxes
./install.sh
```

`install.sh` checks Docker, writes a `.env`, builds the sandbox base image + the
control plane, and starts the stack. The API is then live at
`http://127.0.0.1:9090` (verify: `curl http://127.0.0.1:9090/healthz` → `ok`).

### 2. Have an agent build an app

The base image already includes the **OpenCode** and **Claude Code** CLIs. Hand
a sandbox a prompt and watch it build (OpenCode runs on its free plan out of the
box; pass your own provider key via `env` to use your account):

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

To use your own model account instead of the free plan, inject a key at create
time — it's available to the agent and any shell in the sandbox:

```bash
curl -s -XPOST $API/sandbox -d '{"ports":[3000],"env":{"ANTHROPIC_API_KEY":"sk-ant-..."}}'
```

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

> **Just want a shell, no agent?** Skip step 2 and run anything via the exec API:
> `curl -XPOST $API/sandbox/$ID/exec -d '{"cmd":["bash","-lc","cd ~/workspace/app && python3 -m http.server 3000"]}'`
> then open the same preview URL.

## API

Base URL = `http://127.0.0.1:9090` (set by `SANDBOXED_API_BIND`). Auth is **off
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
| `POST /v1/sandboxes/{id}/tasks` | `{"prompt":"…","agent":"opencode"}` | run a coding agent headlessly |
| `GET /v1/sandboxes/{id}/tasks/{taskId}` | — | task result |
| `GET /v1/sandboxes/{id}/tasks/{taskId}/events` | — | live task event stream (SSE) |
| `GET/PUT /v1/sandboxes/{id}/files` | `{"path","content","append"}` | list / read / write workspace files |
| `GET /healthz`, `GET /readyz` | — | liveness / readiness |

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
| `SANDBOXED_DATA_DIR` | `/var/lib/sandboxed` | where workspaces + state live |
| `SANDBOXED_API_BIND` | `127.0.0.1:9090` | where the control-plane API is published |
| `SANDBOXD_API_AUTH_DISABLED` | `true` | open API for local use; set `false` + tokens for prod |

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

Safe by default — it removes only what sandboxed created (containers labelled
`sandboxed.managed=true`, the compose stack, the network) and **keeps your
workspaces** unless you pass `--data`/`--all`.

## Roadmap & current limitations

sandboxed v1 optimizes for "runs anywhere with just Docker." A few things are
deliberately simple — none affect the core loop (create → build → preview →
idle → wake → persist), and each is a known place to harden:

- **No hard per-workspace disk quota.** Workspaces are plain directories on a
  shared filesystem. Add fs/volume quotas if you need them.
- **Soft memory throttle off by default.** The hard per-sandbox `--memory`
  ceiling still applies; the gentler cgroup `memory.high` is opt-in.
- **Egress is default-allow, unlogged.** Add host firewall rules / a proxy for
  egress control.
- **Snapshots/templates are experimental** on directory storage.

Contributions toward any of these — and toward more agent backends — are very
welcome. See [`CONTRIBUTING.md`](CONTRIBUTING.md).

## Security

Be deliberate before exposing this to the open internet. Honest notes:

- **Sandboxes run real user code under hardened `runc`** (dropped capabilities,
  `no-new-privileges`, read-only rootfs, memory/PID/file-descriptor limits) — but
  this is **container isolation, not VM isolation**. It's designed for
  **authenticated, accountable users running their own code**, not anonymous
  hostile multi-tenancy. If you need to run untrusted strangers' code, put each
  trust domain on its own VM, or add a stronger runtime (gVisor/Kata/Firecracker).
- **The control plane holds the Docker socket**, which is root-equivalent on the
  host. Treat the host as part of your trust boundary, keep it patched, and don't
  co-locate unrelated sensitive workloads.
- **The API ships with auth disabled** for a smooth local start. **Enable it
  before any non-local deployment** (`SANDBOXD_API_AUTH_DISABLED=false` +
  `SANDBOXD_API_TOKENS`) and never publish the API port to the internet
  unauthenticated.
- **Egress is unrestricted by default** — a sandbox can reach the network freely.
  Add firewall/egress controls if that's a concern for your users.
- **Preview URLs are unauthenticated by default** (anyone with the URL can view
  a public sandbox). Private sandboxes gate access via a forward-auth hook; wire
  it up before serving sensitive previews.

None of this is exotic — it's the standard "you're running a server that
executes code" checklist. Follow it and sandboxed is a solid base.

## Is this a good foundation for a startup?

Yes — that's exactly the point. If you want to ship an **AI app-builder or agent
SaaS** without first spending months building multi-tenant isolation, preview
routing, idle/wake cost control, and agent orchestration, sandboxed gives you
that core on day one, on a single inexpensive server, with margins you control.
It's a **strong, honest starting point** — beta-quality, MIT-licensed, and
designed to be read and extended. Launch lean on it, harden the items above as
you grow, and contribute the improvements back.

## License

[MIT](LICENSE). Use it, ship it, sell what you build on it.
