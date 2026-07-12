# sandboxd PaaS — Roadmap & Intentions

> **Vision.** An **all-in, self-hostable PaaS** — build apps with an AI agent in a
> **dev sandbox**, then **deploy them to production** (custom domains, prod env,
> multiple servers) — all from **one console**, with **sandboxd as the open-source
> core engine**. The console is one client; the engine stays lean, agnostic, and
> usable by any tool.

This document is the human-readable mirror of our GitHub Project board. It exists
so the direction is **trackable and public** even before we implement. Nothing
here is committed to a date; it's intent, ordered by our current thinking.

**Design north star:** *mechanism in the core, policy in the clients.* The engine
ships primitives (run a sandbox, inject env, route a container); the console/CLI
build the PaaS experience (dev/prod, deploy, domains, teams) on top.

---

## Columns

- **💬 To Discuss** — open questions we must resolve before planning.
- **📋 Planned** — agreed direction, specced or spec-ready, not yet built.
- **🔬 Exploring** — spikes / investigations to de-risk a Planned item.
- **🚧 In Progress** — actively being built.
- **✅ Shipped** — done and live.

---

## ✅ Shipped

| Item | Notes |
|---|---|
| Docs site refreshed for 0.3 | concepts (deep), Roadmap page, API via OpenAPI — live on sandboxd.io |
| Cloud waitlist (double opt-in) | Cloudflare Worker + D1 + Resend; confirm-before-list — live |
| Public Roadmap + Discussions | roadmap page + GitHub Discussions enabled |

## 🚧 In Progress

| Item | Notes |
|---|---|
| **Ship 0.3.0** | merge dev line → `main`, tag `v0.3.0` (first public release) |
| **Upgrade/migration hardening** | audit done → add install.sh DB backup, downgrade guard, v(N-1)→vN smoke lane |

## 📋 Planned

| Item | Area | Notes |
|---|---|---|
| **Environment variables — core primitive** | core | L0: `env` on sandbox-create → inject as process env; L1: durable named env sets (`app_config.environment`) |
| **Deploy · Path A — prod-as-sandbox** | console | fork → always-on sandbox + prod env; **zero core**; dev+prod on one server |
| **Deploy · Path B — prod-as-image ("one-click deploy")** | core | build image → lean `docker run --env-file` container → route → `deployments` table |
| **Custom domains** | core | Traefik route-registration seam over the file provider + per-domain TLS |
| **Multi-server (beyond one host)** | core | scheduling / add worker nodes; run prod off the dev box |
| **1-click update** | core+console | CLI `sandboxd update` (backup→pull→build→up); `GET /v1/system/version` + console "update available" banner |
| **Console · Environment variables editor** | console | dev/prod switcher, row editor, bulk `.env` developer view |
| **Managed databases & sidecars** | core | attach Postgres/Redis to an app |
| **Snapshots / templates hardening** | core | durable directory-tar backend |
| **Stronger isolation providers** | core | gVisor / Kata / microVM per tenant for untrusted code |
| **Egress controls in OSS build** | core | first-class toggle for the existing nftables egress subsystem |

## 🔬 Exploring

| Item | Question it de-risks |
|---|---|
| Build strategy for Path B | app Dockerfile vs buildpack/Nixpacks vs base-image + baked workspace |
| Non-Docker backends | a containerd / OCI provider behind the same API |
| Zero-downtime deploy | rolling / health-gated cutover (later, likely post-1.0) |

## 💬 To Discuss

| Question | Why it matters |
|---|---|
| **Multi-sandbox per app** | today it's one sandbox/app (prod needs a fork). Do we allow dev+prod under one app? |
| **Secrets: strict write-only vs owner-reveal** | we chose write-only; revisit for developer UX |
| **Predefined `SANDBOXD_*` vars** | inject preview URL / `PORT` / sandbox id (like Coolify's `COOLIFY_*`)? |
| **Image registry for Path B** | local store vs external registry; who cleans up |
| **Custom-domain TLS strategy** | per-domain cert resolver vs shared wildcard |
| **sandboxd Cloud** | managed-hosting scope, regions, pricing, no-lock-in guarantee |
| **Multi-tenant hardening posture** | when to flip egress / VM isolation on by default |
| **Boundary check** | what stays *core mechanism* vs *console policy* as we add PaaS features |

---

### How this maps to releases (indicative, not committed)

- **0.3** — the dev experience (console, presets, agents, previews). *Shipping.*
- **0.4** — **environment variables** + **Deploy Path A** (prod-as-sandbox) +
  1-click update banner.
- **0.5** — **Deploy Path B** (real image deploy) + **custom domains**; groundwork
  for **multi-server**.
- **1.0** — stable `/v1`, hardening; the PaaS story complete on one-to-few servers.

*The live board (GitHub Project) is the source of truth for status; this file is
its readable snapshot.*
