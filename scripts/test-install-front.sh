#!/usr/bin/env bash
# install.sh --front=… decisions in a disposable fixture: port/proxy probing,
# the front door chosen, the .env it writes, the machine-readable result line,
# and that installs without --front are unchanged. Docker, ss, curl, sudo and
# the root-only user setup are stubbed; nothing on the host is touched.
# (The user/sudo/helper setup itself runs for real in scripts/test-frontdoor.sh.)
set -euo pipefail
[ "$(uname -s)" = Linux ] || { echo "SKIP: --front is Linux-only"; exit 0; }
ROOT="$(cd "$(dirname "$0")/.." && pwd)"
TMP="$(mktemp -d "${TMPDIR:-/tmp}/sandboxd-fronttest.XXXXXX")"
trap 'rm -rf "$TMP"' EXIT
mkdir -p "$TMP/bin" "$TMP/repo/control-plane" "$TMP/repo/scripts/frontdoor"
cp "$ROOT/install.sh" "$TMP/repo/install.sh"
touch "$TMP/repo/docker-compose.yml" "$TMP/repo/console-login.sh" "$TMP/repo/upgrade.sh"
# The real front-door library, with the root-only user setup recorded instead.
{ cat "$ROOT/scripts/install-front.sh"; cat <<'EOF'
front_install_user() { printf '%s|%s|%s\n' "$1" "$2" "${3:-}" > "$USER_LOG"; }
EOF
} > "$TMP/repo/scripts/install-front.sh"
cat > "$TMP/repo/scripts/install-images.sh" <<'STUB'
acquire_images() { SANDBOXD_IMAGE=base:test; SANDBOXD_CONTROL_PLANE_IMAGE=api:test; SANDBOXD_CONSOLE_IMAGE=console:test; }
STUB

# docker: everything succeeds; `ps` for our own compose project reports the
# ports in $FAKE_SELF (sandboxd's own Caddy/Traefik holding them).
cat > "$TMP/bin/docker" <<'STUB'
#!/bin/sh
case "$1" in info) exit 0 ;; version) echo 27.0-stub; exit 0 ;; esac
if [ "$1" = compose ] && [ "$2" = version ]; then echo 2.0-stub; exit 0; fi
if [ "$1" = ps ]; then
  case "$*" in *working_dir=*) for p in $FAKE_SELF; do printf '0.0.0.0:%s->%s/tcp, [::]:%s->%s/tcp\n' "$p" "$p" "$p" "$p"; done ;; esac
  exit 0
