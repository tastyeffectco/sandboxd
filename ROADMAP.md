# Roadmap

sandboxd is open source and MIT-licensed — self-host the whole thing on one
machine in one command. This is the honest state of the project: what works
today, what's coming, and how versioning works before 1.0. It's a direction, not
a contract — priorities shift with what self-hosters actually need.

> Roadmap items are decided in the open. Weigh in — or propose your own — in
> [Discussions](https://github.com/tastyeffectco/sandboxd/discussions).
> [Sponsoring](https://github.com/sponsors/tastyeffectco) directly funds the
> deploy roadmap and ongoing maintenance.

## Shipped — 0.3.0 (first public release)

The full platform, self-hostable on one Docker host today:

- **Web console** — create/open apps, live preview, agent chat, in-browser files
  editor, git diff/commit/push, task history + revert, config & secrets,
  snapshots, activity, settings. (Optional — the engine also runs headless over `/v1`.)
- **One-command install** — builds the image + control plane and starts the stack.
- **Runtime presets** — React/Vite, Next.js, Node/Express, FastAPI, Worker.
- **Built-in coding agents** — OpenCode (default) & Claude Code via a
  credential-injecting proxy, so **no key ever enters a sandbox**. OpenCode Zen
  (`zen` / `zengo`) supported.
- **Agent tasks** — streamed progress, honest results (`build_status`,
  `preview_ok`, `app_healthy`), every task checkpointed and **revertible**.
- **Live preview URLs** + **sleep & wake**.
- **Git import + commit + push**, **config & secrets** (encrypted, write-only),
  **snapshots / fork / restore**, **activity timeline**, **per-process logs**.
- **Runtime detection** (40+ recipes) & **App Store recipes**.
- **Hardened container isolation** + encrypted credentials at rest.

Full detail in [CHANGELOG.md](CHANGELOG.md).

## Next — 0.4.x

Near-term focus, funded by the deploy roadmap:

- **One-click deploy / publish** — take an app from sandbox to a real, persistent
  deployment. The headline next feature.
- **Console polish** — files editor, git flow, task history.
- **Egress controls in the OSS build** — the nftables egress subsystem exists but
  ships off; make it a first-class toggle for multi-tenant hardening.
- **Snapshot backend hardening** — a durable directory-tar backend.

## Later

Bigger bets, roughly by demand:

- **Stronger isolation providers** — gVisor / Kata / Firecracker microVM-per-tenant
  for running *untrusted* code.
- **Non-Docker backends** — a containerd / OCI provider behind the same API.
- **Managed databases & sidecars** — attach a Postgres/Redis to an app.
- **Beyond one server** — multi-host scheduling and workspace sharding.
- **Per-workspace disk quotas** and richer resource governance.
- **App Store expansion** — more curated one-click open-source recipes.

## Versioning & stability

sandboxd follows [SemVer](https://semver.org/). **Pre-1.0**, a minor bump
(`0.3 → 0.4`) may add features and can carry breaking changes — kept to a minimum
and always noted in the CHANGELOG. If you self-host, **pin a version and be ready
to update** as new ones land.

**1.0** means the `/v1` API is stable and frozen — estimated 6–12 months out. The
path there is polish, the deploy feature, and hardening, not new surface area.
