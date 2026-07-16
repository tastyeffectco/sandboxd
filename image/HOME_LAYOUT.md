# `/home/sandbox/` — the sandbox home contract

This document is the source of truth for what lives where under `/home/sandbox`
inside a running sandbox container, who owns what, and what survives each
destruction boundary. The control plane, snapshots, and everything downstream read
from this contract — change it here, not in individual consumers.

---

## Layout

```
/home/sandbox/
├── workspace/        # user project code — this is where users cd, clone repos, run dev servers
├── .bashrc           # minimal shell defaults
├── .profile          # sources /etc/profile.d/sandbox-env.sh and ~/.bashrc
├── .gitconfig        # init defaults only; no fake user identity
├── .config/          # tool configs (claude-code, opencode, anything user-installed)
├── .cache/           # package-manager caches:
│   ├── pnpm-store/   #   pnpm content-addressable store
│   ├── pip/          #   pip wheel cache
│   ├── uv/           #   uv cache
│   └── bun/          #   bun install cache
├── .local/           # user-installed binaries + data (pnpm global, uv-installed tools)
│   ├── bin/
│   └── share/pnpm/   #   PNPM_HOME — pnpm's own bin/global packages
└── .bun/             # bun runtime data + bin
```

**Stable path**: user project code lives at **`/home/sandbox/workspace`**. Do not
change it — the control plane, snapshots, and preview routing all assume it.

---

## Ownership

Everything under `/home/sandbox` is owned by the **`sandbox` user (uid 1000, gid
1000)** inside the container. In the default build (`SANDBOXD_USERNS=host`) that is
also uid 1000 on the host, which keeps workspace ownership deterministic. On a
daemon with `userns-remap` enabled, the same uid maps to the remapped host range;
the control plane's provisioning chowns the workspace to match either way.

---

## User-modifiable vs platform-managed

**Everything under `/home/sandbox` is user-modifiable.** There is no protected
platform area inside the home. Users can edit `.bashrc`, customise `.gitconfig`,
install global pnpm packages into `~/.local/share/pnpm`, fill `.cache/` to the
workspace quota, and structure `workspace/` however they like. Provider
credentials never live here — they're held control-plane side (see
[agent-auth](../docs/agent-auth.md)).

---

## What survives each destruction boundary

| Boundary | Survives? | Notes |
|---|---|---|
| Container restart / stop→wake | ✅ Everything in `/home/sandbox` | The container rootfs is `--read-only` and tmpfs `/tmp` / `/var/tmp` evaporate, but the workspace mount at `/home/sandbox` is untouched. |
| Container destroy + recreate, same id | ✅ Everything | The workspace lives on host disk under `/var/lib/sandboxed/workspaces/<id>/`; the destroy path preserves it. |
| Container destroy + new id | ❌ Nothing | A new id means a new workspace. There is no copy-from-id primitive — use a **snapshot / fork** to carry a workspace forward. |
| Host reboot | ✅ Everything | Workspaces are on the data-dir partition. On boot the reconciler converges running rows back to containers. |
| Base-image upgrade | ✅ Everything | The home is a bind mount; rebuilding the image never touches it. Skel changes only affect *new* workspaces (seeding happens once, at creation). |
| Snapshot / restore | ✅ in the snapshot | The snapshot flow exports the workspace; restore/fork spin a new sandbox from it. |

---

## The seeding rule

`/opt/sandbox-skel/` inside the image is the **canonical skeleton**. It is copied
into a new workspace **once, at provision time**, by the control plane. After that,
the home belongs to the user.

- **The container entrypoint does not seed.** There is exactly one seeding path
  (provisioning); the entrypoint never checks emptiness or branches on first-mount.
- **Re-provisioning an existing workspace does not re-seed** — the seed step is
  gated on the workspace being empty. Any user files present → skel is skipped.
- **To restore a wiped default**, copy it back by hand:
  ```bash
  cp /opt/sandbox-skel/.bashrc ~/.bashrc
  ```
  `/opt/sandbox-skel/` exists inside every running container (owned
  `sandbox:sandbox`, mode 700), so the user can always read it.

Why one-shot and no entrypoint re-seed: silent re-seeding would overwrite user
customisation — the worst failure mode. The user owns their home; the platform
owns only the *initial* contents.

