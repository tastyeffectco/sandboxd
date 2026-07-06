# Security Policy

sandboxd runs untrusted code by design: each app executes inside its own
Docker sandbox, and the control plane orchestrates those containers. Because of
that, we take security reports seriously and ask you to disclose them privately.

## Reporting a vulnerability

**Do not open a public issue for security problems.**

Report privately through **GitHub's private vulnerability reporting**:
[Security → Report a vulnerability](https://github.com/tastyeffectco/sandboxd/security/advisories/new).
If you cannot use GitHub advisories, email **security@tastyeffectco.dev** (fill in
your real contact) with the details.

Please include:

- affected component — **sandboxd core** (control plane / `runtimed` / Traefik
  config) or the **console** client;
- a description, impact, and reproduction steps or a proof of concept;
- affected version / commit and your deployment configuration (relevant `.env`).

We aim to acknowledge within **72 hours**, agree on a disclosure timeline, and
credit you in the release notes unless you prefer to remain anonymous.

## Scope & trust model

Some behaviors are **intentional** and are not vulnerabilities on their own —
know the trust model before reporting:

- The control-plane container mounts the host Docker socket to orchestrate
  sibling sandboxes. It is effectively host-root; run it on a host you control.
- Agents run inside the throwaway sandbox container with
  `--dangerously-skip-permissions`; the containment boundary is the container,
  not the agent.
- `SANDBOXD_API_AUTH_DISABLED` defaults to `true` for local use and the API is
  published on loopback only. Exposing it without token auth is a
  misconfiguration, not a bug. See `docs/production-safety.md`.
- Public preview routes are intentionally framable (the edge strips
  `X-Frame-Options` so the console can embed them). Treat previews as public.

In scope: sandbox escape, cross-sandbox access, credential/secret disclosure,
auth bypass, control-plane RCE, SSRF against the host/metadata, and privilege
escalation beyond the documented trust model.

## Supported versions

Security fixes land on the latest release line. Until 1.0, only the most recent
minor (`0.x`) is supported — please upgrade before reporting.
