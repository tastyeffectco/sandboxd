# ADR 0003 — Go live: environments on your own servers

**Status:** Proposed · **Date:** 2026-11-02

## Context

The desktop makes building an app easy. What happens next is where vibe-coded
apps die. Five problems come up again and again (from a widely read
r/vibecoding thread: "30+ apps in 2 years, 6 still in production"):

- **Deployment sprawl.** Apps end up on many providers, with many accounts and bills.
- **You become the sysadmin.** A crash at 3 a.m., a full disk, updates, backups.
- **Going public is a checklist few people finish:** https, backups, rate
  limiting, and seeing errors before users report them.
- **Lost context.** An app reopened six months later is a stranger.
- **Maintenance outlives the app.** "An app with no users but real maintenance
  is not a product, it's a pet that bites you."

Many vibe coders already rent a VPS that runs Docker, and often nginx or Caddy
in front of other sites. Others have no server and should not need to know what
one is. Servers differ a lot; a single rigid setup will not fit them.

What sandboxd has today (main, v0.3.26):

- The same engine runs on a laptop (desktop app) and on a Linux server (`install.sh`).
- Each app is a hardened container. Its workspace is a host folder holding a
  git repository. Snapshots copy that folder; there is no export or import.
- `sandbox.yaml` declares one web process (`web.command`, usually a dev
  server such as `pnpm dev`), a build check and workers. There is no production
  start command, and no notion of data that must survive a new version.
- `idle_policy = 'always_on'` exists: such an app is never put to sleep.
- The edge (Traefik) listens on port 80 only. TLS needs a hand-supplied certificate.
  It already routes only containers labelled `sandboxd.managed=true`, so it ignores
  other stacks on the same Docker host.
- sandboxd only manages its own containers (`s-…` apps, `a-…` agents) on its own network.
- The desktop is a client of one engine at a time: the local one, or a remote
  engine by URL (https required).

## Decision

### 1. One engine everywhere; the desktop drives both

A server is just another sandboxd engine. Publishing moves an app from the engine
on your computer to the engine on your server. **The desktop is the only thing
that talks to both, over `/v1`.** Engines never talk to each other, hold no
credentials for one another, and core gains no notion of "remote".

### 2. Environments

In the desktop, every app has **environments**:

```
[ This computer · editing ]   [ Test · v12 ]   [ Live · v11 ]   [ + Add environment ]
                                                 myshop.com · healthy · backed up 3h ago
```

- **This computer** is always there. It is where you build with the agent.
- **Live** (and an optional **Test**) is the app on one of your servers. Adding
  one means picking a server from a list; servers are added once, in
  Settings → Servers.
- **Publish** is one button ("Publish to Live", or "Test, then move to Live").
  If anything fails, the live version keeps running.
- Each environment has its own address, domain, secrets, data, backups and
  version history. Live data never flows back to your computer unless you click
  "Copy live data to this computer".

An environment is a desktop concept: it is an app on an engine. Core only
stores free-form **labels** on the app (`env=live`, `source=<local app id>`), so
a reinstalled desktop or a second laptop rebuilds the environment list from the
servers themselves.

### 3. Connecting a server: three doors, one result

| The user has… | They do | The desktop does |
|---|---|---|
| A VPS and its SSH login (most vibe coders) | Paste the address and password from the provider's email, once | Connects over SSH and installs the engine. It installs its own SSH key, then forgets the password. |
| A VPS, and they prefer a terminal | Paste one line shown by the app: `curl … \| bash -s -- --pair 4F7K-92QD` | The server installs, then registers with the pairing code; the desktop picks it up. |
| No server | "Connect DigitalOcean", choose a size | Creates the droplet with the install in its cloud-init, then pairs as above (phase 3). |

**SSH, securely:**

- One key per device, kept in the OS keychain (Windows Credential Manager,
  macOS Keychain). Passwords are never stored.
- The server's host key is pinned on first connection. If it ever changes, the
  app stops and says so in plain words.
- Admin rights are used only to install. Afterwards the desktop connects as a
  limited `sandboxd` user. That user may do only what the chosen front door
  needs (for example `nginx -t` and reloading nginx), through a narrow sudo rule.
