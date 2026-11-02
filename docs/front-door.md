# Servers: the front door

On a server, something has to own ports 80 and 443 and https, and pass each
request on to sandboxd's router (Traefik), which picks the app by hostname.
That something is the **front door**. It is chosen when the engine is
installed (`install.sh --front=…`) and the desktop sets it up for you; this
page is the reference behind it. Design: [ADR 0003](adr/0003-go-live-on-your-own-server.md),
sections 3 and 4.

Installs without `--front` (the desktop app on your computer, `./install.sh` on
a laptop) are unchanged: Traefik listens on port 80 and is its own front door.

## Install

```bash
curl -fsSL https://raw.githubusercontent.com/tastyeffectco/sandboxd/main/install.sh \
  | sudo bash -s -- --front=auto
```

| Flag | Meaning |
|---|---|
| `--front=auto` | `caddy` when ports 80 and 443 are both free, else `existing`. On a re-run, keeps the front door chosen last time. |
| `--front=caddy` | sandboxd runs Caddy in a container on 80/443 (compose profile `caddy`). Fails if the ports are taken. |
| `--front=existing` | Your nginx or Caddy stays in charge; the desktop adds one site per app through the front-door helper. |
| `--front=manual` | You connect your web server yourself (forward to `127.0.0.1:8088`, keep the `Host` header). |
| `--domain=<D>` | App addresses go under `D`. Default: `<public-ip-with-dashes>.sslip.io`, which needs no DNS. |
| `--desktop-key="<ssh public key>"` | Lets the desktop in as the limited `sandboxd` user (see below). Re-running with the same key replaces its line. |

With `--front`, the installer writes to `.env`:

| Key | Value | Why |
|---|---|---|
| `SANDBOXD_FRONT` | `caddy`, `existing` or `manual` | Remembered for re-runs, `upgrade.sh` and `frontdoor facts` |
| `PREVIEW_DOMAIN` | `--domain` or `<ip>.sslip.io` | |
| `PREVIEW_HOST_STYLE` | `flat` | Hosts are `s-<id>-<port>.<D>`, `console.<D>`, `api.<D>`: one level under `D`, which sslip.io resolves and Let's Encrypt can issue for |
| `PREVIEW_URL_SCHEME` | `https` | Public URLs are `https://…` with no port |
| `SANDBOXD_HTTP_BIND`, `HTTP_PORT` | `127.0.0.1`, `8088` | Traefik is only the internal router |
| `SANDBOXD_RATELIMIT_MIDDLEWARE` | `sandbox-ratelimit-proxied@file` | Per-client rate limit for production apps, trusting the one front door in front of Traefik |
| `COMPOSE_PROFILES` | `caddy` (caddy only) | Plain `docker compose up -d` keeps Caddy |
| `SANDBOXD_CONSOLE` | `0` unless already `1` | The desktop is the client, over SSH. A public console would let the first visitor choose its password. |

The last line of output is machine-readable:

```
SANDBOXD_INSTALL_RESULT {"front":"caddy","domain":"203-0-113-7.sslip.io","proxy":"none","api_token":"sk_…","api":"127.0.0.1:9090"}
```

`proxy` is the web server found on the host: `nginx`, `caddy`, `apache`,
`traefik`, `coolify` or `none`. `api_token` is the bootstrap key in
`SANDBOXD_API_TOKENS` (the `default=` entry, else the first one). `api` is
where the engine API listens on the server; reach it through the SSH tunnel.

## The managed Caddy (`--front=caddy`)

`caddy/Caddyfile`, image `caddy:2.11.2-alpine`, data under
`$SANDBOXD_DATA_DIR/caddy`. Every https name gets a certificate on demand, the
first time it is visited, but only if the engine answers 200 to
`GET http://sandboxd:9000/edge/tls-allowed?domain=<host>`:

- a preview hostname under `PREVIEW_DOMAIN` whose sandbox exists, or
- the console or API hostname.

Anything else gets 404, so nobody can make the server request certificates
for random names. The endpoint needs no credential, is not under `/v1`, and
refuses requests relayed through Traefik (they carry `X-Forwarded-For`).
Plain http redirects to https; ACME challenges still work.

## The limited `sandboxd` user

With `--front`, the installer also creates:

- a system user `sandboxd` with no password (shell `/bin/sh`, needed to run
  its forced command);
- `/usr/local/lib/sandboxd/frontdoor` and `frontdoor-ssh`, owned by root;
- `/etc/sudoers.d/sandboxd-frontdoor`: `sandboxd` may run
  `/usr/local/lib/sandboxd/frontdoor` as root without a password, and nothing
  else;
