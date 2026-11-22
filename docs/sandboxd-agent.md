# The sandboxd agent ("Ask sandboxd")

One main assistant per person: it is the console's **Home**, the first page
and the single front door. The person talks to it in plain words; it acts on
their sandboxd: builds and changes apps, installs open-source projects from
GitHub, lists their agents, gives them tasks and reads what they did. Design: `docs/superpowers/specs/2026-11-16-sandboxd-agent-design.md` (PR #186).

## How it works

- It is the **coding agent of a hidden project** (tag `sandboxd:assistant`),
  so it runs on whatever the person picks in that chat: OpenCode's free
  models, a Claude or ChatGPT subscription (Claude Code, Codex), or any model
  connection (Pi), chosen with the chat's **AI** button; a new chat starts on
  the AI set in Settings → AI (`agents.default_ai`). The console hides the
  project from **My apps** and its replies are signed "sandboxd". Checkpoint
  **Restore** is not offered in this chat.
- If the engine has no sandboxd agent (an older engine: `POST /v1/assistant`
  answers 404), Home quietly opens **My apps** instead.
- Files attached on Home (the **+** button, drop or paste) are saved in the
  hidden project's `.attachments/` folder, and the message says where they
  are.
- `POST /v1/assistant` makes it on first use and, on every call, writes the
  project's tool settings again with a fresh secret and sandboxd's current
  address: `opencode.json` (OpenCode), `.mcp.json` + `.claude/settings.json`
  (Claude Code), `.codex/config.toml` (Codex: `codex exec` loads a project's
  config), plus `AGENTS.md` (how sandboxd works) and `CLAUDE.md`.
- Its tools are served over MCP on the agent proxy port at `/assistant/mcp`,
  authenticated by that secret (only its hash is stored, table `assistant`).
  They act as the owner through sandboxd's own `/v1` routes:

  | Tool | Does |
  |---|---|
  | `list_apps`, `create_app`, `ask_app`, `app_status` | Same as the agents' tools (no daily app limit: it acts on the person's direct request) |
  | `install_project` | New app from a GitHub repo; its coding agent writes `sandbox.yaml` under the Free apps rules, or says why it can't run |
  | `read_memory` | An app's `BRAIN.md` and `brain/*.md` |
  | `remove_app` | Apps it installed or created itself (tag `created-by:sandboxd`) go at once, to clean up a failed install. Any other app only after the person says yes (below) |
  | `remove_agent` | One of the person's agents, only after they say yes |
  | `add_agent` | Adds an agent from the agent catalog (without a package: lists the catalog), on the person's first AI connection agents can use |
  | `read_agent_memory` | An agent's memory notes from its own folder (MEMORY.md, USER.md, memories/, memory/), read-only and size-limited |
  | `list_agents`, `message_agent`, `read_agent_conversation` | The person's agents (`message_agent` starts a paused agent first); a task sent to one shows in that agent's chat as "From the user's sandboxd agent: ..." and its answer comes back (waits up to 4 minutes) |

Builds and installs it starts run on the same AI the person chose for it (the
engine remembers the agent, connection and model of the latest message sent to
it), not on the free tier. Before installing, it reads the project's
requirements; the install task stops at once, with a reason, when the project
can't run in a workspace (for example JSON Crack 5 needs Node 24).

Removing what the person made needs their yes: the first `remove_app` or
`remove_agent` call removes nothing and tells the agent to ask; a second call
works only during the person's very next message, so the agent can't confirm by
itself within one turn (the engine counts the messages sent to it).

## Verified (Windows PC, OpenCode free model)

- "Which agents do I have, and how many apps?" → named Hermes, OpenClaw and
  OpenMausBot and counted the apps.
- "Ask Hermes: what is the capital of Portugal?" → "Hermes answered: Lisbon",
  and the exchange is in Hermes's own chat.
- "Install the 2048 game for me (github.com/gabrielecirulli/2048)" → a new
  app, pinned to a commit, playing in the preview after 154 s.

- On OpenRouter (DeepSeek V4.1 Flash), from the real chat: "Install the 2048
  game for me" → checked the license and stack, installed it, said after 3
  minutes it would appear in projects; it did.
- "What does Hermes remember about me?" → two facts from Hermes's memory notes.
- "Add a new Hermes agent for me called Researcher" → added and running in 15 s.
- CADCraft from Free apps: running 13 s after the click, with no AI involved.
- "Please delete my app Old notes" → asked "Remove Old notes? Its files are
  deleted for good." and kept it; after "Yes, delete it." the app was gone. The
  same for an agent ("remove the agent Temp" → asked, then removed after "Yes.").
- Codex: with the generated `.codex/config.toml`, `codex exec` (0.162.1, as
  runtimed runs it) connects with the secret, lists the tools and calls them
  (checked against a stand-in model and tool server), then for real on the
  Windows PC with the person's ChatGPT login (GPT-5.6-Luna): "Which agents do I
  have, and how many apps?" → the three agents and 14 apps; "Ask Hermes the
  capital of Japan" → started the paused Hermes itself, then "Tokyo." (49 s).

## Not yet

- Pi has no MCP support, so on Pi the agent can talk but not use the tools.
