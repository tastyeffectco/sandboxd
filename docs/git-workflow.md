# Git workflow (optional)

**Status: feature branches `feat/v0.4.2-git-credentials` → `feat/v0.4.8-git-push`,
stacked off `release/v0.4-apps-console`. Post-v0.4.0; NOT in the v0.4.0 launch and
NOT on `main`.**

Git support is an **optional workflow layered on top of sandboxd core** — a way to
import a private repo into an app, inspect what the agent changed, and commit/push
it back. It is driven entirely through the public `/v1` API; the web console adds a
UI for it, but the console is optional and the core engine works without any of
this. If you never touch the Git endpoints, nothing here is active.

It is intentionally small and PAT-based: **HTTPS + a personal access token only**
— no SSH, no GitHub/GitLab App, no OAuth, no provider APIs.

---

## The end-to-end flow

1. **Add a credential** (once): store an encrypted, owner-scoped PAT.
2. **Import**: create an app from a private repo URL using that credential.
   sandboxd clones it host-side into the workspace before the sandbox starts.
3. **Inspect the runtime** (advisory): detect the framework + suggested preset.
4. **Work**: the agent edits files in the sandbox as usual.
5. **Review**: read-only `git status` / `git diff` of the workspace.
6. **Commit**: commit selected changes locally (in-sandbox).
7. **Push**: push the local commits to a **new branch** on the remote (host-side),
   then open a PR yourself on your Git host.

## Endpoints

| Step | Endpoint | Where it runs |
|---|---|---|
| Credentials | `POST` / `GET /v1/git-credentials`, `DELETE /v1/git-credentials/{id}` | control plane |
| Import | `POST /v1/apps` with a `git: {repo_url, branch, credential_id}` block | clone host-side |
| Runtime detect | `GET /v1/apps/{id}/runtime-inspect` | control plane (read-only) |
| Status | `GET /v1/apps/{id}/git/status` | **in-sandbox** |
| Diff | `GET /v1/apps/{id}/git/diff?path=` | **in-sandbox** |
| Commit | `POST /v1/apps/{id}/git/commit` | **in-sandbox** |
| Push | `POST /v1/apps/{id}/git/push` | control plane (host-side) |

All are owner/tenant-scoped (`auth.Actor.Name`): a caller only ever sees and acts
on its own apps and credentials; cross-owner access returns `404`.

---

## Execution model: who runs git, and why

The split is deliberate and is the heart of the security model:

- **Network git (clone, push) runs host-side in the control plane.** It needs the
  credential and the network — and the sandbox has neither (it runs with **no
  network**, and the token must never enter it). The token is decrypted only in
  these paths and delivered to git only via `GIT_ASKPASS` (see below).
- **Local git (status, diff, commit) runs in-sandbox** via `docker exec` (uid
  1000, locked container). It is **credential-free and offline** — it only reads
  and writes the workspace. Running it in the sandbox keeps an *untrusted* `.git`
  (the agent can write anything there, including hooks and `.gitattributes` that
  can execute programs) inside the existing trust boundary, instead of executing
  it on the control-plane host.
- **Runtime detection** reads workspace files host-side, read-only — it never runs
  app code, installs dependencies, or touches a credential.

## Runtime detection (advisory)

`GET /v1/apps/{id}/runtime-inspect` summarizes any existing `sandbox.yaml`
(authoritative, never overwritten) and suggests a preset (`nextjs`, `react-vite`,
`node-express`, `fastapi`, `worker` are runnable; `astro`, `docusaurus` are
**detect-only** — suggested with a warning, no built-in preset). It is purely
advisory: nothing is applied, and the user always picks a preset manually.

Preview-port note: the resolved web port comes from the imported `sandbox.yaml`
`web.port`, else the selected preset, else `3000`. This drives both the Traefik
preview router and the preview URL, so an imported app that serves on a non-3000
port (e.g. Astro on 4321) previews correctly.

## Status classification

`git/status` keeps `clean` truthful (raw repo state) and additionally reports
`user_clean` plus a separate `runtime_files[]` bucket, so a pristine import isn't
shown as "dirty" just because sandboxd/the toolchain wrote `sandbox.yaml`, a
lockfile, or a framework cache. Nothing is hidden — runtime-generated files are
surfaced separately, and the console renders them collapsed and unchecked.

## Commit

