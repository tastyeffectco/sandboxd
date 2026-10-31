# Agents

Agents are assistants sandboxd runs for you, next to your apps. The first is
[Hermes](https://github.com/NousResearch/hermes-agent) by Nous Research: it
remembers what you tell it, learns skills, runs scheduled jobs, and can create
and change apps on your sandboxd. [OpenClaw](https://github.com/openclaw/openclaw)
works the same way. You talk to them on WhatsApp or Telegram, or try them out
in the console (**Agents**).

They are different from the **coding agents** (OpenCode, Claude Code, Codex,
Pi) that build an app inside its workspace: an agent here works *across* your
apps and asks those coding agents to build or change them.

## Adding Hermes

1. Add an **OpenAI-compatible model connection** in Settings → Model
   connections (for example an OpenRouter key). Hermes cannot use OpenCode's
   free model: that model is only served to OpenCode's own app. The apps
   Hermes creates are still built by the default coding agent, which can use
   the free model.
2. Open **Agents**, choose the model and click **Add Hermes**. The first setup
   downloads Hermes's image (about 3 GB).
3. Link WhatsApp or Telegram on the agent's card (see below).

## Talking to your agent: WhatsApp and Telegram

Both connect outward from your computer, so they work behind a home router
with no address or port to open. The agent answers only you.

- **WhatsApp**: click **Link WhatsApp** and scan the QR code with your phone
  (WhatsApp → Settings → Linked devices → Link a device), as for WhatsApp Web.
  The agent is linked to *your own* WhatsApp account and answers you in your
  "Message yourself" chat; it ignores everyone else. sandboxd runs the
  agent's own WhatsApp support inside its container (Hermes's bridge, or
  OpenClaw's WhatsApp plugin, installed into the agent's state the first
  time) and reads the linked number from the saved login. The login stays in
  the agent's state directory; **Unlink** deletes it.
- **Telegram**: create a bot with @BotFather, paste its token, then send the
  bot any message. That first message tells sandboxd your Telegram user ID, so
  the agent answers only you; you never have to look the ID up. The token is
  checked with Telegram before anything is saved.

The agent pauses with the computer: Telegram keeps messages for a day and
WhatsApp delivers them when the agent is back.

## What Hermes can do on sandboxd

Hermes gets four tools from sandboxd, over MCP:

| Tool | What it does |
|---|---|
| `list_apps` | Your apps, their status and preview links |
| `create_app` | A new app from an idea; its coding agent builds it. At most 10 per agent per day |
| `ask_app` | Asks an app's coding agent for a change |
| `app_status` | Status, preview link and the latest build's outcome |

It cannot delete or stop apps, read their files, or use any other sandboxd API.
Every app it creates or changes shows in that app's activity log ("Hermes
created this app") and is tagged `created-by:Hermes`.

## How it runs

- **Its own container**, from the image Nous Research publishes, pinned by
  digest. sandboxd starts it as a non-root user with all Linux capabilities
  dropped, a read-only root filesystem and no privilege escalation, with a
  2 GB memory limit and 1 GB of shared memory for its browser. Commands Hermes
  runs stay inside that container (no Docker access, no host files).
- **Always on**: it restarts with the engine and the computer. Pause it from
  the Agents page. On a laptop it pauses while the computer sleeps.
- **Its memory** (sessions, skills, scheduled jobs) lives in
  `<data dir>/agents/<id>`. Removing the agent deletes it; your apps stay.
- **No keys inside**: Hermes reaches its model only through sandboxd's proxy,
  which adds the real key, and reaches sandboxd's tools only with its own
  agent secret. The secret is replaced every time the container is
  re-created. The Telegram token is stored encrypted and given to the agent,
  which needs it to talk to Telegram; the WhatsApp login lives only in the
  agent's state directory.
- **Untrusted input**: what Hermes reads from the web, WhatsApp, Telegram or app
  results is data, not instructions. sandboxd limits what it can do (the four
  tools, the daily cap) rather than trusting it.

## API

`/v1/agent-catalog`, `/v1/agent-instances` (list, add), `/v1/agent-instances/{id}` (get,
remove), `/v1/agent-instances/{id}/start|stop|chat`, and
`/v1/agent-instances/{id}/channels` (get), `.../channels/whatsapp|telegram`
(link with POST, unlink with DELETE). See [openapi.yaml](openapi.yaml).
