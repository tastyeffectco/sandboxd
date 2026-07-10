# Upgrade & migration audit — v0.2.0 → 0.3.0 (and the per-release process)

**Date:** 2026-07-09
**Scope:** what breaks for existing users on `v0.2.0` when they take `0.3.0`,
whether data/apps survive, the migration process to standardize per release, and
a 1-click update design.

## Verdict (TL;DR)

**The upgrade is largely SAFE** — additive DB migrations, data + `.env` preserved,
no `down -v`. Four things to handle:

1. ⚠️ **runtimed version skew** — long-running `0.2` sandboxes keep the old
   in-container runtimed until recreated.
2. ⚠️ **No pre-upgrade DB backup** in `install.sh` (rollback safety gap).
3. ℹ️ **New `.env` knobs** (all safe defaults) — one gotcha: the console needs
   `CONSOLE_BASIC_AUTH`.
4. ℹ️ **Agent re-connect** — the credential proxy changes how agents auth (config
   step, not data loss).

## Release state (numbering was ambiguous — pinned)

- Latest **released tag = `v0.2.0`**. There is **no `0.3.0` tag**. Shipping 0.3 =
  merge the dev line to `main` + tag `v0.3.0`.
- Compose image tags: `v0.2.0` shipped `sandboxd-*:1.0.0` (stray numbering); HEAD
  uses `sandboxd-*:0.3.0`. The bump means `docker compose build` produces new
  images; old `:1.0.0` images linger locally (harmless, but see skew below).

## 1. Database — SAFE

- **8 new migrations `0014`–`0021`** apply on upgrade. All are **additive**
  (`CREATE TABLE`/`CREATE INDEX`; the only `NOT NULL`s are on *new* tables). **No
  `DROP`, no destructive `ALTER`, no data rewrite** in the delta. (`0008`'s
  `agent_config` drop predates `v0.2.0` — already applied.)
- Runner (`store/migrate.go`): forward-only, **transactional per file**, tracked
  in a `migration` table, applied in numeric order. Called from `store.go:104`
  during init → **fail-closed**: a bad migration blocks control-plane startup, it
  never runs half-migrated. **No down migrations → downgrade is unsupported.**

## 2. Data, apps & the secrets key — SAFE

- Data dir is an **absolute bind-mount** (`${SANDBOXD_DATA_DIR:-/var/lib/sandboxed}`
  on both sides). `install.sh`/updater never `down -v`, so the **SQLite DB, the
  secrets keyfile, and all workspaces persist** across `compose up -d --build`.
  Existing `app`/`sandbox` rows survive.
- **Secrets key:** `SANDBOXD_SECRETS_KEY` (env) → else an auto-generated `0600`
  `secrets.key` in the data dir. Persists with the data dir. And `0.2` users have
  **no `app_config` secrets yet** (the table is new in 0.3), so **zero secret-loss
  risk** this upgrade. General rule to document: *never wipe the data dir; it holds
  the key.*

## 3. Existing sandboxes — MOSTLY SAFE + one skew risk

- **Reconcile on startup** (`reconcile/reconcile.go`): for each known sandbox —
  container present+running → adopted; container missing → **marked stopped, NOT
  auto-recreated**; orphan containers/mounts (no row) → logged, not deleted. So
  app rows + workspaces are preserved; sandboxes come back via **wake-on-request**,
  which recreates from the workspace using the current `SANDBOXD_IMAGE` (`:0.3.0`).
- ⚠️ **Version skew (the real risk):** a *still-running* `0.2` sandbox keeps the
  `0.2` runtimed baked into `sandboxd-base:1.0.0`. The `0.3` control-plane may call
  runtimed behavior the old binary lacks → failures on that sandbox until it's
  recreated on `sandboxd-base:0.3.0`. **Mitigation:** bounce old sandboxes
  post-upgrade (`stop` → next request wakes them fresh on the new image), or
  delete → respin. **Recommend:** reconcile flags image-mismatched running
  sandboxes for lazy restart, and the release notes tell users to bounce them.

## 4. Config (`.env`) — SAFE, with notes

- `.env` is **preserved**: it's untracked, so the updater's `git reset --hard`
  can't touch it, and `install.sh` explicitly *"leaves it untouched."*
- **New keys a `0.2` `.env` lacks (all have safe code defaults):**
  `SANDBOXD_SECRETS_KEY` (→ auto keyfile), `SANDBOXD_DEFAULT_AGENT` (→ `opencode`),
  `SANDBOXD_OPENCODE_ZEN_PATH` (→ default), `CONSOLE_BASIC_AUTH`.
