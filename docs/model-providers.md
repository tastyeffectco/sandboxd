# Local and remote models with OpenCode and Pi

Choose the coding agent independently of the model server. **OpenCode** and
**Pi** can both use operator-configured **OpenAI-compatible Chat Completions**
or **Anthropic-compatible Messages** APIs. This includes local model servers
and gateways such as LiteLLM. No provider-specific SDK or AWS credentials are
required in sandboxd core.

The base image installs OpenCode **1.19.3** and Pi **1.3.0**
(`@earendil-works/pi-coding-agent`). Rebuild/upgrade the base image and recreate
existing sandboxes to install these versions; saving a connection itself needs
no restart.

## Console setup

1. Open **Settings → Model connections → Add connection**.
2. Enter a name, API format, API base URL, and optional API key.
3. Add the server's exact model IDs or gateway aliases. Slashes and colons are
   preserved. The first model is the connection's default.
4. Set each model's context window and maximum output under **Token limits**.
   Defaults are 32,768 context tokens and 4,096 output tokens; adjust them to
   match your server.
5. Save, then choose a model and click **Test model**. This explicitly sends a
   small inference request; remote provider charges can apply.
6. Open an app's **Agent** panel. Choose **OpenCode** or **Pi**, then the
   connection and model. Send a prompt as usual.

Pi requires a connection. OpenCode also retains its existing built-in
provider/free-tier flow when **Built-in provider** is selected.

## Endpoint examples

These are examples of addresses reachable **from the control-plane container**,
not from your browser or the sandbox:

| Server | API format | Example base URL |
| --- | --- | --- |
| Ollama on the same Docker network | OpenAI-compatible | `http://ollama:11434/v1` |
| LM Studio on a reachable host | OpenAI-compatible | `http://192.168.1.10:1234/v1` |
| vLLM / llama.cpp on the same network | OpenAI-compatible | `http://model-server:8000/v1` |
| LiteLLM gateway | Either supported format | `http://litellm:4000/v1` |
| Anthropic API | Anthropic-compatible | `https://api.anthropic.com/v1` |
| Hosted compatible gateway | Its supported format | `https://models.example.com/api/v1` |

Include the API prefix in the base URL. sandboxd appends `/chat/completions`
or `/messages`. It preserves an existing path prefix. Use a model with
streaming and tool/function calling support for coding tasks.

`localhost` inside the control-plane container means that container. For a
model running on your machine, use a reachable host address, a shared Docker
network name, or a host-gateway mapping appropriate to your Docker setup.
HTTP is supported for trusted local networks; use HTTPS for remote endpoints.
The API key can be omitted for a server that does not require authentication.

### LiteLLM and AWS Bedrock

