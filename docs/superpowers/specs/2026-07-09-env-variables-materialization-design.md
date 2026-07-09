# Environment variables — a console env editor over existing `/v1` primitives

**Date:** 2026-07-09
**Status:** Design approved, pending spec review
**Area:** **console** (the web UI, a pure `/v1` client). **Zero changes to
`sandboxd` core** (control-plane / runtimed) for the MVP.

## Problem

Users need to set/override environment variables for an app — the driving case:
*"I have an Astro blog with a `.env` for local dev. On deploy I want to change
those values without the `.env` living in GitHub."*

App **config & secrets** exists in the DB (`app_config`, encrypted, write-only)
but is **store-only** — nothing delivers it to the running app, and secrets are
never returned, so a client can't read them back to materialize a file. Rather
than build a server-side materialization pipeline, we deliver this **entirely in
the console** using primitives that already exist.

## Decision (the reframe)

**The gitignored `.env` file in the workspace IS the store.** The console is an
**"Environment variables" editor** over that file. No `app_config`, no DB copy,
no runtimed/control-plane changes. Everything runs through existing `/v1` calls.

Consequences we consciously accept:

- **The config-vs-secret flavor collapses.** Encryption/redaction only mattered
  for the DB store; a gitignored file neither encrypts nor redacts. Every entry
  is just an env var in a file. The one security property that matters —
  *"not pushed to GitHub"* — applies to the whole file equally. (The console may
  offer a cosmetic *mask-in-UI* toggle for shoulder-surfing; it's not storage.)
- **Values are viewable** by opening the file in the **Files tab** — which is
  exactly the "how do I see my env" answer we already chose.
- **No auto-apply.** Env is read at process startup for the stacks that matter
  (Node/Express, FastAPI read once; Vite/Next dev only *sometimes* restart on
  `.env` change). So we expose a **manual Restart** instead of relying on HMR.

## Existing primitives — proving zero core

| Need | Existing `/v1` primitive | Verified |
|---|---|---|
| Write `.env.local` / `.env` | `PUT /v1/sandboxes/{id}/files?path=<rel>` — atomic write, path-cleaned, **handles dotfiles** | `v1_files_write.go` + test writes `.config/...` |
| Read current file / `.gitignore` | `GET /v1/sandboxes/{id}/files/content?path=<rel>` | `v1_files.go` |
| Warn if target is git-tracked | `GET /v1/sandboxes/{id}/git/status` (returns `untracked`/`added`/…) | `v1_git.go` |
| Apply the change (restart) | `POST /v1/sandboxes/{id}/stop` → next request **wakes** into a fresh boot that re-reads the file; `stop` keeps the workspace | quickstart + wake handler |
| View values | open the file in the **Files tab** (a files read) | existing |

## Design (console)

### 1. Stack-aware target file

The console picks the target from the app's known preset/stack (no manifest change):

| Preset / stack | Target file | Why |
|---|---|---|
| `react-vite`, `nextjs`, Astro | `.env.local` | native higher-precedence override; gitignored by convention; never touches a committed `.env` |
| `node-express`, `fastapi`, `worker`, unknown | `.env` | plain `dotenv` / `python-dotenv` only read `.env` |

### 2. The env editor

- A **key/value editor** (rows), keys validated as env names (`[A-Za-z_][A-Za-z0-9_]*`).
- **Load:** `GET …/files/content?path=<target>` → parse existing managed block
  (round-trips values the user previously set; ignores/preserves anything outside
  the managed block).
- **Save:** compose a dotenv document — a managed banner + `KEY="value"` lines
  with correct quoting/escaping (newlines, quotes, `#`, spaces) — and
  `PUT …/files?path=<target>` (atomic).
  ```
  # ---- managed by sandboxd console — edit here or in this file ----
  DATABASE_URL="postgres://..."
  PUBLIC_SITE=https://example.com
  ```

### 3. Keep it out of GitHub

- On save, the console reads `.gitignore` (`GET …/files/content`), and if the
  target file isn't ignored, appends a managed block and writes it back
  (`PUT …/files?path=.gitignore`).
- It then calls `GET …/git/status`; if the target shows as **tracked** (not
  `untracked`) — the committed-`.env` case — it shows a **warning banner**:
  *"`.env` is committed to git; values here may be pushed. Run `git rm --cached
  .env` to keep them private."* No auto-untrack.

### 4. Apply = manual Restart

- The console gets a general **"Restart app"** action (useful beyond env),
  implemented as `POST …/stop` (the next preview request wakes a fresh boot).
- After a save, the editor surfaces *"Restart to apply"* pointing at that action.

### 5. View values

Values are shown in the editor (optionally masked in-UI) and are always visible
by opening the target file in the **Files tab**. There is no API that "reveals"
anything the file doesn't already show.

## What we are NOT building (and why)

- ❌ Manifest `env:` section, runtimed `applyEnv`, `POST /env` — unnecessary; the
  console writes the file directly.
- ❌ Control-plane resolve/decrypt/debounce — no DB copy to resolve.
- ❌ `app_config` changes / encryption / key-validation migration — env no longer
  routes through `app_config`.
- ❌ Auto-restart — replaced by manual Restart (HMR is unreliable for env).

## Trade-offs

- **File-only store:** values live only in the gitignored workspace file (plaintext
  on the sandbox disk — unavoidable for any env). Survives restarts and `stop`/wake;
  **lost on a full workspace reset**; **carried into snapshots/forks** (a fork
  inherits its env — usually desired; note it before sharing a snapshot publicly).
- **No server-side guarantee:** a non-console API user manages the file themselves
  — fine, it's just a file. If a first-class server-side env ever becomes needed,
  the earlier core design (managed materialization) is the fallback, additive.

## Docs

- New **Environment variables** guide with the **Astro walkthrough**: local `.env`
  → deploy-time override via a gitignored `.env.local` edited in the console, never
  in GitHub.
- Update `concepts.md` (env vars = a gitignored file the console edits),
  `guides/console.md` (the env editor + Restart), and the presets table
  (target-file column). No architecture change (nothing new server-side).

## Testing (console)

- **dotenv compose/parse:** round-trips tricky values (quotes, newlines, `#`,
  spaces, `=` in value); preserves content outside the managed block.
- **key validation:** rejects non-env-safe names in the editor.
- **gitignore ensure:** appends once, idempotent, preserves existing entries.
- **warn-if-tracked:** banner shows when `git/status` reports the target tracked;
  hidden when `untracked`.
- **target selection:** `.env.local` for Vite/Astro/Next, `.env` otherwise.
- **restart:** Restart action calls `stop`; editor shows "Restart to apply".
- **integration (against a live sandbox):** set vars → file appears, gitignored →
  open in Files tab shows them → restart → app reads them → `git/status` never
  lists the target as staged.

## Optional future (not MVP)

- A lightweight `POST /v1/sandboxes/{id}/processes/{name}/restart` to bounce just
  the web process (avoids the wake cold-start). Small, additive, if UX demands it.
- Server-side materialization (the shelved core design) only if a non-console
  client needs env without writing files itself.

## Open questions

- **Managed-block parsing:** simplest is "the console owns the whole file." If we
  must coexist with hand-edited lines, define the managed-block markers precisely
  (start/end sentinels) — decide during implementation.