---

## Environment-variable boundary: image `ENV` vs `/etc/profile.d/sandbox-env.sh`

The image carries two env layers with distinct scopes. Confusing them causes silent
failures (e.g. a non-interactive `docker exec … pnpm install` not seeing a shell-only
var).

### Authoritative for ALL processes — Dockerfile `ENV`

Set at build time; every process inherits them without sourcing any file —
interactive shells, non-interactive `docker exec`, the entrypoint chain, daemons.

| Var | Value | Why in `ENV` |
|---|---|---|
| `NPM_CONFIG_REGISTRY` | `https://registry.npmjs.org/` | npm + pnpm honor this env var; beats any `.npmrc` file |
| `BUN_CONFIG_REGISTRY` | `https://registry.npmjs.org/` | bun's documented registry knob |
| `PIP_INDEX_URL` | `https://pypi.org/simple/` | pip + uv (uv reads pip's env block) |
| `PATH` | `…:/home/sandbox/.local/bin:/home/sandbox/.bun/bin` | every binary is on PATH without a shell-init step |
| `LANG`, `DEBIAN_FRONTEND` | C.UTF-8 / noninteractive | base locale + apt hygiene |

**Rule of thumb**: if a setting MUST work for a bare `docker exec s-id pnpm install`
(no `bash -l`), it belongs in the Dockerfile `ENV`. Operators who run a caching
registry proxy override `NPM_CONFIG_REGISTRY` / `BUN_CONFIG_REGISTRY` /
`PIP_INDEX_URL` at `docker run` time.

### Interactive-shell ergonomics — `/etc/profile.d/sandbox-env.sh`

Sourced only when `/etc/profile` runs: login shells (`bash -l`, `docker exec -it …
bash -l`) and anything that sources `~/.profile` / `~/.bashrc`. A bare `docker exec
s-id <binary>` does **not** see these.

| Var | Value | Notes |
|---|---|---|
| `PNPM_HOME` | `$HOME/.local/share/pnpm` | pnpm's global-install location |
| `PIP_CACHE_DIR` | `$HOME/.cache/pip` | mirrors `pip.conf`'s `cache-dir=` |
| `UV_CACHE_DIR` | `$HOME/.cache/uv` | makes uv's default explicit |
| `BUN_INSTALL_CACHE_DIR` | `$HOME/.cache/bun` | pins bun's cache under `$HOME` |

Cache locations sit here (not `ENV`) because the defaults are already under `$HOME`
(persistent in the workspace) and stay overridable per shell.

---

## Cache locations (and why they live in the workspace)

The image pins package-manager caches to subdirectories of `/home/sandbox/.cache/`:

| Tool | Path | Set by |
|---|---|---|
| pnpm content store | `/home/sandbox/.cache/pnpm-store` | `store-dir=` in `/etc/npmrc` |
| pip wheel cache | `/home/sandbox/.cache/pip` | `cache-dir=` in `/etc/pip.conf` + `PIP_CACHE_DIR` |
| uv cache | `/home/sandbox/.cache/uv` | `UV_CACHE_DIR` |
| bun install cache | `/home/sandbox/.cache/bun` | `BUN_INSTALL_CACHE_DIR` |

All live inside the workspace, so a returning user resumes with hot caches across
restart. The workspace quota accounts for cache growth; a user can `rm -rf
~/.cache/*` to reclaim space.

---

## Size budget

The image targets **≤ 500 MB compressed** — a goal, not a gate. The Dockerfile
trims dpkg docs/locales/man pages and cleans package caches after each install. We
do **not** drop `--read-only`, change the home contract, or weaken the toolchain to
hit a size number.

---

## How the control plane uses this contract

- Provisions the workspace on create (mount + one-time seed from `/opt/sandbox-skel/`).
- Trusts `/home/sandbox/workspace/` as the user-code path for snapshot/restore/fork.
- Treats `/home/sandbox/.cache/` as a cacheable (safely-losable) subtree.
- **Never seeds automatically** — a reset is a manual copy from `/opt/sandbox-skel/`.

This layout is a stable contract; changing it means updating every consumer that
depends on it.
