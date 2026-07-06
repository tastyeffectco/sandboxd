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

### claude-code subscriptions → credential-injecting proxy (the real token never enters the sandbox)

For a Claude subscription, sandboxd runs a **reverse proxy control-plane-side**
(`internal/authproxy`) that holds the real credential. The sandbox's agent is
pointed at it and given a **dummy** key:

- `ANTHROPIC_BASE_URL` = the proxy (`RUNTIMED_ANTHROPIC_PROXY`)
- `ANTHROPIC_API_KEY` = `sandboxd-proxy-injected` (just enough for the CLI to
  skip its local "Not logged in" gate)

The CLI's requests hit the proxy, which swaps in the real `Authorization:
Bearer …`. **The real subscription token is never mounted into the sandbox and
never appears in its filesystem or env** — a task cannot read or exfiltrate it.

### opencode / codex → mounted auth dir

For the other providers, the connected provider's opaque auth dir
(`<DataDir>/agent-auth/<provider>/`, outside any workspace) is bind-mounted
read-only at `/run/agent-auth/<provider>` — never under `/home/sandbox`, so it
can't be copied into a workspace or snapshot. At task time runtimed points the
agent's `HOME` there.

### API-key connect → one injected var

If a provider was connected by API key, runtimed injects **exactly one** env var
(the provider's key var — `ANTHROPIC_API_KEY` / `OPENAI_API_KEY`) from the stored
file. This is the single deliberate exception to the env scrub below.

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

Submit `POST /v1/sandboxes/{id}/tasks` with `{"prompt":"…","agent":"claude-code","model":"sonnet"}`.
runtimed runs the CLI in the workspace (`--dangerously-skip-permissions`, since
the containment boundary is the throwaway sandbox, not the agent), streams events
over SSE (`/tasks/{taskId}/events`), and returns a final result. `model` maps to
the CLI's `--model`. `SANDBOXD_DEFAULT_AGENT` picks the agent for tasks that
don't specify one.
