# Rakazo as a sandboxd agent (design draft for approval)

Status: draft, needs the maintainer's yes (core change). Date: 2026-11-18.

## Intent

People should be able to add [Rakazo](https://github.com/elie222/rakazo)
(Apache-2.0, self-hosted "AI teammates": memory, routines, group chats,
pages, integrations, a computer per bot) from the Agents page in one click,
like Hermes, OpenClaw and OpenMausBot.

The maintainer's rule: **an update of sandboxd or of the agent must never
break it, and we adapt to agents without changing their core or source.**

## Goals, non-goals, anti-goals

- Goals: one-click add, works on the desktop app (Windows) and on servers;
  uses the person's sandboxd model connections; scheduled work and files reach
  the person through sandboxd like other agents; survives restarts and updates.
- Non-goals (first version): Rakazo's mobile apps, inbound email, phone calls,
  remote computer providers (E2B, Daytona...), team/multi-user Spaces.
- Anti-goals: patching, forking or rebuilding Rakazo's images; giving any
  Rakazo container the host's Docker socket; letting Rakazo update itself;
  a sandboxd update that silently changes the Rakazo version.

## What Rakazo is (checked 2026-11-18, upstream `main`)

Not one container but a Compose stack (`infra/compose/docker-compose.images.yml`):

| Service | Image | Notes |
|---|---|---|
| postgres | `postgres:16` (digest-pinned upstream) | its data |
| migrate | `ghcr.io/elie222/rakazo/app` | one-shot DB migrations |
| api, worker, web | same app image | web serves the UI on :5173 |
| supervisor | same app image | **runs as root with `/var/run/docker.sock`** and creates one "computer" container per bot/team from `ghcr.io/elie222/rakazo/computer` (Xvfb + Chromium, 2 GB each by default) |
| updater | `ghcr.io/elie222/rakazo/updater` | **Docker socket**, pulls new versions |

Useful switches it already has: `SANDBOX_PROVIDER=none` (no computers),
`RAKAZO_LOCAL_MODELS_URL` / `RAKAZO_LOCAL_MODELS_API_KEY` (OpenAI-compatible
models), MCP servers, `RAKAZO_COMPUTER_MEMORY/CPUS/PIDS_LIMIT`,
`SANDBOX_MAX_COMPUTERS_PER_USER`, analytics/Sentry off when unset.

## The adapter contract (applies to every agent, Rakazo included)

1. **Pinned, never floating.** Every image (app, computer, postgres) is pinned
   by digest in the catalog. Rakazo's `updater` service is never started.
   The version only changes when we bump the digests in a sandboxd release.
2. **Upstream untouched.** sandboxd only sets environment, writes config
   files into the agent's own data folder, picks the start command, and calls
   the agent's public HTTP API. No patches, no mounted code, no rebuilt images.
3. **Everything agent-specific is data** in its catalog `Package` (as today),
   so a new agent or a new version is a catalog change, not new core code.
4. **Contract test per agent.** A CI job starts the pinned agent under
   sandboxd's real profile and checks: healthy, owner can sign in, a chat
   round-trip through a fixture model, sandboxd's MCP tools listed, a
   scheduled message reaches the inbox. A digest bump ships only when it passes.
5. **Early warning, no auto-change.** A nightly job runs the same test against
   upstream `latest`; a failure opens an issue ("Rakazo vX changed Y") instead
   of breaking users. We adapt the catalog entry, then bump.
6. **Safe upgrades.** Before a version change, sandboxd snapshots the agent's
   data (for Rakazo: `pg_dump` + its data volume); if the new version fails its
   health check, it goes back to the previous digest and the snapshot.
7. **sandboxd updates don't break agents.** Agents load sandboxd's tools at
   start and containers are re-created on a `Revision` change (existing
   behavior); the contract test runs on every sandboxd PR that touches
   `agentcatalog`, the agent runner or the tools.

## What core needs (the parts to approve)

### A. Multi-service agents

`Package` today is one container plus an optional browser sidecar. Add
`Services []Service`: each a pinned image, command, env renderer, user,
volumes under `<data dir>/agents/<id>/`, and health check, all on a private
per-agent network with the same hardened profile (non-root where the image
allows, caps dropped, no-new-privileges, memory limits). One-shot services
(`migrate`) run before the others. Start, stop, pause, remove and memory
checks act on the whole group. The browser sidecar becomes one case of this.

### B. Computers without the Docker socket

Rakazo's supervisor talks to Docker's API. Instead of the real socket,
sandboxd gives it a **guarded Docker API** (a small proxy in core, on a socket
only that container sees) that allows only: create/start/stop/remove/inspect
containers **from the pinned computer image**, with sandboxd forcing the
limits, labels, non-privileged profile, no host mounts, and the agent's own
network; plus the few read calls the supervisor needs. Everything else is
refused. Rakazo's code is unchanged; it just sees a Docker that says no to
anything outside its job.

Phase 1 ships **without computers** (`SANDBOX_PROVIDER=none`): chat, memory,
routines, pages, integrations work. Phase 2 adds B.

### C. Sign-in

Rakazo has its own accounts. On first start sandboxd creates the owner
account through Rakazo's API with a password only sandboxd knows and keeps
the session, like Hermes's dashboard (`UILogin: "password"`). To check
during build: whether Rakazo trusts a sign-in proxy header (then `ProxyAuth`).

### D. Models and tools

- The chosen connection is given as `RAKAZO_LOCAL_MODELS_URL` pointing at
  sandboxd's model proxy with a per-agent capability; the real key never
  enters Rakazo.
- sandboxd's MCP server (message_user, share_file, apps) is added as an MCP
  server in Rakazo's config so routines can reach the person.

### E. Resources

The stack needs about 2 GB without computers, plus 2 GB per running computer.
Defaults: 1 computer, `RAKAZO_COMPUTER_MEMORY=2g`; the Agents page shows it
("needs about 4 GB of memory"), and the existing "not enough memory" check
covers the whole group.

## Plan outline (after approval)

1. Spike: run the pinned stack by hand under sandboxd's profile with
   `SANDBOX_PROVIDER=none`; list every env/config the adapter must set; confirm
   sign-in and the model proxy work. Throwaway.
2. Core A (multi-service packages) + contract test harness, with Hermes and
   OpenClaw's contract tests first (protects existing agents).
3. Rakazo catalog entry, phase 1; contract test; nightly upstream canary.
4. Verify on a real Windows PC from a fresh install, then on a VPS.
5. Core B (guarded Docker API) and phase 2 computers, behind its own approval.

## Open questions for the maintainer

1. OK for phase 1 to ship without Rakazo's computers?
2. OK to add the guarded Docker API (B) to core in phase 2?
3. Is the contract-test + nightly-canary rule to apply to all agents
   (Hermes, OpenClaw, OpenMausBot), not only Rakazo?
