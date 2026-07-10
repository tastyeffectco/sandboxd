<h1 align="center">sandboxd</h1>

<p align="center">
  <b>The open-source backend behind AI app-builders.</b><br/>
  Type a prompt → an isolated sandbox spins up, a coding agent builds the app,
  and it's live at a preview URL. Self-hosted, one command, MIT.
</p>

<p align="center">
  <a href="https://sandboxd.io"><img alt="Docs" src="https://img.shields.io/badge/docs-sandboxd.io-ff6b00.svg"></a>
  <a href="LICENSE"><img alt="License: MIT" src="https://img.shields.io/badge/license-MIT-green.svg"></a>
  <img alt="Runs on Docker" src="https://img.shields.io/badge/runs%20on-Docker-2496ED.svg">
  <a href="https://github.com/tastyeffectco/sandboxd/releases"><img alt="Release" src="https://img.shields.io/github/v/release/tastyeffectco/sandboxd?color=00ADD8"></a>
  <a href="https://github.com/sponsors/tastyeffectco"><img alt="Sponsor" src="https://img.shields.io/badge/%E2%9D%A4%20Sponsor-tastyeffectco-db61a2?logo=githubsponsors&logoColor=white"></a>
</p>

---

<img width="1100" height="816" alt="sandboxd-demo" src="https://github.com/user-attachments/assets/f794ff9b-8ffe-47e8-bd30-22541f870f09" />

## What is sandboxd?

The apps where you type *"build me a todo app"* and a working site appears at its
own link — Lovable, Bolt, v0, Replit. **sandboxd is the open-source engine that
makes that work, on your own server.** One HTTP request and it:

1. spins up a **private, isolated container** (its own filesystem + limits),
2. runs an **AI coding agent inside it** against your prompt, and
3. hands the app a **live preview URL**.

Idle sandboxes **sleep and wake on demand**, so one ordinary box holds many apps
instead of a VM each. Under the hood it's deliberately small: **one Go program
driving Docker**, Traefik for URLs, SQLite for state — no Kubernetes, no separate
database, no queue.

## Two ways to use it

- **API-first** — everything is a `/v1` call, so you can build sandboxd into your
  own product.
- **Optional web console** — the fastest, **no-code** way to use it hands-on:
  create an app, chat to a coding agent, watch the live preview, edit files, and
  review a git diff & push — all in a browser. It's a **pure `/v1` client**; the
  engine runs perfectly headless without it.

> **New here? Start with the console. Building a product? Drive the API.**

## Quick start

Needs **Docker + the Compose plugin** and **git** on Linux (macOS via Docker
Desktop is best-effort). Install in one line:

```bash
curl -fsSL https://raw.githubusercontent.com/tastyeffectco/sandboxd/main/install.sh | bash
```

It builds the images and starts the stack; the API is then live at
`http://127.0.0.1:9090` (`curl http://127.0.0.1:9090/healthz` → `ok`). Then pick one:

**Web console (no code):**
```bash
export CONSOLE_BASIC_AUTH="admin:$(openssl passwd -apr1 'choose-a-password')"  # fail-closed
docker compose --profile console up -d
# open http://console.localhost, connect an agent, and build
```

**API:** connect an agent once, create a sandbox, hand it a prompt:
```bash
API=http://127.0.0.1:9090
curl -s -XPOST $API/v1/agents/claude-code/api-key -d '{"api_key":"sk-ant-..."}'
ID=$(curl -s -XPOST $API/sandbox -d '{"ports":[3000]}' | sed -E 's/.*"id":"([^"]+)".*/\1/')
curl -s -XPOST $API/v1/sandboxes/$ID/tasks -d '{"prompt":"build a todo app on port 3000","agent":"opencode"}'
# open the result at  http://s-$ID-3000.preview.localhost
```

