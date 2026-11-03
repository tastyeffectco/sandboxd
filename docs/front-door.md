# Servers: the front door

On a server, something has to own ports 80 and 443 and https, and pass each
request on to sandboxd's router (Traefik), which picks the app by hostname.
That something is the **front door**. It is chosen when the engine is
installed (`install.sh --front=…`). On the desktop, the assistant sets it up
for you and asks before each change on the server; this page is the reference
behind it. Design: [ADR 0003](adr/0003-go-live-on-your-own-server.md),
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
| `--front=existing` | Your nginx or Caddy stays in charge and forwards to `127.0.0.1:8088` (see [Your own web server](#your-own-web-server)). |
| `--front=manual` | You connect your web server yourself (forward to `127.0.0.1:8088`, keep the `Host` header). |
| `--domain=<D>` | App addresses go under `D`. Default: `<public-ip-with-dashes>.sslip.io`, which needs no DNS. |

With `--front`, the installer writes to `.env`:

| Key | Value | Why |
|---|---|---|
| `SANDBOXD_FRONT` | `caddy`, `existing` or `manual` | Remembered for re-runs and `upgrade.sh` |
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

## Your own web server

With `--front=existing` or `--front=manual`, sandboxd does not touch your web
server. Whoever sets it up (you, or the desktop's assistant with your
approval for each change) adds one site per published app that:

- forwards to `http://127.0.0.1:8088` (the router), keeping the `Host` header;
- passes websockets (`Upgrade` and `Connection` headers);
- sets `X-Forwarded-For` and `X-Forwarded-Proto`, which the rate limit and the
  app rely on;
- serves https for the app's hostname (certbot for nginx; your Caddy gets the
  certificate itself).

For example, with nginx:

```nginx
server {
    listen 80;
    server_name shop.203-0-113-7.sslip.io;
    location / {
        proxy_pass http://127.0.0.1:8088;
        proxy_set_header Host $host;
        proxy_set_header X-Forwarded-For $proxy_add_x_forwarded_for;
        proxy_set_header X-Forwarded-Proto $scheme;
        proxy_http_version 1.1;
        proxy_set_header Upgrade $http_upgrade;
        proxy_set_header Connection "upgrade";
    }
}
```

and with Caddy, `shop.example.com { reverse_proxy 127.0.0.1:8088 }`.

Test the configuration (`nginx -t`, `caddy validate`) before reloading, and
keep a copy of every file you change so it can be put back.

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
