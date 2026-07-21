<h1 align="center">sandboxd</h1>

<p align="center">
  <b>Open-source, self-hosted AI app builder.</b><br/>
  Prompt it and a coding agent builds real apps in isolated sandboxes on <b>your</b>
  server — each live at a preview URL. You own the infra, code, and data. MIT.
</p>

<h3 align="center">🔥 Trend of the Day on Trendshift</h3>

<p align="center">
  <a href="https://trendshift.io/repositories/45741?utm_source=trendshift-badge&utm_medium=badge&utm_campaign=badge-trendshift-45741" target="_blank" rel="noopener noreferrer">
    <img
      src="https://trendshift.io/api/badge/trendshift/repositories/45741/daily?language=Go"
      alt="sandboxd • Trend of the Day on Trendshift"
      width="320"
    />
  </a>
</p>

<p align="center">
  <a href="https://sandboxd.io/demo/"><img alt="Live demo" src="https://img.shields.io/badge/%E2%96%B6%20Live%20demo-try%20it%20in%20your%20browser-ff6b00?style=for-the-badge"></a>
  &nbsp;
  <a href="https://sandboxd.io/quickstart"><img alt="Get started" src="https://img.shields.io/badge/Get%20started-one%20command-0a0a0a?style=for-the-badge"></a>
  &nbsp;
  <a href="https://github.com/tastyeffectco/sandboxd/stargazers"><img alt="Star sandboxd" src="https://img.shields.io/github/stars/tastyeffectco/sandboxd?style=for-the-badge&label=%E2%98%85%20Star&color=333"></a>
</p>

<p align="center">
  <a href="https://sandboxd.io"><img alt="Docs" src="https://img.shields.io/badge/docs-sandboxd.io-ff6b00.svg"></a>
  <a href="LICENSE"><img alt="License: MIT" src="https://img.shields.io/badge/license-MIT-green.svg"></a>
  <img alt="Runs on Docker" src="https://img.shields.io/badge/runs%20on-Docker-2496ED.svg">
  <a href="https://github.com/tastyeffectco/sandboxd/releases"><img alt="Release" src="https://img.shields.io/github/v/release/tastyeffectco/sandboxd?color=00ADD8"></a>
  <a href="https://github.com/sponsors/tastyeffectco"><img alt="Sponsor" src="https://img.shields.io/badge/%E2%9D%A4%20Sponsor-tastyeffectco-db61a2?logo=githubsponsors&logoColor=white"></a>
</p>

---

<p align="center"><b>Prompt → a real, running app on <i>your</i> server, at its own URL.</b><br/>See it in action 👇</p>

<h3 align="center">1&nbsp;·&nbsp;Build an app from a prompt — in the console</h3>
<p align="center"><i>Describe what you want; a coding agent builds it inside an isolated sandbox and it appears at a live preview URL.</i></p>
<p align="center">
  <img width="900" alt="Create a blog from a prompt in the sandboxd console" src="https://github.com/user-attachments/assets/582f5c46-af57-41c1-b9ed-4081d1075c51" />
</p>

<h3 align="center">2&nbsp;·&nbsp;Or launch an existing open-source app — one click</h3>
<p align="center"><i>n8n, Ghost, Grafana, Gitea… 80+ curated apps, installed and live at their own URL.</i></p>
<p align="center">
  <img width="900" alt="Run an existing open-source app (n8n) in sandboxd" src="https://github.com/user-attachments/assets/a89b03c9-0f4d-4b19-b190-ecd27dd10d10" />
</p>

<h3 align="center">3&nbsp;·&nbsp;Prefer code? Drive it all from the API / CLI</h3>
<p align="center"><i>Every action is a <code>/v1</code> call — headless, scriptable, and easy to build into your own product.</i></p>
<p align="center">
  <img width="900" alt="Drive sandboxd from the API / CLI" src="https://github.com/user-attachments/assets/0854fbd6-6801-42e3-bb42-55f8a4f9ead7" />
</p>

<p align="center"><b>▶ <a href="https://sandboxd.io/demo/">Try the live demo</a></b> — click around a real console with sample data, no install.</p>

---

<h3 align="center">📣 Stay in the loop</h3>

<p align="center">
  Self-hosting is <b>MIT and free, forever</b> — <code>git clone</code> and you're done.<br/>
  Want the <b>1.0 release + occasional product updates</b>, or a <b>managed cloud</b> so you don't run the box yourself? Two links:
</p>

