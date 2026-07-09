# Environment variables — materializing config & secrets into the app

**Date:** 2026-07-09
**Status:** Design approved, pending spec review
**Area:** control-plane (`internal/api`, `internal/store`, `internal/gitimport`), `cmd/runtimed`, presets, docs

## Problem

App **config & secrets** is *store-only* today. The pieces that exist:

- `app_config` table + CRUD API (`/v1/apps/{id}/config`), AES encryption for
  sensitive values (`ValueCiphertext`/`ValueNonce`), redaction on read, tenant
  scoping, and audit — all covered by `v1_app_config_test.go`.

The missing piece: **nothing delivers those values to the running app.**
`ListAppConfig` is only ever read by the API list endpoint; the web process
launches via `bash -lc` with a plain `os.Environ()` (`runtimed/process.go`).
Stored config/secrets never reach the app.

Concrete user need (the driving case): *"I have an Astro blog with a `.env` for
local dev. On deploy I want to change those values without the `.env` living in
GitHub."*

## Goal

Materialize stored config/secrets into a **stack-aware, gitignored managed env
file** that `runtimed` writes into the workspace and reloads live — so setting a
config/secret makes it a real environment variable the app reads, and it is never
pushed to git.

## Non-goals

- Not building the console UI in this plan (the "developer view" is a thin
  **follow-on** that consumes what this exposes — see §9).
- Not adding per-entry "targets" or non-env config consumers (YAGNI; the model
  below leaves room to add later).
- Not encrypting values on disk *inside* the sandbox — resolved values must be
  plaintext for the app to read them (accepted trade-off; see §7).

## Unified model: config/secrets **are** environment variables

There is **one** concept: the app's **environment variables**. Each entry is one
of two flavors, distinguished only by the existing `sensitive` flag:

- **config** — a non-sensitive env var (stored plaintext, returned on read).
- **secret** — a sensitive env var (encrypted at rest, redacted on read).

Both become env vars in the managed file. No separate "env variables" surface.

**Implication:** keys become env-var *names*, so `validConfigKey` is tightened to
`^[A-Za-z_][A-Za-z0-9_]*$` (max 256). Existing rows that violate this are handled
by a **warn-not-break** migration (§8).

**Encryption & visibility (decided).** Only sensitive **values** are encrypted
(with `SANDBOXD_SECRETS_KEY`); keys/names are always plaintext — needed to list,
validate, and write the file. The API stays **strict write-only** for secrets:
values are never returned and are redacted forever on read. *Why encrypt:* the
managed `.env` is plaintext on one isolated, ephemeral container, but the
**database** holds every app's secrets plus backups — encrypting at rest means a
stolen `sandboxd.db` yields no live credentials, and it blocks API-token
exfiltration. *If a developer wants to see an effective value:* they open the
managed env file in the sandbox (Files tab / workspace download) — the file, not
the API, is the source of truth for values. **No reveal endpoint.**

## Design

### 1. Manifest gets an `env` section

Add an optional block to `Manifest` (`cmd/runtimed/manifest.go`):

```yaml
env:
  file: ".env.local"   # target file for managed values; per-stack default, overridable
```

```go
type Manifest struct {
    Version int
    Web     *WebProc
    Build   *BuildSpec
    Workers []Worker
    Env     *EnvSpec   `yaml:"env"` // NEW; nil => stack default
}
type EnvSpec struct {
    File string `yaml:"file"` // e.g. ".env.local" or ".env"
}
```

**Per-preset defaults** (preset manifests in `internal/preset/preset.go`):

| Preset / stack | `env.file` | Why |
|---|---|---|
| `react-vite`, `nextjs` | `.env.local` | native higher-precedence override; gitignored by convention; never touches a committed `.env` |
| `node-express`, `fastapi`, `worker` | `.env` | plain `dotenv` / `python-dotenv` only read `.env` |

**Runtime detection** (imported repos) sets `.env.local` for Vite/Astro/Next
recipes, `.env` otherwise. If a manifest omits `env`, `runtimed` applies the
stack default; a hardcoded fallback of `.env` applies when the stack is unknown.

### 2. Effective-env resolution (control plane)

New helper (e.g. `internal/api` or a small `internal/appenv` package):

```
resolveEffectiveEnv(ctx, appID) -> map[string]string
  - ListAppConfig(appID)
  - for each entry: if sensitive -> secrets.Decrypt(ciphertext,nonce); else plaintext
  - return {KEY: value}
```

The **secrets key never leaves the control plane**; only the resolved values are
sent onward.

### 3. `runtimed` — `applyEnv` + `POST /env`

New endpoint on the runtimed server (`cmd/runtimed/server.go`):

```
POST /env   { file: ".env.local", vars: {KEY: VALUE, ...} }
```

`applyEnv(file, vars)`:

1. **Write** `file` atomically (`tmp` + `rename`) with a managed banner and
   correct dotenv quoting/escaping (values with newlines, quotes, `#`, spaces):
   ```
   # ------------------------------------------------------------
   # Managed by sandboxd — do not edit. Generated from app config.
   # ------------------------------------------------------------
   DATABASE_URL="postgres://..."
   PUBLIC_SITE=https://example.com
   ```
2. **Ensure `.gitignore`** contains `file` — append a managed block if absent:
   ```
   # managed by sandboxd
   .env.local
   ```
3. **Warn if tracked**: if `file` is already tracked by git
   (`git ls-files --error-unmatch <file>` succeeds), emit a `config.env.tracked`
   activity event / log — *"`<file>` is tracked in git; managed values may be
   pushed. Untrack it (`git rm --cached <file>`) to keep them private."* Do **not**
   auto-untrack.
4. **Reload**: restart the `web` process (+ workers); if the stack is build-time
   (has a non-empty `build.command` and a `web` that serves a build), re-run
   `build` before/with the restart.

`applyEnv` also runs at **boot, before the web process starts**, so the app comes
up with values present.

### 4. Control-plane lifecycle

- **On sandbox boot:** `resolveEffectiveEnv` → call runtimed `/env` before web
  start (wired into the existing boot/wake path).
- **On config change** (`POST`/`PATCH`/`DELETE /v1/apps/{id}/config`):
  re-resolve → call `/env`, **debounced** (~500ms) so a burst of edits collapses
  into one rewrite+restart. This delivers the approved *auto-apply + restart*.

### 5. Git-push safety

- The managed file is always in `.gitignore`, so `git add -A` cannot stage it.
- For `.env`-target stacks where the repo committed `.env`, we **warn** (§3.3)
  rather than untracking — the operator decides.
- Optional defense-in-depth: `gitimport/push` skips a managed file that is
  untracked-but-present (already the case via gitignore; no extra pathspec needed
  unless a tracked target is detected, in which case the warning is the contract).

## Data model

No schema change. Reuse `app_config`. Only `validConfigKey` tightens (§Unified
model). `access_policy` is retained but out of scope here.

## API

- Config endpoints unchanged in shape. `GET /v1/apps/{id}/config` responses gain
  a derived, read-only **`env_file`** hint (the resolved target file) so clients
  can show "written to `.env.local`". Optional: a `warnings` field surfacing the
  `tracked` condition.
- No new secret ever returned; redaction unchanged.

## 9. Developer view (console — follow-on, described not built here)

The console Config tab becomes **Environment variables**: one list, each row
badged `secret`/`config`, showing the target `env_file` and a badge *"written to
`.env.local` · gitignored · never pushed"*, plus the `tracked`-warning banner
when present. Secret values render redacted (the API won't reveal them); a
*"view values"* affordance deep-links to the managed env file in the **Files tab**
(the only place values are visible). Consumes `env_file` + `warnings` from §API.
Separate spec/plan.

## Docs

- New **Config & secrets** guide (framed as *Environment variables*) with the
  Astro walkthrough: local `.env` → deploy-time override via managed `.env.local`,
  not in GitHub.
- Update `concepts.md` (the env-variables concept), `reference/architecture.md`
  (add the env-materialization flow), `guides/runtime-and-store.md` (the `env:`
  block), and the presets table (managed-file column).

## Testing

**`runtimed`**
- dotenv writer: quoting/escaping of tricky values; atomic replace; managed banner.
- `.gitignore` ensure: appends once, idempotent, preserves existing entries.
- warn-if-tracked: emits event when target is tracked, silent otherwise; never untracks.
- reload: web (+workers) restart on apply; build-time stack re-runs build.
- boot ordering: `applyEnv` runs before web start.

**Control plane**
- `resolveEffectiveEnv`: decrypts sensitive, passes through plaintext.
- lifecycle: `/env` called on boot and on config change; debounce collapses bursts.
- key validation: rejects non-env-safe names on create; warn-migration for legacy.

**Integration**
- set config → gitignored managed file appears with value → app reads it →
  change value → restart → app sees new value → managed file never staged on push.

## Rollout / migration

- Ship `env.file` defaults in presets; existing sandboxes pick it up on next boot.
- Legacy `app_config` rows with non-env-safe keys: **warn** (event + log) and skip
  those keys from the managed file; do not delete or break. Document the fix
  (rename the key).

## Risks / open questions

- **On-disk plaintext in the sandbox** — inherent to letting the app read env;
  accepted. Mitigated by container isolation + the file being gitignored.
- **Restart churn** on rapid edits — mitigated by debounce; revisit if noisy.
- **Build-time detection** — deciding "is this a build-time stack" from the
  manifest; start with `react-vite`/`nextjs`/Astro-detected and the presence of a
  build step, refine as needed.