- The engine's API listens on `127.0.0.1:9090` only and is reached through the
  SSH tunnel. The pairing door is the exception: there the API is served over
  https on the engine's own hostname, with a long random token and rate limiting.
- Removing a server deletes the device's key from it.

### 4. Exposing apps: a router and a front door

Exposing an app has two parts:

- **The router** is sandboxd's own Traefik. It maps hostnames to app containers
  from labels, as today. It never has to be public: on a server it listens on
  `127.0.0.1:8088` unless it is itself the front door.
- **The front door** owns ports 80/443 and https, and forwards to the router.
  It is **pluggable**, chosen per server, and can be changed later:

| Front door | When | What sandboxd does |
|---|---|---|
| **Caddy, managed by sandboxd** (default on a clean server) | 80/443 are free | Runs Caddy as a container and configures it through its admin API. https is automatic for `sslip.io` addresses and custom domains (on-demand TLS, allowed only for hostnames the engine knows). |
| **Your nginx** | nginx is installed | Writes one file per app, `/etc/nginx/sites-available/sandboxd-<app>.conf`, runs `nginx -t`, reloads, then gets the certificate with certbot. |
| **Your Caddy** | Caddy is installed | Adds `import /etc/caddy/sandboxd/*.caddy` to the Caddyfile once (shown and approved first), then one file per app, `caddy validate`, reload. |
| **Your Traefik / Coolify** | detected | A file-provider dynamic config pointing at the router. |
| **Cloudflare Tunnel** (phase 3) | behind NAT, no open ports, or by choice | A `cloudflared` container. No ports needed, https included. |
| **Manual** | the user wants full control | Shows the snippet to paste and checks the result. |

Rules that make this safe:

- sandboxd edits only files it created and marked `# managed by sandboxd`.
  Every other change (such as the one `import` line) is shown as a plan and
  applied only once the user approves it.
- Every change is applied the same way: back up, write, test (`nginx -t`,
  `caddy validate`), reload, check the address answers over https. If any step
  fails, the backup is restored automatically.
- Existing containers, networks, volumes and sites are never touched.

Addresses: every app gets `https://<app>.<server-ip-dashed>.sslip.io` straight
away, with no domain needed. **Your domain** is a wizard: it shows the one DNS
record to add and checks it points at the server before asking for a certificate.
Errors are in plain words: DNS not updated yet, a wrong record, or Cloudflare's
proxy being on.

Rate limiting per client IP is on by default in the front doors sandboxd manages,
with a generous limit apps can raise.

### 5. The agent handles the unusual cases

Templates cover the common setups above. Real servers are messier: custom nginx
includes, an Apache vhost, a cPanel box, a port already used by something else,
a certificate that will not renew. For those, **the server agent** helps:

1. It reads the server's facts (read-only): running services, proxy configs,
   open ports, DNS answers, recent errors.
2. It explains what it found in plain words and proposes a **plan**: the files to
   write and the commands to test and reload.