Configure the Bedrock model and its AWS authentication **in LiteLLM** following
[LiteLLM's Bedrock guide](https://docs.litellm.ai/docs/providers/bedrock).
In sandboxd, enter the LiteLLM URL, its gateway API key, and the gateway's
`model_name` alias. Choose OpenAI-compatible Chat Completions or LiteLLM's
[Anthropic-compatible Messages endpoint](https://docs.litellm.ai/docs/anthropic_unified).

sandboxd talks to the gateway; LiteLLM handles AWS authentication and model
routing. This is gateway support for [#119](https://github.com/tastyeffectco/sandboxd/issues/119),
not native AWS SigV4 support. AWS account access, region permissions, and
individual Bedrock models must be verified in your own gateway.

## API

Use your normal sandboxd API authentication. See [OpenAPI](openapi.yaml) for
the full schema.

```sh
# Local keyless connection. Set API and SANDBOXD_API_KEY for your instance.
curl "$API/v1/model-providers" \
  -H "Authorization: Bearer $SANDBOXD_API_KEY" \
  -H 'Content-Type: application/json' \
  -d '{
    "name": "Local coder",
    "protocol": "openai-completions",
    "base_url": "http://ollama:11434/v1",
    "models": [
      {"id": "qwen2.5-coder:7b", "context_window": 32768, "max_tokens": 4096}
    ]
  }'

# Use the returned connection id. The same connection works with "opencode".
curl "$API/v1/sandboxes/$SANDBOX_ID/tasks" \
  -H "Authorization: Bearer $SANDBOXD_API_KEY" \
  -H 'Content-Type: application/json' \
  -d '{
    "agent": "pi",
    "provider": "CONNECTION_ID",
    "model": "qwen2.5-coder:7b",
    "prompt": "Build a small todo app",
    "continue": true
  }'
```

For an authenticated gateway, supply `api_key` on creation. It is write-only:
list/create/update responses return `has_api_key`, never the key.
`PUT /v1/model-providers/{id}` replaces the public configuration. Omit
`api_key` to keep the saved key, or send `""` to remove it.
`DELETE /v1/model-providers/{id}` removes the connection.
`POST /v1/model-providers/{id}/test` with `{"model":"exact-model-id"}` performs
one small inference request. An empty object tests the first model.

An omitted task model selects the connection's first model. Explicit models
must exist in that connection. There is no silent fallback to a hosted provider
if a custom connection fails. Custom connections currently serve OpenCode and
Pi; Claude Code's existing subscription/API-key flow remains separate.

## Credential and network boundary

Connections are instance-wide operator settings, protected by the regular
control-plane API authentication. Treat unrestricted API keys as administrative
access. The `upgrade` scope does not grant access to connection management.

- API keys are encrypted in SQLite with the instance's AES-GCM secrets key.
  Back up the database and `secrets.key` together.
- The upstream URL and key stay on the control plane. Runtimed receives the
  proxy URL, selected model, and a signed capability that expires after the task
  timeout plus one minute.
- Capabilities permit inference only for that connection and exact model.
  Updating/deleting a connection or restarting the control plane invalidates
  existing capabilities. Requests already in flight may finish.
- The proxy permits local/private addresses, blocks link-local/metadata,
  multicast and unspecified addresses, validates DNS results when dialing, and
  never follows upstream redirects.
- Client cookies/auth headers and gateway-routing overrides are not forwarded.
  Upstream error bodies and response cookies are removed to avoid accidental
  disclosure. Use the gateway's own logs to diagnose detailed upstream errors.
- Agent configuration files containing temporary capabilities are removed
  after each task. Conversation state is separate for each agent, connection,
  and model under runtimed's state directory, outside the app workspace.

The configured gateway is a trusted operator dependency: it receives prompts,
tool results, and its own API credential. A temporary capability is usable by
the sandbox for the selected model until it expires; it is not a spend budget.
This feature does not change the existing built-in credential proxy's access
model.

## Verification

Regular Go tests cover persistence, API dispatch, protocol routing, credential
redaction, model restrictions and connection revocation. Console tests cover
creation, editing, testing and choosing a connection.

The opt-in real-agent suite runs the pinned CLIs against deterministic local
protocol servers **through the actual credential proxy**. It checks real file
tool execution, model aliases, usage, continuation, provider errors and
cancellation. CI runs it on Linux amd64 and arm64:

```sh
# Install the versions pinned in image/Dockerfile, then:
cd control-plane
SANDBOXD_TEST_REAL_AGENTS=1 go test ./cmd/runtimed -run TestRealProviderAgents -v
```

For a live model behind a gateway supporting both formats:

```sh
cd control-plane
SANDBOXD_TEST_LIVE_PROVIDER_URL=http://127.0.0.1:4000/v1 \
SANDBOXD_TEST_LIVE_MODEL=your-gateway-alias \
SANDBOXD_TEST_LIVE_API_KEY=your-test-gateway-key \
go test ./cmd/runtimed -run TestLiveCompatibleProviders -v -timeout=50m
```

This executes four real coding tasks and can incur provider charges. It is
never enabled automatically. Each task must create the requested file using
the real model and agent tools.

Browser workflow checks run with
`cd console && pnpm exec playwright test --config playwright.providers.config.ts`.
They use API fixtures; API and real-agent integration are tested separately.
