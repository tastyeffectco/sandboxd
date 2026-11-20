# Agents hub: one-click agents in sandboxd desktop

Status: **draft for approval**. Nothing here is built yet.

## Why

Agents like Hermes and OpenClaw keep appearing. Each one is powerful, but it
needs servers, configuration, keys and channels before an ordinary person can
use it. Most people never get past that and lose track of which agents exist.

sandboxd desktop should be the one place where you **pick an agent, click once,
and use it**. It keeps all of the agent's abilities. It lives next to your apps
and can build and change them.

## What the maintainer decided

- Agents run **on this computer by default**. Remote servers come later.
- An agent sees **only the folders the person picks**. It never gets the whole
  disk.
- **Each agent you add is its own instance in the sidebar**, in an "Agents"
  section, like apps under "Projects". The current single Agents page goes
  away.
- **One chat screen works for every agent.** Only a small **connector** changes
  per agent. Adding agent number 3 or 10 must not need a new UI.
- **One gateway for AI models.** sandboxd's model proxy is the gateway: agents
  never see keys. OpenRouter is the suggested default upstream, so one key gives
  DeepSeek and many other models.
- Everything is in plain language for non-technical users, as everywhere in the
  desktop.

## Goals

1. **Catalog → one click → running agent.** If no AI model is connected, adding
   the agent asks for one in the same flow, without sending people to
   Settings.
2. **A sidebar entry per agent instance.** It shows a status dot (getting
   ready, running, paused, needs attention) and opens that agent's screen.
3. **One agentic chat screen.** It streams text, shows each step the agent
   takes (with tool names in plain words), lets you stop it, keeps past
   conversations, and asks for approval when the agent supports approvals.
4. **Channels.** Talk to an agent on Telegram, Slack, Discord or WhatsApp. Each
   is set up from that agent's screen with a guided flow and is owner-only by
   default. Only channels that work without a public web address (no
   webhooks), since desktops sit behind home routers.
5. **Shared folders.** The person picks folders to share with an agent, each
   read-only or read-write. They can be changed later.
6. **Adding a new agent to the catalog is data plus a connector.** No UI
   changes.

## Non-goals (for this design)

- Remote or hosted agents and running agents on the person's servers. That
  comes later and builds on the go-live servers work.
- An agent marketplace with third-party submissions. The catalog is curated.
- Embedding agents' own web UIs. Hermes's is a terminal in a browser and
  OpenClaw's needs device pairing, so neither suits non-technical users.

## Anti-goals (failure even if it "works")

- An agent that silently loses abilities ("lite" versions).
- Keys or tokens visible inside agent containers. Channel tokens are the one
  exception: the agent needs them to talk to the channel, as today with
  Telegram.
- Any step that needs a terminal, a config file or a technical term.

## How it works

### Connector (per agent, in core)

The Go `agentcatalog.Package` grows into a connector that answers:

| Question | Hermes | OpenClaw |
|---|---|---|
| How to start it | as today | as today |
| How to chat (streamed) | `POST /v1/runs` + `GET /v1/runs/{id}/events` (text, `tool.started/completed`, `approval.request`) | gateway WebSocket `chat.send` + `chat`/`agent` events (needs a check that a token-only backend client is allowed from another container) |
| Past conversations | `/api/sessions` | `chat.history`, `sessions.list` |
| Stop | `/v1/runs/{id}/stop` | `chat.abort` |
| Approvals | `/approval` | `exec.approval.resolve` |
| Channels | env vars (`TELEGRAM_*`, `SLACK_*`, `DISCORD_*`, `WHATSAPP_*`) | `channels.*` in openclaw.json; plugins installed once, as PR #153 does for WhatsApp |
| Ready check | chat port answers (done in #176) | same |

Core turns each agent's stream into **one sandboxd event format** under `/v1`:
`text.delta`, `step.started`, `step.finished`, `approval.request`, `done`,
`error`, plus a conversation id. The console only ever speaks this format.
That is what makes "one interface, many agents" true.

### Console

- Sidebar: **Projects** (as today) and **Agents**, with one entry per instance
  and a "+ Add agent" entry that opens the catalog.
- Agent screen: the chat, with a **Channels** tab, a **Folders** tab, and
  **Settings** (AI model, pause, remove).
- Catalog: cards for each agent, saying what it does, how much memory and disk
  it needs, and whether this computer can run it (a warning when memory is
  low; testing showed 2 agents plus a local model leave about 0.8 GB free on
  16 GB).

### Folders

Desktop-only: the person picks folders in the native dialog. The desktop asks
core (new `/v1` field on the agent instance) to mount them into the agent's
container under `/shared/<name>`, read-only or read-write. Changing folders
re-creates the container; the agent's memory is kept.

### Agent #3: OpenMausBot

The maintainer asked for one click to OpenMausBot
([milind-soni/OpenMausBot](https://github.com/milind-soni/OpenMausBot), Apache-2.0).
It differs from Hermes and OpenClaw, and the connector model is what lets it
fit:

- **It is a team platform, not one assistant.** A "harness" server
  (`127.0.0.1:8799`) runs many bots. Each bot drives a coding CLI (Claude,
  Codex, Grok) or an OpenAI-compatible model, and the app talks to it with HTTP
  commands plus one SSE event stream. Its connector maps that stream to
  sandboxd's event format, and a sandboxd agent instance would be one
  OpenMausBot workspace.
- **Models:** its OpenAI-compatible driver takes a base URL and key, so it can
  use sandboxd's model proxy like the others. The CLI-based bots need their own
  sign-ins, which are optional.
- **"Every bot gets a computer"** uses an outside service (boat.dev) or the
  person's own machine. Inside sandboxd's hardened container only the cloud
  option or no computer is possible; this needs a check.
- It ships a Docker image (`ghcr.io/milind-soni/openmausbot`, run as an
  unprivileged user with `HOME=/data`) and a health endpoint (`/api/health`),
  which matches how sandboxd runs agents. The image pin and run profile need
  a test like the ones Hermes and OpenClaw had.

It comes after Hermes and OpenClaw in the order of work: it needs the
connector and event format first.

## Constraints

- The desktop and console are clients of core and use `/v1` only. The
  connector, the event format, channel setup and folder mounts are **core
  changes and need the maintainer's sign-off** (this document is that
  request).
- Agents stay in hardened containers, as today: no capabilities, read-only
  root, non-root user.
- The 16-minute chat window from #176 stays as a backstop. Streaming makes long
  tasks visible rather than silent.

## Order of work

1. Event format + Hermes connector (runs/events) + the new chat screen.
2. Sidebar instances + catalog flow with the model step inline.
3. OpenClaw connector (WebSocket), after checking the backend-client question.
4. Channels: Telegram (reusing #153's flow), then Discord, Slack, WhatsApp.
5. Shared folders.
6. OpenMausBot connector, tested in sandboxd's container profile first.

Each step is tested on a real Windows desktop from a fresh install, including
restarts, before it merges into `release/agents`.

## Left to the builder

- Exact visuals, following the existing design kit.
- How past conversations are listed.
- Event batching and reconnect details.

## Open questions for the maintainer

1. Should one person be able to add **two instances of the same agent** (for
   example "Hermes for work" and "Hermes for home")? The engine API already
   allows it; today's Agents page only offers one per agent.
2. **Slack** needs a Slack app per workspace. Is a pre-filled "create app" link
   (an app manifest) acceptable, or should Slack wait?
3. Do you approve the **core changes** listed under Constraints?
