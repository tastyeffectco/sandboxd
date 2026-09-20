# Telemetry

sandboxd sends a small, anonymous daily heartbeat so the project can see how
many instances run, which versions, whether installs and upgrades succeed, and
which features are used — and so it can tell you when a newer release exists.
Telemetry is **on by default** and easy to turn off. Everything it sends is
listed here; the code that builds it is short:
[`internal/telemetry`](../control-plane/internal/telemetry).

## Opting out

Put either line in `.env` (both are honoured, restart the stack after):

```sh
SANDBOXD_TELEMETRY=off   # also accepts 0, false, no
DO_NOT_TRACK=1           # the cross-tool standard; also accepts true, yes
```

With telemetry off nothing is sent — no heartbeat, no install or upgrade event,
and `install.sh` sends no failure beacon. The random `instance-id` file may still
exist from a prior run; it is never sent while telemetry is off.

## What is collected

**Design rule:** every number is a *bucket*, every string is an *enumerated
label*. No hostnames, IP addresses, file paths, domain names, app or sandbox
names, prompts, code, tokens, or environment values ever leave the host.

### `heartbeat` (on start, then every 24 h) and `install` (once, first start)

| Field | Values | What it tells us |
| --- | --- | --- |
| instance UUID | random v4, generated locally | counts instances; not derived from any host detail |
| `version` | `v0.3.14` | which releases are in use |
| `os`, `arch` | `linux`, `arm64` | platforms to test on |
| `sandbox_bucket` | `0`, `1-3`, `4-10`, `10+` | rough size of the install |
| `apps_bucket` | same buckets | whether the instance is actually used |
| `tasks_7d_bucket` | same buckets, coding tasks in the last 7 days | whether agents are actually run |
| `auth_enabled` | `true`/`false` | whether API auth is on |
| `console_enabled` | `true`/`false` | whether the web console has a password set |
| `preview_kind` | `lan`, `ip`, `domain`, `tunnel` | whether previews are reachable from the internet — **the domain itself is never sent** |
| `preview_tls` | `true`/`false` | whether previews are served over HTTPS |
| `agent_default` | provider name, e.g. `opencode` | which agents to invest in |
| `runtime` | `runc`, `gvisor`, `other` | whether hardening is used |
| `egress_mode` | e.g. `disabled`, `allowlist` | same |
| `storage_mode` | e.g. `directory` | same |
| `install_method` | `install.sh`, `bootstrap`, `cloud-init`, `unknown` | which install path people take |
| `docker_major` | `27` | which Docker versions to support |
| `cpu_bucket` | `1-2`, `3-4`, `5-8`, `9+` | sizing guidance |
| `mem_bucket` | `<4g`, `4-8g`, `8-16g`, `16g+` | sizing guidance |
| `$ip` | always `""` | tells the collector to drop the request IP (no geolocation, no storage) |

When an operator explicitly attaches the instance to **sandboxd Cloud**, the
Cloud installer sets `SANDBOXD_CLOUD_ORG`. Heartbeats then include that Cloud
organization and use `cloud:<org>` as their analytics identity. This connects
the subscriber funnel to the same bucketed usage fields above. It still sends
no app names, prompts, code, paths, domains, secrets, or exact usage counts.

### `upgrade` (once per finished console-driven upgrade)

| Field | Values |
| --- | --- |
| `from`, `to` | release tags, e.g. `v0.3.13` → `v0.3.14` |
| `result` | `succeeded`, `failed`, `rolled_back` |
| `source` | `console` (CLI upgrades via `./upgrade.sh` are not reported) |

This is how we learn whether in-place upgrades and their automatic rollback
work outside our own machines.

### `install_failed` (from `install.sh`, only when the installer aborts)

| Field | Values |
| --- | --- |
| `stage` | `prereqs`, `docker`, `env`, `data-dir`, `base-image`, `api-key`, `console`, `stack` |

One event with a random id and the stage name — no output, no error text, no
host details. A failed install is otherwise invisible to us, and the installer
is the part of sandboxd that most needs to work everywhere.

## Why

- **Install / version counts** — what to support and when to retire old behaviour.
- **Does it work?** — install failures by stage, upgrade rollbacks, previews that
  can't be shared. These decide what we fix first.
- **What gets used** — agents, hardening, apps per instance. These decide what we build.
- **Update notifications** — sandboxd reads the latest GitHub release and surfaces
  `update_available` in `GET /v1/settings`. That check runs regardless of the
  opt-out; it only *reads* the public releases API and sends nothing.

## How it works

- On first start a random UUID is written to `<data>/state/instance-id` (mode
  `0600`) and a one-time `install` event is sent.
- A `heartbeat` is sent at startup and then every 24 hours.
- Every send is best-effort with a short timeout. A slow or unreachable
  collector never blocks or crashes sandboxd.
- `docker-compose.yml` passes `SANDBOXD_TELEMETRY`, `DO_NOT_TRACK`,
  `SANDBOXD_POSTHOG_HOST`, `SANDBOXD_POSTHOG_KEY` and `SANDBOXD_INSTALL_METHOD`
  from `.env` into the control plane.

## Self-hosting the collector

By default events go to the project's PostHog capture endpoint (a write-only
key that can only append events). Point them at your own instance:

```sh
SANDBOXD_POSTHOG_HOST=https://your-posthog.example.com
SANDBOXD_POSTHOG_KEY=phc_your_write_key
```
