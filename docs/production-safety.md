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