3. The desktop shows the plan ("I will add myshop.com to your nginx, which
   already serves two other sites. Nothing else changes."). Advanced users can
   open and edit the exact files.
4. On approval, the desktop applies it with the same back up, test, reload,
   check, roll back steps.

The agent never holds the SSH key or runs commands itself. It only proposes
plans. The desktop checks each plan against an allowlist (config files under
the proxy's directories; test, reload and certbot commands) before running it.
Anything outside the list needs the admin password for that one change.

The same agent answers "my site shows 502" or "https stopped working": it
diagnoses with the same read-only facts and proposes a fix the same way.

### 6. What gets published: a release bundle, not a disk image

Publishing sends the app's **code**, not its workspace disk:

- **Release bundle** = the tracked files at the current commit (`git archive`),
  as `tar.zst`, with `sandbox.yaml`. The bundle excludes:
  - installed dependencies (`node_modules`, `.venv`), which the server installs itself;
  - `BRAIN.md` and agent files;
  - local data and secrets.
- The bundle is a few MB instead of a multi-GB image. It is the same code you
  tested, rebuilt the same way on the same base image. Its version is the commit.

Uncommitted changes are committed first ("Publish saves your latest changes"),
because coding tasks already work through git.

### 7. Production mode

An app in a Test or Live environment runs in **production mode**:

- **Always on** (`idle_policy = always_on`). Restarted when it crashes.
- **No coding agent** inside, and no agent credentials on the server (phase 1).
- **Production start.** `sandbox.yaml` gains an optional `web.start`, the
  production command (for example `next start`, `uvicorn app:app` without
  `--reload`, or a static file server for Vite). Each preset gets a sensible
  default. `build.command` runs at publish; **a failing build stops the publish
  and the live version keeps running.**
- **Data that survives versions.** `sandbox.yaml` gains an optional `data:` list
  of paths in the app (default `data/`). They live on a persistent volume that
  is never in a bundle, never replaced by a release, and is what backups copy.
  SQLite files and uploads go there. Managed databases come later, as bricks.

Both manifest fields are additive (schema v1 stays valid). On the desktop,
`web.start` also lets a user "try the production version" locally.

### 8. Releases, switch-over and rollback

On the server, `POST /v1/apps/{id}/releases` (bundle upload) then:

1. builds the release in a new container next to the live one;
2. waits for its `health_path` to answer 200;
3. switches the app's route to it;
4. keeps the previous release stopped.

A failed step leaves the live release untouched. **Go back** activates a
previous release (`POST /v1/apps/{id}/releases/{rid}/activate`). The last three
releases are kept. "Move Test to Live" publishes the exact bundle Test runs.

### 9. Secrets

Secrets keep using the app config store (encrypted at rest, per engine key).
Each environment has its own values. Publishing asks once: "Use the same values
in Live?". Values are set through the server's `/v1` API and never travel in a
bundle.

### 10. Backups

Every night the server snapshots each production app's `data:` volume
(`tar.zst`, 7 days kept). **The desktop downloads the latest one whenever it
connects**, so there is an off-server copy without another account. An
S3-compatible target (Backblaze, DigitalOcean Spaces, R2) is optional
(phase 2). Restoring a backup is a button.

### 11. Knowing when something is wrong

The server engine checks each production app's health every minute:

- after 3 failures it restarts the app;
- if the app still fails, it records an event, as it does for low disk
  (85%), a failed backup or a failed certificate renewal.

`GET /v1/events` streams these events. The desktop shows them as notifications,
and when the owner has an agent linked to WhatsApp or Telegram (#153), the
desktop asks it to tell them, in plain words, with what to click. The server
agent (section 5) can then diagnose.

### 12. Updating the server

The server engine updates with the same signed-image upgrade, backup and
rollback steps the desktop already uses (#148, #152). The desktop offers
"Update your server" when it pins a newer engine, and optionally does it at night.

## Core changes (each to be agreed before code)

| # | Change | Phase |
|---|---|---|
| C1 | `sandbox.yaml`: optional `web.start`, `build.release` (the production build, separate from the post-task check that some presets skip) and `data:` (additive, validated) | 1 |
| C2 | Release bundles: `GET /v1/apps/{id}/bundle`; `POST/GET /v1/apps/{id}/releases`; `POST …/releases/{rid}/activate` | 1 |
| C3 | Production mode for an app: always on, start command, data volume, no agent tasks, build at publish | 1 |
| C4 | Router: listen on `127.0.0.1:8088` when it is not the front door; app hostnames (`sslip.io`, custom) | 1 |
| C5 | Managed Caddy front door (container, admin API, on-demand TLS gated by an engine "is this hostname mine?" endpoint, rate limiting) | 1 |
| C6 | Installer: `--front=auto\|caddy\|nginx\|existing\|tunnel\|manual`, port and proxy detection, the limited `sandboxd` user, `--pair <code>` | 1 (`--pair` in 2, `tunnel` in 3) |
| C7 | App labels: free-form key/values on an app (`env`, `source`), set at create, listed and filtered on `GET /v1/apps` | 1 |
| C8 | Custom domains: `PUT /v1/apps/{id}/domains` with a DNS check | 2 |
| C9 | Nightly data backups: `GET /v1/apps/{id}/backups`, restore | 2 |
| C10 | Health monitor and `GET /v1/events` | 2 |

Everything else is in the desktop (and the console it bundles): the server list,
SSH install and tunnel, environments, publishing, the host-proxy adapters (nginx,
your Caddy, Traefik) and their apply/roll-back steps, the server agent's plans,
the domain wizard, backup downloads, notifications, and DigitalOcean.

## Phases

1. **Publish to your server.** Add a server over SSH; a Live environment;
   Publish; an https `sslip.io` address; releases with Go back; production
   secrets; front doors: managed Caddy, your nginx, your Caddy, manual.
2. **Keep it alive.** Test environments and "Move Test to Live"; your domain;
   nightly backups (pulled to the desktop); health checks and alerts (desktop,
   WhatsApp/Telegram); the server agent for unusual setups and diagnosis; the
   pairing line; server updates; Traefik/Coolify front doors.
3. **No server needed.** Create a DigitalOcean server; Cloudflare Tunnel;
   export a standard `Dockerfile`/`compose.yaml` (no lock-in); Pulse, a view of
   visits and errors across apps.

## Alternatives considered

- **One fixed edge (Traefik with Let's Encrypt on 80/443).** Simplest to build,
  but fails on the many VPSs that already run nginx or Caddy, and gives users no
  choice. Kept only as the internal router.
- **Agent-only exposure (no templates).** Flexible, but slow and less
  predictable for the common case. Templates handle the common setups; the agent
  handles the rest, always through an approved plan.
- **Letting the agent run commands over SSH.** Rejected: a model with root on a
  production server is one bad answer from an outage. The agent proposes; the
  desktop checks, applies and rolls back.
- **Copy the workspace disk image to the server.** Simplest transfer, but large
  (GBs of dependencies). It ships dev files, agent notes and possibly secrets,
  and it is not a reproducible build. Rejected.
- **Deploy to Vercel, Netlify, Cloudflare or Railway.** Many providers and
  accounts: the sprawl the users want to escape. Workers and SQLite apps don't
  fit everywhere. Kept as a later "export" target, not the default.
- **Hand off to Coolify, Dokploy or CapRover.** Good tools, but they start where
  the app is already finished. You would lose context (`BRAIN.md`, the coding
  agents) and the guarantee of the same engine in development and production.
  Phase 3's export lets their users build with sandboxd anyway.
- **Engines that sync with each other.** Engine-to-engine trust, credentials and
  a protocol in core. The desktop carrying bundles over `/v1` is simpler and
  keeps core abstract.
- **Kubernetes.** Wrong size for one person's VPS.

## Consequences

- One server can host many small apps cheaply, next to whatever already runs on
  it. sandboxd takes on the sysadmin part (restarts, https, backups, alerts,
  updates).
- The desktop gains SSH and host-proxy logic outside `/v1`. It is limited to
  install, the tunnel and the front-door adapters, each with backup and rollback.
- `sandbox.yaml` becomes the contract for production too (`web.start`,
  `data:`). The coding agent must keep it right. The post-task build check and
  the run-settings guardrails extend to `web.start`.
- **Hostnames depend on sslip.io** (a public wildcard DNS service). If it is
  unavailable, custom domains still work. A sandboxd-run wildcard domain is an
  option for later.
- **Production data lives only on the server.** Desktop copies are backups,
  never live data. Pulling production data back into development is an
  explicit action.

## Open questions

1. **Login for published apps.** An "auth brick" shared by all apps, or keep
   auth inside each app? (ADR on bricks.)
2. **Databases beyond SQLite.** A shared Postgres per server as a brick, or one
   per app?
3. **Several apps per domain** (paths), or only one app per hostname in v1?
4. **Agents on the server.** Should operator agents (Hermes, OpenClaw) also run
   on the server, so they stay on when the laptop sleeps (#153)? The server
   agent of section 5 could then be one of them.
5. **Which model runs the server agent?** The free OpenCode tier by default, or
   the user's own connection when they have one.
