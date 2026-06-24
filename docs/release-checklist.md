# Release acceptance checklist (v0.4.0)

Manual smoke test on a **disposable VPS** before cutting a release. Automated
gates (`go test`, OpenAPI contract test, console typecheck/build, console smoke
tests) cover the wiring; this checklist covers the things that need a real Docker
host, a real agent, and real time (idle/wake) — which CI deliberately does not.

> **sandboxd is the product; the console is a client.** Verify the API behavior
> first (curl / SDK); the console is a convenience on top of the same `/v1`.

## 0. Prerequisites
- [ ] Fresh Ubuntu VPS (not prod, not a shared host with another sandboxd/Traefik).
- [ ] An agent credential available in the sandbox env (e.g. `ANTHROPIC_API_KEY`)
      — required for the "run agent task" steps; without it the post-task pipeline
      still runs but the agent itself fails.

## 1. Install
- [ ] `scripts/dev/install-v04-ubuntu.sh` completes; stack healthy.
- [ ] `curl -fsS http://127.0.0.1:<API_PORT>/healthz` → `ok`.
- [ ] `GET /v1/presets` lists all five presets.
- [ ] (console) `console.<domain>` loads and shows the app list.

## 2. React / Vite
- [ ] Create an app with `runtime_preset=react-vite` (console New App, or `POST /v1/apps`).
- [ ] Create its sandbox; preview (`/`) returns **200**.
- [ ] Run an agent task that edits the homepage; task completes.
- [ ] Preview reflects the change (Vite HMR), still **200**.

## 3. Next.js (build-provoking)
- [ ] Create a `nextjs` app + sandbox; `/` and a `/_next/static/chunks/*` asset → **200**.
- [ ] Run an agent task that triggers a production build (e.g. asks for `next build`).
- [ ] After the task the preview is **not poisoned**: `/` and chunks still **200**
      (`restart_after_task` heals it; `web` process `restarts` incremented).

## 4. FastAPI (add endpoint + reload)
- [ ] Create a `fastapi` app + sandbox; `/health` → **200** on port **3000**
      (external/public preview, not just the internal probe).
- [ ] Run an agent task that adds an endpoint (e.g. `/ping`).
- [ ] `/ping` works **without a manual restart** (`uvicorn --reload`).

## 5. Node/Express & Worker (restart-after-task)
- [ ] `node-express`: agent adds a route → route live after the task (no manual restart).
- [ ] `worker`: agent changes `worker.sh` output → `worker.log` reflects it after the
      task; preview status is `none` (valid), worker process running.

## 6. Snapshot / fork / restore
- [ ] Stop an app's sandbox; `POST /v1/snapshots` succeeds (snapshot of running → 409).
- [ ] Snapshot excludes `node_modules`/`.next`/`.venv` (small artifact).
- [ ] `POST /v1/apps/{id}/fork` → new app boots **healthy**: `$HOME` owned by the
      sandbox user, deps reinstall **without EACCES**, fork preview returns **200**,
      and app changes (added endpoint/route) are preserved.
- [ ] `POST /v1/apps/{id}/restore` replaces the app's sandbox from a snapshot.

## 7. Process API + logs
- [ ] `GET /v1/sandboxes/{id}` includes `processes[]` (name/kind/running/pid/restarts).
- [ ] `GET /v1/sandboxes/{id}/processes/{name}/logs` returns recent lines;
      a bad/unknown process name → 400/404 (no path traversal).

## 8. Observability
- [ ] `GET /v1/apps/{id}/events` shows a newest-first timeline (app.created,
      config.created, task/build events).
- [ ] (console) Activity panel renders the same.

## 9. Config & Secrets
- [ ] `POST` a sensitive config value; `GET` returns `value_set:true` but **no
      plaintext** (redacted). Non-sensitive values are returned.
- [ ] (console) Config & Secrets panel shows the same; secret never displayed.

## 10. Time-aware lifecycle
- [ ] **Idle reaper**: an idle sandbox is stopped after the idle threshold.
- [ ] **Wake-on-request**: hitting a stopped app's preview wakes it (~1–2s) and serves.
- [ ] **Keepalive**: `POST /sandbox/{id}/keepalive {"until":<ts>}` keeps a sandbox
      alive past the idle threshold; a non-kept control is reaped.

## Known limitations (acceptable for v0.4.0)
- `DELETE /v1/sandboxes/{id}` **purges** the workspace; legacy `DELETE /sandbox/{id}`
  stops and keeps it. (Console uses the v1 purge path — confirm the wording.)
- `keepalive_until` is honored but not surfaced in `GET /v1/sandboxes/{id}`.
- The wake/warming interstitial returns HTTP `200` (status code can't distinguish
  "warming" from "ready").
- Per-task `agent.log` may be empty on task timeout (transcript persistence WIP).

## Sign-off
- [ ] All boxes above checked, or any failure understood + documented.
- [ ] No prod / shared-host collision during testing.
- [ ] `CHANGELOG.md` v0.4.0 reviewed.
