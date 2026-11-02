# shellcheck shell=bash
# sandboxd installer — front door setup for servers (install.sh --front=…).
# Sourced by install.sh only when --front is given, so desktop and local
# installs never run any of this. Expects from install.sh: REPO_ROOT, DOCKER,
# info/ok/die. Every function is safe to re-run.
#
#   front door   who owns 80/443                      Traefik (the router)
#   caddy        sandboxd's Caddy container            127.0.0.1:8088
#   existing     your nginx or Caddy (frontdoor helper) 127.0.0.1:8088
#   manual       whatever you set up                   127.0.0.1:8088

FRONT_LIB=/usr/local/lib/sandboxd
FRONT_SUDOERS=/etc/sudoers.d/sandboxd-frontdoor
FRONT_USER=sandboxd

front_sudo() { if [ "$(id -u)" = 0 ]; then "$@"; else sudo "$@"; fi; }

# front_port_owner <port>: none | self (sandboxd's own containers) | nginx |
# caddy | apache | docker (another container) | unknown | <process name>
front_port_owner() {
  local p=$1 out name
  command -v ss >/dev/null 2>&1 || { printf unknown; return; }
  out=$(front_sudo ss -ltnpH "( sport = :$p )" 2>/dev/null || ss -ltnH "( sport = :$p )" 2>/dev/null)
  [ -n "$out" ] || { printf none; return; }
  name=$(printf '%s\n' "$out" | sed -n 's/.*users:(("\([^"]*\)".*/\1/p' | head -n1)
  case "$name" in
    docker-proxy|"")
      if $DOCKER ps --filter "label=com.docker.compose.project.working_dir=$REPO_ROOT" --format '{{.Ports}}' 2>/dev/null \
          | grep -Eq "(^|[ ,])[^ ,]*:$p->"; then
        printf self
      elif [ -n "$name" ]; then printf docker
      else printf unknown; fi ;;
    apache2|httpd) printf apache ;;
    *) printf '%s' "$name" ;;
  esac
}

front_port_free() { case "$(front_port_owner "$1")" in none|self) return 0 ;; esac; return 1; }

# front_containers: "<image> <name>" of running containers that are not ours.
front_containers() {
  $DOCKER ps --format '{{.Image}} {{.Names}} {{.Label "com.docker.compose.project.working_dir"}}' 2>/dev/null \
    | awk -v self="$REPO_ROOT" '$3 != self { print $1, $2 }'
}

# front_detect_proxy: the web server in charge of this host's 80/443, else the
# first one installed: nginx | caddy | apache | traefik | coolify | none.
front_detect_proxy() {
  local o c
  c=$(front_containers)
  for o in "$(front_port_owner 443)" "$(front_port_owner 80)"; do
    case "$o" in
      nginx|caddy|apache) printf '%s' "$o"; return ;;
      docker)
        if printf '%s\n' "$c" | grep -qi coolify; then printf coolify; return; fi
        if printf '%s\n' "$c" | grep -qi traefik; then printf traefik; return; fi
        if printf '%s\n' "$c" | grep -qi caddy; then printf caddy; return; fi
        if printf '%s\n' "$c" | grep -qi nginx; then printf nginx; return; fi ;;
    esac
  done
  if command -v nginx >/dev/null 2>&1; then printf nginx
  elif command -v caddy >/dev/null 2>&1; then printf caddy
  elif command -v apache2 >/dev/null 2>&1 || command -v httpd >/dev/null 2>&1; then printf apache
  elif printf '%s\n' "$c" | grep -qi coolify; then printf coolify
  elif printf '%s\n' "$c" | grep -qi traefik; then printf traefik
  else printf none; fi
}

# front_resolve <requested>: the front door to use. auto picks caddy when 80
# and 443 are both free (or already ours), else existing.
front_resolve() {
  case "$1" in
    auto)
      if front_port_free 80 && front_port_free 443; then printf caddy; else printf existing; fi ;;
    caddy)
      front_port_free 80 && front_port_free 443 \
        || die "Ports 80 and 443 are already used by $(front_port_owner 80)/$(front_port_owner 443). Use --front=existing to work with the web server that is already there."
      printf caddy ;;
    existing|manual) printf '%s' "$1" ;;
    *) die "Unknown front door: $1 (use auto, caddy, existing or manual)" ;;
  esac
}

front_valid_ipv4() {
  [[ $1 =~ ^([0-9]{1,3})\.([0-9]{1,3})\.([0-9]{1,3})\.([0-9]{1,3})$ ]] || return 1
  local o
  for o in "${BASH_REMATCH[@]:1}"; do [ "$o" -le 255 ] || return 1; done
}

