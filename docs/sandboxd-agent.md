# The sandboxd agent ("Ask sandboxd")

One main assistant per person, at the top of the console's sidebar. The person
talks to it in plain words; it acts on their sandboxd: builds and changes apps,
installs open-source projects from GitHub, lists their agents, gives them tasks
and reads what they did. Design: `docs/superpowers/specs/2026-11-16-sandboxd-agent-design.md` (PR #186).

## How it works

- It is the **coding agent of a hidden project** (tag `sandboxd:assistant`),
  so it runs on whatever the person picks in that chat: OpenCode's free
  models, a Claude or ChatGPT subscription (Claude Code, Codex), or any model
  connection (Pi). The console hides the project from the project list.
- `POST /v1/assistant` makes it on first use and, on every call, writes the
  project's tool settings again with a fresh secret and sandboxd's current
  address: `opencode.json` (OpenCode), `.mcp.json` + `.claude/settings.json`
  (Claude Code), plus `AGENTS.md` (how sandboxd works) and `CLAUDE.md`.
- Its tools are served over MCP on the agent proxy port at `/assistant/mcp`,
  authenticated by that secret (only its hash is stored, table `assistant`).
  They act as the owner through sandboxd's own `/v1` routes:

  | Tool | Does |
  |---|---|
  | `list_apps`, `create_app`, `ask_app`, `app_status` | Same as the agents' tools (no daily app limit: it acts on the person's direct request) |
  | `install_project` | New app from a GitHub repo; its coding agent writes `sandbox.yaml` under the Free apps rules, or says why it can't run |
  | `read_memory` | An app's `BRAIN.md` and `brain/*.md` |
  | `remove_app` | Only apps it installed or created itself (tag `created-by:sandboxd`): it cleans up an install that failed or was refused |
  | `list_agents`, `message_agent`, `read_agent_conversation` | The person's agents; a task sent to one shows in that agent's chat as "From the user's sandboxd agent: ..." and its answer comes back (waits up to 4 minutes) |

Builds and installs it starts run on the same AI the person chose for it (the
engine remembers the agent, connection and model of the latest message sent to
it), not on the free tier. Before installing, it reads the project's
requirements; the install task stops at once, with a reason, when the project
can't run in a workspace (for example JSON Crack 5 needs Node 24).

## Verified (Windows PC, OpenCode free model)

- "Which agents do I have, and how many apps?" → named Hermes, OpenClaw and
  OpenMausBot and counted the apps.
- "Ask Hermes: what is the capital of Portugal?" → "Hermes answered: Lisbon",
  and the exchange is in Hermes's own chat.
- "Install the 2048 game for me (github.com/gabrielecirulli/2048)" → a new
  app, pinned to a commit, playing in the preview after 154 s.

## Not yet

- Codex reads MCP servers only from its global config, so the tools reach
  OpenCode and Claude Code for now.
- Adding agents from the chat (`add_agent`) and removing apps or agents (with
  the person's confirmation).
