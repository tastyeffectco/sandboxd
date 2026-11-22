# Choosing the AI: free first, then subscriptions, OpenRouter or CometAPI (design draft for approval)

Status: draft, needs the maintainer's yes (core: saving which AI an agent or
app uses). Date: 2026-11-21.

## Intent

A person opens sandboxd and talks to it right away, for free, with nothing to
sign up for. Later, if they want stronger or faster models, they connect what
they already pay for (a coding-plan subscription) or a model gateway
(OpenRouter, or CometAPI through sandboxd's affiliate link) in one click, and
pick which AI each agent and each app uses, without reading about providers,
keys or model IDs.

## What already exists (main, 2026-11-21)

- **Free tier.** An OpenCode task with no connected account and no model
  connection runs on OpenCode Zen's keyless free model (`big-pickle`,
  `agentauth.OpencodeFreeModel`, operator-overridable). The console's
  `defaultChoice` is `{agent: 'opencode', provider: '', model: ''}`, so the
  sandboxd agent, every new app and every agent chat already start free.
- **Subscriptions.** Settings connects Codex, OpenCode and Pi logins (API key or
  imported `<cli> login` bundle; `docs/agent-auth.md`). Claude Pro/Max signs in
  inside each project through Claude Code's own flow (Anthropic does not allow
  third-party apps to store or relay Claude logins).
- **Model connections.** OpenAI- or Anthropic-compatible gateways, keys
  encrypted in the control plane, injected by the proxy
  (`docs/model-providers.md`). The desktop app has a one-click OpenRouter
  sign-in that starts on `openrouter/free`.
- **Defaults per coding agent.** Settings → `agents.default_models`.
- **Per-message picker.** `AgentPicker` in Home, app chats and agent chats.

## Gaps

1. Nothing tells a new person they are already on a free AI, or what an
   upgrade would bring.
2. No CometAPI option.
3. The picker's choice is not saved: every chat opens on `defaultChoice` again.
   The sandboxd agent's choice lives only in the control plane's memory
   (`assistantRegistry.choices`): after an engine restart it is empty until the
   next message, and since the console does not remember the choice either,
   that message (and the builds it starts) goes back to the free tier.
4. No place answers "which AI does this agent / this app use?".

## Design

### 1. First run: "You're ready" (console only)

Home shows one small card the first time, and in Settings → AI afterwards:

> **sandboxd is ready, free.** It uses OpenCode's free AI, no account needed.
> Want stronger models? *Use my subscription* · *OpenRouter* · *CometAPI* ·
> *Not now*

- *Use my subscription* opens the existing Settings connect flow, listing the
  plans in plain words ("ChatGPT Plus/Pro (Codex)", "OpenCode", "Claude
  Pro/Max: sign in inside a project").
- *OpenRouter* is the existing one-click sign-in.
- *CometAPI*, see 2.
- Dismissing it is remembered per browser (a convenience only).

When the free tier fails (rate limit, Zen outage), the chat error says so in
plain words and offers the same three buttons, instead of a raw error.

### 2. CometAPI preset, marked as an affiliate link (console only)

- A "CometAPI" card next to OpenRouter in Settings → Model connections and in
  the first-run card.
- *Create a CometAPI account* opens `https://www.cometapi.com/?ref=abdelouahed`
  in the person's browser. The card always shows, next to the button:
  **"Affiliate link: sandboxd earns a commission if you sign up. It costs you
  nothing extra."** Never hidden, never pre-checked.
- *I have a key* asks for the key and saves a normal model connection:
  `protocol: openai-completions`, `base_url: https://api.cometapi.com/v1`
  (answers `401 no token provided` without a key, as an OpenAI-style API
  does), with one tested default model pre-filled. To verify with a real key
  before shipping: model list, tool calls with OpenCode and Pi, and whether its
  Responses API works for Codex (if not, CometAPI is simply not offered to
  Codex, as for other Chat-Completions-only gateways).
- Unlike OpenRouter there is no OAuth sign-in, so CometAPI needs one paste.

### 3. Which AI runs what: three levels (core part to approve)

Resolution order for any task, first match wins:

1. **This message**: the picker in the chat (exists).
2. **This agent / this app**: a saved choice per agent instance and per app,
   set from the picker ("Use this for <name> from now on") or from the item's
   settings. *New.*
3. **Everyone's default**: Settings → AI default (today
   `agents.default_models` per coding agent; add which coding agent and which
   connection are the default). *Extended.*
4. Nothing set: OpenCode free.

Core changes (for the maintainer):

- Store `{agent, provider, model}` on the app row and on the agent instance
  row (nullable; empty means "use the default"), and on the assistant row for
  the sandboxd agent, replacing the in-memory `assistantRegistry.choices` so
  its choice survives restarts.
- An `ai` field on the existing `PATCH /v1/apps/{id}` and
  `PATCH /v1/agent-instances/{id}`, and a new `PATCH /v1/assistant`,
  and `POST /v1/sandboxes/{id}/tasks` resolving levels 2–4 when the request
  has no agent/model.
- A saved choice pointing at a connection that was later removed falls back to
  the default and the chat says so once ("CometAPI was disconnected, using the
  free AI").

Console:

- The picker shows where the current choice comes from ("default", "this
  app") and offers "Use this for <name> from now on".
- Settings → AI lists every agent and app with its AI in one table, with a
  dropdown per row: the answer to "which uses what".
- Builds and installs the sandboxd agent starts inherit its AI unless the
  person picked one for that app (the current behaviour, made durable).

## Plain-language rules

- Say "AI" and the product names people know (ChatGPT, Claude, OpenRouter,
  CometAPI), not "provider", "connection", "protocol" or model IDs, except in
  an Advanced section.
- Every paid option says it may cost money; the free one says it is free.
- The affiliate disclosure is always visible where the link is.

## Testing

- Console: vitest for the first-run card, CometAPI card (link carries `ref`,
  disclosure visible), picker "use from now on", Settings → AI table.
- Core: store migration, `/v1` round-trip, task resolution order 1–4, removed
  connection fallback, sandboxd agent choice surviving a control-plane restart.
- Real machine: fresh Windows install, first message on the free tier, then
  connect OpenRouter and CometAPI, restart Windows, check the sandboxd agent
  still uses the chosen AI.

## Order of work

1. Console only, no approval needed: first-run card, CometAPI preset with
   disclosure, friendlier free-tier errors.
2. After approval: saved choice per agent/app/assistant (core), then the
   Settings → AI table and picker changes.