fi
printf '%s\n' "$*" >> "$DOCKER_LOG"
exit 0
STUB
# ss: $FAKE_BUSY is "port:process ..." (process docker-proxy, nginx, …).
cat > "$TMP/bin/ss" <<'STUB'
#!/bin/sh
for a in "$@"; do case "$a" in *"sport = :"*) port=${a#*:}; port=${port%% *} ;; esac; done
for e in $FAKE_BUSY; do
  [ "${e%%:*}" = "$port" ] && printf 'LISTEN 0 511 0.0.0.0:%s 0.0.0.0:* users:(("%s",pid=1,fd=6))\n' "$port" "${e#*:}"
done
exit 0
STUB
printf '#!/bin/sh\necho 203.0.113.7\n' > "$TMP/bin/curl"
printf '#!/bin/sh\nexit 0\n' > "$TMP/bin/mkdir"; cp "$TMP/bin/mkdir" "$TMP/bin/chmod"
printf '#!/bin/sh\nexec "$@"\n' > "$TMP/bin/sudo"
chmod +x "$TMP/bin/"*
export USER_LOG="$TMP/user.log" DOCKER_LOG="$TMP/docker.log"

FAILS=0
pass() { printf '  ok   %s\n' "$*"; }
bad()  { printf '  FAIL %s\n' "$*"; FAILS=$((FAILS + 1)); }
check() { local d=$1; shift; if "$@"; then pass "$d"; else bad "$d"; fi; }
envv() { sed -n "s/^$1=//p" "$TMP/repo/.env" | tail -n1; }

run() { # run <keep-env:0|1> args… ; output in $TMP/out, status in $RC
  local keep=$1; shift
  [ "$keep" = 1 ] || rm -f "$TMP/repo/.env"
  rm -f "$USER_LOG" "$DOCKER_LOG"
  printf 'SANDBOXD_DATA_DIR=%s/data\nSANDBOXD_LOG_DIR=%s/log\nSANDBOXD_API_TOKENS=default=sk_test\nPREVIEW_DOMAIN=localhost\nHTTP_PORT=80\nSANDBOXD_FRONT=\n' "$TMP" "$TMP" > "$TMP/repo/.env.example"
  RC=0
  (cd "$TMP/repo" && PATH="$TMP/bin:$PATH" SANDBOXD_TELEMETRY=off SANDBOXD_NO_SPONSOR=1 SANDBOXD_DESKTOP=0 bash install.sh "$@") > "$TMP/out" 2>&1 || RC=$?
}
last() { tail -n1 "$TMP/out"; }

echo "== without --front nothing changes"
FAKE_BUSY="" FAKE_SELF="" run 0
check "install succeeds" [ "$RC" = 0 ]
check "no result line" bash -c "! grep -q SANDBOXD_INSTALL_RESULT '$TMP/out'"
check "Traefik stays the front door on :80" [ "$(envv HTTP_PORT)" = 80 ]
check "no front recorded" [ -z "$(envv SANDBOXD_FRONT)" ]
check "no user setup" [ ! -e "$USER_LOG" ]
check "no caddy profile" bash -c "! grep -q 'profile caddy' '$DOCKER_LOG'"

echo "== --front=auto on a clean server -> managed Caddy"
KEY="ssh-ed25519 AAAAC3NzaC1lZDI1NTE5AAAAIFakeKeyForInstallerTests000000000000000 me@laptop"
export FAKE_BUSY="" FAKE_SELF=""
run 0 --front=auto "--desktop-key=$KEY"
check "install succeeds" [ "$RC" = 0 ]
check "front=caddy" [ "$(envv SANDBOXD_FRONT)" = caddy ]
check "sslip.io domain from the public IP" [ "$(envv PREVIEW_DOMAIN)" = 203-0-113-7.sslip.io ]
check "flat hostnames" [ "$(envv PREVIEW_HOST_STYLE)" = flat ]
check "https URLs" [ "$(envv PREVIEW_URL_SCHEME)" = https ]
check "router on 127.0.0.1:8088" [ "$(envv SANDBOXD_HTTP_BIND):$(envv HTTP_PORT)" = 127.0.0.1:8088 ]
check "proxied rate limit" [ "$(envv SANDBOXD_RATELIMIT_MIDDLEWARE)" = sandbox-ratelimit-proxied@file ]
check "compose profile caddy" [ "$(envv COMPOSE_PROFILES)" = caddy ]
check "console off on a public server" [ "$(envv SANDBOXD_CONSOLE)" = 0 ]
check "caddy started" grep -q 'profile caddy' "$DOCKER_LOG"
check "user setup got the helpers, .env and key" [ "$(cat "$USER_LOG")" = "$TMP/repo/scripts/frontdoor|$TMP/repo/.env|$KEY" ]
# "proxy" is whatever web server this machine has installed (CI runners ship
# nginx/apache binaries), so only its shape is checked here.
check "result is the last line" bash -c "tail -n1 '$TMP/out' | grep -Eqx 'SANDBOXD_INSTALL_RESULT \\{\"front\":\"caddy\",\"domain\":\"203-0-113-7.sslip.io\",\"proxy\":\"(none|nginx|caddy|apache|traefik|coolify)\",\"api_token\":\"sk_test\",\"api\":\"127.0.0.1:9090\"\\}'"
check "summary shows https URLs without :8088" bash -c "grep -q 'https://s-<id>-<port>.203-0-113-7.sslip.io\$' '$TMP/out'"

echo "== re-run: our own Caddy holds 80/443"
FAKE_BUSY="80:docker-proxy 443:docker-proxy" FAKE_SELF="80 443" run 1 --front=caddy
check "re-run succeeds and keeps caddy" sh -c "[ $RC = 0 ] && [ '$(envv SANDBOXD_FRONT)' = caddy ]"
check "re-run keeps the domain" [ "$(envv PREVIEW_DOMAIN)" = 203-0-113-7.sslip.io ]
check "re-run: one line per key" [ "$(grep -c '^SANDBOXD_FRONT=' "$TMP/repo/.env")" = 1 ]

echo "== --front=auto with nginx on 80/443 -> existing"
FAKE_BUSY="80:nginx 443:nginx" FAKE_SELF="" run 0 --front=auto
check "front=existing" [ "$(envv SANDBOXD_FRONT)" = existing ]
check "router on 127.0.0.1:8088" [ "$(envv SANDBOXD_HTTP_BIND):$(envv HTTP_PORT)" = 127.0.0.1:8088 ]
check "no caddy profile" sh -c "[ -z '$(envv COMPOSE_PROFILES)' ] && ! grep -q 'profile caddy' '$DOCKER_LOG'"
check "result names nginx" bash -c "tail -n1 '$TMP/out' | grep -q '\"front\":\"existing\".*\"proxy\":\"nginx\"'"
check "helpers installed without a key" [ "$(cat "$USER_LOG")" = "$TMP/repo/scripts/frontdoor|$TMP/repo/.env|" ]

echo "== --front=caddy when ports are taken"
FAKE_BUSY="443:apache2" FAKE_SELF="" run 0 --front=caddy
check "refused with a plain message" sh -c "[ $RC != 0 ] && grep -q 'Ports 80 and 443 are already used' '$TMP/out'"

echo "== --front=manual --domain"
FAKE_BUSY="80:caddy" FAKE_SELF="" run 0 --front=manual --domain=Apps.Example.COM
check "front=manual, domain lowercased" sh -c "[ '$(envv SANDBOXD_FRONT)' = manual ] && [ '$(envv PREVIEW_DOMAIN)' = apps.example.com ]"
check "result" [ "$(last)" = 'SANDBOXD_INSTALL_RESULT {"front":"manual","domain":"apps.example.com","proxy":"caddy","api_token":"sk_test","api":"127.0.0.1:9090"}' ]

echo "== bad flags"
FAKE_BUSY="" run 0 --front=nginx-please; check "unknown front refused" [ "$RC" != 0 ]
FAKE_BUSY="" run 0 --domain=x.example.com; check "--domain without --front refused" [ "$RC" != 0 ]
FAKE_BUSY="" run 0 --front=manual --domain='bad domain'; check "invalid domain refused" [ "$RC" != 0 ]

echo "== helpers"
# shellcheck source=scripts/install-front.sh
. "$ROOT/scripts/install-front.sh"
for k in "ssh-ed25519 AAAAC3NzaC1lZDI1NTE5AAAAIFakeKeyForInstallerTests0000 me@laptop" \
         "ssh-ed25519 AAAAC3NzaC1lZDI1NTE5AAAAIFakeKeyForInstallerTests0000" \
         "ecdsa-sha2-nistp256 AAAAE2VjZHNhLXNoYTItbmlzdHAyNTYAAAAIbmlzdHAyNTY= x"; do
  check "valid key: ${k:0:24}…" front_valid_ssh_key "$k"
done
for k in "" "ssh-ed25519" "command=\"sh\" ssh-ed25519 AAAAC3NzaC1lZDI1NTE5AAAAIFake0000000000" \
         "ssh-ed25519 AAAAC3NzaC1lZDI1NTE5AAAAIFake0000000000 me\"x" \
         $'ssh-ed25519 AAAAC3NzaC1lZDI1NTE5AAAAIFake0000000000\nssh-rsa AAAAB3NzaC1yc2EAAAADAQAB0000' \
         "ssh-dss AAAAB3NzaC1kc3MAAACBAP0000000000"; do
  check "invalid key refused: $(printf '%q' "${k:0:30}")" bash -c "! { . '$ROOT/scripts/install-front.sh'; front_valid_ssh_key \"\$1\"; }" _ "$k"
done
check "sslip domain" [ "$(front_sslip_domain 203.0.113.7)" = 203-0-113-7.sslip.io ]
for ip in 1.2.3.4 255.255.255.255; do check "valid ip $ip" front_valid_ipv4 "$ip"; done
for ip in 256.1.1.1 1.2.3 a.b.c.d "1.2.3.4 "; do check "invalid ip '$ip'" bash -c "! { . '$ROOT/scripts/install-front.sh'; front_valid_ipv4 \"\$1\"; }" _ "$ip"; done
check "token: default= wins" [ "$(front_api_token 'ci=a,default=sk_b')" = sk_b ]
check "token: else the first" [ "$(front_api_token 'ci=sk_a,ops=sk_b')" = sk_a ]
check "json escaping" [ "$(front_json_str 'a"b\c')" = '"a\"b\\c"' ]

[ "$FAILS" = 0 ] && echo "PASS: install.sh --front decisions (isolated fixture)" || { echo "FAIL: $FAILS"; exit 1; }