<p align="center">
  <a href="https://sandboxd.io/?news=1"><img alt="Get 1.0 + release news" src="https://img.shields.io/badge/%F0%9F%93%AC%20Get%201.0%20%2B%20release%20news-subscribe-0a0a0a?style=for-the-badge"></a>
  &nbsp;
  <a href="https://sandboxd.io/?waitlist=cloud"><img alt="sandboxd Cloud waitlist" src="https://img.shields.io/badge/%E2%98%81%EF%B8%8F%20sandboxd%20Cloud-join%20the%20waitlist-ff6b00?style=for-the-badge"></a>
</p>

<p align="center"><sub>No spam, one-click unsubscribe, and the cloud is <b>optional</b> — the self-hosted engine is always the full product.</sub></p>

---

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

## What can you run? (a lot — from one console, no code)

The console isn't only for coders. Open it and, in **one click or one prompt**, you can:

- **🚀 Run a ready-made open-source app** — a **Ghost** blog, **n8n** automations, a **Gitea** git host, **Grafana** / **Metabase** dashboards, **Uptime Kuma**, **Jupyter**, **Keycloak**… **80+ curated apps**, installed and live at their own URL.
- **🧩 Start from a starter** — a React/Vite, Next.js, or FastAPI scaffold that boots to a live preview; then just *chat* to shape it.
- **📥 Bring your own repo** — import any **public** Git repo (no credential needed) and let a coding agent work on it.
- **✨ Build from scratch** — describe an app and watch the agent build it in the live preview, then commit &amp; push.

<p align="center">
  <img src="https://raw.githubusercontent.com/tastyeffectco/sandboxd/main/console/public/app-icons/ghost.webp" height="38" alt="Ghost" title="Ghost" hspace="7" />
  <img src="https://raw.githubusercontent.com/tastyeffectco/sandboxd/main/console/public/app-icons/n8n.svg" height="38" alt="n8n" title="n8n" hspace="7" />
  <img src="https://raw.githubusercontent.com/tastyeffectco/sandboxd/main/console/public/app-icons/directus.svg" height="38" alt="Directus" title="Directus" hspace="7" />
  <img src="https://raw.githubusercontent.com/tastyeffectco/sandboxd/main/console/public/app-icons/gitea.svg" height="38" alt="Gitea" title="Gitea" hspace="7" />
  <img src="https://raw.githubusercontent.com/tastyeffectco/sandboxd/main/console/public/app-icons/forgejo.svg" height="38" alt="Forgejo" title="Forgejo" hspace="7" />
  <img src="https://raw.githubusercontent.com/tastyeffectco/sandboxd/main/console/public/app-icons/grafana.svg" height="38" alt="Grafana" title="Grafana" hspace="7" />
  <img src="https://raw.githubusercontent.com/tastyeffectco/sandboxd/main/console/public/app-icons/metabase.svg" height="38" alt="Metabase" title="Metabase" hspace="7" />
  <img src="https://raw.githubusercontent.com/tastyeffectco/sandboxd/main/console/public/app-icons/code-server.webp" height="38" alt="code-server" title="code-server" hspace="7" />
  <img src="https://raw.githubusercontent.com/tastyeffectco/sandboxd/main/console/public/app-icons/jupyter.svg" height="38" alt="Jupyter" title="Jupyter" hspace="7" />
  <img src="https://raw.githubusercontent.com/tastyeffectco/sandboxd/main/console/public/app-icons/keycloak.svg" height="38" alt="Keycloak" title="Keycloak" hspace="7" />
  <img src="https://raw.githubusercontent.com/tastyeffectco/sandboxd/main/console/public/app-icons/pocketbase.svg" height="38" alt="PocketBase" title="PocketBase" hspace="7" />
  <img src="https://raw.githubusercontent.com/tastyeffectco/sandboxd/main/console/public/app-icons/uptime-kuma.svg" height="38" alt="Uptime Kuma" title="Uptime Kuma" hspace="7" />
  <img src="https://raw.githubusercontent.com/tastyeffectco/sandboxd/main/console/public/app-icons/vikunja.svg" height="38" alt="Vikunja" title="Vikunja" hspace="7" />
  <img src="https://raw.githubusercontent.com/tastyeffectco/sandboxd/main/console/public/app-icons/wikijs.svg" height="38" alt="Wiki.js" title="Wiki.js" hspace="7" />
  <img src="https://raw.githubusercontent.com/tastyeffectco/sandboxd/main/console/public/app-icons/triliumnext.svg" height="38" alt="Trilium" title="Trilium" hspace="7" />
  <img src="https://raw.githubusercontent.com/tastyeffectco/sandboxd/main/console/public/app-icons/memos.webp" height="38" alt="Memos" title="Memos" hspace="7" />
  <img src="https://raw.githubusercontent.com/tastyeffectco/sandboxd/main/console/public/app-icons/meilisearch.svg" height="38" alt="Meilisearch" title="Meilisearch" hspace="7" />
  <img src="https://raw.githubusercontent.com/tastyeffectco/sandboxd/main/console/public/app-icons/qdrant.svg" height="38" alt="Qdrant" title="Qdrant" hspace="7" />
  <img src="https://raw.githubusercontent.com/tastyeffectco/sandboxd/main/console/public/app-icons/chroma.svg" height="38" alt="Chroma" title="Chroma" hspace="7" />
  <img src="https://raw.githubusercontent.com/tastyeffectco/sandboxd/main/console/public/app-icons/open-webui.svg" height="38" alt="Open WebUI" title="Open WebUI" hspace="7" />
  <img src="https://raw.githubusercontent.com/tastyeffectco/sandboxd/main/console/public/app-icons/navidrome.svg" height="38" alt="Navidrome" title="Navidrome" hspace="7" />
  <img src="https://raw.githubusercontent.com/tastyeffectco/sandboxd/main/console/public/app-icons/audiobookshelf.svg" height="38" alt="Audiobookshelf" title="Audiobookshelf" hspace="7" />
  <img src="https://raw.githubusercontent.com/tastyeffectco/sandboxd/main/console/public/app-icons/calibre-web.svg" height="38" alt="Calibre-Web" title="Calibre-Web" hspace="7" />
  <img src="https://raw.githubusercontent.com/tastyeffectco/sandboxd/main/console/public/app-icons/actualbudget.svg" height="38" alt="Actual Budget" title="Actual Budget" hspace="7" />
  <img src="https://raw.githubusercontent.com/tastyeffectco/sandboxd/main/console/public/app-icons/syncthing.svg" height="38" alt="Syncthing" title="Syncthing" hspace="7" />
  <img src="https://raw.githubusercontent.com/tastyeffectco/sandboxd/main/console/public/app-icons/prefect.svg" height="38" alt="Prefect" title="Prefect" hspace="7" />
  <img src="https://raw.githubusercontent.com/tastyeffectco/sandboxd/main/console/public/app-icons/marimo.svg" height="38" alt="marimo" title="marimo" hspace="7" />
  <img src="https://raw.githubusercontent.com/tastyeffectco/sandboxd/main/console/public/app-icons/homepage.webp" height="38" alt="Homepage" title="Homepage" hspace="7" />
  <img src="https://raw.githubusercontent.com/tastyeffectco/sandboxd/main/console/public/app-icons/ntfy.svg" height="38" alt="ntfy" title="ntfy" hspace="7" />
  <img src="https://raw.githubusercontent.com/tastyeffectco/sandboxd/main/console/public/app-icons/gotify.svg" height="38" alt="Gotify" title="Gotify" hspace="7" />
  <img src="https://raw.githubusercontent.com/tastyeffectco/sandboxd/main/console/public/app-icons/linkding.svg" height="38" alt="linkding" title="linkding" hspace="7" />
  <img src="https://raw.githubusercontent.com/tastyeffectco/sandboxd/main/console/public/app-icons/superset.svg" height="38" alt="Superset" title="Superset" hspace="7" />