front_valid_domain() {
  local label='[a-z0-9]([a-z0-9-]{0,61}[a-z0-9])?'
  [ "${#1}" -le 200 ] && [[ $1 =~ ^$label(\.$label)+$ ]]
}

# front_public_ip: this server's public IPv4 (ipify, else the first global
# address on an interface).
front_public_ip() {
  local ip
  ip=$(curl -fsS -m 5 https://api.ipify.org 2>/dev/null | tr -d '[:space:]')
  if ! front_valid_ipv4 "$ip"; then
    ip=$(ip -4 -o addr show scope global 2>/dev/null | awk '{ sub(/\/.*/, "", $4); print $4; exit }')
  fi
  front_valid_ipv4 "$ip" && printf '%s' "$ip"
}

# front_sslip_domain <ipv4>: 203.0.113.7 -> 203-0-113-7.sslip.io
front_sslip_domain() { printf '%s.sslip.io' "${1//./-}"; }

# front_env_set <file> <key> <value>: replace (or add) one KEY=value line.
front_env_set() {
  local f=$1 k=$2 v=$3 tmp
  tmp="$(mktemp "${TMPDIR:-/tmp}/sandboxd-env.XXXXXX")"
  grep -vE "^$k=" "$f" > "$tmp" 2>/dev/null || true
  printf '%s=%s\n' "$k" "$v" >> "$tmp"
  cat "$tmp" > "$f"; rm -f "$tmp"
}

front_env_unset() {
  local f=$1 k=$2 tmp
  tmp="$(mktemp "${TMPDIR:-/tmp}/sandboxd-env.XXXXXX")"
  grep -vE "^$k=" "$f" > "$tmp" 2>/dev/null || true
  cat "$tmp" > "$f"; rm -f "$tmp"
}

# front_write_env <env-file> <front> <domain>: the settings each front door needs.
# Traefik always becomes the internal router on 127.0.0.1:8088, public URLs are
# https on flat hostnames (s-<id>-<port>.<domain>: one level under the domain,
# which sslip.io resolves and one certificate per name covers), and the rate
# limit trusts the one front door in front of Traefik.
front_write_env() {
  local f=$1 front=$2 domain=$3
  front_env_set "$f" SANDBOXD_FRONT "$front"
  front_env_set "$f" PREVIEW_DOMAIN "$domain"
  front_env_set "$f" PREVIEW_HOST_STYLE flat
  front_env_set "$f" PREVIEW_URL_SCHEME https
  front_env_set "$f" SANDBOXD_HTTP_BIND 127.0.0.1
  front_env_set "$f" HTTP_PORT 8088
  front_env_set "$f" SANDBOXD_RATELIMIT_MIDDLEWARE sandbox-ratelimit-proxied@file
  # The desktop reaches the engine through SSH; a console on the public
  # internet would let the first visitor pick its password. Off unless asked.
  grep -q '^SANDBOXD_CONSOLE=1' "$f" 2>/dev/null || front_env_set "$f" SANDBOXD_CONSOLE 0
  if [ "$front" = caddy ]; then
    if grep -q '^SANDBOXD_CONSOLE=1' "$f"; then front_env_set "$f" COMPOSE_PROFILES console,caddy
    else front_env_set "$f" COMPOSE_PROFILES caddy; fi
  else
    front_env_unset "$f" COMPOSE_PROFILES
  fi
}

# front_valid_ssh_key <key>: "<type> <base64> [comment]" with a known type and
# nothing that could smuggle options into authorized_keys.
front_valid_ssh_key() {
  [[ $1 =~ ^(ssh-ed25519|ssh-rsa|ecdsa-sha2-nistp(256|384|521)|sk-ssh-ed25519@openssh\.com|sk-ecdsa-sha2-nistp256@openssh\.com)\ [A-Za-z0-9+/]{16,}={0,3}(\ [A-Za-z0-9@._:+-]{0,100})?$ ]]
}

# front_install_user <helpers-dir> <env-file> [ssh-public-key]: the limited
# `sandboxd` user, the root-owned helpers, the one sudo rule, and the
# desktop's key (restricted to the forced command + the API tunnel).
front_install_user() {
  local src=$1 envf=$2 key=${3:-} home ak tmp opts blob
  if ! id "$FRONT_USER" >/dev/null 2>&1; then
    front_sudo useradd --system --create-home --home-dir "/home/$FRONT_USER" --shell /bin/sh \
      --comment "sandboxd desktop access" "$FRONT_USER" || die "Could not create the $FRONT_USER user."
    ok "created the limited user '$FRONT_USER'"
  fi
  # No password at all ("*" is neither a password nor a locked account, so
  # key logins keep working).
  front_sudo usermod -p '*' "$FRONT_USER" >/dev/null 2>&1 || true

  front_sudo install -d -o root -g root -m 0755 "$FRONT_LIB"
  front_sudo install -o root -g root -m 0755 "$src/frontdoor" "$FRONT_LIB/frontdoor"
  front_sudo install -o root -g root -m 0755 "$src/frontdoor-ssh" "$FRONT_LIB/frontdoor-ssh"
  printf '# managed by sandboxd\nenv_file=%s\n' "$envf" | front_sudo tee "$FRONT_LIB/frontdoor.conf" >/dev/null
  front_sudo chmod 0644 "$FRONT_LIB/frontdoor.conf"

  tmp="$(mktemp "${TMPDIR:-/tmp}/sandboxd-sudoers.XXXXXX")"
  cat > "$tmp" <<EOF
# managed by sandboxd: the desktop's limited user may run the front-door
# helper as root, and nothing else.
Defaults:$FRONT_USER !requiretty
$FRONT_USER ALL=(root) NOPASSWD: $FRONT_LIB/frontdoor
EOF
  if ! front_sudo visudo -cf "$tmp" >/dev/null; then rm -f "$tmp"; die "The sudo rule for $FRONT_USER did not validate; nothing was installed."; fi
  front_sudo install -o root -g root -m 0440 "$tmp" "$FRONT_SUDOERS"; rm -f "$tmp"
  ok "front-door helper installed ($FRONT_LIB/frontdoor)"

  [ -n "$key" ] || return 0
  front_valid_ssh_key "$key" || die "The desktop key is not a valid SSH public key."
  home=$(getent passwd "$FRONT_USER" | cut -d: -f6); [ -n "$home" ] || home="/home/$FRONT_USER"
  ak="$home/.ssh/authorized_keys"
  opts="restrict,port-forwarding,permitopen=\"127.0.0.1:9090\",command=\"$FRONT_LIB/frontdoor-ssh\""
  blob=$(printf '%s' "$key" | awk '{ print $2 }')
  front_sudo install -d -o "$FRONT_USER" -g "$(id -gn "$FRONT_USER")" -m 0700 "$home/.ssh"
  tmp="$(mktemp "${TMPDIR:-/tmp}/sandboxd-ak.XXXXXX")"
  front_sudo cat "$ak" 2>/dev/null | grep -vF " $blob" > "$tmp" || true
  printf '%s %s\n' "$opts" "$key" >> "$tmp"
  front_sudo install -o "$FRONT_USER" -g "$(id -gn "$FRONT_USER")" -m 0600 "$tmp" "$ak"; rm -f "$tmp"
  ok "desktop key authorised for '$FRONT_USER' (tunnel to the API + front-door commands only)"
}

# front_remove_user: undo front_install_user (uninstall.sh).
front_remove_user() {
  front_sudo rm -f "$FRONT_SUDOERS" "$FRONT_LIB/frontdoor" "$FRONT_LIB/frontdoor-ssh" "$FRONT_LIB/frontdoor.conf"
  front_sudo rmdir "$FRONT_LIB" 2>/dev/null || true
  if id "$FRONT_USER" >/dev/null 2>&1 && [ "$(getent passwd "$FRONT_USER" | cut -d: -f7)" = /bin/sh ] \
      && getent passwd "$FRONT_USER" | cut -d: -f5 | grep -q '^sandboxd desktop access'; then
    front_sudo userdel -r "$FRONT_USER" >/dev/null 2>&1 || front_sudo userdel "$FRONT_USER"
    return 0
  fi
  return 1
}

# front_api_token <SANDBOXD_API_TOKENS value>: the default= key, else the first.
front_api_token() {
  local t
  t=$(printf '%s' "$1" | tr ',' '\n' | sed -n 's/^default=//p' | head -n1)
  [ -n "$t" ] || t=$(printf '%s' "$1" | tr ',' '\n' | head -n1 | cut -d= -f2-)
  printf '%s' "$t"
}

# front_json_str <s>: a JSON string literal.
front_json_str() {
  local s=$1
  s=${s//\\/\\\\}; s=${s//\"/\\\"}
  printf '"%s"' "$(printf '%s' "$s" | tr -d '\000-\037')"
}

front_result_line() { # front domain proxy token api
  printf 'SANDBOXD_INSTALL_RESULT {"front":%s,"domain":%s,"proxy":%s,"api_token":%s,"api":%s}\n' \
    "$(front_json_str "$1")" "$(front_json_str "$2")" "$(front_json_str "$3")" "$(front_json_str "$4")" "$(front_json_str "$5")"
}
