#!/usr/bin/env bash
# Runs only inside the desktop-owned WSL distribution, as Linux root.
set -euo pipefail
umask 077
export DEBIAN_FRONTEND=noninteractive
export DO_NOT_TRACK=1 SANDBOXD_TELEMETRY=off
ref="$1"
[[ "$ref" =~ ^[a-f0-9]{40}$ ]] || exit 2
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

cat > /usr/local/sbin/sandboxd-start-docker <<'START'
#!/bin/sh
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
    git fetch -q --depth 1 origin "$ref" >> /var/log/sandboxd-setup.log 2>&1
    git checkout -q --detach FETCH_HEAD
  fi
  [ "$(git rev-parse HEAD)" = "$ref" ]
  if [ ! -f .env ]; then
    cp .env.example .env
    sed -i 's/^HTTP_PORT=.*/HTTP_PORT=8080/' .env
    printf '\nSANDBOXD_TELEMETRY=off\n' >> .env
    chmod 600 .env
  fi
  stage engine
  # Keep bootstrap keys and verbose build output out of renderer progress events.
  bash install.sh --build --no-desktop --no-console >> /var/log/sandboxd-setup.log 2>&1
else
  stage starting
  cd /opt/sandboxd
  docker compose up -d --no-build --pull never >> /var/log/sandboxd-setup.log 2>&1
fi
stage health
for attempt in $(seq 1 90); do
  if curl -fsS --max-time 2 http://127.0.0.1:9090/readyz >/dev/null; then
    touch /var/lib/sandboxd-desktop/ready
    stage ready
    exit 0
  fi
  sleep 2
done
echo 'The engine did not become ready. Check /var/log/sandboxd-setup.log.'
exit 1
