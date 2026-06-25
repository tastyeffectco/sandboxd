# sandboxd v0.4.0 — Self-hosted control plane for AI-built apps

v0.4 turns sandboxd into a complete, operable platform for AI-built apps: a **web
console**, one-step **runtime presets**, **live preview URLs**, **agent tasks**,
**app config & secrets**, **snapshots / fork / restore**, an **activity/events**
timeline, **per-process logs**, and a **settings** view with editable lifecycle
controls — all self-hosted on one machine that just needs Docker.

## Highlights

- **Web console** — create and open apps, watch live previews, run agent tasks,
  and manage config/secrets, snapshots, activity, and settings from a browser
  (or use the same public `/v1` API directly).
- **Runtime presets** — `GET /v1/presets` + `runtime_preset` on create for
  **React/Vite, Next.js, Node/Express, FastAPI, Worker**. Each seeds a starter,
  writes a `sandbox.yaml`, boots, and reloads after agent edits (Vite HMR,
  `uvicorn --reload`, or a post-task process restart — including healing a
  Next.js `next build` that would otherwise poison the dev server).
- **Live preview URLs** — every app gets its own shareable link; idle sandboxes
  sleep and wake on request.
- **Agent tasks** — submit a prompt, stream progress, get an honest result
  (`build_status` passed/failed/skipped, `preview_ok`, `app_healthy`).
- **App config & secrets** — per-app key/values; sensitive values are write-only,
  encrypted at rest, and never returned.
- **Snapshots / fork / restore** — capture a workspace (generated/dependency dirs
  excluded), fork it into a new app, or restore in place — with workspace
  ownership normalized so forks boot cleanly.
- **Activity / events** — a durable per-app timeline.
- **Process logs** — per-process status (web + workers) and tail-able logs.
- **Settings / lifecycle** — a read-only instance overview, plus editable
  idle-reap / keepalive tuning that hot-applies (a strict allowlist; nothing else
  is editable, and no secret is ever shown).

## Install

On a fresh Ubuntu host:

```bash
git clone https://github.com/tastyeffectco/sandboxd && cd sandboxd
git checkout v0.4.0
scripts/dev/install-v04-ubuntu.sh
```

It builds the base image + control plane + console and brings the stack up with
Docker Compose (Traefik for preview/console URLs, the API on loopback). See
[`docs/release-checklist.md`](release-checklist.md) for the full smoke test.

## Known limitations (non-blocking)

- `DELETE /v1/sandboxes/{id}` **purges** the workspace; the legacy internal
  `DELETE /sandbox/{id}` stops and keeps it.
- `keepalive_until` is honored but not surfaced in `GET /v1/sandboxes/{id}`.
- The wake/warming interstitial returns HTTP `200`.
- Per-task `agent.log` can be empty on task timeout.
- **Docker backend only** (OCI/containerd/Kata are a future provider).
- **Not yet:** Git/GitHub import, managed databases/sidecars, and
  Docker-Compose-inside-the-sandbox.

## Validation

Validated by a from-zero VPS release-candidate QA pass against this branch
(install → console → presets → previews → agent tasks → snapshots/fork →
process logs → events → config/secrets redaction → settings/lifecycle →
idle/wake/keepalive), plus the full automated gate (Go test + OpenAPI contract +
console typecheck/test/build).

**Full changelog:** see [`CHANGELOG.md`](../CHANGELOG.md).
