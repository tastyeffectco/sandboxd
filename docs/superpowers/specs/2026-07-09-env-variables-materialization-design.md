# Environments — env vars as a core engine primitive (mechanism, not policy)

**Date:** 2026-07-09
**Status:** FINAL — ready for an implementation plan
**Area:** `sandboxd` core (control-plane + a migration); clients (console, CLI)
build the *policy* on top.

## The requirement that decides the architecture

Env vars must be **usable by every client** (console, CLI, CI, other tools) and
support: *"for production, spin a **new** sandbox with **different** env vars, no
code changes."* A hand-written file in one workspace can't do that — the values
must be held by the **engine**, above any single sandbox, and injected when you
boot one. So this is a lean **core** capability, not a console-only feature.

## Principle: mechanism in core, policy in clients

The engine ships the *mechanism* — "store env per app per environment" and
"inject an environment's env when booting a sandbox." It has **no opinion** about
what environments exist, dev/prod semantics, promotion, diffing, or UI — those are
**client policy**. This is what keeps the engine usable across very different use
cases.

### The irreducible primitive, and two layers

Strip every use-case assumption and the core reduces to **one thing**:
`POST /apps/{id}/sandbox { env: {K:V} }` → inject at boot. The engine knows
nothing about "dev"/"prod"/"environments"; the **caller** decides what env means.
Named env sets are *optional sugar*, not the core concept. Hence two layers:

| Layer | What | Consumers |
|---|---|---|
| **L0 — primitive (irreducible core)** | `env` map on sandbox-create → process env at boot | everyone (console, CLI, CI, any tool) |
| **L1 — durable named sets (optional core sugar)** | store env sets on the app under a free-form `environment` label; boot with `{ environment }` to pull one | clients that don't hold their own env |
| **Policy (NOT core)** | dev/prod meaning, promotion, inheritance, UI, `.env` files | console + other clients |

The console uses L1; another tool can use L0 or build its own env system. The
engine is the substrate; the console is one client. **This is the design.**

## Model: App · Environment · Sandbox

```
APP (durable: code, git, runtime_preset)
  └── environments:  dev {K=v}   production {K=v}   …   ← named env-var sets, in the engine
                          │
   POST /v1/apps/{id}/sandbox { environment: "production" }
                          │
                          ▼
              SANDBOX  ← engine injects that set as PROCESS ENV at boot
```
Dev → prod = which environment you boot a sandbox with. Same code, zero changes.

## What the engine already provides (verified in code)

1. **Injection at boot exists.** The internal create body has
   `Env map[string]string` → `docker run --env` (`handlers.go` builds `envFlags`
   with validation) → visible to runtimed (PID 1) and **inherited by the web
   process** (`process.go` runs `bash -lc` with `os.Environ()`). Used today for
   agent keys / `RUNTIMED_TEMPLATE`.
2. **Durable per-app store exists.** `app_config` (`migrations/0014`): encrypted
   secrets (`value_ciphertext`/`value_nonce`), write-only over the API,
   `access_policy` with a "broker" already anticipated.

**Gaps:** `app_config` isn't environment-scoped, and the v1 create request
(`v1CreateAppSandboxReq`, `v1_apps.go`) doesn't expose `env`.

## Key decision: inject **process env**, do not write a file in the workspace

- Injecting at the container level means **nothing is written to the workspace**
  → nothing to `.gitignore`, nothing to leak to GitHub. The original ".env in
  git" concern **disappears** because there is no file in the repo.
- **Stack-agnostic:** Node/Express + FastAPI read `process.env`/`os.environ`;
  Vite/Astro read `process.env` for prefixed vars at build. One mechanism, all
  stacks.
- A stack-aware **`.env` file in the workspace** is an **opt-in client
  convenience** for a stack that reads *only* a file — a client drops it via the
  existing files API. **Not core.**

### Build-time works for free (unlike a separate `docker build`)

