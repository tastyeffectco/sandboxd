# Deploy sandboxd to a VPS

sandboxd runs on a single Linux server with Docker — nothing else. Two ways to
install:

**1. cloud-init (zero SSH).** Paste [`cloud-init.yaml`](cloud-init.yaml) into your
provider's *user data* field when creating the server — it installs on first boot.

**2. bootstrap script (after you SSH in).** On a fresh server:

```bash
curl -fsSL https://raw.githubusercontent.com/tastyeffectco/sandboxd/main/deploy/bootstrap.sh | sudo bash
```

Prefix `PREVIEW_DOMAIN=sandboxd.example.com` to serve previews at
`*.preview.<domain>`.

## Previews on a real domain

Set `PREVIEW_DOMAIN` (in [`cloud-init.yaml`](cloud-init.yaml), or as an env var for
[`bootstrap.sh`](bootstrap.sh)) and add a **wildcard DNS record** —
`*.preview.<domain>` → the server's IP. For HTTPS, see
[Production / TLS](https://sandboxd.io/guides/production-tls). Without a domain,
previews are served on the server IP / `localhost`.

## Where to paste cloud-init (user-data), per provider

| Provider | Where to paste `cloud-init.yaml` | Console |
|---|---|---|
| DigitalOcean | Create Droplet → **Advanced options → Add Initialization scripts (user data)** | https://cloud.digitalocean.com |
| Vultr | Deploy → **Cloud-Init User-Data** (Additional Features) | https://my.vultr.com |
| Linode / Akamai | Create Linode → **Add User Data** (or use the StackScript below) | https://cloud.linode.com |
| Kamatera | Create Server → **Server Options → Script / Cloud-Init** | https://console.kamatera.com |
| OVHcloud | Instance creation → **user data / post-installation script** | https://www.ovhcloud.com |
| Contabo | Setup → **Cloud-Init** (choose a cloud-init–capable image) | https://my.contabo.com |
| Hostinger | Create VPS → OS with **cloud-init / setup script** | https://www.hostinger.com/vps-hosting |
| IONOS | Create server → **Cloud-Init user data** | https://www.ionos.com |

> **Linode StackScript.** Linode also supports a one-click StackScript
> ([`linode-stackscript.sh`](linode-stackscript.sh)) with fields for your preview
> domain and an optional API token (which turns on API auth). Create a StackScript
> from that file, then deploy a Linode from it.

## Sizing

sandboxd is dense — idle sandboxes sleep and free their RAM — so start small and
grow:

| Tier | vCPU | RAM | Disk | Good for |
|---|---|---|---|---|
| Kick the tires | 1 | 2 GB | 25 GB | 1–2 light apps |
| **Recommended start** | **2** | **4 GB** | **50 GB** | a handful of apps |
| Small production | 4 | 8 GB | 80 GB | ~10–20 apps |
| Busy | 8 | 16 GB+ | 160 GB+ | many concurrent apps |

**RAM is the limit that matters** — each running sandbox holds memory until it
idles. Add swap and raise the disk if your apps install large dependencies.

## After it's up

- **Console:** `cd /opt/sandboxd && ./console-login.sh` prints the URL + login.
- **API:** `http://<server-ip>:9090` — published on loopback by default; read
  [Configuration → Authentication](https://sandboxd.io/reference/configuration)
  before exposing it, and enable API auth.
- **HTTPS / production:**
  [sandboxd.io/guides/production-tls](https://sandboxd.io/guides/production-tls).
- **Re-run / update:** the bootstrap is idempotent — re-running it updates the
  checkout and preserves your `.env`.