- **Gotcha:** the new **console** profile is **fail-closed** — enabling it without
  `CONSOLE_BASIC_AUTH` returns `401`. Non-console users are unaffected.

## 5. Behavior / API changes 0.2 → 0.3

- **Credential-injecting proxy + `opencode` default:** agents now auth via the
  control-plane proxy; users **re-connect an agent** through `/v1/agents/*` (a
  config step, not data loss). No secret ever enters a sandbox.
- No destructive endpoint removals found in a spot-check of the delta. *(Not an
  exhaustive endpoint-by-endpoint diff — see "follow-ups".)*

## 6. Upgrade mechanism today

- Re-run the one-liner **or** `git pull && ./install.sh`. The bootstrap does
  `git fetch --depth 1 && git reset --hard FETCH_HEAD` (preserves untracked `.env`
  + the absolute data dir), then `docker compose up -d --build`. **Idempotent.**
- ⚠️ **Gap: no DB backup** before upgrade. A failed migration or a rollback need =
  no snapshot to restore.

## 7. Proposed standard per-release migration process

1. **Migrations forward-only + additive.** Destructive changes done as
   **expand → migrate → contract** across two releases (add new, backfill, drop the
   old only next release) so any single upgrade is safe and reversible-in-practice.
2. **Pre-upgrade DB backup in `install.sh`:** `cp sandboxd.db
   backups/sandboxd-<ts>.db` (retain last N) before `up -d`. One-line, big safety.
3. **Downgrade guard:** control-plane refuses to start if the DB records a
   migration id **higher** than the binary knows, with a clear message (prevents a
   silent partial downgrade).
4. **CHANGELOG "Upgrade notes"** every release: new/changed `.env` keys, image
   bumps, and any manual step (e.g. "bounce sandboxes after this release").
5. **Image-bump handling:** document that sandboxes upgrade runtimed **lazily on
   next wake**; provide a "bounce all sandboxes" step (or endpoint).
6. **Upgrade smoke lane:** `release-checklist.md` covers from-zero; **add a
   v(N-1)→vN lane** (install v0.2.0, create an app, upgrade, verify app + DB + wake).

## 8. One-click update — design

**Today: none.** Path forward, low-risk first:

- **CLI `sandboxd update` (near-term):** backup DB → `git fetch`/`reset` to latest
  tag → `docker compose build` → `up -d` → wait healthy → print `old → new`
  version. Just wraps the updater with a backup + version report. Low risk.
- **Console — read-only first (MVP):** add `GET /v1/system/version` (running
  version + latest release from GitHub) → the console shows an **"update available"
  banner** with the **copy-paste command**. No privileged action; ships safely.
- **Console — true 1-click (later):** `POST /v1/system/update` (control-plane has
  the docker socket) that pulls/builds/recreates the stack. **Hard part:** it must
  recreate the very container it runs in → needs a **detached updater** (a
  short-lived helper container or a host unit) so it doesn't kill itself mid-update.
  Privileged + security-sensitive → gate behind auth, phase after the MVP.

## 9. Audit result

| Area | Risk | Status | Action |
|---|---|---|---|
| DB migrations 0014–0021 | data loss | ✅ safe (additive, fail-closed) | none |
| Data dir / apps / workspaces | loss on upgrade | ✅ preserved (abs bind-mount) | document "never wipe data dir" |
| Secrets key | undecryptable secrets | ✅ safe (keyfile persists; no secrets yet) | keep data dir |
| Running 0.2 sandboxes | runtimed skew | ⚠️ lazy | bounce post-upgrade; reconcile flag |
| `.env` new keys | missing config | ✅ safe defaults | list in upgrade notes |
| Console profile | 401 lockout | ℹ️ by design | require `CONSOLE_BASIC_AUTH` |
| Agent auth (proxy) | broken agents | ℹ️ reconfig | re-connect via `/v1/agents` |
| Rollback | no backup | ⚠️ gap | add DB backup to `install.sh` |

## Follow-ups (not blocking 0.3)

- Add the `install.sh` DB-backup + the downgrade guard (§7.2, §7.3).
- Exhaustive `v0.2.0 → 0.3` **API surface diff** (contract test the OpenAPI spec
  against both tags) to certify no silent breaking removals.
- `GET /v1/system/version` + console banner + `sandboxd update` CLI (§8).
