# sandboxd console

An optional web console for managing **apps** on top of sandboxd — see your
apps, open one, view its live preview, submit agent tasks, watch task logs, and
start/stop/snapshot its sandbox.

A Vite + React SPA. It talks **only** to the public sandboxd `/v1` API
(`docs/openapi.yaml`) — no Go imports, no database, no workspace access. That
boundary is deliberate: once `/v1` stabilizes, this folder splits cleanly into a
standalone `sandboxd-console` repo.

## Run it (console mode)

From the repo root:

```bash
docker compose --profile console up
```

Then open <http://console.localhost> (or `console.<PREVIEW_DOMAIN>:<HTTP_PORT>`).
Core mode (`docker compose up`, no profile) runs sandboxd without the console.

The console is routed through the **same Traefik as the previews**, by Host
header — `console.<domain>` → console, `*.preview.<domain>` → sandboxes — so it
shares one entrypoint. nginx serves the built SPA and proxies `/v1` to the
`sandboxd` service on the internal network, so the browser uses same-origin
relative paths: no CORS, and no auth in the single-user default.

## Develop

```bash
pnpm install
pnpm dev            # http://127.0.0.1:8787, proxies /v1 to $SANDBOXD_URL (default 127.0.0.1:9090)
pnpm build
pnpm test:e2e       # Playwright — needs the stack up (see above)
```

## App detail screen

Per app: live **Preview / endpoint** (worker-only apps show endpoint `none`,
which is valid), agent **task** submit + streamed logs, **start/stop**,
**Config & Secrets** (sensitive values are write-only — set once, never shown),
**Snapshots** (capture, plus confirm-gated restore/fork), an **Activity**
timeline (durable app events, newest-first), and a **Processes** panel
(name/kind/running/pid/restarts with per-process recent logs).

New App offers a **runtime preset** picker (React/Vite, Next.js, Node/Express,
FastAPI, Worker), data-driven from `GET /v1/presets`; the chosen preset is stored
on the app and applied to its sandbox.

## Scope (MVP)

Single-user, auth-off, public previews. Multi-user auth, private-preview
embedding, and a richer UI come later. The console assumes a local/loopback
sandboxd.
