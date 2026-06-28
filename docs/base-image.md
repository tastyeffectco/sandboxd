# Base image & the Custom Base Image Contract

In sandboxd three concerns are deliberately separate:

- **base image** — the environment/tools available *inside* a sandbox (OS, language
  toolchains, and `runtimed`).
- **runtime preset** — *how* to run the app (`sandbox.yaml`: web/workers/build).
- **starter / import** — the app *source* in the workspace.

The default base image is **`sandboxd-base`** (built from `image/Dockerfile`). v0.4
uses **one base image instance-wide**; you can swap it for your own as long as it
meets the contract below.

## Selecting a base image

Set **`SANDBOXD_IMAGE`** to any compatible image; every sandbox on the instance
uses it (it's also the seed image). The current value is shown read-only in the
console **Settings → Runtime** (`base_image`). There is no per-app image selection
in v0.4 (see Roadmap) — **`SANDBOXD_IMAGE` is read once at startup**, so after
changing it you must **recreate the control-plane stack** for it to take effect
(it also becomes the snapshot seed image). A stray `image` field in a create body
is **rejected with a 400** ("per-app image selection is not supported; set
SANDBOXD_IMAGE instance-wide"), not silently ignored.

## Custom Base Image Contract

A compatible image **must**:

1. **Run `runtimed` as the container's main process** (`CMD`/entrypoint;
   `sandbox-base` uses `/usr/local/bin/runtimed`). runtimed is the in-sandbox
   supervisor that serves the control socket, runs the manifest processes, and
   executes tasks.
2. **Have a `sandbox` user with uid/gid `1000:1000`.** Workspace files on the host
   bind-mount are owned by `1000` (under the default `--userns=host`), so the
   in-container user must be `1000` or it cannot read/write its own workspace.
   (Under a userns-remapped daemon the image user must map to the remapped owner.)
3. **Use the workspace path `/home/sandbox`** (the bind-mount target). The app lives
   at **`/home/sandbox/workspace/app`**.
4. **May ship a seed skeleton at `/opt/sandbox-skel`** — copied into an empty
   workspace on first boot (home dotfiles, cache locations). Optional but
   recommended for a clean home.
5. **May ship preset templates at `/opt/templates/<preset>`** — starter files a
   runtime preset seeds (e.g. `react-standard`, `fastapi-standard`). Only needed
   for the presets the operator intends to use.
6. **Must run unprivileged**: works under cap-drop ALL, `no-new-privileges`, and
   without a writable rootfs assumption. **No `--privileged`, no Docker socket, no
   extra capabilities.**
7. **Must provide the toolchains the presets you use require** — e.g. `node` +
   `pnpm` for `react-vite` / `nextjs` / `node-express`, and `python3` + `venv` for
   `fastapi`. (Presets install their dependencies at runtime, but the interpreter/
   package manager must already be present.)
8. **Toolchains must be on the login PATH.** runtimed runs app/web/worker commands
   with **`bash -lc`**, which resets PATH from the login profile — a Dockerfile
   `ENV PATH=...` alone is **not** enough. Put toolchains on the login PATH via
   `/etc/profile.d/<tool>.sh` **and/or** symlink the binaries into `/usr/local/bin`.

This is what `image/Dockerfile` already guarantees; a custom image just has to
match the same surface.

## Native languages (Go / PHP / Ruby / Rust / Java / .NET / Deno)

The default base ships **Node 22** (npm 10, pnpm 9, bun) and **Python 3.13 + uv**
**with `setuptools`** (so node-gyp can build native modules — better-sqlite3,
sqlite3, sharp — on Python 3.12+ where stdlib `distutils` was removed), plus
`git`/`make`/`gcc`/`g++`/`curl`/`perl`. Node, Python, and Bun stacks therefore run
on the stock image. (Node 22 also has `worker_threads.markAsUncloneable`, so libs
like undici 8 no longer crash the dev server on boot.) Self-check the image with
`docker run --rm <image> bash /opt/verify-base.sh`. **Go, PHP, Ruby, Rust, Java, .NET, and the `sqlite3` CLI are
not in the base** — those need a **custom image** (operator-scoped, instance-wide):

1. `FROM sandboxd-base:<tag>`, install the toolchain (e.g. Go under `/opt/go`), and
   put it on the **login PATH** (`/etc/profile.d/*.sh`) and/or symlink into
   `/usr/local/bin` — see contract item 8 (`bash -lc` resets PATH).
2. Keep the rest of the contract (runtimed CMD, uid/gid `1000`, workspace
   `/home/sandbox/workspace/app`, unprivileged).
3. Set `SANDBOXD_IMAGE=<image>` and recreate the stack.

This is the supported path for native languages today; per-app image selection is
roadmap, not current behaviour.

## Footguns (what a non-compatible image breaks)

- **No `runtimed`** → the sandbox never becomes healthy (no control socket, no
  preview, no tasks).
- **Wrong uid/gid** → `EACCES` writing the workspace (`~/.cache`, `node_modules`,
  `.next`, `.venv`, generated files).
- **Wrong workspace path** → the app won't seed or run (runtimed can't find the
  app dir).
- **Missing toolchain** (`node`/`pnpm`/`python3`/…) → the corresponding presets
  fail to boot.
- **Requires Docker / privileged / extra caps** → **not supported.** sandboxd runs
  every sandbox hardened and with no Docker access by design.

## Roadmap (not in v0.4)

Deferred to later phases — intentionally not built now:

- **Startup preflight** — a one-shot check that a configured image meets the
  contract (runtimed present, uid `1000`, `/home/sandbox` writable, no privileged),
  surfaced as a compatibility flag in Settings.
- **Image profiles** — named images advertising capabilities
  (`sandboxd-base` → node/pnpm/python3; later `sandboxd-browser` → +chromium; a
  heavier `sandboxd-python`), with snapshots recording the profile they were taken
  on so fork/restore can pick a compatible image.
- **App-level image selection** — choosing a profile per app (from an
  instance-approved allowlist), not arbitrary per-app images.

Explicit non-goals (now and for these later phases): **no Dockerfile builder UI,
no Docker Compose, no registry credentials, no image build pipeline, no arbitrary
per-app image mutation.**
