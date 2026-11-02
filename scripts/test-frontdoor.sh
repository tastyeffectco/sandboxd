#!/usr/bin/env bash
# Exercise the front-door helpers (scripts/frontdoor/) against real nginx and
# Caddy inside throwaway containers: install of the limited user + sudo rule,
# apply/status/remove, roll back on a failed test or certificate, refusing
# files sandboxd did not create, and the SSH forced command refusing anything
# that is not a front-door command. Nothing runs on the host itself.
#
#   bash scripts/test-frontdoor.sh            # needs Docker
#
# Images: debian:12 and caddy:2.11.2-alpine (the managed front door's tag).
set -euo pipefail
ROOT="$(cd "$(dirname "$0")/.." && pwd)"
DEBIAN_IMAGE=${FRONTDOOR_TEST_DEBIAN:-debian:12}
CADDY_IMAGE=${FRONTDOOR_TEST_CADDY:-caddy:2.11.2-alpine}

FD=/usr/local/lib/sandboxd/frontdoor
FDSSH=/usr/local/lib/sandboxd/frontdoor-ssh
FAILS=0
pass() { printf '  ok   %s\n' "$*"; }
bad()  { printf '  FAIL %s\n' "$*"; FAILS=$((FAILS + 1)); }
check() { local d=$1; shift; if "$@"; then pass "$d"; else bad "$d"; fi; }
# last JSON line of output $1 contains every fixed string that follows
# eventually <want> <curl args…>: nginx/Caddy reloads finish asynchronously.
eventually() { local want=$1 got; shift; for _ in 1 2 3 4 5 6 7 8 9 10; do got=$(curl -s "$@" 2>/dev/null || true); [ "$got" = "$want" ] && return 0; sleep 0.3; done; printf '       got: %s\n' "$got"; return 1; }
never() { local want=$1 got; shift; for _ in 1 2 3 4 5 6 7 8 9 10; do got=$(curl -s "$@" 2>/dev/null || true); [ "$got" != "$want" ] && return 0; sleep 0.3; done; return 1; }
json_has() { local line; line=$(printf '%s\n' "$1" | tail -n1); shift; local p; for p in "$@"; do case "$line" in *"$p"*) ;; *) printf '       got: %s\n' "$line"; return 1 ;; esac; done; }

# Run the forced command as the desktop's SSH session would.
as_desktop() { runuser -u sandboxd -- env SSH_ORIGINAL_COMMAND="$1" $FDSSH 2>/dev/null; }

sshx() { ssh -i /root/desktop_key -o StrictHostKeyChecking=no -o UserKnownHostsFile=/dev/null -o BatchMode=yes -o LogLevel=ERROR sandboxd@127.0.0.1 "$@"; }

# Install the helpers the way install.sh does (scripts/install-front.sh).
install_helpers() {
  ok() { :; }; info() { :; }; die() { printf 'die: %s\n' "$*" >&2; exit 1; }
  DOCKER=false; REPO_ROOT=/opt/sbx
  # shellcheck source=scripts/install-front.sh
  . /src/scripts/install-front.sh
  mkdir -p /opt/sbx
  printf 'SANDBOXD_FRONT=existing\nSANDBOXD_HTTP_BIND=127.0.0.1\nHTTP_PORT=8088\n' > /opt/sbx/.env
  front_install_user /src/scripts/frontdoor /opt/sbx/.env "$KEY"
}
KEY="ssh-ed25519 AAAAC3NzaC1lZDI1NTE5AAAAIFrontdoorTestKeyOnlyForTests0000000000 desktop@test"

