# Isolation &amp; the security model

sandboxd runs AI-generated code you didn't write. That code is untrusted, so the
honest question is: **what is the boundary around it, and what is that boundary
actually worth?** This page answers that directly — no hand-waving. If you're
evaluating sandboxd for anything beyond your own laptop, read this first.

## What runs where

There are two kinds of container, and they are not equally trusted:

- **The sandbox** — one throwaway Docker container per app. The agent
  (opencode/claude-code) and the code it writes run *here*, with
  `--dangerously-skip-permissions`. Everything you don't trust lives inside this
  container.
- **The control plane** — orchestrates the sandboxes. It mounts the host Docker
  socket, so it is **effectively host-root**. Nothing untrusted runs here.

The boundary around untrusted code is therefore **the sandbox container** — a
Linux container, not a VM.

## Be clear-eyed: a container is not a security boundary against a determined attacker

A shared-kernel Linux container is a strong *isolation* boundary and a weak
*security* boundary. A container escape (kernel bug, misconfiguration) drops the
attacker onto the host. We don't pretend otherwise, and you shouldn't deploy as
if a container were a VM. What the sandbox container **does** give you today:

- **A locked-down container** — `--cap-drop=ALL`, `--security-opt
  no-new-privileges`, a **read-only root filesystem** (writes go to tmpfs +
  the app workspace), and memory / PID / file-descriptor limits.
- **A fresh, disposable filesystem per app** — no host bind-mounts of your home
  or system dirs; destroy the sandbox and the code is gone.
- **No credentials inside the sandbox.** Agent provider keys/subscription tokens
  are held by the control plane and injected on the wire by a credential proxy —
  the untrusted workspace can neither read, exfiltrate, nor clobber them. (See
  [agent-auth.md](agent-auth.md).)
- **Public previews are treated as public** — framable by design, so never put a
  secret behind a preview URL and assume it's private.

**Be equally clear about what is *not* locked down by default:**

- **Egress is open by default in the OSS build.** A sandbox can reach the network
  — your LAN and cloud metadata endpoints included. The per-sandbox egress
  policy (default-deny, allow only the model provider + preview) is not wired in
  the plain docker-compose deployment; scoping egress is on the roadmap below.
  Until then, run sandboxd on a host whose network you're comfortable exposing to
  the code it runs, or put it on an isolated network segment.
- **The container may run as root** unless your base image drops to an
  unprivileged user — combined with the open egress, another reason not to share
  one host across trust domains yet.

## What sandboxd is (and isn't) good for today

- ✅ **Your own machine / a host you control**, building your own apps — the
  default, and what the one-line installer sets up (API on loopback only).
- ✅ **A trusted team** on a host you control, with auth on and egress scoped.
- ⚠️ **Multi-tenant / hostile users sharing one host** — a container escape is a
  cross-tenant escape. Don't run mutually-distrusting tenants on one kernel until
  the VM-isolation work below lands. Give each tenant their own host/VM instead.
- ❌ **Never** expose the control-plane API to the internet without token auth —
  it's host-root. See [production-safety.md](production-safety.md).

## The roadmap to a real boundary

The container is the boundary *today*; it isn't the destination. The plan, in
order of impact:

1. **gVisor (`runsc`) as the sandbox runtime** — a user-space kernel that turns a
   container escape into an escape from a sandboxed syscall surface. Opt-in
   first, then default for the multi-tenant profile.
2. **Firecracker/Cloud-Hypervisor microVMs** for the hostile-multi-tenant case —
   a real VM boundary per sandbox, at near-container startup cost.
3. **seccomp profile tightening** and a default-deny egress policy per sandbox.

We'd rather ship an honest container story with a credible VM roadmap than
market "secure isolation" over a shared kernel. If isolation is your gating
concern, track these items — and until then, **one host per trust domain**.

## Reporting

Security issues: **do not open a public issue** — use
[private vulnerability reporting](https://github.com/tastyeffectco/sandboxd/security/advisories/new).
Full policy and the in-scope/out-of-scope trust model: [SECURITY.md](../SECURITY.md).