Coolify needs a build-vs-runtime split because it runs a *separate* `docker
build` that can't see runtime env, so build vars become Dockerfile `ARG`s /
`--env-file`. sandboxd **builds inside the already-running container**
(`build.command` / dev server via the manifest), so injected **process env is
present at build time automatically** — Vite/Astro inline prefixed vars during the
in-container build with no ARG plumbing. We get with one mechanism what Coolify
needs two for. (Revisit only if we move to pre-baked images.)

### Injection mechanism: prefer an env-file *outside the workspace* over `--env` flags

The current path passes each var as `docker run --env KEY=VAL` and **rejects
newlines** (`handlers.go`), so **multiline values (PEM keys, certs, JSON) can't go
through** — the same case Coolify solves with a quoted `.env`. Recommended:
control-plane writes the resolved set to an **env-file outside the git workspace**
(e.g. `/run/sandboxd/env`) that `runtimed` sources into the app's process
environment. This keeps process-env semantics (build-time works), handles
multiline + large sets, and — being outside the repo — never risks GitHub. The
simple `--env` path stays valid for small single-line sets.

## Scope: core vs not

| Piece | Core? | Rationale |
|---|---|---|
| Inject env as process env at boot | ✅ exists | universal mechanism |
| Expose `env` on `POST /apps/{id}/sandbox` | ✅ tiny | any tool spins a sandbox with env |
| `app_config.environment` column | ✅ small | durable, named env sets |
| Boot with `{ environment }` → resolve + inject | ✅ small | "spin prod, no changes" |
| Environment names, dev/prod, promotion, diff, inheritance | ❌ client | engine stays unopinionated |
| `.env` file materialization (stack-aware) | ❌ opt-in client | process env covers the common case |
| Env editor UI, masking, env switcher | ❌ console | pure `/v1` client |

## Core design

### Phase 1 — enabling primitive (smallest change, ships value alone)
- Add `Env map[string]string` to `v1CreateAppSandboxReq` (`v1_apps.go`).
- Pass it into `createBody["env"]` before delegating to `handleCreate` (which
  already validates keys/values and injects). No new injection code.
- **Result:** any client spins a sandbox with explicit env → dev/prod for tools
  that hold their own env set.

### Phase 2 — durable, environment-scoped store
- Migration `0022`: `ALTER TABLE app_config ADD COLUMN environment TEXT NOT NULL
  DEFAULT 'default'`; uniqueness becomes `(app_id, environment, key)`; existing
  rows fall to `'default'`.
- Store + API: config CRUD gains an `environment` scope
  (`?environment=` / body field), default `'default'`. Reuses existing
  encryption, write-only, redaction, tenant scoping, tests.

### Phase 3 — boot by environment name
- `v1CreateAppSandboxReq` also accepts `Environment string`.
- On create: resolve `app_config WHERE app_id, environment` → decrypt sensitive
  values (same seam agent keys use, key never leaves the CP) → merge into the
  injected `env` map (an explicit `env` on the request overrides stored values).
- **Result:** `POST /apps/{id}/sandbox { environment: "production" }` → different
  values, same code.

## Final plan — build order (file-level)

**Phase 1 · L0 primitive** (ships value alone; enables every use case)
1. `internal/api/v1_apps.go` — add `Env map[string]string` (+ later `Environment
   string`) to `v1CreateAppSandboxReq`; validate keys `^[A-Za-z_][A-Za-z0-9_]*$`;
   put into `createBody["env"]`.
2. `internal/api/handlers.go` — accept `env` on the internal create body; **relax
   the newline rejection** by routing values through an env-file instead of only
   `--env` flags.
3. `cmd/runtimed/*` — write the resolved set to an env-file **outside the
   workspace** (e.g. `/run/sandboxd/env`) and source it into the app process env
   **before web start** (extends the existing `os.Environ()` path in
   `process.go`). Simple single-line sets may still use `--env`.
4. Tests: v1 passes `env` through; multiline value survives; `printenv` in the
   sandbox shows it; agent-key/`RUNTIMED_TEMPLATE` injection unregressed.

**Phase 2 · L1 durable named sets**
5. `migrations/0022_app_config_environment.sql` — `ADD COLUMN environment TEXT NOT
   NULL DEFAULT 'default'`; new unique `(app_id, environment, key)`; backfill.
