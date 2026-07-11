# Managed sandboxd (BYOC) — plan & roadmap

**Date:** 2026-07-11
**Idea:** a **managed** offering where **we host only the sandboxd control plane**
(cheap), and **users connect their own servers over SSH** — sandboxes run on
*their* compute, on *their* cloud bill. Open-source stays single-server (or
self-connect your own fleet). This keeps our GCP spend near-flat while revenue
scales → very high earning ratio.

## The two products

| | Open-source (self-host) | Managed (SaaS, closed) |
|---|---|---|
| Who runs the control plane | you | **us** (GCP, tiny VM) |
| Who runs sandbox **compute** | you (local or your remote servers) | **the user's own servers** (BYOC over SSH) |
| We pay for | nothing | just the control plane (~flat) |
| User pays for | their server | subscription (the brain) **+** their own server |

**Principle (same as the whole project): mechanism in the core, business in the
managed layer.** The multi-host capability is open-source; hosting it
multi-tenant + billing + onboarding is the closed managed layer.

## Why this keeps GCP spend low (the earning-ratio point)

- We run **only the control plane** — metadata orchestration + SSH connections +
  reconciliation. It's light: **one always-on `e2-small` (~$13/mo)** serves many
  tenants. Your **$2,000 credits → ~12+ months** of runway, and cost is **~flat**
  as users grow.
- **Compute (the expensive part) never touches our cloud** — it runs on users'
  servers. Revenue scales with users; our bill barely moves.
- Rule to protect the ratio: **for managed users we never run sandbox compute on
  GCP.** GCP hosts the brain only.

## What the code says (feasibility, grounded)

The engine drives Docker through **one function** — `docker.Client.run()` →
`exec.CommandContext(ctx, "docker", …)`. The Docker CLI natively targets a remote
daemon via **`DOCKER_HOST=ssh://user@host`**. So:

- **Remote container lifecycle (run/stop/rm/exec/inspect/list): EASY.** Add a
  `Host` field to `Client`, set `DOCKER_HOST` in `run()`. A few lines.
- **Remote workspace / files / git / snapshots: HARD.** Today these are
  **host-local filesystem** ops on the control plane (bind-mounted data dir,
  git clone/push host-side, checkpoints, `.img` snapshots). On a remote host the
  workspace lives *there* — these must move to run against the remote (via
  `docker exec`/`docker cp` into the sandbox, or a small node agent).
- **Remote preview routing: MEDIUM-HARD.** Traefik uses the **local** docker
  provider; a container on a *remote* docker is invisible to it. Each connected
  server must run **its own Traefik**, and previews are served from that server.

No `servers`/`nodes` concept exists yet (migrations end at 0021).

## Core engine changes (open-source — the "multi-host" primitive)

### Layer A — Remote container lifecycle (the enabler; small)
- `Client{ Bin, Host }`; `run()` sets `DOCKER_HOST` when `Host != ""`.
- Migration: a **`servers`** table — `id, name, connection (ssh://user@ip),
  ssh_key_ref (encrypted), status, capacity, tenant, created_at`. Default row =
  the local host.
- Sandbox row gains **`server_id`**. A **scheduler** picks a server: user-chosen
  (Settings → "deploy to…") or least-loaded.

### Layer B — Remote workspace, files, git (the real work)
- Move workspace-scoped ops off the control-plane disk:
  - **Files API** (list/read/write/export) → `docker exec`/`docker cp` against the
    remote sandbox (works over `DOCKER_HOST=ssh`).
  - **Git import/commit/push** → run inside a container **on the remote**, not on
    the control-plane host.
  - **Checkpoints/snapshots** → remote-side (git ref in the workspace; snapshot as
    a remote image/tar).
- This decouples the control plane from local disk — the prerequisite for remote.

### Layer C — Remote preview routing
- Each connected server runs **its own Traefik** (reads its local docker).
- **DNS/TLS:** managed platform provisions a wildcard **per server** —
  `*.<server-slug>.sandboxd.app → server IP` (Cloudflare DNS API), TLS via the
  server's Traefik (Let's Encrypt) or Cloudflare. OSS self-hosters use their own
  per-server domain.
- The control plane returns the **server-specific** preview URL.

### Layer D — Provisioning + health
- **Add a server:** user pastes host; we install over SSH in one script — Docker +
  the sandboxd **node stack** (Traefik + base image + optional agent). Reuse
  `install.sh` in a "node mode". SSH key generated + stored **encrypted** (existing
  secrets store).
