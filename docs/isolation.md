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
- **Runs as a non-root user** — the base image drops to `sandbox` (uid 1000),
  and `--userns` remaps it on the host, so a sandbox process is not host-root
  even before an escape is considered.
- **A fresh, disposable filesystem per app** — no host bind-mounts of your home
  or system dirs; destroy the sandbox and the code is gone.
- **No credentials inside the sandbox.** Agent provider keys/subscription tokens
  are held by the control plane and injected on the wire by a credential proxy —
  the untrusted workspace can neither read, exfiltrate, nor clobber them. (See
  [agent-auth.md](agent-auth.md).)
- **Public previews are treated as public** — framable by design, so never put a
  secret behind a preview URL and assume it's private.

**Be equally clear about what is *not* locked down by default:**

- **Network egress is open in the self-hosted build.** A sandbox is on a normal
  Docker bridge network, so it can reach the internet, your LAN, and cloud
  metadata endpoints. There *is* an nftables-based egress subsystem in the code
  (per-sandbox source tracking + connection logging + drop metrics), but it is
  **compiled off** in the docker-compose build (`egressMgr = nil`) because it
  needs host nftables scaffolding (the `inet sandbox_platform` table) that a
  plain deployment doesn't install — `/v1/settings` honestly reports egress
  `disabled`. It is also **source-based today** — it governs *whether* a sandbox
  is subject to a single host-defined policy, not a per-sandbox, per-domain
  allowlist. A configurable "allow only these domains, per sandbox" egress
  control is on the roadmap below. **Until then, run sandboxd on a host whose
  network you're comfortable exposing to the code it runs**, or place it on an
  isolated network segment.

## What sandboxd is (and isn't) good for today

- ✅ **Your own machine / a host you control**, building your own apps — the
  default, and what the one-line installer sets up (API on loopback only).
- ✅ **A trusted team** on a host you control, with auth on and the host placed
  on a network you're willing to expose (egress is open by default — see above).
- ⚠️ **Multi-tenant / hostile users sharing one host** — a container escape is a
  cross-tenant escape. Don't run mutually-distrusting tenants on one kernel until
  the VM-isolation work below lands. Give each tenant their own host/VM instead.
- ❌ **Never** expose the control-plane API to the internet without token auth —
  it's host-root. See [production-safety.md](production-safety.md).

## The roadmap to a real boundary

The container is the boundary *today*; it isn't the destination. The plan, in
order of impact:

1. **Per-sandbox egress allowlist** — wire the existing nftables subsystem into
   the self-hosted build and add **domain-level** rules ("allow only
   `api.stripe.com` + npm, deny the rest") per sandbox, via an L7 egress proxy
   (the credential proxy generalized to all outbound traffic). This is the
   nearest-term, highest-leverage control — it doesn't need a new kernel or VM.
2. **gVisor (`runsc`) as the sandbox runtime** — a user-space kernel that turns a
   container escape into an escape from a sandboxed syscall surface. Opt-in
   first, then default for the multi-tenant profile.
3. **Firecracker/Cloud-Hypervisor microVMs** for the hostile-multi-tenant case —
   a real VM boundary per sandbox, at near-container startup cost.
4. **seccomp profile tightening** on the sandbox container.

We'd rather ship an honest container story with a credible VM roadmap than
market "secure isolation" over a shared kernel. If isolation is your gating
concern, track these items — and until then, **one host per trust domain**.

## Reporting

Security issues: **do not open a public issue** — use
[private vulnerability reporting](https://github.com/tastyeffectco/sandboxd/security/advisories/new).
Full policy and the in-scope/out-of-scope trust model: [SECURITY.md](../SECURITY.md).
