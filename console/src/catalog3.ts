// App Catalog — expansion set (exhaustive verified list, part 3).
// Ports the remaining live-verified QA-sweep recipes (chunks 2–6, 11–16, 24).

import type { CatalogRecipe } from './catalog'

const SH = `#!/bin/bash
set -e
cd /home/sandbox/workspace/app
export TMPDIR=/home/sandbox/workspace/tmp
mkdir -p "$TMPDIR"
echo "▸ preparing workspace"
`

const gh = (repo: string, match: string, exclude = 'sha|sig|asc') =>
  `U=$(curl -s https://api.github.com/repos/${repo}/releases/latest | grep browser_download_url | grep -iE '${match}' | grep -viE '${exclude}' | cut -d'"' -f4 | head -1)`

const JRE = `${gh('adoptium/temurin21-binaries', 'jre_aarch64_linux_hotspot.*tar.gz')}
  curl -sL "$U" -o jre.tgz && tar xzf jre.tgz
  mv jdk-*-jre jre 2>/dev/null || mv jdk-* jre 2>/dev/null || true`

const PHP = `P=$(curl -s https://dl.static-php.dev/static-php-cli/bulk/ | grep -oE 'php-8.3.[0-9]+-cli-linux-aarch64.tar.gz' | sort -V | tail -1)
  curl -sL "https://dl.static-php.dev/static-php-cli/bulk/$P" -o php.tgz && tar xzf php.tgz && chmod +x php`

// In-sandbox TCP bridge for apps whose port is not configurable (QA chunk 18).
const BRIDGE = (upstream: number) => `python3 -c "
import socket,threading
def pipe(a,b):
    try:
        while True:
            d=a.recv(65536)
            if not d: break
            b.sendall(d)
    except Exception: pass
    finally:
        for s in (a,b):
            try: s.close()
            except Exception: pass
srv=socket.socket(); srv.setsockopt(socket.SOL_SOCKET,socket.SO_REUSEADDR,1)
srv.bind(('0.0.0.0',3000)); srv.listen(128)
while True:
    c,_=srv.accept()
    try: u=socket.create_connection(('127.0.0.1',${upstream}))
    except Exception: c.close(); continue
    threading.Thread(target=pipe,args=(c,u),daemon=True).start()
    threading.Thread(target=pipe,args=(u,c),daemon=True).start()
"`

