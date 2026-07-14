# Telemetry

sandboxd sends a small, anonymous usage heartbeat so the project can see how
many instances are running and which versions are in use, and so it can tell you
when a newer release is available. Telemetry is **on by default** and easy to
turn off.

## What is collected

Each heartbeat contains only these fields:

| Field             | Example        | Notes                                                        |
| ----------------- | -------------- | ------------------------------------------------------------ |
| instance UUID     | `9f1c…` (v4)   | Random, generated locally. Not derived from any host detail. |
| `version`         | `v0.3.0`       | The running build version.                                   |
| `arch`            | `arm64`        | CPU architecture (`GOARCH`).                                 |
| `os`              | `linux`        | Operating system (`GOOS`).                                   |
| `sandbox_bucket`  | `1-3`          | Coarse bucket of the sandbox count: `0`, `1-3`, `4-10`, `10+`. Never an exact number. |
| `auth_enabled`    | `true`         | Whether API auth is enabled.                                 |
| `console_enabled` | `false`        | Whether the web console has an admin password set.           |

The request IP address is **not** stored: every event is sent with an empty
`$ip`, which tells the collector to drop it (no geolocation, no storage).

## What is NOT collected

No hostnames, no IP addresses, no file paths, no environment values, no API
tokens or credentials, no sandbox names, and no user or code content of any
kind. The instance UUID is random and cannot be traced back to a machine.

The full payload is built in
[`internal/telemetry`](../control-plane/internal/telemetry) — it is short and
worth reading if you want to confirm exactly what leaves the host.

## Why

- **Install / version counts** — a rough sense of how many instances run each
  version, which guides what to support and when to retire old behaviour.
- **Update notifications** — sandboxd checks the latest GitHub release and
  surfaces `update_available` in `GET /v1/settings` so the console can tell you
  when to upgrade. (The update check runs regardless of the telemetry opt-out;
  it only *reads* the public releases API and sends nothing.)

## How it works

- On first start, a random UUID is written to `<data>/state/instance-id`
  (mode `0600`) and a one-time `install` event is sent.
- A `heartbeat` event is sent at startup and then every 24 hours.
- Every send is best-effort with a short timeout. A slow or unreachable
  collector never blocks or crashes sandboxd.

## Opting out

Set either of these before starting sandboxd:

```sh
SANDBOXD_TELEMETRY=off   # also accepts 0, false, no
# or the cross-tool standard:
DO_NOT_TRACK=1           # also accepts true, yes
```

With telemetry disabled, no heartbeat is sent and no instance UUID event is
emitted. (The random `instance-id` file may still exist from a prior run; it is
never sent while telemetry is off.)

### Self-hosting the collector

By default events go to the project's PostHog capture endpoint. You can point
them at your own instance:

```sh
SANDBOXD_POSTHOG_HOST=https://your-posthog.example.com
SANDBOXD_POSTHOG_KEY=phc_your_write_key
```
