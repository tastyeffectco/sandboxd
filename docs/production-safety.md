## Production Safety Checklist

sandboxd is beta infrastructure for authenticated users running agent-built apps.

It is not a VM security boundary for anonymous hostile code. If you run untrusted public workloads, use stronger isolation such as VM-per-tenant, gVisor, Kata, or Firecracker.

Before production use:

- [ ] Run sandboxd on a dedicated host or VM.
- [ ] Do not expose the control-plane API without auth.
- [ ] Set `SANDBOXD_API_AUTH_DISABLED=false`.
- [ ] Configure `SANDBOXD_API_TOKENS`.
- [ ] Use HTTPS with a real wildcard preview domain.
- [ ] Understand that preview URLs may be public unless protected.
- [ ] Keep the host kernel and Docker Engine patched.
- [ ] Back up workspaces and SQLite state under `SANDBOXD_DATA_DIR`.
- [ ] Monitor disk usage; per-sandbox disk quotas are not included yet.
- [ ] Monitor memory and active sandbox count.
- [ ] Review egress needs; outbound network access is open by default.
- [ ] Do not keep unrelated production secrets on the same host.

## Trust boundaries to understand

These are **intentional** design choices, not bugs — but you must understand
them before exposing sandboxd:

- **The control plane mounts the host Docker socket** (`/var/run/docker.sock`)
  to orchestrate sibling sandbox containers. That makes it effectively
  **host-root**. Run it only on a host you fully control; never expose the
  control-plane API to untrusted callers.
- **Previews are framable by any origin.** The edge deliberately forwards a
  trusted upstream `Host` (so dev servers like Vite stop returning 403) and
  **strips `X-Frame-Options`** on preview routes so the console can embed apps
  in an iframe. Consequently every preview — including `visibility:"public"`
  ones — can be embedded anywhere (clickjacking surface). Gate sensitive apps
  with `visibility:"private"` (per-sandbox forward-auth), and don't serve
  anything you wouldn't put on the open web from a public preview.
- **`passHostHeader=false`** on preview routes means an app that derives absolute
  URLs or cookies from the `Host` header sees the backend address, not the public
  preview host. Fine for dev previews; be aware if your app depends on it.
- **Agents run with `--dangerously-skip-permissions`.** The containment boundary
  is the throwaway sandbox container, not the agent — a task can run arbitrary
  commands inside its own sandbox by design.
- **Egress is unrestricted by default.** The nftables egress policy (block cloud
  metadata, RFC1918, cross-sandbox, SMTP, abuse lists) is disabled in the
  portable build; enable it (or front sandboxes with an egress proxy) for
  multi-tenant hardening.
