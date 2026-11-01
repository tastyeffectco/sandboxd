# ADR 0003 — Go live: publish apps to your own server

**Status:** Proposed · **Date:** 2026-11-01

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

Many vibe coders already rent a VPS that runs Docker. Others have no server and
should not need to know what one is.

What sandboxd has today (main, v0.3.26):

- The same engine runs on a laptop (desktop app) and on a Linux server (`install.sh`).
- Each app is a hardened container. Its workspace is a loopback disk image and a
  git repository. Snapshots copy that image; there is no export or import.
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

The desktop gets a list of engines: **This computer** plus any number of
**servers**. Each app shows where it is built and where it is live.

### 2. Connecting a server: three doors, one result

| The user has… | They do | The desktop does |
|---|---|---|
| A VPS and its SSH login (most vibe coders) | Paste the address and password from the provider's email, once | Connects over SSH and installs the engine (`install.sh --pull --no-console --edge=auto`). It installs its own SSH key, then forgets the password. |
| A VPS, and they prefer a terminal | Paste one line shown by the app: `curl … \| bash -s -- --pair 4F7K-92QD` | The server installs, then registers with the pairing code; the desktop picks it up. |
| No server | "Connect DigitalOcean", choose a size | Creates the droplet with the install in its cloud-init, then pairs as above (phase 3). |

**The engine's API is never public over SSH.** The desktop reaches it through
an SSH tunnel to `127.0.0.1:9090` on the server. The pairing door is the
exception: there the API is served over https on the engine's own hostname,
with a long random token and rate limiting.

### 3. Living next to what is already on the server

The installer probes ports 80 and 443 before taking them (`--edge=auto`):

- **Free** → `own`: sandboxd's edge takes 80/443 and handles https itself.
- **Taken** (nginx, Caddy, Traefik, Coolify…) → it never stops or reconfigures
  them. The desktop offers two choices in plain words:
  - `proxy`: sandboxd's edge listens on `127.0.0.1:8088`, and the app shows a
    ready-to-paste snippet for the detected proxy. https stays with the
    existing proxy.
  - `tunnel` (phase 3): a Cloudflare Tunnel container. No ports, https
    included, works behind any setup.

Existing containers, networks and volumes are never touched. That is already
true of the edge and the reconciler, and the installer must keep it true.

### 4. What gets published: a release bundle, not a disk image

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

### 5. Production mode

An app published to a server runs in **production mode**:

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

### 6. Releases, switch-over and rollback

On the server, `POST /v1/apps/{id}/releases` (bundle upload) then:

1. builds the release in a new container next to the live one;
2. waits for its `health_path` to answer 200;
3. switches the app's route to it;
4. keeps the previous release stopped.

A failed step leaves the live release untouched. **Go back** activates a
previous release (`POST /v1/apps/{id}/releases/{rid}/activate`). The last three
releases are kept.

### 7. Addresses and https

- **Straight away:** `https://<app>.<server-ip-dashed>.sslip.io`, with no domain
  needed. Certificates come from Let's Encrypt (Traefik ACME, HTTP-01) in
  `own` mode.
- **Your domain:** `PUT /v1/apps/{id}/domains`. The desktop shows the one DNS
  record to add and checks that it points at the server before asking for a
  certificate. Plain-language errors cover DNS that has not propagated yet,
  wrong records, and Cloudflare's proxy being on.
- **Rate limiting** per client IP, on by default (Traefik middleware), with a
  generous limit that apps can raise.

### 8. Secrets

Secrets keep using the app config store (encrypted at rest, per engine key).
Publishing asks once: "Use the same values in production?". Production values
are set through the server's `/v1` API and never travel in a bundle.

### 9. Backups

Every night the server snapshots each production app's `data:` volume
(`tar.zst`, 7 days kept). **The desktop downloads the latest one whenever it
connects**, so there is an off-server copy without another account. An
S3-compatible target (Backblaze, DigitalOcean Spaces, R2) is optional
(phase 2). Restoring a backup is a button.

### 10. Knowing when something is wrong

The server engine checks each production app's health every minute:

- after 3 failures it restarts the app;
- if the app still fails, it records an event, as it does for low disk
  (85%), a failed backup or a failed certificate renewal.

`GET /v1/events` streams these events. The desktop shows them as notifications,
and when the owner has an agent linked to WhatsApp or Telegram (#153), the
desktop asks it to tell them, in plain words, with what to click.

### 11. Updating the server

The server engine updates with the same signed-image upgrade, backup and
rollback steps the desktop already uses (#148, #152). The desktop offers
"Update your server" when it pins a newer engine, and optionally does it at night.

## Core changes (each to be agreed before code)

| # | Change | Phase |
|---|---|---|
| C1 | `sandbox.yaml`: optional `web.start` and `data:` (additive, validated) | 1 |
| C2 | Release bundles: `GET /v1/apps/{id}/bundle`; `POST/GET /v1/apps/{id}/releases`; `POST …/releases/{rid}/activate` | 1 |
| C3 | Production mode for an app: always on, start command, data volume, no agent tasks, build at publish | 1 |
| C4 | Edge: Let's Encrypt resolver, `websecure` entrypoint, app hostnames (`sslip.io`), rate-limit middleware | 1 |
| C5 | Installer: `--edge=auto\|own\|proxy\|tunnel`, port probing, `--pair <code>` | 1 (`auto`/`own`/`proxy`), 2 (`--pair`), 3 (`tunnel`) |
| C6 | Custom domains: `PUT /v1/apps/{id}/domains` with a DNS check | 2 |
| C7 | Nightly data backups: `GET /v1/apps/{id}/backups`, restore | 2 |
| C8 | Health monitor and `GET /v1/events` | 2 |

Everything else is in the desktop (and the console it bundles), on `/v1` only:
the engine list, the SSH install and tunnel, publishing, the domain wizard,
backup downloads, notifications, and DigitalOcean.

## Phases

1. **Publish to your server.** Connect a server over SSH; Publish; an https
   `sslip.io` address; releases with Go back; production secrets; `own` and
   `proxy` edge modes.
2. **Keep it alive.** Your domain, nightly backups (pulled to the desktop),
   health checks and alerts (desktop, WhatsApp/Telegram), the pairing line,
   server updates.
3. **No server needed.** Create a DigitalOcean server; Cloudflare Tunnel;
   export a standard `Dockerfile`/`compose.yaml` (no lock-in); Pulse, a view of
   visits and errors across apps.

## Alternatives considered

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

- One server can host many small apps cheaply, as the target users already do.
  sandboxd takes on the sysadmin part (restarts, https, backups, alerts, updates).
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
   on the server, so they stay on when the laptop sleeps (#153)?
