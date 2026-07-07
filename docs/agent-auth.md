# Agent auth &amp; context

How sandboxd gives a sandbox's coding agent its credentials — and its
environment context — **without** putting secrets in the workspace, snapshots,
container env, logs, events, or task results.

Two agents ship today behind one adapter interface: **opencode** and
**claude-code** are runnable (they have `runtimed` task adapters); **codex** can
be connected but is not runnable yet. Every provider carries its **own**
credentials, connected by the owner and stored opaquely; a provider's material
is only ever used for that provider.

## Connecting a provider

`GET /v1/agents` lists providers with `installed_state`, `status`, `method`,
`supports_oauth`, `supports_api_key`, and `runnable`. Connect a provider three
ways (connecting **replaces** the provider's stored material atomically, so a
provider holds exactly one at a time):

| Method | Endpoint | Notes |
| --- | --- | --- |
| **API key** | `POST /v1/agents/{provider}/api-key` `{"api_key":"…"}` | Stored opaquely; injected as the provider's one key env var at task time. |
| **Import** | `POST /v1/agents/{provider}/import` `{"credentials":"…"}` | Paste the bundle your local `<cli> login` produced. Written verbatim, never parsed. |
| **Guided OAuth** (claude-code) | `POST /v1/agents/claude-code/oauth/start` → open URL → `…/oauth/finish` `{"code":"<code#state>"}` | A real `claude setup-token` **PKCE** flow, driven server-side. |

`POST /v1/agents/{provider}/disconnect` wipes the material. Stored
credentials are encrypted/opaque, never returned, echoed, logged, or emitted in
events; the console inputs are write-only. Auth changes take effect on the next
sandbox (re)create.

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
"go" subscription).

- **claude-code** — `ANTHROPIC_BASE_URL` = `<proxy>/claude-code/anthropic`,
  `ANTHROPIC_API_KEY` = `sandboxd-proxy-injected`. Works for both an API key and
  a Claude **subscription** (OAuth bearer + `anthropic-beta` are injected proxy-side).
- **opencode** — routed via an `OPENCODE_CONFIG` file that runtimed writes, which
  sends opencode's requests to the proxy with the dummy key. Which OpenCode Zen
  endpoint it uses is set by `SANDBOXD_OPENCODE_ZEN_PATH` — see
  [OpenCode Zen: subscription vs pay-as-you-go](#opencode-zen-subscription-vs-pay-as-you-go)
  below. (Implementation note: opencode's Zen provider ignores base-URL env vars
  and its Bun runtime rejects a bare hostname, so we can't just set a `*_BASE_URL`;
  the config file defines a custom `@ai-sdk/openai-compatible` provider pointed at
  the proxy's resolved **IP**, and the task's model is rewritten to `proxy/<id>`.)
- **codex** — parked: its ChatGPT-subscription backend is a WebSocket that can't be
  proxied yet, so codex is hidden from the run picker.

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
container env / `docker inspect`, task results, events, and logs. For claude-code
subscriptions the token isn't even on the container filesystem — it lives only
behind the control-plane proxy.

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
  starts fresh on the first task; send `true`/`false` to force the choice. Each
  sandbox is one workspace, so "most recent" is naturally per-sandbox.

## Configuration reference

Operator knobs for agent auth & task defaults (all optional; sensible defaults shown):

| Env var | Default | What it does |
| --- | --- | --- |
| `SANDBOXD_AGENT_PROXY_URL` | `http://sandboxd:9100` | In-network URL of the credential-injecting proxy. Set/empty toggles the proxy vs. legacy mounted-auth model. |
| `SANDBOXD_DEFAULT_AGENT` | `opencode` | Agent used for tasks that don't specify one. |
| `SANDBOXD_OPENCODE_ZEN_PATH` | `zen` | OpenCode Zen endpoint for opencode: `zen` (pay-as-you-go, full catalog) or `zengo` (the "go" subscription's models). See [OpenCode Zen: subscription vs pay-as-you-go](#opencode-zen-subscription-vs-pay-as-you-go). |
| `SANDBOXD_OPENCODE_MODEL` | *(unset)* | Global default `--model` for opencode tasks that don't pass one. |

Per-task, `POST /tasks` also accepts `agent`, `model`, and `continue` (tri-state —
see [Running a task](#running-a-task)).