# shellcheck disable=SC2120 # false positive from the quoted nginx shim heredoc
inside_nginx() {
  export DEBIAN_FRONTEND=noninteractive
  apt-get -qq update >/dev/null && apt-get -qq install -y --no-install-recommends nginx curl iproute2 sudo procps openssh-server openssh-client >/dev/null
  echo "== nginx (debian 12, sites-available layout, no certbot)"
  install_helpers; install_helpers   # twice: idempotent
  check "sudoers validated and installed 0440" [ "$(stat -c %a /etc/sudoers.d/sandboxd-frontdoor)" = 440 ]
  check "helpers root-owned" [ "$(stat -c %U:%a $FD)" = root:755 ]
  check "authorized_keys has the key once" [ "$(grep -c FrontdoorTestKey /home/sandboxd/.ssh/authorized_keys)" = 1 ]
  check "authorized_keys restricts the key" grep -q '^restrict,port-forwarding,permitopen="127.0.0.1:9090",command="/usr/local/lib/sandboxd/frontdoor-ssh" ssh-ed25519 ' /home/sandboxd/.ssh/authorized_keys
  check "sandboxd has a shell for the forced command" [ "$(getent passwd sandboxd | cut -d: -f7)" = /bin/sh ]

  # The router stand-in: what sandboxd's Traefik would answer on 127.0.0.1:8088.
  printf 'server { listen 127.0.0.1:8088; location / { return 200 "router $host $http_x_forwarded_proto\\n"; } }\n' > /etc/nginx/conf.d/router.conf
  nginx
  local out before
  out=$($FD facts 2>/dev/null)
  check "facts: nginx, layout, no certbot, owner of :80" json_has "$out" '"ok":true' '"proxies":["nginx"]' '"nginx_layout":"sites-available"' '"certbot":false' '"80":"nginx"' '"target":"nginx"' '"front":"existing"' '"router":"127.0.0.1:8088"'

  out=$($FD apply shop shop.example.test www.shop.example.test 2>/dev/null) || true
  check "apply: ok, https not possible without certbot" json_has "$out" '"ok":true' '"https":false'
  check "apply: file marked managed" [ "$(head -n1 /etc/nginx/sites-available/sandboxd-shop.conf)" = "# managed by sandboxd" ]
  check "apply: enabled via symlink" [ "$(readlink /etc/nginx/sites-enabled/sandboxd-shop.conf)" = /etc/nginx/sites-available/sandboxd-shop.conf ]
  check "apply: host forwarded to the router" eventually "router www.shop.example.test http"  -H 'Host: www.shop.example.test' http://127.0.0.1/
  out=$($FD status shop 2>/dev/null)
  check "status: configured with both hosts" json_has "$out" '"configured":true' '"proxy":"nginx"' '"managed":true' '"hosts":["shop.example.test","www.shop.example.test"]'

  # Roll back on a failing `nginx -t`: a shim fails the test whenever the new
  # config mentions fail.example.test (the real nginx runs otherwise).
  mkdir -p /usr/local/sbin
  cat > /usr/local/sbin/nginx <<'SHIM'
#!/bin/sh
if [ "$1" = -t ] && grep -Rqs fail.example.test /etc/nginx/sites-enabled/ /etc/nginx/conf.d/; then
  echo 'nginx: [emerg] simulated failure' >&2; exit 1
fi
exec /usr/sbin/nginx "$@"
SHIM
  chmod +x /usr/local/sbin/nginx
  before=$(cat /etc/nginx/sites-available/sandboxd-shop.conf)
  out=$($FD apply shop shop.example.test fail.example.test 2>/dev/null) && bad "apply with bad config should fail" || true
  check "rollback: reports nginx-test in plain words" json_has "$out" '"ok":false' '"step":"nginx-test"' 'previous setup was restored'
  check "rollback: previous file restored" [ "$(cat /etc/nginx/sites-available/sandboxd-shop.conf)" = "$before" ]
  check "rollback: old site still served" eventually "router shop.example.test http"  -H 'Host: shop.example.test' http://127.0.0.1/
  out=$($FD apply fresh fail.example.test 2>/dev/null) || true
  check "rollback: a new site leaves nothing behind" sh -c '[ ! -e /etc/nginx/sites-available/sandboxd-fresh.conf ] && [ ! -L /etc/nginx/sites-enabled/sandboxd-fresh.conf ]'
  rm -f /usr/local/sbin/nginx

  # A broken configuration that is not ours: refuse to touch anything.
  printf 'this is not nginx\n' > /etc/nginx/conf.d/broken.conf
  out=$($FD apply other other.example.test 2>/dev/null) || true
  check "pre-broken nginx: nothing changed" json_has "$out" '"ok":false' '"step":"nginx-test"' 'already has an error'
  check "pre-broken nginx: no file written" [ ! -e /etc/nginx/sites-available/sandboxd-other.conf ]
  rm -f /etc/nginx/conf.d/broken.conf

  # Files sandboxd did not create are never modified or removed.
  printf 'server { listen 80; server_name mine.example.test; }\n' > /etc/nginx/sites-available/sandboxd-mine.conf
  out=$($FD apply mine mine.example.test 2>/dev/null) || true
  check "unmanaged file: apply refused" json_has "$out" '"ok":false' '"step":"refused"'
  out=$($FD remove mine 2>/dev/null) || true
  check "unmanaged file: remove refused" json_has "$out" '"ok":false' '"step":"refused"'
  check "unmanaged file: untouched" grep -q 'mine.example.test' /etc/nginx/sites-available/sandboxd-mine.conf
  rm -f /etc/nginx/sites-available/sandboxd-mine.conf

  # certbot: failure rolls back, success reports https.
  printf '#!/bin/sh\necho "certbot: DNS problem" >&2\nexit 1\n' > /usr/local/bin/certbot; chmod +x /usr/local/bin/certbot
  before=$(cat /etc/nginx/sites-available/sandboxd-shop.conf)
  out=$($FD apply shop shop.example.test api.shop.example.test 2>/dev/null) || true
  check "certbot failure: rolled back" json_has "$out" '"ok":false' '"step":"certbot"'
  check "certbot failure: previous file restored" [ "$(cat /etc/nginx/sites-available/sandboxd-shop.conf)" = "$before" ]
  printf '#!/bin/sh\necho "$@" > /tmp/certbot-args\nexit 0\n' > /usr/local/bin/certbot
  out=$($FD apply shop shop.example.test 2>/dev/null) || true
  check "certbot success: https" json_has "$out" '"ok":true' '"https":true'
  check "certbot called non-interactively for the host" grep -q -- '--nginx -d shop.example.test --non-interactive --agree-tos --register-unsafely-without-email --redirect' /tmp/certbot-args
  rm -f /usr/local/bin/certbot

  out=$($FD remove shop 2>/dev/null) || true
  check "remove: ok" json_has "$out" '"ok":true'
  check "remove: file and link gone" sh -c '[ ! -e /etc/nginx/sites-available/sandboxd-shop.conf ] && [ ! -L /etc/nginx/sites-enabled/sandboxd-shop.conf ]'
  check "remove: host no longer forwarded" never "router shop.example.test http" -H 'Host: shop.example.test' http://127.0.0.1/
  check "backups kept" [ -n "$(ls /var/backups/sandboxd-frontdoor)" ]

  echo "== nginx (conf.d layout)"
  rm -rf /etc/nginx/sites-available /etc/nginx/sites-enabled
  /usr/sbin/nginx -s reload; sleep 1
  out=$($FD apply shop shop.example.test 2>/dev/null) || true
  check "conf.d: apply ok" json_has "$out" '"ok":true'
  check "conf.d: file written" [ "$(head -n1 /etc/nginx/conf.d/sandboxd-shop.conf)" = "# managed by sandboxd" ]
  check "conf.d: host forwarded" eventually "router shop.example.test http"  -H 'Host: shop.example.test' http://127.0.0.1/
  out=$($FD remove shop 2>/dev/null) || true
  check "conf.d: removed" [ ! -e /etc/nginx/conf.d/sandboxd-shop.conf ]

  echo "== argument validation"
  for args in "apply Shop x.example.test" "apply shop *.example.test" "apply shop example" "apply ../x x.example.test" \
      "apply shop X.example.test" "apply shop -x.example.test" "remove" "status a/b" "facts extra" "bogus"; do
    # shellcheck disable=SC2086 # word splitting is the point
    out=$($FD $args 2>/dev/null) && bad "frontdoor $args should fail" || true
    check "frontdoor $args refused" json_has "$out" '"ok":false' '"step":"usage"'
  done

  echo "== SSH forced command"
  local c
  out=$(as_desktop "frontdoor facts") || true
  check "allowed: frontdoor facts (through sudo)" json_has "$out" '"ok":true' '"step":"facts"'
  out=$(as_desktop "frontdoor status shop") || true
  check "allowed: frontdoor status shop" json_has "$out" '"ok":true' '"step":"status"'
  while IFS= read -r c; do
    rm -f /tmp/pwned
    out=$(as_desktop "$c") && bad "should refuse: $c" || true
    check "refused: $(printf '%q' "$c")" json_has "$out" '"ok":false' '"step":"refused"'
    check "  ...and nothing ran" [ ! -e /tmp/pwned ]
  done <<'CASES'

bash
sh -c touch /tmp/pwned
facts
facts; touch /tmp/pwned
frontdoor facts; touch /tmp/pwned
frontdoor facts && touch /tmp/pwned
frontdoor facts | touch /tmp/pwned
frontdoor $(touch /tmp/pwned)
frontdoor `touch /tmp/pwned`
frontdoor status 'shop'
frontdoor status "shop"
frontdoor apply shop x.example.test>/tmp/pwned
frontdoor apply shop *.example.test
frontdoor apply Shop x.example.test
frontdoor apply shop x.example.test,y
frontdoor status ../../etc
frontdoor facts --help
frontdoor facts extra
frontdoor remove
frontdoor bogus
/usr/local/lib/sandboxd/frontdoor facts
sudo id
scp -t /tmp
rsync --server . /tmp
CASES
  rm -f /tmp/pwned
  out=$(as_desktop "frontdoor facts"$'\n'"touch /tmp/pwned") && bad "newline accepted" || true
  check "refused: newline-separated command" json_has "$out" '"step":"refused"'
  check "  ...and nothing ran" [ ! -e /tmp/pwned ]
  out=$(as_desktop "frontdoor facts"$'\t'"x") && bad "tab accepted" || true
  check "refused: tab" json_has "$out" '"step":"refused"'
  check "sudo allows nothing but the helper" bash -c '! runuser -u sandboxd -- sudo -n /bin/id >/dev/null 2>&1'
  check "sudo does not allow a shell via the forced command" bash -c '! runuser -u sandboxd -- sudo -n /usr/local/lib/sandboxd/frontdoor-ssh >/dev/null 2>&1'

  echo "== real sshd (authorized_keys options + forced command)"
  mkdir -p /run/sshd; ssh-keygen -A >/dev/null; /usr/sbin/sshd
  ssh-keygen -q -t ed25519 -N '' -C desktop@test -f /root/desktop_key
  front_install_user /src/scripts/frontdoor /opt/sbx/.env "$(cat /root/desktop_key.pub)"
  front_install_user /src/scripts/frontdoor /opt/sbx/.env "$(cat /root/desktop_key.pub)"
  check "real key: one line per key after re-install" [ "$(grep -cF "$(cut -d' ' -f2 /root/desktop_key.pub)" /home/sandboxd/.ssh/authorized_keys)" = 1 ]
  out=$(sshx frontdoor facts 2>/dev/null) || true
  check "ssh: frontdoor facts" json_has "$out" '"ok":true' '"step":"facts"'
  rm -f /tmp/pwned
  out=$(sshx 'frontdoor facts; touch /tmp/pwned' 2>/dev/null) && bad "ssh injection accepted" || true
  check "ssh: injection refused" json_has "$out" '"step":"refused"'
  check "  ...and nothing ran" [ ! -e /tmp/pwned ]
  out=$(sshx 2>/dev/null </dev/null) && bad "ssh shell accepted" || true
  check "ssh: no interactive shell" json_has "$out" '"step":"refused"'
  check "ssh: no pty" bash -c "! ssh -tt -i /root/desktop_key -o StrictHostKeyChecking=no -o UserKnownHostsFile=/dev/null -o BatchMode=yes -o LogLevel=ERROR sandboxd@127.0.0.1 2>&1 </dev/null | grep -q '[$#] *$'"
  # The API tunnel: only 127.0.0.1:9090 may be forwarded.
  printf 'server { listen 127.0.0.1:9090; return 200 "api\\n"; }\nserver { listen 127.0.0.1:9091; return 200 "other\\n"; }\n' > /etc/nginx/conf.d/ports.conf
  /usr/sbin/nginx -s reload; sleep 0.5
  sshx -N -o ExitOnForwardFailure=yes -L 19090:127.0.0.1:9090 -L 19091:127.0.0.1:9091 & tun=$!
  sleep 1.5
  check "ssh: tunnel to 127.0.0.1:9090 works" eventually "api" http://127.0.0.1:19090/
  check "ssh: tunnel to any other port is refused" bash -c '[ "$(curl -s -m 3 http://127.0.0.1:19091/)" != other ]'
  kill "$tun" 2>/dev/null || true
  rm -f /etc/nginx/conf.d/ports.conf

  echo "== uninstall"
  front_remove_user
  check "user removed" bash -c '! id sandboxd >/dev/null 2>&1'
  check "sudoers and helpers removed" sh -c "[ ! -e /etc/sudoers.d/sandboxd-frontdoor ] && [ ! -e $FD ] && [ ! -e $FDSSH ]"
  [ "$FAILS" = 0 ]
}

