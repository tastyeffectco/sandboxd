# shellcheck shell=bash
# sandboxd installer — front door setup for servers (install.sh --front=…).
# Sourced by install.sh only when --front is given, so desktop and local
# installs never run any of this. Wiring an existing web server to the router
# is not done here: the desktop's assistant proposes those changes and the
# user approves each one. Expects from install.sh: REPO_ROOT, DOCKER,
# info/ok/die. Every function is safe to re-run.
#
#   front door   who owns 80/443                      Traefik (the router)
#   caddy        sandboxd's Caddy container            127.0.0.1:8088
#   existing     your nginx or Caddy                   127.0.0.1:8088
#   manual       whatever you set up                   127.0.0.1:8088

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
