# Agents

Agents are assistants sandboxd runs for you, next to your apps. The first is
[Hermes](https://github.com/NousResearch/hermes-agent) by Nous Research: it
remembers what you tell it, learns skills, runs scheduled jobs, and can create
and change apps on your sandboxd. You chat with it in the console
(**Agents**) or on Telegram.

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
3. Optional: paste a Telegram bot token (from @BotFather) and your Telegram
   user ID. Only the listed users can talk to it. Telegram uses long polling,
   so it works behind a home router.

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
  re-created. The Telegram token is stored encrypted and given to Hermes,
  which needs it to talk to Telegram.
- **Untrusted input**: what Hermes reads from the web, Telegram or app
  results is data, not instructions. sandboxd limits what it can do (the four
  tools, the daily cap) rather than trusting it.

## API

`/v1/agent-catalog`, `/v1/agent-instances` (list, add), `/v1/agent-instances/{id}` (get,
remove), `/v1/agent-instances/{id}/start|stop|chat`. See [openapi.yaml](openapi.yaml).