inside_caddy() {
  apk add -q --no-progress bash curl sudo shadow util-linux >/dev/null
  echo "== host Caddy (alpine)"
  cat > /etc/caddy/Caddyfile <<'EOF'
{
	local_certs
	skip_install_trust
	admin localhost:2019
}

# The router stand-in: what sandboxd's Traefik would answer on 127.0.0.1:8088.
:8088 {
	bind 127.0.0.1
	respond "router {host}"
}

taken.example.test {
	respond "someone else's site"
}
EOF
  caddy start --config /etc/caddy/Caddyfile --adapter caddyfile >/dev/null 2>&1
  install_helpers
  local out before
  out=$($FD facts 2>/dev/null)
  check "facts: caddy without the import yet" json_has "$out" '"ok":true' '"caddy"' '"caddy_layout":"caddyfile"' '"target":"caddy"'
  out=$($FD apply shop shop.example.test 2>/dev/null) || true
  check "apply before import: clear error naming the approval step" json_has "$out" '"ok":false' '"step":"caddy-import"' 'enable-caddy-import'

  mkdir -p /etc/caddy/sandboxd; printf 'not { a caddyfile\n' > /etc/caddy/sandboxd/broken.caddy
  before=$(cat /etc/caddy/Caddyfile)
  out=$($FD enable-caddy-import 2>/dev/null) || true
  check "enable-caddy-import rolls back when Caddy rejects it" json_has "$out" '"ok":false' '"step":"caddy-validate"'
  check "  ...Caddyfile restored" [ "$(cat /etc/caddy/Caddyfile)" = "$before" ]
  rm -f /etc/caddy/sandboxd/broken.caddy
  out=$($FD enable-caddy-import 2>/dev/null) || true
  check "enable-caddy-import adds the line" json_has "$out" '"ok":true' '"changed":true'
  check "  ...exactly once" [ "$(grep -c '^import /etc/caddy/sandboxd/\*\.caddy$' /etc/caddy/Caddyfile)" = 1 ]
  out=$($FD enable-caddy-import 2>/dev/null) || true
  check "enable-caddy-import is idempotent" json_has "$out" '"ok":true' '"changed":false'

  out=$($FD apply shop shop.example.test www.shop.example.test 2>/dev/null) || true
  check "apply: ok" json_has "$out" '"ok":true' '"proxy":"caddy"'
  check "apply: file marked managed" [ "$(head -n1 /etc/caddy/sandboxd/shop.caddy)" = "# managed by sandboxd" ]
  check "apply: https host forwarded to the router" eventually "router shop.example.test" -k --resolve shop.example.test:443:127.0.0.1 https://shop.example.test/
  out=$($FD status shop 2>/dev/null)
  check "status: configured" json_has "$out" '"configured":true' '"proxy":"caddy"' '"hosts":["shop.example.test","www.shop.example.test"]'

  before=$(cat /etc/caddy/sandboxd/shop.caddy)
  out=$($FD apply shop shop.example.test taken.example.test 2>/dev/null) && bad "duplicate site should fail" || true
  check "rollback: Caddy rejects a host another site owns" json_has "$out" '"ok":false' '"step":"caddy-validate"'
  check "rollback: previous file restored" [ "$(cat /etc/caddy/sandboxd/shop.caddy)" = "$before" ]
  check "rollback: other site untouched" eventually "someone else's site" -k --resolve taken.example.test:443:127.0.0.1 https://taken.example.test/

  printf 'mine.example.test {\n\trespond mine\n}\n' > /etc/caddy/sandboxd/mine.caddy
  out=$($FD apply mine mine.example.test 2>/dev/null) || true
  check "unmanaged file: refused" json_has "$out" '"ok":false' '"step":"refused"'
  rm -f /etc/caddy/sandboxd/mine.caddy

  out=$($FD remove shop 2>/dev/null) || true
  check "remove: ok" json_has "$out" '"ok":true'
  check "remove: file gone" [ ! -e /etc/caddy/sandboxd/shop.caddy ]
  check "remove: host no longer served" never "router shop.example.test" -k --resolve shop.example.test:443:127.0.0.1 https://shop.example.test/
  caddy stop >/dev/null 2>&1 || true
  [ "$FAILS" = 0 ]
}

case "${1:-}" in
  --inside-nginx) inside_nginx; exit ;;
  --inside-caddy) inside_caddy; exit ;;
esac

rc=0
docker run --rm -v "$ROOT:/src:ro" "$DEBIAN_IMAGE" bash /src/scripts/test-frontdoor.sh --inside-nginx || rc=1
docker run --rm -v "$ROOT:/src:ro" --entrypoint /bin/sh "$CADDY_IMAGE" -c 'apk add -q --no-progress bash >/dev/null && bash /src/scripts/test-frontdoor.sh --inside-caddy' || rc=1
[ "$rc" = 0 ] && echo "PASS: front-door helpers (nginx, host Caddy, forced command)" || echo "FAIL: front-door helpers"
exit "$rc"
