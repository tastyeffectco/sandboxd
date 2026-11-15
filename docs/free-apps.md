# Free apps (one-click open-source apps)

The console's **Free apps** page installs popular open-source apps in one
click, each in its own project on the person's engine. It uses only the
`/v1` API; the list lives in [`console/src/catalog.ts`](../console/src/catalog.ts).

## How an install works

1. `POST /v1/apps` with the app's name, pitch and the tag `catalog:<id>`.
   Apps built from source pass `git` (the upstream repo at a pinned tag).
2. `POST /v1/apps/{id}/sandbox` (`template: blank` for downloaded releases).
3. The first task asks the coding agent for one thing: write the entry's
   `sandbox.yaml`. Its `web.command` downloads or builds the app on first
   start (guarded, so later starts are instant) and serves it on port 3000.
   runtimed applies the new start command and waits for the app.

Because the agent only writes one file, installs behave the same on any
model, including OpenCode's free one. Source apps also get a `build`
command, run after each task, so a change the person asks for shows up.

## Adding an app

An entry must meet all of these:

- **Open source** (OSI license). Source-available or "fair-code" licenses
  (n8n, tldraw, Open WebUI's branding clause) are out.
- **Runs in a sandbox:** Node 22 or Python 3.13, or a prebuilt Linux
  binary/archive from the project's releases. No Docker, Java, PHP, Go or
  Rust toolchains, and no Postgres/MySQL/Redis/MongoDB (SQLite or files are
  fine). Fits in 4 GB of memory.
- **Useful without an account or API key.**
- **Pinned:** a release URL with a version, or a git tag. Never `latest`.
- **Works in the preview frame:** no `X-Frame-Options`/`frame-ancestors`
  refusal, no `SharedArrayBuffer` requirement, and a health path that
  answers 200 without rate-limiting frequent checks (set `health` if `/`
  redirects or returns 429).
- **Tested on a real Windows PC through the console:** click Install, the
  app's first screen appears, and its first-run step (`firstRun`) is what
  the person actually sees. For `source: true`, ask the coding agent for a
  small visible change and check it shows up.

Tried and left out (and why): Actual Budget (needs `SharedArrayBuffer`),
Trilium v0.107.0 (setup page fails, refuses framing), OpenResume (Next.js 13
fails on Node 22), Puter (needs its own domain), VERT (bun only), copyparty (works, but a cryptic UI
with files reachable only on this PC), Ghost 6
(wants MySQL even in local mode), Etherpad 3 (needs Node 24), Grist 1.7 (builds and runs, but
the preview cannot load its home page redirect), NocoDB (Sustainable Use
License since 2026).
