# Free apps (one-click open-source apps)

The console's **Free apps** page installs popular open-source apps in one
click, each in its own project on the person's engine. It uses only the
`/v1` API; the list lives in [`console/src/catalog.ts`](../console/src/catalog.ts).

## How an install works

1. `POST /v1/apps` with the app's name, pitch and the tag `catalog:<id>`.
   Apps built from source pass `git` (the upstream repo at a pinned tag).
2. `POST /v1/apps/{id}/sandbox` (`template: blank` for downloaded releases),
   then wait until the workspace runs (a source app's repo is copied first).
3. `PUT /v1/sandboxes/{id}/files` writes the entry's small files (`files`,
   for example a one-page web front for a command-line tool) and its
   `sandbox.yaml`, whose `web.command` downloads or builds the app on first
   start (guarded, so later starts are instant) and serves it on port 3000.
4. Stop and start the workspace, so it starts with that `sandbox.yaml`.

No AI is involved, so an install works the same when the free model is used
up. Source apps also get a `build` command, run after each task, so a change
the person asks the coding agent for shows up.

## Trending on GitHub

Entries marked `trending` are shown on the home page under "Trending on
GitHub": projects that are hyped right now, made usable by someone who is not
technical. Same bar as below; a command-line tool needs a plain web page in
`console/src/catalog-files/`.

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
  redirects or returns 429). The preview proxy sends the sandbox's own
  address as the Host header, so an app must not build absolute redirects
  from Host (relative redirects, or honouring X-Forwarded-Host, are fine).
- **Tested on a real Windows PC through the console:** click Install, the
  app's first screen appears, and its first-run step (`firstRun`) is what
  the person actually sees. For `source: true`, ask the coding agent for a
  small visible change and check it shows up.

Tried and left out (and why): Actual Budget (needs `SharedArrayBuffer`),
Trilium v0.107.0 (setup page fails, refuses framing), OpenResume (Next.js 13
fails on Node 22), Puter (needs its own domain), VERT (bun only), copyparty (works, but a cryptic UI
with files reachable only on this PC), Ghost 6
(wants MySQL even in local mode), Etherpad 3 (needs Node 24), Grist 1.7 (redirects to an
absolute URL built from the Host header), NocoDB (Sustainable Use
License since 2026), PairDrop (sends files between devices, but the preview
opens only on this PC), Mermaid Live Editor (needs Node 24, no release tags).

Waiting on a core change: the sandbox image has no headless-browser libraries
(libnss3, libgbm, libX11 and the rest) and no ffmpeg, and a project cannot
install them without root. Projects that render video or drive a browser
(video-shotcraft, rea-video, agent-browser, openfilm) need them in
`image/Dockerfile` first. Daily Stock Analysis
needs the person's own AI connection (OpenCode's free models only work inside
OpenCode).
