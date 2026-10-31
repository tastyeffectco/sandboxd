# Changelog

All notable changes to sandboxd are documented here. The format is based on
[Keep a Changelog](https://keepachangelog.com/), and the project follows
[Semantic Versioning](https://semver.org/) (pre-1.0: **rolling releases bump the
patch** — each lands the meaningful changes merged since the last one — and a
**minor bump marks a milestone** release).

## [Unreleased]

### Added

- **OpenClaw joins Hermes on the Agents page.** Add it with your model
  connection, chat with it in sandboxd, and ask it to build or change apps
  through the same tools (no deleting, at most 10 new apps a day). It runs from
  its official image under the same protections as Hermes and uses up to 3 GB
  of memory; the Agents page shows each agent's download and memory before you
  add it.

### Fixed

- **Updating the desktop app now updates its engine.** The app installs its
  engine once; until now an app update kept the old engine, so new features
  (like Agents) did not work. On the next launch after an update, the app
  backs up the engine's database and settings, installs the new signed engine
  release and goes back to the previous one if the update does not finish or
  start. Offline, the current engine starts and the update waits for the next
  launch.

## [0.3.25] — 2026-10-31

### Added

- **Agents, starting with Hermes.** Add Nous Research's Hermes assistant from
  the new Agents page, chat with it there or on Telegram, and ask it to build
  apps: it creates and changes apps on your sandboxd (at most 10 new apps a
  day, no deleting, every action in the app's activity log). Hermes runs in its
  own container from its official image, pinned, as a non-root user with no
  Linux capabilities and a read-only root; it reaches its model through your
  model connection and sandboxd's tools through its own secret, never seeing a
  real key. It needs an OpenAI-compatible model connection. See
  [agents](docs/operator-agents.md).

## [0.3.24] — 2026-10-30

### Added

- **The stack follows the idea.** With "Choose stack" on Automatic, the home
  screen picks Next.js, Express, FastAPI or a background worker when the idea
  mentions them (React / Vite otherwise) and says why under the prompt. Ideas
  that name a framework without a ready-made stack (Vue, Svelte, Astro, Flask,
  Django…) or a language the workspace does not have (PHP, Ruby, Java, .NET,
  Go, Rust, native mobile) get a plain warning before anything is built.
- **The preview follows a change of framework.** When a task edits
  `sandbox.yaml`, runtimed applies it right away (new start command, health and
  build check) instead of at the next workspace start, and the chat says what
  happened. Changes that would break the preview — an invalid or removed file,
  another port, switching between a web app and a background worker — are
  undone and the app keeps running. A Restore applies the restored settings too.
  The agent is told how to change frameworks safely.

### Changed

- The console and desktop focus on building new apps from a prompt. Removed the
  App library of ready-made apps, the starter templates, Import from Git and
  Blank project from the home screen; the engine API still supports Git import
  and empty apps.

### Fixed

- **Desktop setup no longer runs Windows out of memory.** On a busy 16 GB
  laptop, unpacking the engine images filled the engine's file cache until
  Windows ran out of memory and stopped the engine mid-download. Setup now keeps
  that cache small, starts the download over if a previous one was cut off
  (which left empty files and no app preview), and a stopped setup says in
  plain words to check the connection, close some apps and click Retry setup.
- **Restore after a change of framework or dependencies brings the app back.**
  Installed packages are not part of a restore point, so restoring a React app
  after a switch to Astro left Astro's packages and the preview stayed down.
  Restore now reinstalls dependencies when the files that define them changed.
- **A used-up free model no longer leaves the agent stuck.** OpenCode only logged
  its free usage limit and waited until the task timed out. The task now ends
  right away with: "The free AI model has reached its usage limit for now. Try
  again later, or connect your own account in Settings to keep building."
- **An agent can no longer take over its own workspace.** A coding agent that ran
  `runtimed --help` started a second supervisor, which failed the running task
  and cut the workspace off. runtimed now refuses arguments and runs only once.
- A background worker restarted after a task is no longer reported as down.

## [0.3.23] — 2026-10-28

### Breaking changes

- **Claude subscriptions now sign in inside each project.** A Claude Pro/Max
  login connected in Settings → Coding agents is removed on upgrade, because
  Anthropic doesn't allow third-party apps to store or relay Claude.ai logins.
  To keep using it, open a project, choose Claude Code and click **Sign in with
  Claude**. Claude API keys are not affected.

### Changed

- **Claude subscriptions sign in inside each project.** Anthropic doesn't allow
  third-party apps to store or relay Claude.ai logins, so sandboxd no longer
  keeps a Claude subscription token or proxies it, and no longer imports Claude
  logins (including from OpenCode/Pi or the macOS Keychain). "Sign in with
  Claude" runs Claude Code's own `claude auth login` in the project; the login
  stays in that workspace. A previously stored subscription login is deleted at
  startup. Claude API keys work as before.

### Fixed

- OpenCode's built-in free tier works again with nothing connected. Zen now
  serves its free models only to OpenCode's own client, so free-model tasks run
  on OpenCode's built-in provider instead of the credential proxy. Connected
  keys, subscriptions and model connections still use the proxy.
- New React/Vite, Node/Express and FastAPI projects ignore their dependencies
  and build output in Git. Previously `node_modules` showed up as thousands of
  "changed files" after the first task and could be committed or pushed.

### Changed

- Update the base image to OpenCode 1.19.5 and Pi 1.4.0, verified by the
  real-agent provider tests.

### Added

- Rework the console around prompt-first app creation, stack and model selection,
  and a persistent coding conversation beside preview, code, terminal and Git.
  Add responsive navigation, searchable commands, workspace versions and grouped
  settings for model connections, agent accounts, Git credentials and security.
- Add guided credential dialogs, explicit secret rotation, draft preservation and
  unsaved-edit warnings. Retain the app library, project memory, process controls
  and activity; make preview sharing and deployment limits explicit.

- Configurable OpenAI-compatible and Anthropic-compatible model connections,
  usable by OpenCode and the new Pi agent. Manage URLs, keys, exact model aliases
  and token limits in Settings; select the agent, connection and model per task.
  Supports local model servers and LiteLLM gateways (#119), with encrypted
  control-plane credentials and temporary model-scoped proxy capabilities.
- Test OpenCode and Pi with actual tool execution
  through both protocols. Add console browser workflow tests and opt-in live
  model tests. See [model connections](docs/model-providers.md).

- Offer MiniMax M3.1 Flash before M3 in the OpenCode model picker and route it
  through the direct MiniMax proxy. Preserve saved defaults and explicit legacy
  model requests; M2.7 is no longer offered in the ordinary picker.

**Full Changelog**: https://github.com/tastyeffectco/sandboxd/compare/v0.3.22...v0.3.23

## [0.3.22] — 2026-10-12

### Fixed

- Fix the control-plane build by keeping telemetry's instance and cloud organization
  identifiers in scope. [#123](https://github.com/tastyeffectco/sandboxd/pull/123)
- Recover previews when the database reports a running sandbox but its Docker
  container is missing or exited. The wake handler now checks the container state
  and enters the existing recovery path instead of repeatedly reporting it as
  already running. Includes regression tests for missing, exited, healthy, and
  inspect-error cases. [#121](https://github.com/tastyeffectco/sandboxd/pull/121)

### Contributors

Thanks to @gowchin for both fixes and their first contributions to sandboxd.

No new configuration or breaking API changes are introduced by this patch.

**Full Changelog**: https://github.com/tastyeffectco/sandboxd/compare/v0.3.21...v0.3.22

## [0.3.21] — 2026-09-20
**Full Changelog**: https://github.com/tastyeffectco/sandboxd/compare/v0.3.20...v0.3.21

## [0.3.20] — 2026-08-28
**Full Changelog**: https://github.com/tastyeffectco/sandboxd/compare/v0.3.19...v0.3.20

## [0.3.19] — 2026-08-28
**Full Changelog**: https://github.com/tastyeffectco/sandboxd/compare/v0.3.18...v0.3.19

## [0.3.18] — 2026-08-28
**Full Changelog**: https://github.com/tastyeffectco/sandboxd/compare/v0.3.17...v0.3.18

## [0.3.17] — 2026-08-28
**Full Changelog**: https://github.com/tastyeffectco/sandboxd/compare/v0.3.16...v0.3.17

## [0.3.16] — 2026-08-28
**Full Changelog**: https://github.com/tastyeffectco/sandboxd/compare/v0.3.15...v0.3.16

## [0.3.15] — 2026-08-28
**Full Changelog**: https://github.com/tastyeffectco/sandboxd/compare/v0.3.14...v0.3.15

## [0.3.14] — 2026-08-28
**Full Changelog**: https://github.com/tastyeffectco/sandboxd/compare/v0.3.13...v0.3.14

## [0.3.13] — 2026-08-28
**Full Changelog**: https://github.com/tastyeffectco/sandboxd/compare/v0.3.12...v0.3.13

## [0.3.12] — 2026-08-25
**Full Changelog**: https://github.com/tastyeffectco/sandboxd/compare/v0.3.11...v0.3.12

## [0.3.11] — 2026-08-25
**Full Changelog**: https://github.com/tastyeffectco/sandboxd/compare/v0.3.10...v0.3.11

## [0.3.10] — 2026-08-25
**Full Changelog**: https://github.com/tastyeffectco/sandboxd/compare/v0.3.9...v0.3.10

## [0.3.9] — 2026-08-25
**Full Changelog**: https://github.com/tastyeffectco/sandboxd/compare/v0.3.8...v0.3.9

## [0.3.8] — 2026-08-25
**Full Changelog**: https://github.com/tastyeffectco/sandboxd/compare/v0.3.7...v0.3.8

## [0.3.7] — 2026-08-25

* Fix broken Star History chart in README by @FaintFlower in https://github.com/tastyeffectco/sandboxd/pull/103

### New Contributors
* @FaintFlower made their first contribution in https://github.com/tastyeffectco/sandboxd/pull/103

**Full Changelog**: https://github.com/tastyeffectco/sandboxd/compare/v0.3.6...v0.3.7

## [0.3.6] — 2026-08-01

* fix(console): stop double-prefixing write paths (console saves went to a phantom dir) by @tastyeffectco in https://github.com/tastyeffectco/sandboxd/pull/99
* feat(detect): detect the project's package manager (yarn/npm/bun), not just pnpm by @tastyeffectco in https://github.com/tastyeffectco/sandboxd/pull/100


**Full Changelog**: https://github.com/tastyeffectco/sandboxd/compare/v0.3.5...v0.3.6

## [0.3.5] — 2026-07-30

* fix: self-healing start — recreate sandboxes whose container is stale or missing by @tastyeffectco in https://github.com/tastyeffectco/sandboxd/pull/97
* fix(authproxy): fail fast with the provider's real message instead of a timeout by @tastyeffectco in https://github.com/tastyeffectco/sandboxd/pull/98


**Full Changelog**: https://github.com/tastyeffectco/sandboxd/compare/v0.3.4...v0.3.5

## [0.3.4] — 2026-07-30

* feat(brain): spoke notes (brain/*.md) + shared-concept radar by @tastyeffectco in https://github.com/tastyeffectco/sandboxd/pull/96


**Full Changelog**: https://github.com/tastyeffectco/sandboxd/compare/v0.3.3...v0.3.4

## [0.3.3] — 2026-07-30

* feat(brain): [[wikilinks]] between brains + knowledge graph view by @tastyeffectco in https://github.com/tastyeffectco/sandboxd/pull/95


**Full Changelog**: https://github.com/tastyeffectco/sandboxd/compare/v0.3.2...v0.3.3

## [0.3.2] — 2026-07-30

* feat: Project Brain — persistent per-app memory (BRAIN.md) by @tastyeffectco in https://github.com/tastyeffectco/sandboxd/pull/94


**Full Changelog**: https://github.com/tastyeffectco/sandboxd/compare/v0.3.1...v0.3.2

## [0.3.1] — 2026-07-30

* docs: remove dev-process/phase artifacts; make md match the code by @tastyeffectco in https://github.com/tastyeffectco/sandboxd/pull/76
* install: BSD-safe mktemp on macOS by @tastyeffectco in https://github.com/tastyeffectco/sandboxd/pull/74
* docs(git): document token rotation (delete + recreate) by @tastyeffectco in https://github.com/tastyeffectco/sandboxd/pull/72
* Opt-in gVisor (runsc) isolation for sandboxes — verified end-to-end by @tastyeffectco in https://github.com/tastyeffectco/sandboxd/pull/69
* fix(console): terminal WebSocket died with 400 through the console nginx by @tastyeffectco in https://github.com/tastyeffectco/sandboxd/pull/77
* fix(console): terminal origin check failed on non-default ports ($host strips the port) by @tastyeffectco in https://github.com/tastyeffectco/sandboxd/pull/78
* console: terminal connects on explicit click (not on tab open) by @tastyeffectco in https://github.com/tastyeffectco/sandboxd/pull/79
* readme: CTAs for release news + Cloud waitlist by @tastyeffectco in https://github.com/tastyeffectco/sandboxd/pull/80
* console(demo): make visitors notice the live preview is a real running app by @tastyeffectco in https://github.com/tastyeffectco/sandboxd/pull/81
* readme: affiliate disclosure above the deploy links by @tastyeffectco in https://github.com/tastyeffectco/sandboxd/pull/83
* console(home): explainer + live overview stats on the Apps home by @tastyeffectco in https://github.com/tastyeffectco/sandboxd/pull/82
* readme: compact 2-column screenshot grid by @tastyeffectco in https://github.com/tastyeffectco/sandboxd/pull/85
* feat(agent-auth): add direct MiniMax credentials and upstreams by @octo-patch in https://github.com/tastyeffectco/sandboxd/pull/89
* upgrade: rebuild the sandbox base image when runtimed changes by @tastyeffectco in https://github.com/tastyeffectco/sandboxd/pull/90
* console: update-available notification (+ checker false-positive fix) by @tastyeffectco in https://github.com/tastyeffectco/sandboxd/pull/91
* release tooling: ./release.sh (rolling patch releases + generated changelog) by @tastyeffectco in https://github.com/tastyeffectco/sandboxd/pull/92

### New Contributors
* @octo-patch made their first contribution in https://github.com/tastyeffectco/sandboxd/pull/89

**Full Changelog**: https://github.com/tastyeffectco/sandboxd/compare/v0.3.0...v0.3.1

## [0.3.0] — 2026-07-07

The major platform release: a **web console**, one-step **runtime presets**, live
preview URLs, agent tasks, app config &amp; secrets, snapshots / fork / restore,
and git import / commit / push — with one headline change: **every coding agent
now reaches its model provider through a credential-injecting proxy, so no API key
or OAuth token ever enters a sandbox.**

### Added
- **Credential-injecting auth proxy for all agents.** claude-code and opencode
  route through a control-plane proxy (`internal/authproxy`) that holds the real
  credential and injects it on the wire; the sandbox gets only a base URL + a
  dummy key, and nothing secret is mounted or env-injected into the workspace.
  `SANDBOXD_OPENCODE_ZEN_PATH` selects the OpenCode Zen endpoint (`zen`
  pay-as-you-go or `zengo` subscription).
- **OpenCode is the default agent, and `--continue` is the default** for
  follow-up tasks — tri-state (`continue` omitted → continue when a prior session
  exists, gated so the first task in a sandbox starts fresh; `true`/`false` force
  it).

### Platform
This release adds the full self-hosted platform: a **web console**;
one-step **runtime presets** (React/Vite, Next.js, Node/Express, FastAPI,
Worker); **live preview URLs**; **agent tasks**; **app config &amp; secrets**
(write-only secrets); **snapshots / fork / restore**; managed **agent auth**
(API-key / import / guided OAuth); **git import, commit &amp; push**; **runtime
detection &amp; manifest**; an **activity / events** timeline; **per-process
logs**; and a **settings** view with editable idle / keepalive lifecycle
controls.

## [0.2.0] — 2026-06-22

Reliability fixes across the core, and durable "apps" as first-class entities
above sandboxes.

### Added
- **Durable app model.** Apps are now first-class entities above sandboxes. An
  app owns the user-facing concept (name, description, tags) and outlives the
  sandbox that is its current running instance. New tenant-scoped `/v1/apps` API
  (`POST` / `GET` / `GET {id}` / `PATCH {id}` / `POST {id}/sandbox`) with optional
  `external_*` integration tags; sandboxes gain a nullable `app_id`. Additive and
  backwards-compatible — the existing sandbox API is unchanged. (#31)
- **Selectable app templates.** A working Vite + React + TypeScript
  `react-standard` scaffold ships in the image at `/opt/templates/<name>` and is
  seeded into a new workspace on first boot (default `react-standard`;
  `template: "blank"` for an empty workspace). The agent now edits a known-good
  app with a passing build and a live preview instead of scaffolding from an
  empty directory. (#29)
- **Per-task timeout.** `timeout_s` on `POST /v1/sandboxes/{id}/tasks` (0 or
  omitted → 10m default, max 24h). The control-plane task watcher now derives its
  streaming window from the task timeout instead of a fixed 15 minutes, so long
  tasks are no longer marked failed prematurely. (#25)
- **Per-sandbox idle policy.** `idle_policy: sleep | always_on`. (#14)
- **End-to-end + image-smoke CI.** A job that builds the base image and drives
  the real create → seed → install → serve → wake lifecycle on a Docker daemon,
  and asserts the agent CLIs and the default template are present on the image.
  Adds `go vet` to the Go job. (#30)

### Fixed
- **Snapshot capture** targeted the old loopback `.img` model and returned 500 on
  the default directory-storage workspaces; it now copies the workspace tree
  crash-consistently and round-trips through `from_snapshot`. (#24)
- **`POST /v1/sandboxes`** returned `400` on a clean install because it forced an
  unseeded `react-standard` template; a no-template create is now provisioned
  cleanly. (#28)
- Four confirmed correctness items from the security/code audit. (#21)

### Changed
- The image installs Claude Code via the official native installer, alongside
  OpenCode. (#18)

### Removed
- The dormant single-token auto-git-push path (undocumented, unused). (#23)

## [0.1.1] — 2026-06-07

- Renamed the project to **sandboxd** and standardized the `SANDBOXD_` env prefix.
- Docs: production-safety checklist; Rancher Desktop / k3s port-80 preview note.

## [0.1.0] — 2026-06-06

- Initial release.

[0.3.0]: https://github.com/tastyeffectco/sandboxd/releases/tag/v0.3.0
[0.2.0]: https://github.com/tastyeffectco/sandboxd/releases/tag/v0.2.0
[0.1.1]: https://github.com/tastyeffectco/sandboxd/releases/tag/v0.1.1
[0.1.0]: https://github.com/tastyeffectco/sandboxd/releases/tag/v0.1.0
