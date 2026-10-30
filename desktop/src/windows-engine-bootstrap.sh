#!/usr/bin/env bash
# Runs only inside the desktop-owned WSL distribution, as Linux root.
set -euo pipefail
umask 077
export DEBIAN_FRONTEND=noninteractive
export DO_NOT_TRACK=1 SANDBOXD_TELEMETRY=off
tag="$1" commit="$2"
[[ "$tag" =~ ^v[0-9]+\.[0-9]+\.[0-9]+$ ]] || exit 2
[[ "$commit" =~ ^[a-f0-9]{40}$ ]] || exit 2
stage() { printf 'SANDBOXD_SETUP:%s\n' "$1"; }
mkdir -p /var/lib/sandboxd-desktop
exec 9>/var/lib/sandboxd-desktop/setup.lock
flock -n 9 || { echo 'Setup is already running inside this engine.'; exit 1; }
trap 'echo "Engine setup failed. Details are in /var/log/sandboxd-setup.log inside the managed engine."' ERR

if [ ! -f /var/lib/sandboxd-desktop/packages-ready ]; then
  stage packages
  printf '#!/bin/sh\nexit 101\n' > /usr/sbin/policy-rc.d
  chmod 755 /usr/sbin/policy-rc.d
  apt-get update > /var/log/sandboxd-setup.log 2>&1
  apt-get install -y --no-install-recommends ca-certificates curl git openssl docker.io docker-buildx docker-compose-v2 iptables >> /var/log/sandboxd-setup.log 2>&1
  touch /var/lib/sandboxd-desktop/packages-ready
fi
# cosign verifies the signed release images; same version the release workflow signs with.
if ! command -v cosign >/dev/null 2>&1; then
  case "$(dpkg --print-architecture)" in
    amd64) sum=bcfeae05557a9f313ee4392d2f335d0ff69ebbfd232019e3736fb04999fe1734 ;;
    arm64) sum=2cbcea1873ad76274c3f241ef175d204654e3aac3e73e6ec4504e5227015cb0a ;;
    *) echo 'Unsupported processor for the engine.'; exit 1 ;;
  esac
  curl -fsSL -o /tmp/cosign "https://github.com/sigstore/cosign/releases/download/v2.5.2/cosign-linux-$(dpkg --print-architecture)" >> /var/log/sandboxd-setup.log 2>&1
  echo "$sum  /tmp/cosign" | sha256sum -c - >> /var/log/sandboxd-setup.log 2>&1
  install -m 755 /tmp/cosign /usr/local/bin/cosign
  rm -f /tmp/cosign
fi

cat > /usr/local/sbin/sandboxd-start-docker <<'START'
#!/bin/sh
# WSL runs boot commands without PATH; dockerd needs it to find docker-proxy,
# otherwise Docker (and the engine) stays down after every Windows restart.
export PATH=/usr/local/sbin:/usr/local/bin:/usr/sbin:/usr/bin:/sbin:/bin
docker info >/dev/null 2>&1 && exit 0
mkdir -p /run
if [ -f /run/docker.pid ] && ! kill -0 "$(cat /run/docker.pid)" 2>/dev/null; then rm -f /run/docker.pid; fi
# Detach the session as well as ignoring SIGHUP: otherwise docker-proxy children
# inherit the WSL setup terminal and disappear when its session closes.
nohup setsid dockerd --host=unix:///var/run/docker.sock > /var/log/sandboxd-docker.log 2>&1 < /dev/null 9>&- &
START
chmod 755 /usr/local/sbin/sandboxd-start-docker
# Settings apply only to this dedicated distribution. Never change .wslconfig.
cat > /etc/wsl.conf <<'CONFIG'
[boot]
command=/usr/local/sbin/sandboxd-start-docker
[automount]
enabled=false
[interop]
enabled=false
appendWindowsPath=false
CONFIG
# Keep the engine's file cache small while images download and unpack. WSL
# hands freed memory back to Windows but keeps cached files, and on a busy
# 16 GB laptop that cache ran Windows out of memory and stopped the engine.
( while sleep 3; do sync; echo 1 > /proc/sys/vm/drop_caches; done ) 9>&- >/dev/null 2>&1 &
trim=$!
trap 'kill "$trim" 2>/dev/null || true' EXIT
stage docker
/usr/local/sbin/sandboxd-start-docker
for attempt in $(seq 1 90); do
  docker info >/dev/null 2>&1 && break
  sleep 2
done
docker info >/dev/null 2>&1

if [ ! -f /var/lib/sandboxd-desktop/ready ]; then
  stage source
  if [ ! -d /opt/sandboxd/.git ]; then
    mkdir -p /opt/sandboxd
    git -C /opt/sandboxd init -q
    git -C /opt/sandboxd remote add origin https://github.com/tastyeffectco/sandboxd.git
  fi
  cd /opt/sandboxd
  if ! git rev-parse --verify HEAD >/dev/null 2>&1; then
    # Keep the tag locally: the installer only pulls images for an exact release tag.
    git fetch -q --depth 1 origin "refs/tags/$tag:refs/tags/$tag" >> /var/log/sandboxd-setup.log 2>&1
    git checkout -q --detach "$tag"
  fi
  [ "$(git rev-parse HEAD)" = "$commit" ]
  if [ ! -f .env ]; then
    cp .env.example .env
    sed -i 's/^HTTP_PORT=.*/HTTP_PORT=8080/' .env
    printf '\nSANDBOXD_TELEMETRY=off\n' >> .env
    chmod 600 .env
  fi
  stage engine
  # A setup stopped mid-download (Windows out of memory, sleep, a restart) can
  # leave image layers Docker treats as complete although their files never
  # reached the disk. Nothing of the user's exists before the first ready, so
  # start the images over instead of reusing them.
  if [ -f /var/lib/sandboxd-desktop/images-started ]; then
    docker compose down --remove-orphans >> /var/log/sandboxd-setup.log 2>&1 || true
    docker system prune -af >> /var/log/sandboxd-setup.log 2>&1
  fi
  touch /var/lib/sandboxd-desktop/images-started
  sync
  # Signed release images only, no source build. Keep bootstrap keys and
  # verbose output out of renderer progress events.
  bash install.sh --pull --no-desktop --no-console >> /var/log/sandboxd-setup.log 2>&1
  # Projects need the base image's tools; empty files mean a damaged download.
  image="$(sed -n 's/^SANDBOXD_IMAGE=//p' .env)"
  if ! docker run --rm --network none --entrypoint sh "$image" -c 'test -s /usr/bin/node && test -s /usr/bin/git' \
      >> /var/log/sandboxd-setup.log 2>&1; then
    echo 'The engine download was damaged; retry setup to download it again.'
    exit 1
  fi
else
  stage starting
  cd /opt/sandboxd
  docker compose up -d --no-build --pull never >> /var/log/sandboxd-setup.log 2>&1
fi
stage health
for attempt in $(seq 1 90); do
  if curl -fsS --max-time 2 http://127.0.0.1:9090/readyz >/dev/null; then
    sync
    touch /var/lib/sandboxd-desktop/ready
    sync
    stage ready
    exit 0
  fi
  sleep 2
done
echo 'The engine did not become ready. Check /var/log/sandboxd-setup.log.'
exit 1
