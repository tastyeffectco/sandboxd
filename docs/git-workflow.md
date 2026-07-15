# Git workflow (optional)

Git support is an **optional workflow layered on top of sandboxd core** — a way to
import a repo into an app, inspect what the agent changed, and commit/push it back.
It is driven entirely through the public `/v1` API; the web console adds a UI for
it, but the console is optional and the core engine works without any of this. If
you never touch the Git endpoints, nothing here is active.

It is intentionally small and HTTPS-based (no SSH, no GitHub/GitLab App, no
provider APIs). Two import modes:

- **Public repos** — a curated **starter project** or any public URL clones
  **tokenless**: pass `git:{repo_url, branch}` with **no** `credential_id`. The
  URL is tokenless and `GIT_TERMINAL_PROMPT=0` makes a private repo fail cleanly
  rather than hang.
- **Private repos** — reference an encrypted, owner-scoped **PAT** by
  `credential_id`; it is decrypted host-side at clone time via a `GIT_ASKPASS`
  helper and never enters the sandbox.

For claude-code subscriptions there is also a real server-side OAuth flow (see
[`agent-auth.md`](./agent-auth.md)) — that is agent auth, separate from Git PATs.

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
| Import (public) | `POST /v1/apps` with `git: {repo_url, branch}` (no credential) | clone host-side, tokenless |
| Import (private) | `POST /v1/apps` with `git: {repo_url, branch, credential_id}` | clone host-side, via PAT |
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
(authoritative, never overwritten), suggests a stack from the advisory recipe
registry (runnable presets `nextjs`/`react-vite`/`node-express`/`fastapi`/`worker`,
or detect-only recipes like `astro`/`docusaurus`/`gatsby`/`nuxt`/`sveltekit`/
`vite-vue`/`remix-vite`/`eleventy`), and for detect-only stacks returns a
`suggested_manifest` + `config_snippets`. It is purely advisory: **for an imported
repo sandboxd does not write `sandbox.yaml` or rewrite framework config** — you
adopt the suggestion explicitly (Copy YAML / Ask agent / manual edit). See
[`web-framework-recipes.md`](./web-framework-recipes.md).

Preview-port note: **runtime recipes standardize on `3000`, but a manifest can
declare any `web.port` and sandboxd routes the declared preview port.** The
resolved port is the imported `sandbox.yaml` `web.port`, else the selected preset,
else `3000`, and it drives both the Traefik router and the preview URL (so an
imported app on, say, 4321 previews correctly once its manifest declares it).

**Restart on change:** the manifest is read at boot, so **after you change
`sandbox.yaml`, restart the sandbox for runtimed to re-read it.**

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
user.name= -c user.email=` — never written to `.git/config`.

**First-commit limitation.** `git/commit` expects an **existing HEAD** — it handles
*subsequent* commits, not repository initialization. A brand-new repo with no commits
returns `reason:"empty_repo_unsupported"`. The **initial commit must be made
in-sandbox** for now (e.g. the agent runs `git init && git add -A && git commit` in
the workspace); after that, `git/commit`/`git/push` work normally. First-commit
support via the API is intentionally out of scope here.

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
- **Rotating a token** — the token is write-only, so there's **no in-place edit**.
  To rotate, **delete the credential and add it again** with the new token
  (`DELETE /v1/git-credentials/{id}` then `POST /v1/git-credentials`, or **Settings →
  Git credentials** in the console).
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