export const CATALOG3: CatalogRecipe[] = [
  // ── binaries ──────────────────────────────────────────────────────────────
  {
    id: 'weaviate',
    name: 'Weaviate',
    blurb: 'AI-native vector database (API-only)',
    category: 'ai',
    effort: 'instant',
    modifiable: 'config',
    repo: 'https://github.com/weaviate/weaviate',
    healthPath: '/v1/.well-known/ready',
    entryPath: '/v1/meta',
    note: 'API-only — check /v1/meta. Anonymous access enabled.',
    script:
      SH +
      `if [ ! -f .catalog-installed ]; then
  ${gh('weaviate/weaviate', 'weaviate-.*-linux-arm64.tar.gz')}
  curl -sL "$U" -o w.tgz && tar xzf w.tgz && chmod +x weaviate
  touch .catalog-installed
fi
export PERSISTENCE_DATA_PATH=./data AUTHENTICATION_ANONYMOUS_ACCESS_ENABLED=true DEFAULT_VECTORIZER_MODULE=none CLUSTER_HOSTNAME=node1
exec ./weaviate --host 0.0.0.0 --port 3000 --scheme http
`,
  },
  {
    id: 'openobserve',
    name: 'OpenObserve',
    blurb: 'Logs, metrics & traces in a single binary',
    category: 'dev',
    effort: 'instant',
    modifiable: 'config',
    repo: 'https://github.com/openobserve/openobserve',
    healthPath: '/healthz',
    entryPath: '/web/',
    note: 'Pinned v0.14.7 (later releases ship no arm64 asset). Login root@example.com / Complexpass#123.',
    script:
      SH +
      `if [ ! -f .catalog-installed ]; then
  curl -sL "https://github.com/openobserve/openobserve/releases/download/v0.14.7/openobserve-v0.14.7-linux-arm64.tar.gz" -o o.tgz
  tar xzf o.tgz && chmod +x openobserve
  touch .catalog-installed
fi
export ZO_DATA_DIR=./data ZO_ROOT_USER_EMAIL=root@example.com 'ZO_ROOT_USER_PASSWORD=Complexpass#123' ZO_HTTP_ADDR=0.0.0.0 ZO_HTTP_PORT=3000
exec ./openobserve
`,
  },
  {
    id: 'seaweedfs',
    name: 'SeaweedFS',
    blurb: 'Fast distributed object store (master API)',
    category: 'data',
    effort: 'instant',
    modifiable: 'config',
    repo: 'https://github.com/seaweedfs/seaweedfs',
    healthPath: '/cluster/status',
    entryPath: '/cluster/status',
    note: 'API-only — check /cluster/status.',
    script:
      SH +
      `if [ ! -f .catalog-installed ]; then
  ${gh('seaweedfs/seaweedfs', 'linux_arm64.tar.gz')}
  curl -sL "$U" -o sw.tgz && tar xzf sw.tgz && chmod +x weed && mkdir -p data
  touch .catalog-installed
fi
exec ./weed server -dir=./data -ip=0.0.0.0 -master.port=3000
`,
  },
  {
    id: 'newapi',
    name: 'New API',
    blurb: 'LLM gateway & API management (One API fork)',
    category: 'ai',
    effort: 'instant',
    modifiable: 'config',
    repo: 'https://github.com/QuantumNous/new-api',
    healthPath: '/api/status',
    script:
      SH +
      `if [ ! -f .catalog-installed ]; then
  ${gh('QuantumNous/new-api', 'new-api-arm64|linux-arm64')}
  curl -sL "$U" -o newapi && chmod +x newapi
  touch .catalog-installed
fi
export PORT=3000
exec ./newapi --port 3000
`,
  },
  {
    id: 'spacebot',
    name: 'Spacebot',
    blurb: 'Self-hosted agentic AI workspace',
    category: 'ai',
    effort: 'instant',
    modifiable: 'config',
    repo: 'https://github.com/spacedriveapp/spacebot',
    healthPath: '/api/health',
    script:
      SH +
      `if [ ! -f .catalog-installed ]; then
  ${gh('spacedriveapp/spacebot', 'aarch64-unknown-linux-gnu|linux.*arm64')}
  curl -sL "$U" -o sb.tgz && tar xzf sb.tgz
  f=$(find . -type f -name 'spacebot' | head -1); cp "$f" ./spacebot 2>/dev/null || true; chmod +x spacebot
  printf '[api]\\nport = 3000\\nbind = "0.0.0.0"\\n' > sb.toml
  touch .catalog-installed
fi
export SPACEBOT_DEPLOYMENT=docker
exec ./spacebot start -c sb.toml -f
`,
  },
  {
    id: 'traccar',
    name: 'Traccar',
    blurb: 'GPS tracking platform (Java, H2 storage)',
    category: 'network',
    effort: 'build',
    modifiable: 'config',
    repo: 'https://github.com/traccar/traccar',
    healthPath: '/',
    note: 'Portable JRE; slow first boot (~1 min).',
    script:
      SH +
      `if [ ! -f .catalog-installed ]; then
  ${JRE}
  ${gh('traccar/traccar', 'traccar-other-.*.zip')}
  curl -sL "$U" -o tr.zip && mkdir -p tr && (cd tr && unzip -o ../tr.zip >/dev/null)
  mkdir -p tr/logs tr/data
  sed -i "s#<entry key='web.port'>[0-9]*</entry>#<entry key='web.port'>3000</entry>#" tr/conf/traccar.xml 2>/dev/null || true
  grep -q 'web.port' tr/conf/traccar.xml || sed -i "s#</properties>#<entry key='web.port'>3000</entry>\\n</properties>#" tr/conf/traccar.xml
  touch .catalog-installed
fi
cd tr
exec /home/sandbox/workspace/app/jre/bin/java -jar tracker-server.jar conf/traccar.xml
`,
  },

  // ── PHP via static-php (chunk 11) ─────────────────────────────────────────
  {
    id: 'organizr',
    name: 'Organizr',
    blurb: 'HTPC/homelab services organizer',
    category: 'productivity',
    effort: 'instant',
    modifiable: 'config',
    repo: 'https://github.com/causefx/Organizr',
    healthPath: '/',
    script:
      SH +
      `if [ ! -f .catalog-installed ]; then
  ${PHP}
  git clone --depth 1 https://github.com/causefx/Organizr org
  mkdir -p org/data && chmod -R 777 org/data
  touch .catalog-installed
fi
exec ./php -S 0.0.0.0:3000 -t org
`,
  },
  {
    id: 'drupal',
    name: 'Drupal',
    blurb: 'Enterprise CMS (SQLite installer)',
    category: 'productivity',
    effort: 'instant',
    modifiable: 'config',
    repo: 'https://github.com/drupal/drupal',
    healthPath: '/core/install.php',
    entryPath: '/core/install.php',
    note: 'Choose SQLite in the installer.',
    script:
      SH +
      `if [ ! -f .catalog-installed ]; then
  ${PHP}
  curl -sL https://ftp.drupal.org/files/projects/drupal-10.4.1.tar.gz -o dr.tgz
  mkdir -p dr && tar xzf dr.tgz -C dr --strip-components=1
  mkdir -p dr/sites/default/files
  cp dr/sites/default/default.settings.php dr/sites/default/settings.php
  chmod -R 777 dr/sites/default
  touch .catalog-installed
fi
exec ./php -S 0.0.0.0:3000 -t dr
`,
  },

  // ── Python (chunks 5, 12) ─────────────────────────────────────────────────
  {
    id: 'prefect',
    name: 'Prefect',
    blurb: 'Workflow orchestration for data pipelines',
    category: 'ai',
    effort: 'quick',
    modifiable: 'config',
    repo: 'https://github.com/PrefectHQ/prefect',
    healthPath: '/api/health',
    script:
      SH +
      `if [ ! -d .venv ]; then
  uv venv --python-preference only-managed --python 3.12 >/dev/null
  . .venv/bin/activate && uv pip install prefect
fi
. .venv/bin/activate
export PREFECT_HOME=/home/sandbox/workspace/app/.prefect PREFECT_SERVER_API_HOST=0.0.0.0
exec prefect server start --host 0.0.0.0 --port 3000
`,
  },
  {
    id: 'litellm',
    name: 'LiteLLM',
    blurb: 'Unified proxy for 100+ LLM APIs',
    category: 'ai',
    effort: 'quick',
    modifiable: 'config',
    repo: 'https://github.com/BerriAI/litellm',
    healthPath: '/health/liveliness',
    agentNotes: 'Add providers/models in `config.yaml` (model_list) and restart. OpenAI-compatible endpoints at /v1/*.',
    script:
      SH +
      `if [ ! -d .venv ]; then
  uv venv --python-preference only-managed --python 3.12 >/dev/null
  . .venv/bin/activate && uv pip install 'litellm[proxy]'
  printf 'model_list: []\\n' > config.yaml
fi
. .venv/bin/activate
exec litellm --config /home/sandbox/workspace/app/config.yaml --host 0.0.0.0 --port 3000
`,
  },
  {
    id: 'open-webui',
    name: 'Open WebUI',
    blurb: 'Chat UI for LLMs (Ollama/OpenAI-compatible)',
    category: 'ai',
    effort: 'build',
    modifiable: 'config',
    repo: 'https://github.com/open-webui/open-webui',
    healthPath: '/',
    note: 'Heavy pip install (~3 min); auth disabled for the sandbox.',
    script:
      SH +
      `if [ ! -d .venv ]; then
  uv venv --python-preference only-managed --python 3.11 >/dev/null
  . .venv/bin/activate && uv pip install open-webui
fi
. .venv/bin/activate
export DATA_DIR=/home/sandbox/workspace/app/owdata WEBUI_AUTH=False
exec open-webui serve --host 0.0.0.0 --port 3000
`,
  },
  {
    id: 'babybuddy',
    name: 'Baby Buddy',
    blurb: 'Track feedings, sleep & more for caregivers',
    category: 'productivity',
    effort: 'quick',
    modifiable: 'source',
    repo: 'https://github.com/babybuddy/babybuddy',
    healthPath: '/login/',
    entryPath: '/login/',
    script:
      SH +
      `R=/home/sandbox/workspace/app/bb
if [ ! -d "$R/.venv" ]; then
  rm -rf "$R" && git clone --depth 1 https://github.com/babybuddy/babybuddy "$R"
  cd "$R" && uv venv --python-preference only-managed --python 3.12
  . .venv/bin/activate && uv pip install -r requirements.txt
  SECRET_KEY=x DJANGO_SETTINGS_MODULE=babybuddy.settings.base python manage.py migrate --noinput
fi
cd "$R" && . .venv/bin/activate
export SECRET_KEY=catalog-changeme DJANGO_SETTINGS_MODULE=babybuddy.settings.base DEBUG=False ALLOWED_HOSTS='*'
exec python manage.py runserver 0.0.0.0:3000
`,
  },
  {
    id: 'searxng',
    name: 'SearXNG',
    blurb: 'Privacy-respecting metasearch engine',
    category: 'network',
    effort: 'build',
    modifiable: 'source',
    repo: 'https://github.com/searxng/searxng',
    healthPath: '/',
    script:
      SH +
      `R=/home/sandbox/workspace/app/sx
if [ ! -d "$R/.venv" ]; then
  rm -rf "$R" && git clone --depth 1 https://github.com/searxng/searxng "$R"
  cd "$R" && uv venv --python-preference only-managed --python 3.12
  . .venv/bin/activate && uv pip install -r requirements.txt setuptools wheel
  uv pip install -e . --no-build-isolation
  sed -i 's/^  secret_key:.*/  secret_key: "changemechangeme"/; s/^    bind_address:.*/    bind_address: "0.0.0.0"/; s/^    port:.*/    port: 3000/; s/^  limiter:.*/  limiter: false/' searx/settings.yml
fi
cd "$R" && . .venv/bin/activate
export SEARXNG_SETTINGS_PATH="$R/searx/settings.yml"
exec python -m searx.webapp
`,
  },
  {
    id: 'linkding',
    name: 'linkding',
    blurb: 'Minimal bookmark manager',
    category: 'productivity',
    effort: 'build',
    modifiable: 'source',
    repo: 'https://github.com/sissbruecker/linkding',
    healthPath: '/login/',
    entryPath: '/login/',
    note: 'Installs runtime deps minus uwsgi (base image lacks libpython for linking).',
    script:
      SH +
      `R=/home/sandbox/workspace/app/ld
if [ ! -d "$R/.venv" ]; then
  rm -rf "$R" && git clone --depth 1 https://github.com/sissbruecker/linkding "$R"
  cd "$R" && uv venv --python-preference only-managed --python 3.12
  . .venv/bin/activate
  DEPS=$(python3 -c "import tomllib;print(' '.join(repr(d) for d in tomllib.load(open('pyproject.toml','rb'))['project']['dependencies'] if 'uwsgi' not in d.lower()))" | tr -d "'")
  uv pip install $DEPS
  mkdir -p data
  LD_SUPERUSER_NAME=admin LD_SUPERUSER_PASSWORD=admin123456 python manage.py migrate --noinput || DJANGO_SETTINGS_MODULE=bookmarks.settings.dev python manage.py migrate --noinput
fi
cd "$R" && . .venv/bin/activate
export DJANGO_SETTINGS_MODULE=bookmarks.settings.dev LD_HOST_NAME=0.0.0.0 ALLOWED_HOSTS='*'
exec python manage.py runserver 0.0.0.0:3000
`,
  },
  {
    id: 'calibre-web',
    name: 'Calibre-Web',
    blurb: 'Browse & manage your ebook library',
    category: 'media',
    effort: 'quick',
    modifiable: 'config',
    repo: 'https://github.com/janeczku/calibre-web',
    healthPath: '/',
    note: 'cps has no port flag (fixed 8083) — served through an in-sandbox bridge.',
    script:
      SH +
      `if [ ! -d .venv ]; then
  uv venv --python-preference only-managed --python 3.12 >/dev/null
  . .venv/bin/activate && uv pip install calibreweb
fi
. .venv/bin/activate
cps >/home/sandbox/workspace/app/cps.log 2>&1 &
exec ${BRIDGE(8083)}
`,
  },

  // ── Node (chunks 4, 13, 14, 24) ───────────────────────────────────────────
  {
    id: 'pairdrop',
    name: 'PairDrop',
    blurb: 'AirDrop-style local file sharing in the browser',
    category: 'network',
    effort: 'quick',
    modifiable: 'source',
    repo: 'https://github.com/schlagmichdoch/PairDrop',
    healthPath: '/',
    script:
      SH +
      `R=/home/sandbox/workspace/app/pd
if [ ! -d "$R/node_modules" ]; then
  rm -rf "$R" && git clone --depth 1 https://github.com/schlagmichdoch/PairDrop "$R"
  cd "$R" && npm install --no-audit --no-fund
fi
cd "$R"
export PORT=3000
exec node server/index.js
`,
  },
  {
    id: 'uptime-kuma',
    name: 'Uptime Kuma',
    blurb: 'Fancy self-hosted uptime monitoring',
    category: 'dev',
    effort: 'build',
    modifiable: 'source',
    repo: 'https://github.com/louislam/uptime-kuma',
    healthPath: '/',
    script:
      SH +
      `R=/home/sandbox/workspace/app/uk
if [ ! -d "$R/node_modules" ]; then
  rm -rf "$R" && git clone --depth 1 https://github.com/louislam/uptime-kuma "$R"
  cd "$R" && npm ci --omit=dev && npm run download-dist
fi
cd "$R"
export UPTIME_KUMA_HOST=0.0.0.0 UPTIME_KUMA_PORT=3000 DATA_DIR=./data/ NODE_ENV=production
exec node server/server.js
`,
  },
  {
    id: 'soketi',
    name: 'Soketi',
    blurb: 'Pusher-compatible WebSockets server',
    category: 'network',
    effort: 'quick',
    modifiable: 'config',
    repo: 'https://github.com/soketi/soketi',
    healthPath: '/',
    note: 'API/WS server — the root path returns OK, WebSockets are the product.',
    script:
      SH +
      `if [ ! -d node_modules ]; then
  npm init -y >/dev/null
  npm install @soketi/soketi
  npm install 'uNetworking/uWebSockets.js#v20.51.0'
fi
export SOKETI_HOST=0.0.0.0 SOKETI_PORT=3000
exec ./node_modules/.bin/soketi start
`,
  },
  {
    id: 'next-image-transformation',
    name: 'Next Image Transformation',
    blurb: 'Drop-in image resize/optimize service (Bun)',
    category: 'media',
    effort: 'quick',
    modifiable: 'source',
    repo: 'https://github.com/coollabsio/next-image-transformation',
    healthPath: '/health',
    entryPath: '/health',
    script:
      SH +
      `R=/home/sandbox/workspace/app/nit
if [ ! -d "$R/node_modules" ]; then
  rm -rf "$R" && git clone --depth 1 https://github.com/coollabsio/next-image-transformation "$R"
  cd "$R" && bun install
fi
cd "$R"
exec bun run ./index.js
`,
  },
  {
    id: 'codimd',
    name: 'CodiMD',
    blurb: 'Realtime collaborative markdown notes',
    category: 'productivity',
    effort: 'build',
    modifiable: 'source',
    repo: 'https://github.com/hackmdio/codimd',
    healthPath: '/status',
    note: 'Long webpack build (~5 min) with the openssl-legacy-provider workaround.',
    script:
      SH +
      `R=/home/sandbox/workspace/app/cm
if [ ! -d "$R/public/build" ]; then
  rm -rf "$R" && git clone --depth 1 https://github.com/hackmdio/codimd "$R"
  cd "$R"
  npm install --ignore-scripts --legacy-peer-deps
  npm i sqlite3@5 --build-from-source
  npm i babel-polyfill @mattermost/types
  printf '{"production":{"loglevel":"info","hsts":{"enable":false}}}' > config.json
  NODE_OPTIONS='--openssl-legacy-provider --max-old-space-size=3072' npm run build
fi
cd "$R"
export CMD_DB_URL='sqlite:./db.sqlite' CMD_PORT=3000 CMD_URL_ADDPORT=false CMD_PROTOCOL_USESSL=false CMD_ALLOW_ORIGIN='*' NODE_ENV=production
exec node app.js
`,
  },
  {
    id: 'audiobookshelf',
    name: 'Audiobookshelf',
    blurb: 'Audiobook & podcast server',
    category: 'media',
    effort: 'build',
    modifiable: 'source',
    repo: 'https://github.com/advplyr/audiobookshelf',
    healthPath: '/',
    note: 'Client build needs a few minutes (max-old-space-size=3072).',
    script:
      SH +
      `R=/home/sandbox/workspace/app/abs
if [ ! -d "$R/client/dist" ]; then
  rm -rf "$R" && git clone --depth 1 https://github.com/advplyr/audiobookshelf "$R"
  cd "$R" && npm ci
  cd client && npm ci && NODE_OPTIONS=--max-old-space-size=3072 npm run generate
fi
cd "$R"
export PORT=3000 HOST=0.0.0.0 CONFIG_PATH="$R/config" METADATA_PATH="$R/metadata"
exec node index.js
`,
  },
  {
    id: 'web-check',
    name: 'Web Check',
    blurb: 'All-in-one OSINT & website analyzer',
    category: 'dev',
    effort: 'build',
    modifiable: 'source',
    repo: 'https://github.com/Lissy93/web-check',
    healthPath: '/',
    script:
      SH +
      `R=/home/sandbox/workspace/app/wc
if [ ! -d "$R/node_modules" ]; then
  rm -rf "$R" && git clone --depth 1 https://github.com/Lissy93/web-check "$R"
  cd "$R" && corepack yarn install --network-timeout 600000 && corepack yarn build
fi
cd "$R"
export PORT=3000 HOST=0.0.0.0
exec corepack yarn start
`,
  },
  {
    id: 'bento-pdf',
    name: 'BentoPDF',
    blurb: 'Privacy-first in-browser PDF toolkit',
    category: 'productivity',
    effort: 'build',
    modifiable: 'source',
    repo: 'https://github.com/alam00000/bentopdf',
    healthPath: '/',
    script:
      SH +
      `R=/home/sandbox/workspace/app/bp
if [ ! -d "$R/dist" ]; then
  rm -rf "$R"
  git clone --depth 1 https://github.com/alam00000/bentopdf "$R" || git clone --depth 1 https://github.com/BentoPDF/bentopdf "$R"
  cd "$R" && npm install && npm run build
fi
cd "$R/dist"
exec python3 -m http.server 3000 --bind 0.0.0.0
`,
  },
]