</p>

<p align="center">
  <i><b>This is just a taste — if it's open-source, you can almost certainly run it.</b><br/>
  Any Node, Python, or static-binary app boots as-is; anything else, bring your own base image or preset.</i>
</p>

<p align="center">
  <a href="https://sandboxd.io/guides/apps-in-the-console"><b>Ways to run an app →</b></a> &nbsp;·&nbsp;
  <a href="https://sandboxd.io/guides/images-stacks-presets">Base image, stacks &amp; presets</a> &nbsp;·&nbsp;
  <a href="https://sandboxd.io/reference/architecture">Architecture</a>
</p>

> **Dev → prod, never shorter:** prompt an app into a sandbox, iterate on a live
> preview, then self-host it on your own server in [one command](#-deploy-to-a-vps-in-one-click).

## Quick start

Needs **Docker + the Compose plugin** and **git** on Linux (macOS via Docker
Desktop is best-effort). Runs natively on **amd64 and arm64** — including
Apple Silicon Macs and arm64 Linux hosts (e.g. AWS Graviton) — every image
builds from multi-arch bases with no cross-compilation. Install in one line:

```bash
curl -fsSL https://raw.githubusercontent.com/tastyeffectco/sandboxd/main/install.sh | bash
```

It pulls the prebuilt images (or builds from source if none are published for
your version), starts the stack **with the web console**, and prints your
**console URL + a generated login** — no password step. Open it, connect an agent
under **Settings**, create an app, and build. No code needed.

- **Build from source instead:** run **`./install.sh --build`** — a first-class,
  fully-supported path. Compiles every image locally so you audit exactly what
  runs. Prebuilt images are multi-arch, **cosign-signed with SLSA provenance**
  (built only by [CI](.github/workflows/release-images.yml) from the tagged
  commit) — so pulling is verifiable, and building yourself is always an option.
- **Console:** `http://console.localhost` — the installer prints your login; lost
  it? run **`./console-login.sh`** to see it again anytime
- **API:** `http://127.0.0.1:9090` (`curl http://127.0.0.1:9090/healthz` → `ok`)
- **Headless (no console):** run with `SANDBOXD_CONSOLE=0` (or `--no-console`)
- **Upgrade later:** run **`./upgrade.sh`** — it backs up your database first,
  health-checks the new version, and rolls back automatically if it fails
  ([Upgrading](docs/upgrading.md)). `./upgrade.sh --check` shows your version.

Prefer the API? Connect an agent once, create a sandbox, hand it a prompt:
```bash
API=http://127.0.0.1:9090
curl -s -XPOST $API/v1/agents/claude-code/api-key -d '{"api_key":"sk-ant-..."}'
ID=$(curl -s -XPOST $API/sandbox -d '{"ports":[3000]}' | sed -E 's/.*"id":"([^"]+)".*/\1/')
curl -s -XPOST $API/v1/sandboxes/$ID/tasks -d '{"prompt":"build a todo app on port 3000","agent":"opencode"}'
# open the result at  http://s-$ID-3000.preview.localhost
```

**Full walkthrough → [sandboxd.io/quickstart](https://sandboxd.io/quickstart).**

## 🚀 Deploy to a VPS in one click

sandboxd needs one Linux server with Docker — nothing else. Grab a server
below (2 vCPU / 4 GB is plenty to start), paste our
[cloud-init file](deploy/cloud-init.yaml) at creation, and it installs itself.

> **Disclosure:** we earn a referral commission when you sign up through the
> provider links below, at no additional cost to you. It helps fund sandboxd's
> development.

[![Deploy on Vultr](https://img.shields.io/badge/Deploy%20on-Vultr-007BFC?logo=vultr&logoColor=white&style=for-the-badge)](https://www.vultr.com/?ref=9912150)

<!-- ENABLE THESE AS EACH AFFILIATE LINK ARRIVES (uncomment + replace placeholder):
[![Deploy on DigitalOcean](https://img.shields.io/badge/Deploy%20on-DigitalOcean-0080FF?logo=digitalocean&logoColor=white&style=for-the-badge)](YOUR_DO_AWIN_LINK)
[![Deploy on Kamatera](https://img.shields.io/badge/Deploy%20on-Kamatera-FF6A00?style=for-the-badge)](YOUR_KAMATERA_AFF_LINK)
[![Deploy on Hostinger](https://img.shields.io/badge/Deploy%20on-Hostinger-673DE6?logo=hostinger&logoColor=white&style=for-the-badge)](YOUR_HOSTINGER_AFF_LINK)
Linode removed — cash affiliate program discontinued (credits only as of Jul 2026).
-->

Then on the new server (or paste [`deploy/cloud-init.yaml`](deploy/cloud-init.yaml) as user-data and skip this):

```bash
curl -fsSL https://raw.githubusercontent.com/tastyeffectco/sandboxd/main/deploy/bootstrap.sh | sudo bash
```

Full per-provider walkthrough: [deploy/DEPLOY.md](deploy/DEPLOY.md).

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

## Managed — we run it for you

Don't want to run the server yourself? We'll **install, configure, and manage
sandboxd on your own box** — BYOC (your compute, your data, no lock-in). One-time
setup + optional monthly management. **[See Managed →](https://sandboxd.io/managed)**

## Documentation

Full docs live at **[sandboxd.io](https://sandboxd.io)**:

| Getting started | Guides | Reference |
|---|---|---|
| [What is sandboxd?](https://sandboxd.io/what-is-sandboxd) | [The web console](https://sandboxd.io/guides/console) · [Get an app running](https://sandboxd.io/guides/apps-in-the-console) | [API (OpenAPI)](https://sandboxd.io/reference/api) |
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

## ⭐ If sandboxd is useful, star it

Stars are how other builders find sandboxd — it's the fastest way to support the
project (and it keeps us going). Thank you 🙏

[![Star History Chart](https://api.star-history.com/svg?repos=tastyeffectco/sandboxd&type=Date)](https://star-history.com/#tastyeffectco/sandboxd&Date)

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