- with `--desktop-key`, one line in `~sandboxd/.ssh/authorized_keys`:

  ```
  restrict,port-forwarding,permitopen="127.0.0.1:9090",command="/usr/local/lib/sandboxd/frontdoor-ssh" ssh-ed25519 AAAA…
  ```

  The key can forward `127.0.0.1:9090` (the engine API: `ssh -N -L 9090:127.0.0.1:9090 sandboxd@server`)
  and run front-door commands. It cannot open a shell, a pty, copy files, or
  forward anything else.

`uninstall.sh` removes the user, the sudo rule and the helpers. Sites the
helper added to your nginx or Caddy stay; it lists them.

## Front-door commands

Over SSH as `sandboxd`: `ssh sandboxd@server frontdoor <command> …`. The
forced command accepts only these words, built from `[a-z0-9.-]`, re-checks
every argument, and runs `sudo -n /usr/local/lib/sandboxd/frontdoor …`.
Anything else, including an empty command, prints a refusal and exits 1.

| Command | Does |
|---|---|
| `frontdoor facts` | Reads the server's web setup (changes nothing) |
| `frontdoor apply <site> <host> [host…]` | Publishes the hosts through your nginx or Caddy, forwarding to the router |
| `frontdoor remove <site>` | Takes the site off again |
| `frontdoor status <site>` | Says what is configured for the site |
| `frontdoor enable-caddy-import` | Adds `import /etc/caddy/sandboxd/*.caddy` to `/etc/caddy/Caddyfile`. Show this to the user and get approval first. |

`<site>` matches `^[a-z0-9][a-z0-9-]{0,62}$`; hosts are lowercase DNS names
with at least one dot, no wildcards, up to 20 per site.

Progress goes to stderr. stdout is one JSON line:

```json
{"ok":true,"step":"done","message":"The site is live over https.","https":true,"proxy":"nginx","certbot":true}
```

`ok` and `message` (plain words, fit to show) are always there. `step` names
where it stopped: `done`, `facts`, `status`, `usage`, `refused`, `lock`,
`permission`, `no-proxy`, `nginx-layout`, `nginx-test`, `reload`, `certbot`,
`caddy-import`, `caddy-validate`, `caddy-missing`, `backup` or `write`. Extra
fields per command:

- `facts`: `os`, `arch`, `proxies` (list), `ports` (`{"80":…,"443":…}`: `none`,
  `nginx`, `caddy`, `apache`, `docker`, `unknown` or a process name),
  `nginx_layout` (`sites-available`, `conf.d` or `none`), `caddy_layout`
  (`import` when the sandboxd import line is there, `caddyfile`, `none`),
  `certbot` (bool), `target` (`nginx`, `caddy` or `none`: what `apply` will
  configure), `front` (from `.env`), `router` (`127.0.0.1:8088`).
- `apply`: `proxy`, `https` (bool), and `certbot` (bool) for nginx.
- `status`: `configured`, and when configured `proxy`, `file`, `managed`,
  `hosts`, `https`.
- `enable-caddy-import`: `changed`.

How each change is made:

- **nginx**: `/etc/nginx/sites-available/sandboxd-<site>.conf` plus its
  `sites-enabled` link (or `/etc/nginx/conf.d/sandboxd-<site>.conf`), proxying
  to `http://127.0.0.1:8088` with the `Host` header, websockets and
  `X-Forwarded-*`. Then `nginx -t`, reload, and
  `certbot --nginx -d … --non-interactive --agree-tos --register-unsafely-without-email --redirect`
  when certbot is installed (without it the site works over http and the
  result says `"https":false`).
- **Your Caddy**: `/etc/caddy/sandboxd/<site>.caddy` (`<hosts> { reverse_proxy 127.0.0.1:8088 }`),
  `caddy validate`, reload. Caddy gets the certificate itself. Needs the
  import line; without it `apply` fails with step `caddy-import`.

Every change is backed up first (`/var/backups/sandboxd-frontdoor`), tested,
and reloaded. If any step fails, including certbot, the previous files are put
back and the web server is reloaded. The helper only writes or removes files
whose first line is `# managed by sandboxd`, plus that one import line. It
refuses to start if your configuration is already broken.

## Rate limiting

`traefik/dynamic/ratelimit.yml` defines two Traefik middlewares, 100 requests
per second per client on average with bursts of 200:

- `sandbox-ratelimit`: the client is the connection's address (Traefik faces
  the internet);
- `sandbox-ratelimit-proxied`: the client is the address the front door added
  to `X-Forwarded-For`.

Traefik keeps `X-Forwarded-*` only from loopback and private addresses (the
front door on the same host or Docker network), so clients cannot forge them.
The middleware named in `SANDBOXD_RATELIMIT_MIDDLEWARE` is meant for
production app routers; previews are never rate limited.
