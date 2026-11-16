# The sandboxd agent: one assistant for all your apps and agents

Status: **draft for approval**. Nothing here is built yet. It changes core
(control plane, runtimed), so it needs the maintainer's sign-off.

## Why

A person using sandboxd desktop ends up with many apps and several agents.
Today they talk to each one separately, and they can only install what is in
the catalog. The maintainer asked for **one main agent**, the person's single
correspondent, which:

- knows how sandboxd works, so it can install apps and agents that are **not
  in the catalog** ("install this GitHub project for me");
- creates apps and agents, reads their memories, and **hands tasks to them**;
- runs on **any coding subscription or API** the person already has.

## What the person sees

- **"sandboxd" at the top of the sidebar**, above Projects and Agents. It opens
  one chat, the same chat screen agents use. The home page's build box can
  send to it too.
- They write what they want, in plain words:
  - "Install Immich from GitHub": it reads the project, decides whether it can
    run here, installs it and replies with the link, or says in one sentence
    why it can't ("it needs a GPU").
  - "Every morning, ask Hermes for the news and put the three best stories in
    my dashboard app": it dispatches to Hermes, then to the app's coding agent.
  - "What did OpenClaw do yesterday?": it reads OpenClaw's memory and
    conversation.
- Every step shows in the chat in plain words ("Asked Hermes", "Created the
  app Recipes"). Anything that deletes or costs money asks first.

## Decisions (mine, to check)

1. **It is a coding agent, not a new agent runtime.** The sandboxd agent runs
   the person's chosen coding agent (OpenCode, Claude Code, Codex or Pi) in its
   own hidden workspace. That gives "any subscription or API" for free:
   Claude and ChatGPT sign-ins, OpenCode's free models, and any model
   connection through Pi already work there. Switching is the existing
   agent-and-model picker.
2. **Its powers are sandboxd tools over MCP.** Agents already have an MCP
   endpoint with `list_apps`, `create_app`, `ask_app`, `app_status`,
   `message_user` and `share_file` (`internal/api/agent_proxy.go`). The
   sandboxd agent gets the same endpoint, wired into its coding agent's MCP
   config by runtimed (OpenCode, Claude Code and Codex all read MCP servers),
   plus these tools:

   | Tool | What it does |
   |---|---|
   | `install_project` | Creates a project from a GitHub repo (or a catalog id) and writes its `sandbox.yaml`, the way Free apps installs work. |
   | `list_agents`, `add_agent` | The person's agents, and adding one from the agent catalog. |
   | `message_agent` | Sends a task to an agent and waits for its reply (the agent's conversation, as the console uses it). |
   | `read_memory` | An app's project memory (`BRAIN.md`, `brain/`) or an agent's memory and recent conversation. |
   | `remove_app`, `remove_agent` | Only after the person confirms in the chat. |

3. **It knows the architecture from a guide, not from training.** Its
   workspace gets an `AGENTS.md` (also loaded as a skill) that explains
   sandboxd in its terms: projects, `sandbox.yaml`, port 3000, the preview
   frame rules, pinned versions, what a sandbox can't do (no Docker, no root,
   no Postgres, no GPU, no headless browser yet), and the bar in
   `docs/free-apps.md`. That bar is how it decides "can this run here".
4. **Scope:** its token is the person's own (owner-scoped), like the console's.
   It can't reach other users' apps or the engine's settings.
5. **Reuse, don't duplicate.** The chat is the agent chat from #180 (one
   conversation kept by the engine). Dispatching to an agent uses the same
   message API the console uses. The catalog install is the console's
   install, moved to core so the agent and the console share it.

## Not now

- Agents talking to each other without the sandboxd agent in the middle.
- Running on remote servers.
- Letting it change engine settings, models or sign-ins.

## Failure even if it "works"

- It installs something that never starts and doesn't say so. Every install
  waits for the preview and reports the real outcome.
- It needs the person to know a technical word to get something done.
- It quietly spends a paid subscription in the background. Background work
  only runs on what the person scheduled.

## Order of work

1. Core: the MCP endpoint for a sandbox's coding agent (token, runtimed MCP
   config for OpenCode first, then Claude Code and Codex), with the existing
   six tools. Prove it in a hidden workspace.
2. `install_project` + the guide; the console's catalog install moves behind
   it. Test with the trending list and with projects that must be refused.
3. `list_agents`, `add_agent`, `message_agent`, `read_memory`.
4. Console: the "sandboxd" entry and chat; the home page's build box can hand
   to it.

## Open question for the maintainer

Name in the sidebar: **"sandboxd"** (my default), "Assistant", or something
else.