- **Health/capacity** polling per server; scheduler avoids full/unhealthy; the
  reconciler runs **per server**.

## Managed layer (closed — the business)

1. **Hosted control plane on GCP** — one always-on `e2-small` VM; SQLite on a
   persistent disk to start (fine for metadata), **Cloud SQL/Postgres only when
   the control plane needs to scale horizontally**.
2. **Multi-tenancy** — the engine already has `owner_token`/tenant scoping; harden
   it so a tenant only sees/deploys to **their own** servers + sandboxes.
3. **Server-connect UX** — Settings → Servers → add via SSH (the Layer-D flow).
4. **Billing (Stripe)** — subscription **per workspace / seat / connected server**,
   **never per compute**. Meter active sandboxes/servers for limits.
5. **Onboarding** — "connect your first server" wizard; affiliate link (Hetzner/DO)
   for users without a box.
6. **DNS automation** — Cloudflare API points per-server wildcards at user IPs
   (you already use Cloudflare for the docs/waitlist).

## Recommended tech

- **Remote compute:** **Docker over SSH** (`DOCKER_HOST=ssh://`). Simplest, no TLS
  cert ops, key-based — Coolify-proven. (A `tcp://`+mTLS backend can come later.)
- **Control-plane host:** GCP **e2-small always-on VM** (reconciliation + SSH need
  always-on; Cloud Run later for the stateless API only).
- **DB:** **SQLite** now (control-plane metadata is light); **Cloud SQL/Postgres**
  when multi-instance.
- **DNS/TLS:** **Cloudflare** (DNS API + per-server wildcard) + Let's Encrypt on
  each server's Traefik.
- **Billing:** **Stripe**.
- **Secrets:** existing encrypted secrets store for SSH keys / server creds.
- **Node agent:** minimal — Docker + Traefik + base image via the one-line
  installer; add a tiny `sandboxd-node-agent` only if docker-over-SSH isn't enough
  for files/health.

## Cost & earning-ratio model

| | Amount |
|---|---|
| Our monthly cost | **~$13** control-plane VM + Cloudflare (free tier) + Stripe fees — **~flat** |
| $2,000 GCP credits runway | **~12+ months** on the control plane alone |
| User's cost | their server (Hetzner ~€4, DO ~$6, or an existing box) — **they pay compute** |
| Suggested price | e.g. **$19/mo per workspace or connected server** |
| Gross margin | **~95%+** (we pay only the brain) |

**The lever:** compute is BYOC, so our cost decouples from user count. Guard it by
never scheduling managed sandboxes onto GCP.

## Roadmap (phased)

- **0.5 · Multi-host core (OSS)** — Layer A + `servers` table + scheduler +
  Settings "add server (SSH)". A sandbox can run on a connected remote server
  (files/preview may be limited first).
- **0.6 · Remote workspace + previews (OSS)** — Layer B + C: full sandbox
  functionality on remote hosts (files, git, per-server previews + TLS).
- **0.7 · Provisioning + health (OSS)** — Layer D: one-click server add,
  health/capacity, per-server reconcile.
- **Managed beta (closed)** — hosted control plane on GCP + multi-tenant hardening
  + Stripe + DNS automation + onboarding wizard.
- **Managed GA** — scheduling/quotas, team seats, observability, autoscaling hints.

## Security corners (deep)

- **SSH creds** per server, **encrypted at rest** (existing secrets key); a
  least-privilege remote user (docker group), optionally a forced/restricted SSH
  command.
- **Trust boundaries:** (1) our control plane → users' servers (least-priv, keys
  rotm-able); (2) sandboxes run untrusted-ish code on the **user's** box — their
  isolation posture (container today; VM/gVisor if they run untrusted strangers).
  Document it.
- **Multi-tenant:** strict ownership scoping on `servers` + `sandboxes`; a tenant
  can only deploy to servers they connected. Per-tenant preview auth + wildcard
  scoping.
- **Billing abuse:** metering + hard limits on active sandboxes/servers per plan.
- **Blast radius:** one tenant's bad server can't affect another's (separate
  Traefik, separate wildcard, separate SSH).

## Open questions

- Preview domains: platform-managed `*.<slug>.sandboxd.app` vs user-BYO domain
  (support both; default managed).
- Snapshots/fork across servers (move a workspace between hosts) — later.
- Control-plane HA / multi-instance (needs Postgres + leader election) — later.
- Node agent vs pure docker-over-SSH for file ops — spike in 0.6.