`git/commit` stages **only the selected, actually-changed paths** (defaults to the
user files from `git/status`; runtime files are excluded unless explicitly listed
in `runtime_paths`). It **never** uses `git add -A`, makes a path-scoped commit so
any changes the agent pre-staged are not swept in, runs with `--no-verify` (so an
untrusted repo hook can't run), and sets an **ephemeral author** via `git -c
user.name= -c user.email=` — never written to `.git/config`. Empty repos (no HEAD)
are unsupported in this slice (`reason:"empty_repo_unsupported"`).

## Push

`git/push` pushes `HEAD` to a **new branch** (default `sandboxd/<slug>-<shortsha>`)
on the remote. Key properties:

- The push **target comes from app metadata** (`app.git_repo_url` +
  `app.git_credential_id`), **never** the repo's `.git/config` origin.
- The credential reaches git only via a **host-checking** `GIT_ASKPASS`: the token
  is emitted only when git's credential prompt resolves to the **exact** expected
  host; any mismatch or unparsable prompt emits nothing.
- A **pre-flight config audit refuses** a repo whose local config contains
  `insteadOf` / `pushInsteadOf` / `sshCommand` / `fsmonitor` / `hooksPath` / proxy
  (`reason:"unsafe_repo_config"`).
- The invocation is hardened: hooks disabled, `protocol.ext/file.allow=never`,
  `GIT_ALLOW_PROTOCOL=https`, clean `HOME`/global/system config, no
  `credential.helper`, argv-only.
- **New branch only** (`main`/`master` and the import branch are rejected),
  **never `--force`**, no PR creation.
- It **works whether the sandbox is running or stopped** (it reads the workspace
  host-side), and refuses with `no_local_commits` when there's nothing to push
  (computed locally, no network).

## Concurrency

Commit and push take the same **per-workspace exclusive lock** (`git:<sandbox-id>`
— per workspace, not global, so different apps never block each other). The loser
of a commit race re-evaluates under the lock and returns
`{committed:false, reason:"no_changes"}` instead of a scary error; push holds the
lock across its whole pre-flight so it can't publish a stale `HEAD`. Read-only
`status`/`diff` stay unlocked. Workspace purge also acquires this lock before
deleting the workspace dir, so it can't race an in-flight commit/push.

---

## Security boundaries

- **Token at rest** — encrypted (AES-GCM), owner-scoped, **write-only** through the
  API (`token_set` is the only field ever returned). Decrypted **only**
  control-plane-side, in the clone and push paths.
- **Token in motion** — reaches git solely via `GIT_ASKPASS` reading a `0600`
  temp file (removed after the op). It is **never** present in: argv (URLs are
  tokenless), the git process environment (only the file *path*), `.git/config`,
  the workspace, snapshots, `docker inspect`, logs, or events. Push additionally
  uses the host-checking askpass so the token can't be exfiltrated to a rewritten
  host.
- **The sandbox never sees the token.** Network git is host-side; the sandbox has
  no network and no credential.
- **Untrusted `.git`** — in-sandbox git is hardened (`-c core.fsmonitor=`,
  `--no-ext-diff --no-textconv`, `-c safe.directory=*`); host-side push disables
  hooks and restricts protocols to https. The push never trusts the repo's origin
  remote.
- **Events/audit** carry repo URL (tokenless) + branch + result only.

## Limitations (current)

- **HTTPS + PAT only** — no SSH, GitHub/GitLab App, OAuth, or provider APIs.
- **Shallow clone** (`--depth=1`). If a remote rejects a push for shallowness, it
  returns `shallow_push_unsupported` — there is **no automatic deepen/unshallow**.
- **New branch only** — no direct push to `main`/`master` or the import branch, and
  **no PR creation** (open the PR yourself on your Git host).
- **Empty repos** (no `HEAD`) can't be committed or pushed in this slice.
- **status/diff/commit need a running sandbox**; push works whether running or
  stopped.
- **Fork/restore degrades Git lineage** — snapshot spin-up resets the workspace
  `.git`. The push target survives (it's app metadata), but on a forked/restored
  app the local history/baseline is degraded; the direct *import → edit → commit →
  push* path is the supported one.
- **No secret scanning** of committed/pushed content — pushing publishes whatever
  is in the selected files. The new-branch default and it being your own private
  repo limit the blast radius.

## Explicit non-goals

No PR creation, no repo discovery/picker, no direct `main`/default-branch push, no
shallow deepen/unshallow, no `git fetch`/`pull`/`merge`, no GitHub/GitLab API
calls, no SSH/App/OAuth, no secret scanning. These are out of scope, not pending.