6. `internal/store/app_config.go` — thread `environment` through
   create/list/patch/delete + the unique constraint.
7. `internal/api/v1_app_config.go` — `?environment=` (default `'default'`) on the
   config endpoints; encryption/redaction/write-only unchanged.
8. Tests: env-scoped isolation; migration backfill; tenant scoping holds.

**Phase 3 · L1 boot-by-name**
9. `internal/api/v1_apps.go` — on create, if `Environment` set, resolve
   `app_config WHERE app_id, environment`, decrypt sensitive (existing secrets
   seam, key stays in CP), merge under any explicit `env` (explicit wins), inject.
10. Tests: right set resolved+decrypted; explicit overrides stored; unknown
    environment → empty (not error).

**Phase 4 · policy (NOT core) — separate specs/plans**
11. Console: environment switcher, row editor, **bulk `.env` Developer View**,
    "Spin production".
12. Docs: `App · Environment · Sandbox` concept + Astro walkthrough; presets table.
13. Optional: predefined `SANDBOXD_*` vars (preview URL, `PORT`, sandbox id).

Phases 1–3 are the core work; each merges independently. Phase 1 alone already
delivers "spin a sandbox with any env."

## Not in core (client policy + follow-ons)
- **Console:** environment switcher, env editor (keys validated
  `[A-Za-z_][A-Za-z0-9_]*`, values masked in-UI), **a bulk `.env` "Developer
  View"** (Coolify-style paste/edit of the whole set), a "Spin production" action.
  All over `/v1`.
- **Optional `.env` drop:** for a stack that reads only a file, a client writes a
  gitignored `.env`/`.env.local` via `PUT …/files` + `.gitignore`. Documented, not
  engine behavior.
- **Scoped/shared variables + `{{scope.VAR}}` inheritance, preview-env sets,
  promotion / diff** — all client-side (Coolify does these as policy above the
  engine). Roadmap, not core.

## Coolify parallels (reference)
Validated against Coolify's model: environment scope (prod/staging), write-only
"locked secrets", encryption at rest, and the bulk `.env` Developer View all match
this design. Left as client policy (as Coolify layers them): `{{ }}` shared-var
inheritance, preview-deployment env, `SERVICE_*` magic secrets. Optional small
core extension worth considering: a few predefined `SANDBOXD_*` vars (preview URL,
`PORT`, sandbox id), mirroring Coolify's `COOLIFY_*`/`PORT`/`HOST`.

## Secrets model (retained)
Sensitive values encrypted at rest in `app_config` (protects DB/backups), API
strict write-only. At boot they're injected as **process env** (not written to
disk), which is *stronger* than a file: no plaintext lands in the workspace. To
inspect effective values, a client reads process env inside the sandbox (e.g.
exec `printenv`) — no reveal endpoint.

## Docs
- New concept **App · Environment · Sandbox** (`concepts.md`); a guide with the
  **Astro walkthrough** (local `.env` stays local; deploy = spin a sandbox in the
  `production` environment; nothing committed); presets table unaffected (process
  env, no per-stack file needed by default).

## Testing
- **Phase 1:** v1 create passes `env` through; `handleCreate` injects; web process
  sees it (integration: `printenv` in the sandbox).
- **Phase 2:** migration backfills `'default'`; env-scoped CRUD isolates
  environments; encryption/redaction unchanged; tenant scoping holds.
- **Phase 3:** boot-by-environment resolves + decrypts the right set; explicit
  `env` overrides stored; unknown environment → empty set (not an error).
- **Regression:** existing agent-key injection + `RUNTIMED_TEMPLATE` still work.

## Trade-offs / open questions
- **Process env vs file:** default is process env (no git risk, stack-agnostic).
  File materialization is opt-in per stack — decide the exact trigger (manifest
  hint vs client choice) if/when a file-only stack shows up.
- **One live sandbox per app today** — dev and prod are sequential (delete then
  respin) until multi-sandbox-per-app lands; the model already supports it when
  it does.
- **Changing env on a running sandbox** = respin (a new environment is a new
  sandbox by design); no in-place mutation in core.
