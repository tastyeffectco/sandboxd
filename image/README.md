# image/ — the sandbox base image

This directory builds **`sandboxd-base`**, the image every sandbox container boots
from. It bakes in the in-sandbox supervisor (`runtimed`) and the language
toolchains a coding agent needs, and seeds a clean `/home/sandbox` on first use.

The sandbox **lifecycle** (create / run / sleep / wake / destroy) is owned by the
control plane — see [`control-plane/README.md`](../control-plane/README.md). This
directory only builds the image; it does not manage running sandboxes.

Read [`HOME_LAYOUT.md`](HOME_LAYOUT.md) first for the contract of what lives inside
a running sandbox's home.

## Layout

```
image/
├── Dockerfile            # sandboxd-base — multi-stage: builds runtimed, then the runtime image
├── build.sh             # build helper (native arch; tag via arg or $SANDBOXD_IMAGE)
├── verify-base.sh       # in-image self-check (Node/pnpm/bun, python3/uv, build tools)
├── etc/                 # registry-proxy + cache config baked into the image
│   ├── npmrc            #   npm/pnpm registry + store-dir + prefer-offline
│   ├── pip.conf         #   pip registry + cache-dir
│   └── profile.d/sandbox-env.sh   # PNPM_HOME + cache env + PATH
├── skel/                # the canonical /home/sandbox skeleton (seeded once)
├── templates/           # runtime starter templates (react, nextjs, node-express, fastapi, worker)
├── php/Dockerfile       # optional PHP variant image
├── ruby/Dockerfile      # optional Ruby variant image
└── HOME_LAYOUT.md       # the in-sandbox home contract — read this first
```

## Building

```bash
bash image/build.sh                       # builds sandboxd-base:0.3.0 (the default tag)
bash image/build.sh 0.3.1                 # a different tag
SANDBOXD_IMAGE=my-base:dev bash image/build.sh   # a fully custom name:tag
```

`build.sh` runs a **multi-stage** `docker build` from the repo root: stage 1
compiles `runtimed` from `control-plane/` (so the host needs only Docker, not Go);
stage 2 is a `debian:stable-slim` runtime with **Node 22 + pnpm + bun** and
**Python 3 + uv** baked in. It builds for the **native architecture** (works on
amd64 and arm64) and prints the image size. There is no `latest` tag — the control
plane pins an exact version (`SANDBOXD_IMAGE`, default `sandboxd-base:0.3.0`). The
image runs as the non-root **`sandbox`** user (uid 1000).

## Seeding

The skeleton in `skel/` is baked to `/opt/sandbox-skel/` and copied into a fresh
sandbox home **exactly once**, when the workspace is provisioned. After that the
home belongs to the user — the container entrypoint never re-seeds. `/opt/sandbox-skel/`
stays readable inside every sandbox, so a wiped dotfile can be restored by hand:

```bash
cp /opt/sandbox-skel/.bashrc ~/.bashrc
```

See [`HOME_LAYOUT.md`](HOME_LAYOUT.md) for the full home contract.

## Self-check

```bash
docker run --rm sandboxd-base:0.3.0 bash /opt/verify-base.sh
```

Confirms the toolchains (Node/npm/pnpm/bun, python3/uv/setuptools, build tools) are
present and working in the built image.
