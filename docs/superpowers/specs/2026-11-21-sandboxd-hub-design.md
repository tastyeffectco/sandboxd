# sandboxd as the hub: connected apps, one vault, permissions (design draft for approval)

Status: draft, needs the maintainer's yes (core: new store tables, `/v1`
endpoints, new sandboxd tools). Date: 2026-11-21.

## Intent

sandboxd is the person's general space. Their accounts (Gmail, Google Calendar,
GitHub, Slack, Notion, Google Drive…) and their secrets (API keys, passwords)
are connected **once, in sandboxd**. Every agent (the sandboxd agent, Hermes,
OpenClaw, OpenMausBot, future ones) and every app can use them through
sandboxd: allowed by default, or after asking the person. No agent's source
code changes.

## What already exists (main, 2026-11-21)

- **Tools over MCP.** Agents already reach sandboxd as an MCP server:
  `POST /agents/{id}/mcp` (`create_app`, `list_apps`, `app_status`, `ask_app`,
  `share_file`, `message_user`) and the sandboxd agent at `/assistant/mcp`
  (apps, agents, memories). Each call is authenticated by the agent's own
  secret, so sandboxd always knows **which agent** is asking. Hermes,
  OpenClaw, OpenMausBot and the coding agents speak MCP natively, so new tools
  reach them with no change to their code.
- **Per-app secrets.** `v1_app_config.go`: AES-256-GCM at rest, write-only over
  the API, never logged. Scoped to one app.
- **Inbox / message_user.** Agents can already ask the person something.

## Design

### 1. Connected apps (account level)

Settings → **Connected apps**: a card per service (Gmail, Calendar, Drive,
GitHub, Slack, Notion…), each with what agents could *read* and *write*, in
plain words, and **Connect**.

How a connection works, without sandboxd shipping its own integration for
every service:

- Prefer the **service's official MCP server** (GitHub, Notion, Slack and
  others publish one; remote ones use OAuth). sandboxd connects to it once,
  holds the OAuth tokens in the control plane, and **re-exposes its tools**
  to agents, namespaced (`github.search_issues`, `gmail.send_draft`). Tokens
  never enter a workspace or an agent.
- Where no official server exists, an open-source MCP server runs in a
  sandboxd-owned container with the token injected by sandboxd, same rule.
- OAuth sign-in opens the person's browser and returns to the desktop app, as
  the OpenRouter sign-in already does.

### 2. One vault (account level)

Settings → **Vault**: named secrets (`STRIPE_KEY`, `SMTP_PASSWORD`), same
encryption as per-app secrets, write-only over the API. Per-app secrets stay
and can reference a vault entry instead of a copy.

Agents never read a secret's value by default. Two ways to use one:

- **Injected:** an app or a connection is configured to use `STRIPE_KEY`;
  sandboxd adds it where needed (environment of that app, header of that
  request). The agent only sees the name.
- **Revealed (opt-in):** `vault.get` returns the value only when the person
  allowed that agent to see that secret.

### 3. Permissions: who can use what

A grant is `(agent or app) × (connection or secret) × (read | write)` with one
of: **Always allow**, **Ask each time**, **Never**.

- Default when connecting something: **Ask each time** for every agent, with a
  switch on the connection card, "Let all my agents use this", for people who
  want it simple.
- When an agent calls a tool it has no grant for, sandboxd does not fail: it
  asks the person (in the chat and the inbox), in plain words: "**Hermes** wants
  to **send an email from Gmail**: *Weekly plan to Alex*. Allow once ·
  Always allow · Deny". The tool call waits (with a timeout) and then runs or
  returns a clear refusal the agent can explain.
- Settings → Connected apps → *a connection* → **Who can use it**: a list of
  agents and apps with their grant, editable. Every use is logged in the
  activity feed (who, what, when; never secret values or message bodies).

### 4. Tools agents get (no agent code change)

Added to the existing MCP servers:

- `list_connections`: what is connected and what this agent may do.
- `request_access {connection, why}`: ask the person ahead of time.
- The connected services' tools, namespaced, filtered to what this agent may
  call (or shown with "needs your approval").
- `vault.list` (names only) and `vault.get` (value only if granted).

The sandboxd agent also gets `settings.get` / `settings.set` (default AI,
time zone…) and `connections.manage` so people can say "connect my Gmail" or
"let Hermes use my calendar" in chat; any change it makes is shown to the
person for one click of approval.

## Plain-language rules

- Say what an agent will be able to do ("read your emails", "create calendar
  events"), never scopes or token names.
- Every approval prompt names the agent, the action and the thing it touches.

## Security

- Tokens and secrets only in the control plane, encrypted; never in
  workspaces, snapshots, logs, events, task results or agent memory.
- Grants checked on every call by the agent's own credential; an agent cannot
  act for another.
- Removing an agent removes its grants; disconnecting a service revokes its
  tokens at the provider when the provider allows it.

## Order of work

1. Vault (account level) + names-only `vault.list` + injected use by apps.
2. Grants and the approval prompt (chat + inbox), with `request_access`.
3. Connected apps, first set (asked for by the maintainer): **Gmail, Google
   Calendar, Google Drive, Slack, Notion, GitHub**. Same card, connect flow and
   grants for all six; GitHub and Notion first because their official remote
   MCP servers use standard OAuth with dynamic client registration (no app to
   register), then Slack, then the three Google ones (see risks).
4. sandboxd agent: settings and connections in chat.

## Risks to settle before building

- **Google (Gmail, Calendar, Drive)** needs an OAuth client that sandboxd
  registers with Google. Gmail's read/send scopes are "restricted": a public
  app needs Google's verification and a yearly security assessment before
  people other than test users can connect. Options: (a) ship as a sandboxd
  OAuth app and go through verification (weeks), starting with the 100
  test-user limit for the preview; (b) let each person use their own Google
  Cloud client (not acceptable for non-technical users); (c) start with
  Calendar and Drive (sensitive but not restricted scopes for narrower access)
  and add Gmail after verification. Recommended: (a) with (c)'s order.
- **Slack** apps are registered per workspace unless distributed; the Slack
  app needs to be created once by sandboxd and made distributable.
- Which official remote MCP servers exist and their exact OAuth flows must be
  checked at build time (they change quickly); the hub falls back to an
  open-source MCP server in a sandboxd container where needed.

## Testing

- Go: grant checks on every proxied tool call, approval wait/timeout, secrets
  never in responses or logs, agent A cannot use agent B's grant.
- Console: connect flow, approval prompt, "Who can use it" editing.
- Real machine: connect GitHub on Windows, ask Hermes to list issues (prompt,
  allow once, then always), revoke and see it refused.
