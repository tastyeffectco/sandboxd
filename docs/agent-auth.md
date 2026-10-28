# Agent auth &amp; context

How sandboxd gives a sandbox's coding agent its credentials — and its
environment context — **without** putting secrets in the workspace, snapshots,
container env, logs, events, or task results.

Four coding agents ship behind one adapter interface: **OpenCode**, **Pi**,
**Claude Code**, and **Codex**. Connect their accounts in Settings. Credentials
stay in the control plane and are injected into requests by the auth proxy.

For **local models, remote compatible APIs, and LiteLLM**, use
[Model connections](model-providers.md). OpenCode and Pi support both
OpenAI-compatible and Anthropic-compatible endpoints through that API.
These connections store keys encrypted in SQLite and use temporary proxy
capabilities. The built-in agent-auth flows described below use the existing
opaque credential-file store.

## Connecting a provider

`GET /v1/agents` lists providers with `installed_state`, `status`, `method`,
`supports_oauth`, `supports_api_key`, and `runnable`. Connect a provider three
ways (connecting **replaces** the provider's stored material atomically, so a
provider holds exactly one at a time):

| Method | Endpoint | Notes |
| --- | --- | --- |
| **API key** | `POST /v1/agents/{provider}/api-key` `{"api_key":"…"}` | Stored server-side; injected into upstream requests by the proxy. |
| **Import** | `POST /v1/agents/{provider}/import` `{"credentials":"…"}` | Codex, OpenCode and Pi. Paste the bundle your local `<cli> login` produced. Parsed only in the control plane for account discovery, routing and refresh. |

### Claude subscriptions sign in inside each project

