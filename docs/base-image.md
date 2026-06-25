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
in v0.4 (see Roadmap).

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

This is what `image/Dockerfile` already guarantees; a custom image just has to
match the same surface.

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