**Full walkthrough → [sandboxd.io/quickstart](https://sandboxd.io/quickstart).**

## What you get

- **Isolated sandboxes** — a hardened container per app with a workspace + live
  preview URL; sleep/wake so idle apps cost nothing.
- **Built-in agents** — OpenCode & Claude Code. **No credential ever enters a
  sandbox** (a proxy injects it on the wire), and every task is **checkpointed &
  revertible**.
- **Runtime presets** — React/Vite, Next.js, Node/Express, FastAPI, Worker; boot
  to a preview and reload after agent edits.
- **Files, Git & secrets** — in-browser editor + diffs, commit & push, per-app
  config/secrets (encrypted, write-only).
- **Snapshots / fork / restore**, an activity timeline, per-process logs, and live
  lifecycle tuning.

## Who's it for?

**✅ Use it** if you run **many sandboxes for other people** — an AI app-builder,
an agent platform, a coding playground, per-user or per-branch preview
environments, or team multi-app hosting.

**❌ Skip it** if you just need one or two containers for yourself — a shell
script or `docker run` is simpler.
([Why not just a script?](https://sandboxd.io/what-is-sandboxd))

## Documentation

Full docs live at **[sandboxd.io](https://sandboxd.io)**:

| Getting started | Guides | Reference |
|---|---|---|
| [What is sandboxd?](https://sandboxd.io/what-is-sandboxd) | [The web console](https://sandboxd.io/guides/console) | [API (OpenAPI)](https://sandboxd.io/reference/api) |
| [Quickstart](https://sandboxd.io/quickstart) | [Coding agents](https://sandboxd.io/guides/agents) | [Configuration](https://sandboxd.io/reference/configuration) |
| [Core concepts](https://sandboxd.io/concepts) | [Base image, stacks &amp; presets](https://sandboxd.io/guides/images-stacks-presets) | [Architecture](https://sandboxd.io/reference/architecture) |
| [Roadmap](https://sandboxd.io/roadmap) | [Presets &amp; `sandbox.yaml`](https://sandboxd.io/guides/images-stacks-presets) · [Auto-detection &amp; App Store](https://sandboxd.io/guides/runtime-and-store) · [Undo a task](https://sandboxd.io/guides/tasks) · [Production / TLS](https://sandboxd.io/guides/production-tls) · [Hardening](https://sandboxd.io/guides/hardening) · [Uninstall](https://sandboxd.io/guides/uninstall) | [`AGENTS.md`](AGENTS.md) · [`ARCHITECTURE.md`](ARCHITECTURE.md) |

Building on it from your own agent? **[`AGENTS.md`](AGENTS.md)** is a
copy-pasteable runbook.

## Roadmap &amp; community

- 🗺️ **Roadmap** — [what's shipped &amp; next](https://sandboxd.io/roadmap) · [public project board](https://github.com/users/tastyeffectco/projects/3)
- 💬 **Discussions** — [ask &amp; get self-hosting help](https://github.com/tastyeffectco/sandboxd/discussions)
- 🤝 **Contribute** — good first PRs: add a runtime preset or an App Store recipe ([`CONTRIBUTING.md`](CONTRIBUTING.md))
- 🔒 **Security** — report privately per [`SECURITY.md`](SECURITY.md)

> **Beta · 0.x.** Container isolation (not VMs), single-server, and API auth off
> by default — all fine for your own users; tighten before untrusted
> multi-tenancy. See [Hardening](https://sandboxd.io/guides/hardening). Expect the
> occasional breaking change before 1.0 — pin a version and update as you go.

## License

[MIT](LICENSE). Use it, ship it, sell what you build on it.

## Sponsors

sandboxd is free and MIT-licensed. **Sponsors keep it maintained and fund the
[deploy roadmap](https://github.com/sponsors/tastyeffectco)** — thank you to
everyone who chips in.

<!--
  SPONSOR LOGO WALL — how to add a sponsor:
  Drop a cell into the <p align="center"> grid below (highest tiers first):
      <a href="https://SPONSOR_SITE"><img src="LOGO_URL" width="130" alt="Sponsor Name"></a>
  Guidelines: logo ~130px wide, PNG or SVG, hosted on a stable URL. Remove the
  placeholder <em> line once the first real logo lands.
-->
<p align="center">
  <!-- sponsor logos go here -->
  <em>No sponsors yet — <a href="https://github.com/sponsors/tastyeffectco">be the first</a>.</em>
</p>