Anthropic doesn't allow third-party apps to collect, store or relay Claude.ai
logins ([legal and compliance](https://code.claude.com/docs/en/legal-and-compliance)):
a Claude Pro/Max subscription must sign in to the unmodified Claude Code binary
through Anthropic's own flow. So sandboxd never stores, imports or proxies a
Claude subscription login:

- **Sign in with Claude** (the project's Agent panel, or the API below) runs
  Claude Code's own `claude auth login` in that project's sandbox. The user opens
  Anthropic's sign-in page, approves, and pastes the code it shows; sandboxd
  types it into Claude Code, as a terminal would. Claude Code keeps the login in
  the project's workspace (`~/.claude`); the control plane never sees it.
- Without a connected Claude **API key**, claude-code tasks run Claude Code on
  that login, talking to Anthropic directly (no proxy). Sign in once per project.
- A Claude **API key** connected in Settings still goes through the proxy for
  every project.
- A Claude subscription inside an imported OpenCode or Pi login is ignored.
  A subscription login stored by an earlier version is deleted at startup.

| Call | Purpose |
| --- | --- |
| `GET /v1/sandboxes/{id}/claude-login` | `{"logged_in": bool, "method"}` from `claude auth status` |
| `POST /v1/sandboxes/{id}/claude-login` | Starts `claude auth login`; returns `{"url"}` (Anthropic's sign-in page) |
| `POST /v1/sandboxes/{id}/claude-login/code` | `{"code"}` from that page; returns the new status |
| `DELETE /v1/sandboxes/{id}/claude-login` | `claude auth logout` |

`POST /v1/agents/{provider}/disconnect` wipes the material. Stored
credentials are stored in restricted files outside workspaces (built-in auth)
or encrypted in SQLite (model connections). They are never returned to the console.
With the proxy enabled, account changes apply to subsequent requests.

## How the credential reaches the agent — two models

### Every agent → credential-injecting proxy (the real credential never enters the sandbox)

When the auth proxy is enabled (`SANDBOXD_AGENT_PROXY_URL`, default
`http://sandboxd:9100`), **all** runnable agents reach their model provider
through a **reverse proxy control-plane-side** (`internal/authproxy`) that holds
the real credentials. No credential — API key or OAuth token — is mounted or
env-injected into the sandbox; the agent gets only a base URL and a **dummy**
key, and the proxy swaps in the real `Authorization` / `X-Api-Key` header on the
wire. A task can neither read, exfiltrate, nor clobber the credential.

Sandbox base URLs are `<proxy>/<agent>/<upstream>/…`; the proxy resolves the
agent's stored credential for that upstream and injects it. Upstreams:
`anthropic`, `openai`, `zen` (OpenCode Zen pay-as-you-go), `zengo` (OpenCode Zen
"go" subscription), and the MiniMax direct endpoints `minimax`,
`minimax-cn` (China), `minimax-anthropic`, `minimax-anthropic-cn` (China).

- **claude-code** with a Claude **API key** — `ANTHROPIC_BASE_URL` =
  `<proxy>/claude-code/anthropic`, `ANTHROPIC_API_KEY` = `sandboxd-proxy-injected`.
  A Claude subscription instead signs in inside the project (above) and uses no proxy.
- **opencode** — routed via an `OPENCODE_CONFIG` file that runtimed writes, which
  sends opencode's requests to the proxy with the dummy key. Which OpenCode Zen
  endpoint it uses for a directly connected Zen/Go key is set by `SANDBOXD_OPENCODE_ZEN_PATH` — see
  [OpenCode Zen: subscription vs pay-as-you-go](#opencode-zen-subscription-vs-pay-as-you-go)
  below. (Implementation note: opencode's Zen provider ignores base-URL env vars
  and its Bun runtime rejects a bare hostname, so we can't just set a `*_BASE_URL`;
  the config file defines a custom `@ai-sdk/openai-compatible` provider pointed at
  the proxy's resolved **IP**, and the task's model is rewritten to `proxy/<id>`.)
- **Codex** — the CLI uses the Responses API through the proxy. ChatGPT logins
  go to the Codex subscription backend; API keys go to the OpenAI API. The real
  access token and account ID are injected server-side. WebSockets are disabled
  for this route so streaming works through the HTTP proxy.
- **Pi / imported OpenCode accounts** — import their CLI auth JSON. Supported
  entries are Anthropic, OpenAI, Codex, OpenCode Zen and OpenCode Go. OpenCode's
  `openai` OAuth entry uses the Codex backend; Pi's corresponding entry is
  `openai-codex`. Unknown providers are not guessed or redirected to Zen.
  Unsupported OAuth refresh flows require re-login in the original CLI.

- **MiniMax** — a credential-only provider: connect a MiniMax API key in
  Settings → AI Agents (`POST /v1/agents/minimax/api-key`). MiniMax is not a
  task agent (no CLI, not in the run picker); its key is injected by the proxy
  for the MiniMax direct upstreams. For model selection use a configured MiniMax
  model connection; the account picker does not invent MiniMax models. Legacy
  direct model routes remain supported by runtimed.

  | Region | OpenAI-compatible | Anthropic-compatible |
  |--------|-------------------|----------------------|
  | `global_en` *(default)* | `https://api.minimax.io/v1` | `https://api.minimax.io/anthropic` |
  | `cn_zh` | `https://api.minimaxi.com/v1` | `https://api.minimaxi.com/anthropic` |

  To use Flash for tasks with no model override, set the OpenCode default in
  Settings to `MiniMax-M3.1-Flash` (or set `SANDBOXD_OPENCODE_MODEL`). Existing
  saved defaults and explicit `MiniMax-M2.7` requests retain their original
  model and direct route; the general OpenCode default is unchanged.

  [MiniMax model metadata](https://platform.minimax.io/docs/guides/models-intro):
  **MiniMax-M3.1-Flash** — 1,000,000-token context, up to 524,288 output tokens,
  text/image/video input, adaptive-only thinking. OpenAI `reasoning_effort`
  and Anthropic `output_config.effort` accept `low`, `medium`, `high`, `xhigh`,
  and `max` (default); disabled thinking and effort `none` are unsupported.
  Standard pricing is $0.15 / $0.60 / $0.015 per million input / output /
  cache-read tokens. **MiniMax-M3** — 1,000,000-token context, image/video input,
  adaptive or disabled thinking (OpenAI defaults to adaptive; Anthropic to
  disabled); effort does not tune its depth. Standard pricing for input lengths
  up to 512k is $0.30 / $1.20 / $0.06, and above 512k $0.60 / $2.40 / $0.12
  per million input / output / cache-read tokens.
  See the [API reference](https://platform.minimax.io/docs/api-reference/text-openai-api)
  and [pricing](https://platform.minimax.io/docs/guides/pricing-paygo).
  The proxy passes request bodies through, including tools, reasoning history,
  and media; it does not add thinking/effort or `service_tier` parameters.
  MiniMax endpoints authorize with `Authorization: Bearer <key>`.

**The real credential never appears in the sandbox filesystem or env** — verify
with `mount | grep agent-auth` (none) and `ls /run/agent-auth` (absent).

### OpenCode Zen: subscription vs pay-as-you-go

OpenCode Zen has **two gateways**, and the same key behaves differently on each.
`SANDBOXD_OPENCODE_ZEN_PATH` picks which one opencode uses:

| Value | Endpoint | Billing | Models |
|-------|----------|---------|--------|
| `zen` *(default)* | `opencode.ai/zen/v1` | Pay-as-you-go — needs a positive wallet balance | Full catalog: `claude-*`, `gpt-*`, `gemini-*`, plus the open models |
| `zengo` | `opencode.ai/zen/go/v1` | Included in the OpenCode **"go" subscription** — no balance needed | Open models only: GLM, Kimi, MiniMax, DeepSeek, Qwen, MiMo… |

**Which one do I want?**

- On the **"go" subscription** → set `zengo`. Your included models run without a
  balance. (This deployment is on the go plan, so `.env` sets `SANDBOXD_OPENCODE_ZEN_PATH=zengo`.)
- **Pre-paid a Zen wallet** and want Claude/GPT/Gemini through opencode → leave it
  `zen` (the default).

**Symptom of the wrong setting:** an opencode task fails with **`Insufficient
balance`** → you're on `zen` (pay-as-you-go) with an empty wallet; switch to
`zengo` (or top up). A task that fails with **`Model … is not supported`** → the
picked model isn't in that path's catalog (e.g. a `claude-*` model on `zengo`).

The console's opencode model dropdown should list models from whichever path you
set — go-subscription models (GLM/Kimi/…) for `zengo`.

### OpenCode free tier — works with no key (zero friction)

**opencode is usable out of the box with nothing connected.** Zen serves a set of
free models (e.g. `big-pickle` and ids ending `-free`) that need **no API key**,
but only to OpenCode's own client: keyless requests from any other client,
including the credential proxy, get `FreeUsageLimitError`. So when opencode has
no connected credential and the task uses the free model, the control plane
marks the task `free_tier` and runtimed runs OpenCode on its **built-in**
`opencode` provider (`opencode/big-pickle`) instead of the proxy. There is no
credential on this path, so nothing secret enters the sandbox. The control plane
defaults the task's model to the free one so a fresh install can build
immediately.

> Zen's API **requires a named model** — there is no keyless "auto" model, and
> only the free models work without a key. Any other model, and every connected
> key or subscription, still goes through the credential proxy.

- **No key** → free tier. The default free model is `big-pickle`; override with
  `SANDBOXD_OPENCODE_FREE_MODEL`.
- **Key connected** (Settings → AI Agents) → the proxy injects it and opencode
  gets the full paid catalog, exactly as before. A connected key always wins.

This is **opencode-only** — it's the one provider with a keyless free tier. Every
other agent (claude-code, codex) still returns a clear "connect it" error when
unconnected. See `internal/authproxy/proxy.go` (`credFor`) and
`internal/api/v1_tasks.go` (model precedence).

### Per-agent default model (optional)

The task picker and **Settings → AI Agents → Default models** query
`GET /v1/agents/{provider}/models`. This is an authenticated provider catalog,
not a bundled list of guessed model IDs or CLI aliases. Imported OpenCode/Pi
models are namespaced by account, for example `openai/<returned-model-id>`.
The exact model ID is preserved when configuring the agent. Catalog failure
shows an error and retry action; it never silently substitutes another model.

For connected accounts, task model selection is checked against the current
catalog. An explicit task selection wins, followed by a saved default, then the
first listed model. A catalog entry is not a guarantee of available quota;
a provider can still reject inference. Custom model connections retain their
operator-configured model IDs and explicit inference test.

OpenCode's keyless free tier retains its configured free default. Free-tier
inference is used only by the real OpenCode CLI, not by other agents.

### Reusing an existing local login

In the Electron desktop, **Connect subscription → Use existing login** appears
when a supported login is found on this computer. A native confirmation names
the destination engine before sending the credential. The credential is read
and transferred by the main process; the renderer receives availability and
success status only. Remote imports require HTTPS.

Known locations are `~/.codex/auth.json`, `~/.local/share/opencode/auth.json`,
and `~/.pi/agent/auth.json`. Claude logins are never read (see above). Custom CLI
credential locations require manual import. An expired login may need renewal
with the original CLI before importing. The browser console supports manual
import; it cannot read your computer's login files.

Supported OAuth credentials are refreshed before model discovery and inference.
Concurrent refreshes are serialized, and an in-flight refresh cannot undo a
Disconnect or overwrite a newly connected account.

### Fallback: no proxy → mounted auth dir

If the proxy is disabled, the connected provider's opaque auth dir
(`<DataDir>/agent-auth/<provider>/`, outside any workspace) is bind-mounted
read-only at `/run/agent-auth/<provider>` and runtimed points the agent's `HOME`
there; an API-key connect instead injects the provider's one key var. This is the
legacy path — with the proxy on (the default), nothing is mounted or injected.

## Env scrub

The agent process env is scrubbed of secret-shaped variables (`*_KEY`,
`*_TOKEN`, `*_SECRET`, `*_PASSWORD`, `*_CREDENTIALS`, and runtimed's own
`RUNTIMED_*`). So:

- An agent can never pick up a stray credential from the container env.
- **Injecting a key via the sandbox `env` no longer reaches the agent** — e.g.
  `ANTHROPIC_API_KEY` set at create time is dropped. Connect the provider
  instead (API-key connect re-injects it from the managed file).

Non-secret config (`PATH`, `HOME`, `LANG`, `*_MODEL`, `*_BASE_URL`, …) is kept.

## Agent context — platform prompt + per-app guide

So an agent understands its environment and doesn't break things, two context
layers are injected with **no change to your source**:

- **Platform system prompt** — a single committed
  [`control-plane/internal/agentprompt/prompt.md`](../control-plane/internal/agentprompt/prompt.md),
  embedded into both the control plane and `runtimed`. runtimed renders it with
  the sandbox's real port/health and appends it to every run — via
  `--append-system-prompt` for claude-code, and as a prompt preamble for
  opencode. It defines the guardrails: bind `0.0.0.0`, don't rewrite
  `sandbox.yaml` carelessly, don't touch `/run/agent-auth`, no destructive git
  on the main branch, verify on the loopback port before finishing. It is
  surfaced **read-only** as `agents.system_prompt` in `GET /v1/settings`
  (Console → Settings → Agent system prompt).
- **Per-app `AGENTS.md`** — the detected stack, upstream repo link, and how-to-run,
  written to `workspace/app/AGENTS.md` on runtime apply (git-ignored via
  `.git/info/exclude`) unless the repo already ships one. This is a **console**
  behavior on top of `/v1`, not a core write.

## What's guaranteed

Credentials are absent from: the workspace, snapshots (workspace tree only), the
container env / `docker inspect`, task results, events, and logs. A Claude
subscription is the exception by design: Claude Code itself keeps that login in
the project's workspace (`~/.claude`), and it never reaches the control plane.

## Running a task

Submit `POST /v1/sandboxes/{id}/tasks` with `{"prompt":"…","agent":"opencode","model":"opencode/glm-5"}`.
runtimed runs the CLI in the workspace (`--dangerously-skip-permissions`, since
the containment boundary is the throwaway sandbox, not the agent), streams events
over SSE (`/tasks/{taskId}/events`), and returns a final result. `model` maps to
the CLI's `--model`.

Defaults:

- **Agent** — `SANDBOXD_DEFAULT_AGENT` (default `opencode`) picks the agent for
  tasks that don't specify one.
- **Continue** — `continue` is a tri-state and **defaults to continuing** the
  sandbox's most recent agent session (`claude/opencode --continue`, codex
  `resume --last`). Omit it and runtimed continues when a prior session exists and
  starts fresh on the first task. Conversation state is separated by agent,
  connection and model. Failed authentication does not create a resumable
  session. Send `false` to start fresh. Existing sessions created before this
  isolation change start fresh once after upgrading.

## Configuration reference

Operator knobs for agent auth & task defaults (all optional; sensible defaults shown):

| Env var | Default | What it does |
| --- | --- | --- |
| `SANDBOXD_AGENT_PROXY_URL` | `http://sandboxd:9100` | In-network URL of the credential-injecting proxy. Set/empty toggles the proxy vs. legacy mounted-auth model. |
| `SANDBOXD_DEFAULT_AGENT` | `opencode` | Agent used for tasks that don't specify one. |
| `SANDBOXD_OPENCODE_ZEN_PATH` | `zen` | OpenCode Zen endpoint for opencode: `zen` (pay-as-you-go, full catalog) or `zengo` (the "go" subscription's models). See [OpenCode Zen: subscription vs pay-as-you-go](#opencode-zen-subscription-vs-pay-as-you-go). |
| `SANDBOXD_OPENCODE_MODEL` | *(unset)* | Global default `--model` for opencode tasks that don't pass one. |
| `SANDBOXD_MINIMAX_REGION` | `global_en` | MiniMax direct endpoint region for MiniMax models: `global_en` (`api.minimax.io`) or `cn_zh` (`api.minimaxi.com`). Requires a connected MiniMax API key. |

Per-task, `POST /tasks` also accepts `agent`, `model`, and `continue` (tri-state —
see [Running a task](#running-a-task)).

## Verifying connected accounts

`TestLiveBuiltinAgents` is opt-in and uses isolated workspaces and a private copy
of the supplied login. It queries the account catalog and performs two actual
file-editing turns. It does not install or restart the production engine.

```sh
cd control-plane
SANDBOXD_TEST_CODEX_AUTH_FILE="$HOME/.codex/auth.json" \
SANDBOXD_TEST_CODEX_MODEL='<exact ID from the account catalog>' \
go test ./cmd/runtimed -run '^TestLiveBuiltinAgents/codex$' -v -count=1
```

Equivalent variables use `CLAUDE_CODE`, `OPENCODE`, or `PI` instead of `CODEX`.
For imported OpenCode/Pi accounts, select the account-prefixed model ID returned
by `/v1/agents/{provider}/models`. `SANDBOXD_TEST_OPENCODE_FREE=1` explicitly
opts into the real OpenCode free-tier check. These calls use the connected
account's quota; they are never enabled in ordinary unit tests.

Catalog discovery and successful inference are separate checks. A failed login
or catalog request must be resolved before calling an account verified.
