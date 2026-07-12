# Changelog

All notable changes to sandboxd are documented here. The format is based on
[Keep a Changelog](https://keepachangelog.com/), and the project follows
[Semantic Versioning](https://semver.org/) (pre-1.0: a minor bump adds features,
a patch is fixes only).

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
