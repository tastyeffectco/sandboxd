# ADR 0002 — Brains, app actions, and operator agents

**Status:** Proposed · **Date:** 2026-10-30

## Context

sandboxd builds apps with coding agents. Two things are missing to make it feel
intelligent and to let agents do more than write code:

1. **Memory is thin.** Each app has a `BRAIN.md` in its workspace that the coding
   agent is *asked* to maintain. Core never writes or reads it, it disappears
   with the workspace, and nothing is shared across apps or about the user. The
   "Project memory" page is a graph computed in the browser.
2. **Agents only build.** The only agents are coding agents inside one app's
   workspace, one task at a time. There is no way to run an agent that *operates*
   apps (answers customers, processes orders, publishes, monitors), and no
   contract that lets an agent use an app without editing its code.

The first operator agents we want to host are open-source, self-hosted
assistants with their own long-running gateway:

| | [OpenClaw](https://github.com/openclaw/openclaw) | [Hermes Agent](https://github.com/NousResearch/hermes-agent) | [OpenMausBot](https://github.com/milind-soni/OpenMausBot) |
|---|---|---|---|
| Runtime | Node 24+ | Python (uv) + Node | Node 24+ |
| Long-running process | `openclaw gateway` (WS/HTTP, 18789) | `hermes gateway` (messaging; API server 8642) | harness `:8799`, webhooks `:8800`, UI `:5199` |
| Talks to people via | WhatsApp, Telegram, Slack, Discord, Signal, Teams… | Telegram, Discord, Slack, WhatsApp, Signal, email | its own chat app, phone web app |
| Runs tools | on its host, or its own Docker sandbox per agent | `terminal.backend`: local, Docker, SSH, Modal… | local CLIs (claude, codex, grok, ACP), "computer use" on a Linux desktop or VM |
| Memory | workspace files + memory | `MEMORY.md`, `USER.md`, skills it writes itself | transcripts in `~/.openmausbot` |
| Schedules | built-in cron/heartbeat | built-in cron | — |
| MCP | via plugins/skills | MCP client | MCP server; engines are MCP clients |
| State dir | `~/.openclaw` | `~/.hermes` | `~/.openmausbot` |

All three want: a Linux userland, a persistent home, one or more ports, outbound
network, sometimes inbound webhooks, sometimes a browser or a whole desktop, and
model credentials. Two of them can start their own Docker sandboxes.

## Decision

### 1. Core stays abstract: one workload primitive, agents are packages (data)

Core does not learn about OpenClaw, Hermes or any other agent. It runs
**workloads** — today's sandbox — of two kinds:

- `app`: built by coding agents, previewed, sleeps when idle (today).
- `agent`: an operator agent, long-running, controlled through its package.

An **agent package** is a validated data file, the same pattern as presets and
recipes (`internal/recipes/data/*.yaml`):

```yaml
# agents/hermes/agent.yaml (illustrative)
id: hermes
label: Hermes
description: A personal assistant that learns as it works and talks to you on Telegram.
image: base            # or a pinned image digest when the base image is not enough
install: "[ -x ~/.local/bin/hermes ] || curl -fsSL https://hermes-agent.nousresearch.com/install.sh | bash"
command: "hermes gateway run"          # foreground; runtimed supervises it
state: [".hermes"]                     # persistent, kept across restarts, snapshotted
ports:
  - { name: api, port: 8642, protocol: http, expose: owner }
capabilities: [browser]                # browser | desktop | docker | gpu
lifecycle: always_on                   # always_on | wake_on_request | scheduled
egress: [api.telegram.org, "*.nousresearch.com"]
secrets:
  - { name: TELEGRAM_BOT_TOKEN, ask: "Your Telegram bot token (from @BotFather)" }
models: { openai_compatible_base_url: env:OPENAI_BASE_URL }   # point it at the credential proxy
mcp: { config_file: .hermes/config.yaml, format: hermes }      # where sandboxd's MCP endpoint is written
setup: [ "hermes config set terminal.backend local" ]          # the workload is the sandbox
```

Core only gains **generic** capabilities that every package uses: kinds,
state volumes, named ports with exposure levels, lifecycle policies,
capabilities, an egress allowlist, secret injection, schedules and triggers,
the brain store, and the gateway. Removing every package leaves core unchanged.

### 2. The workload *is* the agent's sandbox — agents never get Docker

Each agent runs entirely inside its own sandboxd workload and is configured to
run tools locally there (OpenClaw sandbox mode off, Hermes
`terminal.backend local`, OpenMausBot local engines). The sandbox boundary
(read-only root, dropped capabilities, user namespace, optional gVisor, the
credential proxy) applies to the agent and everything it runs.

The host Docker socket is never mounted into a workload: it is host-root.

### 3. "Agents need Linux, not just Docker": runtime classes by capability

What an agent actually needs from "Linux" and how each need is met:

| Need | How |
|---|---|
| userland, processes, persistent home | a container (today) |
| systemd / daemons | not needed: run the gateway in the foreground under runtimed (`openclaw gateway`, `hermes gateway run`, `openmausbot serve`); never `--install-daemon` |
| a browser | `browser` capability: Chromium in the image, `/dev/shm` sized for it |
| a desktop to click on (computer use) | `desktop` capability: virtual X server + window manager + VNC, shown to the owner through noVNC on an `owner` port |
| its own containers | `docker` capability, only in the `vm` runtime class (rootless nested containers inside a microVM); never by mounting the host socket |
| stronger isolation | runtime class `gvisor` (exists) or `vm` (Firecracker/Kata on Linux servers) |
| the user's own Windows/macOS desktop | **not core.** An opt-in "host bridge" in the desktop app, with an OS permission prompt and per-action approvals. The engine's WSL2 VM cannot see the Windows desktop anyway |

Runtime classes: `container` (default), `gvisor`, `vm`. A package requests
capabilities; core picks the weakest class that satisfies them and the host
supports, and says so in plain words when it cannot ("This helper needs a
virtual machine, which this computer does not support yet").

On the Windows desktop the engine already runs in one dedicated WSL2 VM.
Workloads (apps and agents) are containers inside it. A per-agent WSL distro is
the `vm` class on Windows, used only when a package needs `docker`; it is
heavy, so the desktop app shows the memory cost before installing.

### 4. Ports and networks

**Named ports with an exposure level**, declared by the package and fixed when
the workload is created (preview routes are set at creation today):

- `none`: reachable only inside the workload.
- `owner`: through sandboxd's authenticated proxy, like a private preview
  (control UIs: OpenClaw dashboard, Hermes API, OpenMausBot app, noVNC).
- `public`: webhook receivers only, limited to declared `paths`, with a
  signature or bearer secret required, rate limited. Never a whole UI.

**Inbound on a laptop.** A laptop is behind NAT and sleeps. Packages prefer
outbound-only channel modes: Telegram long polling, Slack Socket Mode, the
Discord gateway, WhatsApp Web sessions. A `public` port on the desktop needs the
remote-access tunnel turned on, and the UI says so before it is enabled.

**East-west.** Workloads cannot reach each other. Each agent workload gets its
own network on which only the sandboxd gateway is reachable. An agent reaches an
app only through the gateway (section 6), with scopes and an audit trail.

**Egress.** Packages declare the domains they need; the permission screen shows
them. Enforcement uses the existing nftables egress subsystem once it supports
per-workload domain allowlists (today it is compiled off in the compose build;
docs/isolation.md says so). Until then the declared list is shown, not enforced,
and the UI does not claim otherwise.

**Model credentials.** Agents call models through the credential proxy wherever
the agent accepts an OpenAI- or Anthropic-compatible base URL, so keys never
enter the workload (same as coding agents today). Subscription logins that may
not be relayed (Claude.ai) are signed in inside the workload, as Claude Code is
today. Channel credentials the agent itself must hold (a Telegram bot token, a
WhatsApp session) live in its state volume; snapshots of an agent are marked as
containing credentials.

### 5. Lifecycle, schedules and triggers

- `always_on`: the reaper's idle stop skips it; the memory-pressure reaper may
  still stop it, and the owner is told. On a laptop it pauses while the computer
  sleeps; the UI says "Your helpers pause while this computer sleeps".
- `wake_on_request`: stopped when idle, woken by its `public` port (the wake path
  previews use today) or by a schedule.
- Schedules and triggers are core: cron expressions and app events
  (`order.created` from an app's declared events) wake the workload and deliver
  the trigger to a declared path. Agents' own cron keeps working when they are
  always on; core schedules exist so an agent can sleep between runs.
- Resource limits per workload. The desktop shows a memory budget ("2 helpers
  running, 1.4 GB"), because 16 GB laptops are real (see the setup memory fix).

### 6. The contract between agents and apps: MCP through a sandboxd gateway

Apps declare what they can do; the gateway turns that into MCP tools; agents
get scoped access.

**Apps declare actions, events and data** in `sandbox.yaml` (the builder agent
writes them while building; core validates them):

```yaml
actions:
  - name: list_orders
    description: Orders, newest first
    method: GET
    path: /api/orders
    risk: read                     # read | write | destructive
  - name: refund_order
    description: Refund an order in full
    method: POST
    path: /api/orders/{id}/refund
    input: { type: object, properties: { id: { type: string } }, required: [id] }
    risk: destructive
events:
  - { name: order.created, description: A customer placed an order }
```

**The gateway** is a control-plane component that serves one MCP endpoint per
agent workload:

- tools from every app the agent may use (`shop.list_orders`, `shop.refund_order`),
  called over the internal network against the app's port;
- platform tools: app status, logs, snapshot, submit a build task to the app's
  coding agent (so an operator agent can *ask* for a code change), read and
  write brains, notify the owner;
- MCP is the contract because all three agents speak it (Hermes as a client,
  OpenMausBot's engines, OpenClaw through plugins) and so do the coding agents.

**Permissions** extend the existing API-key scopes (`internal/auth/scopes.go`,
today only `upgrade`) from fixed routes to resources:
`app:<id>:read`, `app:<id>:write`, `app:<id>:destructive`, `app:<id>:tasks`,
`brain:<id>:read|write`. The owner grants them on a permission screen when
adding a helper ("Hermes can read your Shop's orders and ask before refunds").

**Approvals.** `destructive` calls are held by the gateway and shown to the
owner (console and desktop notification) with the exact action and input; the
agent receives "waiting for approval", then the result.

**Audit.** Every gateway call is an event in the existing activity timeline of
both the agent and the app.

**Prompt injection.** Data read from apps is untrusted input to the agent. The
risk levels, approvals, the absence of code-writing tools for operator agents
(they ask the builder through a task instead), and the audit trail are the
mitigations; we do not claim more.

### 7. Brains: owned by core, assembled by core, learned in the open

**App brain** (and, with the same contract, an agent's brain):

- **Facts core maintains:** stack and run settings, routes, config key names
  (never values), dependencies, declared actions, last build and preview health,
  task summaries. Always true because core derives them.
- **Notes:** decisions, gotchas, dead ends — the `BRAIN.md` content.
- **Provenance per entry:** agent, user or core; the task that wrote it; when it
  was last verified.
- **Stored in the control-plane database, versioned per task.** runtimed writes
  `BRAIN.md` into the workspace before a task and returns it after; the control
  plane stores the new version. It survives deleting the workspace, can be read
  while the app is stopped, and restores with Restore.
- API: `GET /v1/apps/{id}/brain`, `PUT` by section, `GET .../brain/versions`.

**About you** (central, per user now; an organisation layer later):
preferences and recurring choices, editable in plain words on one page. Lessons
are **promoted** from app brains only with the owner's approval — suggested when
the same lesson appears in two or more apps or the user corrects the agent the
same way twice.

**Context assembly** moves to core: the control plane sends the task's context
(platform rules, app facts and notes, relevant About-you entries, recent task
summaries) within a size budget, and runtimed renders it for whichever agent
runs. Agents stop being told to "go read files".

**Learning loop:** after a task, proposed brain changes come back as a diff;
core checks size and secrets, records provenance, and the chat shows
"Learned: …" with Undo. Notes with a check command are re-verified by runtimed
and marked stale when they fail.

## Phases

Each phase is shippable on its own and keeps today's behaviour when unused.

1. **App brain in core.** Brain store and versions; runtimed brain sync before
   and after tasks; core-maintained facts; context assembly; "Learned" notices
   and Undo; Project memory page reads from core (works for stopped apps).
2. **About you.** User memory store and page; injection into context;
   promotion suggestions.
3. **App actions and the gateway.** `actions`/`events` in `sandbox.yaml` with
   validation; the builder agent taught to declare them; resource scopes;
   the MCP gateway; approvals; audit events.
4. **Operator agents.** Workload kind `agent`; package format and validation;
   state volumes; named ports and exposure; lifecycle policies; schedules and
   triggers; first packages: Hermes (simplest: Python, outbound channels, MCP
   client), then OpenClaw, then OpenMausBot (needs the `desktop` capability).
   Runtime class `vm` and the desktop host bridge come after these three run
   well on `container`.

## Consequences

- Core grows generic primitives (kinds, ports, lifecycle, gateway, brain store)
  but no agent-specific code; packages are data and are tested like recipes.
- The gateway becomes the only path between agents and apps. That is the point
  (scopes, approvals, audit), and it is also a new critical component: it needs
  its own tests, rate limits and metrics.
- Isolation claims stay honest: a container is the default boundary, gVisor and
  microVMs are opt-in classes, and egress allowlists are shown as declared until
  they are enforced.
- Agent workloads hold credentials the agent itself needs (channel tokens,
  sessions). Snapshots and exports of agents are marked accordingly.
- Laptops sleep and have little memory; always-on helpers say so in the UI and
  have a memory budget.

## Alternatives considered

- **Bake each agent into core** (an "OpenClaw mode"): fastest demo, but every
  new agent becomes a core change and a security review of foreign code paths.
  Rejected for packages-as-data.
- **Give agents the host Docker socket** so their own sandboxes work: host-root
  for untrusted code. Rejected; the workload is the sandbox, nested containers
  only inside a `vm` class.
- **One shared VM for all agents on the desktop:** simpler, but one agent's
  `docker` or desktop needs would weaken every other agent. Rejected; per-
  workload classes.
- **Keep the brain as a workspace file only:** no core change, but lost with the
  workspace, unreadable while stopped, unverifiable. Rejected.
- **sandboxd's own tool API instead of MCP:** more control, but every agent would
  need an adapter. Rejected; MCP outside, sandboxd's API behind the gateway.
