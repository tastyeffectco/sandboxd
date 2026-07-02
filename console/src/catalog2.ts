// App Catalog — expansion set (exhaustive verified list, part 2).
//
// Recipes ported 1:1 from the QA sweep's proven provisioning commands
// (qa-reports/selfhosted/CHUNK-01..16.md + provision-scripts/), adapted to the
// self-bootstrapping catalog-run.sh pattern. Same contract as catalog.ts.

import type { CatalogRecipe } from './catalog'

const SH = `#!/bin/bash
set -e
cd /home/sandbox/workspace/app
export TMPDIR=/home/sandbox/workspace/tmp
mkdir -p "$TMPDIR"
`

const gh = (repo: string, match: string, exclude = 'sha|sig|asc') =>
  `U=$(curl -s https://api.github.com/repos/${repo}/releases/latest | grep browser_download_url | grep -iE '${match}' | grep -viE '${exclude}' | cut -d'"' -f4 | head -1)`

// Portable Temurin 21 JRE — the "runtime-drop" that unblocks the Java family
// with no custom image (QA chunk 8).
const JRE = `${gh('adoptium/temurin21-binaries', 'jre_aarch64_linux_hotspot.*tar.gz')}
  curl -sL "$U" -o jre.tgz && tar xzf jre.tgz
  mv jdk-*-jre jre 2>/dev/null || mv jdk-* jre 2>/dev/null || true`

// Static PHP binary (dl.static-php.dev) — unblocks the PHP family (chunk 7).
const PHP = `P=$(curl -s https://dl.static-php.dev/static-php-cli/bulk/ | grep -oE 'php-8.3.[0-9]+-cli-linux-aarch64.tar.gz' | sort -V | tail -1)
  curl -sL "https://dl.static-php.dev/static-php-cli/bulk/$P" -o php.tgz && tar xzf php.tgz && chmod +x php`

// *arr family config (chunk 9): bind 0.0.0.0:3000, no browser, SQLite.
const ARRCONF = `mkdir -p data && cat > data/config.xml <<'XMLEOF'
<Config>
  <Port>3000</Port>
  <BindAddress>*</BindAddress>
  <UrlBase></UrlBase>
  <EnableSsl>False</EnableSsl>
  <LaunchBrowser>False</LaunchBrowser>
</Config>
XMLEOF`

const arr = (id: string, name: string, repo: string, asset: string, bin: string, exclude = 'sha|sig|asc'): CatalogRecipe => ({
  id,
  name,
  blurb: `${name} — media automation (${id})`,
  category: 'media',
  effort: 'instant',
  modifiable: 'config',
  repo: `https://github.com/${repo}`,
  healthPath: '/ping',
  note: 'Runs with DOTNET_SYSTEM_GLOBALIZATION_INVARIANT=1 (base image has no libicu).',
  agentNotes: `Config: \`data/config.xml\`; full REST API under /api (API key in data/config.xml after first boot).`,
  script:
    SH +
    `if [ ! -f .catalog-installed ]; then
  ${gh(repo, asset, exclude)}
  curl -sL "$U" -o a.tgz && tar xzf a.tgz
  ${ARRCONF}
  touch .catalog-installed
fi
export DOTNET_SYSTEM_GLOBALIZATION_INVARIANT=1
exec ./${bin}/${bin} -nobrowser -data=/home/sandbox/workspace/app/data
`,
})

const php = (
  id: string,
  name: string,
  blurb: string,
  repo: string,
  fetch: string,
  docroot: string,
  entry = '/',
  note?: string,
): CatalogRecipe => ({
  id,
  name,
  blurb,
  category: 'productivity',
  effort: 'instant',
  modifiable: 'config',
  repo,
  healthPath: entry,
  entryPath: entry === '/' ? undefined : entry,
  note,
  agentNotes:
    'Runs on a static PHP binary (`./php`) with its built-in server — app code IS editable PHP source in the docroot, but treat vendored core as upstream; config/data dirs are the intended surface.',
  script:
    SH +
    `if [ ! -f .catalog-installed ]; then
  ${PHP}
  ${fetch}
  touch .catalog-installed
fi
exec ./php -S 0.0.0.0:3000 -t ${docroot}
`,
})

export const CATALOG2: CatalogRecipe[] = [
  // ── binaries (chunks 1–3, 6, 10) ──────────────────────────────────────────
  {
    id: 'pocketbase',
    name: 'PocketBase',
    blurb: 'Backend-in-one-file: SQLite, auth, realtime, admin UI',
    category: 'data',
    effort: 'instant',
    modifiable: 'config',
    repo: 'https://github.com/pocketbase/pocketbase',
    healthPath: '/api/health',
    entryPath: '/_/',
    note: 'Admin UI at /_/ — the root path 404s by design.',
    agentNotes:
      'REST API under /api/ (collections, records, auth). Create the first superuser with `./pocketbase superuser create EMAIL PASS --dir ./pb_data`, then drive collections via /api/collections.',
    skills: [
      {
        name: 'manage-collections',
        content: `# PocketBase: create collections & records via API
1. Create a superuser: \`./pocketbase superuser create admin@example.com admin123456 --dir ./pb_data\`
2. Auth: \`curl -s -X POST 127.0.0.1:3000/api/collections/_superusers/auth-with-password -H 'content-type: application/json' -d '{"identity":"admin@example.com","password":"admin123456"}'\` → take \`.token\`.
3. Create a collection: POST /api/collections with Authorization: Bearer TOKEN and a JSON schema ({name, type:'base', fields:[...]}).
4. CRUD records: POST/GET /api/collections/NAME/records.
Docs: https://pocketbase.io/docs/api-records/`,
      },
    ],
    script:
      SH +
      `if [ ! -f .catalog-installed ]; then
  ${gh('pocketbase/pocketbase', 'linux_arm64')}
  curl -sL "$U" -o pb.zip && unzip -o pb.zip >/dev/null && chmod +x pocketbase
  touch .catalog-installed
fi
exec ./pocketbase serve --http 0.0.0.0:3000 --dir ./pb_data
`,
  },
  {
    id: 'ntfy',
    name: 'ntfy',
    blurb: 'Simple pub-sub push notifications over HTTP',
    category: 'network',
    effort: 'instant',
    modifiable: 'config',
    repo: 'https://github.com/binwiederhier/ntfy',
    healthPath: '/v1/health',
    agentNotes: 'Publish: `curl -d "message" 127.0.0.1:3000/mytopic`; subscribe via /mytopic/json. Config: server.yml.',
    script:
      SH +
      `if [ ! -f .catalog-installed ]; then
  ${gh('binwiederhier/ntfy', 'linux_arm64.tar.gz')}
  curl -sL "$U" -o n.tgz && tar xzf n.tgz
  find . -name ntfy -type f -exec cp {} ./ntfy \\;
  chmod +x ntfy
  printf 'listen-http: ":3000"\\ncache-file: ./cache.db\\nauth-file: ./auth.db\\n' > server.yml
  touch .catalog-installed
fi
exec ./ntfy serve --config ./server.yml
`,
  },
  {
    id: 'vikunja',
    name: 'Vikunja',
    blurb: 'Self-hosted to-do & project management',
    category: 'productivity',
    effort: 'instant',
    modifiable: 'config',
    repo: 'https://github.com/go-vikunja/vikunja',
    healthPath: '/api/v1/info',
    script:
      SH +
      `if [ ! -f .catalog-installed ]; then
  ${gh('go-vikunja/vikunja', 'linux-arm64')}
  curl -sL "$U" -o v.zip
  (unzip -o v.zip >/dev/null 2>&1 || tar xzf v.zip)
  f=$(find . -name 'vikunja*' -type f ! -name '*.zip' | head -1); cp "$f" ./vikunja; chmod +x vikunja
  touch .catalog-installed
fi
export VIKUNJA_DATABASE_TYPE=sqlite VIKUNJA_DATABASE_PATH=./vikunja.db VIKUNJA_SERVICE_INTERFACE=:3000
export VIKUNJA_SERVICE_PUBLICURL=http://localhost:3000 VIKUNJA_CORS_ENABLE=false
exec ./vikunja
`,
  },
  {
    id: 'statusnook',
    name: 'Statusnook',
    blurb: 'Status pages & monitoring in one binary',
    category: 'dev',
    effort: 'instant',
    modifiable: 'config',
    repo: 'https://github.com/goksan/statusnook',
    healthPath: '/setup',
    entryPath: '/setup',
    script:
      SH +
      `if [ ! -f .catalog-installed ]; then
  ${gh('goksan/statusnook', 'linux_arm64')}
  curl -sL "$U" -o sn && chmod +x sn
  touch .catalog-installed
fi
exec ./sn -docker -port 3000
`,
  },
  {
    id: 'meilisearch',
    name: 'Meilisearch',
    blurb: 'Lightning-fast typo-tolerant search engine',
    category: 'data',
    effort: 'instant',
    modifiable: 'config',
    repo: 'https://github.com/meilisearch/meilisearch',
    healthPath: '/health',
    note: 'Master key: masterkey1234567890masterkey',
    skills: [
      {
        name: 'index-and-search',
        content: `# Meilisearch: index documents & search
Auth header: \`Authorization: Bearer masterkey1234567890masterkey\`.
1. Create index + add docs: \`curl -s -X POST '127.0.0.1:3000/indexes/movies/documents' -H "$AUTH" -H 'content-type: application/json' -d '[{"id":1,"title":"Dune"}]'\`
2. Search: \`curl -s -X POST '127.0.0.1:3000/indexes/movies/search' -H "$AUTH" -H 'content-type: application/json' -d '{"q":"dune"}'\`
Tasks are async — poll /tasks/{uid}. Docs: https://www.meilisearch.com/docs/reference/api/overview`,
      },
    ],
    script:
      SH +
      `if [ ! -f .catalog-installed ]; then
  ${gh('meilisearch/meilisearch', 'meilisearch-linux-aarch64')}
  curl -sL "$U" -o meilisearch && chmod +x meilisearch
  touch .catalog-installed
fi
exec ./meilisearch --http-addr 0.0.0.0:3000 --master-key masterkey1234567890masterkey --db-path ./data.ms
`,
  },
  {
    id: 'qdrant',
    name: 'Qdrant',
    blurb: 'Vector database for AI applications (API-only)',
    category: 'ai',
    effort: 'instant',
    modifiable: 'config',
    repo: 'https://github.com/qdrant/qdrant',
    healthPath: '/healthz',
    note: 'API-only (JSON at /) — no HTML dashboard in this build.',
    agentNotes: 'REST API: PUT /collections/NAME to create, PUT /collections/NAME/points to upsert vectors, POST /collections/NAME/points/search.',
    script:
      SH +
      `if [ ! -f .catalog-installed ]; then
  ${gh('qdrant/qdrant', 'qdrant-aarch64-unknown-linux-musl.tar.gz')}
  curl -sL "$U" -o q.tgz && tar xzf q.tgz && chmod +x qdrant
  touch .catalog-installed
fi
export QDRANT__SERVICE__HOST=0.0.0.0 QDRANT__SERVICE__HTTP_PORT=3000
exec ./qdrant
`,
  },
  {
    id: 'navidrome',
    name: 'Navidrome',
    blurb: 'Modern music server & streamer (Subsonic-compatible)',
    category: 'media',
    effort: 'instant',
    modifiable: 'config',
    repo: 'https://github.com/navidrome/navidrome',
    healthPath: '/ping',
    entryPath: '/app/',
    note: 'Drop music files into the workspace `music/` folder.',
    script:
      SH +
      `if [ ! -f .catalog-installed ]; then
  ${gh('navidrome/navidrome', 'linux_arm64.tar.gz')}
  curl -sL "$U" -o n.tgz && tar xzf n.tgz && chmod +x navidrome && mkdir -p music data
  touch .catalog-installed
fi
export ND_ADDRESS=0.0.0.0 ND_PORT=3000 ND_MUSICFOLDER=./music ND_DATAFOLDER=./data
exec ./navidrome
`,
  },
  {
    id: 'pocket-id',
    name: 'Pocket ID',
    blurb: 'Simple OIDC identity provider with passkeys',
    category: 'network',
    effort: 'instant',
    modifiable: 'config',
    repo: 'https://github.com/pocket-id/pocket-id',
    healthPath: '/',
    script:
      SH +
      `if [ ! -f .catalog-installed ]; then
  ${gh('pocket-id/pocket-id', 'linux-arm64')}
  curl -sL "$U" -o pocket-id && chmod +x pocket-id && mkdir -p data
  touch .catalog-installed
fi
export APP_URL=http://localhost:3000 PORT=3000 HOST=0.0.0.0 TRUST_PROXY=true
export ENCRYPTION_KEY=0123456789abcdef0123456789abcdef DB_PROVIDER=sqlite DB_CONNECTION_STRING=data/pocket-id.db
exec ./pocket-id
`,
  },
  {
    id: 'flipt',
    name: 'Flipt',
    blurb: 'Feature flags & experimentation platform',
    category: 'dev',
    effort: 'instant',
    modifiable: 'config',
    repo: 'https://github.com/flipt-io/flipt',
    healthPath: '/health',
    script:
      SH +
      `if [ ! -f .catalog-installed ]; then
  ${gh('flipt-io/flipt', 'flipt_linux_arm64.tar.gz', 'sha|sig|asc|sbom|darwin')}
  curl -sL "$U" -o f.tgz && tar xzf f.tgz && chmod +x flipt
  touch .catalog-installed
fi
export FLIPT_SERVER_HTTP_PORT=3000 FLIPT_SERVER_HOST=0.0.0.0
exec ./flipt server
`,
  },
  {
    id: 'forgejo',
    name: 'Forgejo',
    blurb: 'Self-hosted git forge (Gitea fork)',
    category: 'dev',
    effort: 'instant',
    modifiable: 'config',
    repo: 'https://codeberg.org/forgejo/forgejo',
    healthPath: '/api/healthz',
    agentNotes: 'Config: `data/app.ini`. Create users: `./forgejo admin user create --config data/app.ini --username u --password p --email e --admin`. Full REST API under /api/v1 (swagger at /api/swagger).',
    script:
      SH +
      `if [ ! -f .catalog-installed ]; then
  mkdir -p data
  U=$(curl -s 'https://codeberg.org/api/v1/repos/forgejo/forgejo/releases?limit=1' | grep -oE 'https://[^"]*linux-arm64' | grep -v sha | head -1)
  curl -sL "$U" -o forgejo && chmod +x forgejo
  SEC=$(./forgejo generate secret SECRET_KEY); JWT=$(./forgejo generate secret JWT_SECRET); INT=$(./forgejo generate secret INTERNAL_TOKEN)
  printf '[server]\\nHTTP_ADDR=0.0.0.0\\nHTTP_PORT=3000\\nROOT_URL=http://localhost:3000/\\n[database]\\nDB_TYPE=sqlite3\\nPATH=/home/sandbox/workspace/app/data/forgejo.db\\n[security]\\nINSTALL_LOCK=true\\nSECRET_KEY=%s\\nINTERNAL_TOKEN=%s\\n[oauth2]\\nJWT_SECRET=%s\\n' "$SEC" "$INT" "$JWT" > data/app.ini
  touch .catalog-installed
fi
export GITEA_WORK_DIR=/home/sandbox/workspace/app
exec ./forgejo web --config /home/sandbox/workspace/app/data/app.ini
`,
  },
  {
    id: 'gitea',
    name: 'Gitea',
    blurb: 'Painless self-hosted git service',
    category: 'dev',
    effort: 'instant',
    modifiable: 'config',
    repo: 'https://github.com/go-gitea/gitea',
    healthPath: '/api/healthz',
    agentNotes: 'Config: `data/app.ini`. Create users: `./gitea admin user create --config data/app.ini ...`.',
    skills: [
      {
        name: 'drive-gitea-api',
        content: `# Gitea: create repos/issues via API
1. Create a user + token: \`./gitea admin user create --config data/app.ini --username dev --password devpass123 --email dev@example.com --admin\` then \`curl -s -X POST 127.0.0.1:3000/api/v1/users/dev/tokens -u dev:devpass123 -H 'content-type: application/json' -d '{"name":"t","scopes":["all"]}'\` → \`.sha1\`.
2. Create repo: \`curl -s -X POST 127.0.0.1:3000/api/v1/user/repos -H "Authorization: token SHA1" -H 'content-type: application/json' -d '{"name":"demo","auto_init":true}'\`
3. Push: git remote http://dev:devpass123@127.0.0.1:3000/dev/demo.git
Swagger: /api/swagger`,
      },
    ],
    script:
      SH +
      `if [ ! -f .catalog-installed ]; then
  ${gh('go-gitea/gitea', 'gitea-[0-9][0-9.]*-linux-arm64', 'sha|sig|asc')}
  curl -sL "$U" -o gitea && chmod +x gitea && mkdir -p data
  SEC=$(./gitea generate secret SECRET_KEY); JWT=$(./gitea generate secret JWT_SECRET); INT=$(./gitea generate secret INTERNAL_TOKEN)
  printf '[server]\\nHTTP_ADDR=0.0.0.0\\nHTTP_PORT=3000\\nROOT_URL=http://localhost:3000/\\n[database]\\nDB_TYPE=sqlite3\\nPATH=/home/sandbox/workspace/app/data/gitea.db\\n[security]\\nINSTALL_LOCK=true\\nSECRET_KEY=%s\\nINTERNAL_TOKEN=%s\\n[oauth2]\\nJWT_SECRET=%s\\n' "$SEC" "$INT" "$JWT" > data/app.ini
  touch .catalog-installed
fi
export GITEA_WORK_DIR=/home/sandbox/workspace/app
exec ./gitea web --config /home/sandbox/workspace/app/data/app.ini
`,
  },
  {
    id: 'qbittorrent',
    name: 'qBittorrent',
    blurb: 'BitTorrent client with full web UI',
    category: 'media',
    effort: 'instant',
    modifiable: 'config',
    repo: 'https://github.com/userdocs/qbittorrent-nox-static',
    healthPath: '/',
    note: 'First-run WebUI password is printed in the process logs.',
    script:
      SH +
      `if [ ! -f .catalog-installed ]; then
  ${gh('userdocs/qbittorrent-nox-static', 'aarch64-qbittorrent-nox', 'sha|sig|asc')}
  curl -sL "$U" -o qbittorrent-nox && chmod +x qbittorrent-nox
  touch .catalog-installed
fi
exec ./qbittorrent-nox --webui-port=3000 --confirm-legal-notice --profile=/home/sandbox/workspace/app/qbt
`,
  },
  {
    id: 'librespeed',
    name: 'LibreSpeed',
    blurb: 'Self-hosted network speed test',
    category: 'network',
    effort: 'instant',
    modifiable: 'config',
    repo: 'https://github.com/librespeed/speedtest-go',
    healthPath: '/',
    script:
      SH +
      `if [ ! -f .catalog-installed ]; then
  ${gh('librespeed/speedtest-go', 'linux_arm64|linux-arm64')}
  curl -sL "$U" -o ls.tgz && tar xzf ls.tgz
  f=$(find . -type f -name 'speedtest*' ! -name '*.tgz' | head -1); cp "$f" ./speedtest; chmod +x speedtest
  printf 'listen_port = 3000\\nbind_address = "0.0.0.0"\\n[database]\\ntype = "memory"\\n' > st.toml
  touch .catalog-installed
fi
exec ./speedtest -c st.toml
`,
  },
  {
    id: 'siyuan',
    name: 'SiYuan',
    blurb: 'Privacy-first knowledge base (block editor)',
    category: 'productivity',
    effort: 'instant',
    modifiable: 'config',
    repo: 'https://github.com/siyuan-note/siyuan',
    healthPath: '/check-auth',
    entryPath: '/check-auth',
    note: 'Access code: test',
    script:
      SH +
      `if [ ! -f .catalog-installed ]; then
  ${gh('siyuan-note/siyuan', 'linux-arm64.tar.gz')}
  curl -sL "$U" -o si.tgz && tar xzf si.tgz
  K=$(find . -name 'SiYuan-Kernel' -type f | head -1); chmod +x "$K"; mkdir -p ws
  touch .catalog-installed
fi
K=$(find . -name 'SiYuan-Kernel' -type f | head -1)
R=$(dirname "$(dirname "$K")")
exec "$K" serve --workspace=/home/sandbox/workspace/app/ws --port=3000 --accessAuthCode=test --wd="$R"
`,
  },
  {
    id: 'typesense',
    name: 'Typesense',
    blurb: 'Fast, typo-tolerant open-source search',
    category: 'data',
    effort: 'instant',
    modifiable: 'config',
    repo: 'https://github.com/typesense/typesense',
    healthPath: '/health',
    note: 'API key: xyz',
    agentNotes: 'API header `X-TYPESENSE-API-KEY: xyz`; create collections at POST /collections, documents at /collections/NAME/documents, search via /collections/NAME/documents/search?q=…',
    script:
      SH +
      `if [ ! -f .catalog-installed ]; then
  V=$(curl -s https://api.github.com/repos/typesense/typesense/releases/latest | grep -oE '"tag_name": "[^"]+' | cut -d'"' -f4 | tr -d v)
  curl -sL "https://dl.typesense.org/releases/$V/typesense-server-$V-linux-arm64.tar.gz" -o ts.tgz
  tar xzf ts.tgz && chmod +x typesense-server && mkdir -p tsdata
  touch .catalog-installed
fi
exec ./typesense-server --data-dir /home/sandbox/workspace/app/tsdata --api-key=xyz --api-address 0.0.0.0 --api-port 3000
`,
  },
  {
    id: 'goatcounter',
    name: 'GoatCounter',
    blurb: 'Privacy-friendly web analytics',
    category: 'dev',
    effort: 'instant',
    modifiable: 'config',
    repo: 'https://github.com/arp242/goatcounter',
    healthPath: '/',
    note: 'Login a@a.com / testtest123 (site pre-created).',
    script:
      SH +
      `if [ ! -f .catalog-installed ]; then
  ${gh('arp242/goatcounter', 'linux-arm64')}
  curl -sL "$U" -o gc.gz
  (gunzip -f gc.gz 2>/dev/null && mv gc goatcounter) || mv gc.gz goatcounter
  chmod +x goatcounter && mkdir -p db
  ./goatcounter db create site -vhost localhost -user.email a@a.com -password testtest123 -db 'sqlite+./db/gc.sqlite3' -createdb || true
  touch .catalog-installed
fi
exec ./goatcounter serve -listen 0.0.0.0:3000 -tls=none -db 'sqlite+./db/gc.sqlite3'
`,
  },
  {
    id: 'grafana',
    name: 'Grafana',
    blurb: 'Dashboards & observability platform',
    category: 'dev',
    effort: 'quick',
    modifiable: 'config',
    repo: 'https://github.com/grafana/grafana',
    healthPath: '/api/health',
    note: 'Login admin/admin. ~180 MB download; first boot ~30 s.',
    skills: [
      {
        name: 'dashboards-api',
        content: `# Grafana: create dashboards via API
Basic auth admin:admin (or create an API token via /api/auth/keys).
Create dashboard: \`curl -s -X POST 127.0.0.1:3000/api/dashboards/db -u admin:admin -H 'content-type: application/json' -d '{"dashboard":{"title":"My dash","panels":[]},"overwrite":true}'\`
Datasources: POST /api/datasources. Docs: https://grafana.com/docs/grafana/latest/developers/http_api/`,
      },
    ],
    script:
      SH +
      `if [ ! -f .catalog-installed ]; then
  V=$(curl -s https://api.github.com/repos/grafana/grafana/releases/latest | grep -oE '"tag_name": "[^"]+' | cut -d'"' -f4 | tr -d v)
  curl -sL "https://dl.grafana.com/oss/release/grafana-$V.linux-arm64.tar.gz" -o gf.tgz && tar xzf gf.tgz
  touch .catalog-installed
fi
D=$(find . -maxdepth 1 -type d -name 'grafana-*' | head -1)
cd "$D"
exec ./bin/grafana server --homepath "$PWD" cfg:server.http_addr=0.0.0.0 cfg:server.http_port=3000 cfg:paths.data=/home/sandbox/workspace/app/gfdata cfg:paths.logs=/home/sandbox/workspace/app/gflogs
`,
  },
  {
    id: 'cloudreve',
    name: 'Cloudreve',
    blurb: 'Self-hosted cloud drive with a polished UI',
    category: 'productivity',
    effort: 'instant',
    modifiable: 'config',
    repo: 'https://github.com/cloudreve/Cloudreve',
    healthPath: '/api/v4/site/ping',
    note: 'Register the first account in the UI (becomes admin).',
    script:
      SH +
      `if [ ! -f .catalog-installed ]; then
  ${gh('cloudreve/Cloudreve', 'linux_arm64|linux-arm64')}
  curl -sL "$U" -o cr.tgz && tar xzf cr.tgz && chmod +x cloudreve
  mkdir -p data && printf '[System]\\nListen = :3000\\n' > data/conf.ini
  touch .catalog-installed
fi
exec ./cloudreve server -c /home/sandbox/workspace/app/data/conf.ini
`,
  },

  // ── Java family via portable JRE (chunk 8) ────────────────────────────────
  {
    id: 'metabase',
    name: 'Metabase',
    blurb: 'BI dashboards & analytics (embedded H2 DB)',
    category: 'data',
    effort: 'build',
    modifiable: 'config',
    repo: 'https://github.com/metabase/metabase',
    healthPath: '/api/health',
    note: '~700 MB download + 2–3 min first boot (H2 migrations) — be patient.',
    script:
      SH +
      `if [ ! -f .catalog-installed ]; then
  ${JRE}
  curl -sL https://downloads.metabase.com/latest/metabase.jar -o metabase.jar
  touch .catalog-installed
fi
export MB_JETTY_HOST=0.0.0.0 MB_JETTY_PORT=3000 MB_DB_FILE=/home/sandbox/workspace/app/metabase.db
exec ./jre/bin/java -Xmx1500m -jar metabase.jar
`,
  },
  {
    id: 'keycloak',
    name: 'Keycloak',
    blurb: 'Identity & access management (OIDC/SAML)',
    category: 'network',
    effort: 'build',
    modifiable: 'config',
    repo: 'https://github.com/keycloak/keycloak',
    healthPath: '/',
    note: 'Dev mode, H2 storage; admin/admin. Boots in ~90 s.',
    agentNotes: 'Admin REST API: get a token from /realms/master/protocol/openid-connect/token (client admin-cli, user admin/admin), then manage realms/clients/users under /admin/realms.',
    script:
      SH +
      `if [ ! -f .catalog-installed ]; then
  ${JRE}
  ${gh('keycloak/keycloak', 'keycloak-.*.tar.gz')}
  curl -sL "$U" -o kc.tgz && tar xzf kc.tgz && mv keycloak-* kc
  touch .catalog-installed
fi
export JAVA_HOME=/home/sandbox/workspace/app/jre PATH=/home/sandbox/workspace/app/jre/bin:$PATH
export KEYCLOAK_ADMIN=admin KEYCLOAK_ADMIN_PASSWORD=admin
exec ./kc/bin/kc.sh start-dev --http-host=0.0.0.0 --http-port=3000 --hostname-strict=false
`,
  },

  // ── .NET family, self-contained (chunk 9) ─────────────────────────────────
  arr('prowlarr', 'Prowlarr', 'Prowlarr/Prowlarr', 'linux-core-arm64.tar.gz', 'Prowlarr', 'sha|sig|asc|musl'),
  arr('sonarr', 'Sonarr', 'Sonarr/Sonarr', 'linux-arm64.tar.gz', 'Sonarr', 'sha|sig|asc|musl'),
  {
    id: 'duplicati',
    name: 'Duplicati',
    blurb: 'Encrypted backups to any storage backend',
    category: 'productivity',
    effort: 'instant',
    modifiable: 'config',
    repo: 'https://github.com/duplicati/duplicati',
    healthPath: '/',
    script:
      SH +
      `if [ ! -f .catalog-installed ]; then
  ${gh('duplicati/duplicati', 'linux-arm64-gui.zip', 'sig|deb|rpm|sha|appimage')}
  curl -sL "$U" -o d.zip && mkdir -p dup
  (cd dup && (unzip -o ../d.zip >/dev/null 2>&1 || tar xzf ../d.zip))
  touch .catalog-installed
fi
export DOTNET_SYSTEM_GLOBALIZATION_INVARIANT=1
B=$(find dup -name duplicati-server -type f | head -1)
exec "$B" --webservice-interface=any --webservice-port=3000 --server-datafolder=/home/sandbox/workspace/app/ddata
`,
  },

  // ── PHP family via static-php (chunk 7) ───────────────────────────────────
  php('dokuwiki', 'DokuWiki', 'Wiki without a database (flat files)', 'https://github.com/dokuwiki/dokuwiki',
    `curl -sL https://download.dokuwiki.org/src/dokuwiki/dokuwiki-stable.tgz -o dw.tgz && mkdir -p dw && tar xzf dw.tgz -C dw --strip-components=1`,
    'dw', '/doku.php', 'Run the installer at /install.php on first visit.'),
  php('privatebin', 'PrivateBin', 'Zero-knowledge encrypted pastebin', 'https://github.com/PrivateBin/PrivateBin',
    `U=$(curl -s https://api.github.com/repos/PrivateBin/PrivateBin/releases/latest | grep tarball_url | cut -d'"' -f4)
  mkdir -p pb && curl -sL "$U" -o pb.tgz && tar xzf pb.tgz -C pb --strip-components=1 && mkdir -p pb/data && chmod 777 pb/data`,
    'pb'),
  php('freshrss', 'FreshRSS', 'Self-hosted RSS aggregator', 'https://github.com/FreshRSS/FreshRSS',
    `git clone --depth 1 https://github.com/FreshRSS/FreshRSS fr && mkdir -p fr/data && chmod -R 777 fr/data`,
    'fr/p', '/', 'SQLite via the web installer.'),
  php('grocy', 'Grocy', 'ERP for your kitchen — groceries & chores', 'https://github.com/grocy/grocy',
    `U=$(curl -s https://api.github.com/repos/grocy/grocy/releases/latest | grep browser_download_url | grep '.zip' | head -1 | cut -d'"' -f4)
  mkdir -p gr && curl -sL "$U" -o gr.zip && (cd gr && unzip -o ../gr.zip >/dev/null)
  cp gr/config-dist.php gr/data/config.php 2>/dev/null || true
  mkdir -p gr/data && chmod -R 777 gr/data`,
    'gr/public'),
  php('mediawiki', 'MediaWiki', 'The wiki engine that runs Wikipedia', 'https://github.com/wikimedia/mediawiki',
    `curl -sL https://releases.wikimedia.org/mediawiki/1.43/mediawiki-1.43.0.tar.gz -o mw.tgz && mkdir -p mw && tar xzf mw.tgz -C mw --strip-components=1`,
    'mw', '/', 'Run the SQLite installer on first visit.'),

  // ── Python (chunks 5, 10, 12; this-session verified) ──────────────────────
  {
    id: 'jupyter',
    name: 'JupyterLab',
    blurb: 'Interactive notebooks for Python',
    category: 'ai',
    effort: 'quick',
    modifiable: 'config',
    repo: 'https://github.com/jupyterlab/jupyterlab',
    healthPath: '/',
    script:
      SH +
      `if [ ! -d .venv ]; then
  uv venv --python-preference only-managed --python 3.12 >/dev/null
  . .venv/bin/activate && uv pip install jupyterlab
fi
. .venv/bin/activate
exec jupyter lab --ip=0.0.0.0 --port=3000 --no-browser --IdentityProvider.token='' --ServerApp.allow_origin='*' --ServerApp.allow_remote_access=True
`,
  },
  {
    id: 'marimo',
    name: 'marimo',
    blurb: 'Reactive Python notebooks (next-gen Jupyter)',
    category: 'ai',
    effort: 'quick',
    modifiable: 'config',
    repo: 'https://github.com/marimo-team/marimo',
    healthPath: '/',
    script:
      SH +
      `if [ ! -d .venv ]; then
  uv venv --python-preference only-managed --python 3.12 >/dev/null
  . .venv/bin/activate && uv pip install marimo
fi
. .venv/bin/activate
exec marimo edit --host 0.0.0.0 --port 3000 --no-token --headless --skip-update-check --allow-origins '*'
`,
  },
  {
    id: 'chroma',
    name: 'Chroma',
    blurb: 'AI-native embeddings database (API-only)',
    category: 'ai',
    effort: 'quick',
    modifiable: 'config',
    repo: 'https://github.com/chroma-core/chroma',
    healthPath: '/api/v2/heartbeat',
    note: 'API-only — verify at /api/v2/heartbeat.',
    script:
      SH +
      `if [ ! -d .venv ]; then
  uv venv --python-preference only-managed --python 3.12 >/dev/null
  . .venv/bin/activate && uv pip install chromadb
fi
. .venv/bin/activate
exec chroma run --host 0.0.0.0 --port 3000 --path /home/sandbox/workspace/app/data
`,
  },
  {
    id: 'labelstudio',
    name: 'Label Studio',
    blurb: 'Data labeling for ML teams',
    category: 'ai',
    effort: 'build',
    modifiable: 'config',
    repo: 'https://github.com/HumanSignal/label-studio',
    healthPath: '/user/login',
    entryPath: '/user/login',
    note: 'Heavy pip install (~2 min) + slow first boot.',
    script:
      SH +
      `if [ ! -d .venv ]; then
  uv venv --python-preference only-managed --python 3.12 >/dev/null
  . .venv/bin/activate && uv pip install label-studio
fi
. .venv/bin/activate
export LABEL_STUDIO_BASE_DATA_DIR=/home/sandbox/workspace/app/data
exec label-studio start --host 0.0.0.0 --port 3000 --no-browser
`,
  },
  {
    id: 'superset',
    name: 'Apache Superset',
    blurb: 'Data exploration & visualization platform',
    category: 'data',
    effort: 'build',
    modifiable: 'config',
    repo: 'https://github.com/apache/superset',
    healthPath: '/login/',
    entryPath: '/login/',
    note: 'Login admin/admin123456. SQLite metadata; install ~4 min.',
    script:
      SH +
      `export SUPERSET_SECRET_KEY=changemechangeme SUPERSET_HOME=/home/sandbox/workspace/app/sshome FLASK_APP=superset
if [ ! -d .venv ]; then
  uv venv --python-preference only-managed --python 3.11 >/dev/null
  . .venv/bin/activate && uv pip install apache-superset rich cachetools
  mkdir -p "$SUPERSET_HOME"
  superset db upgrade
  superset fab create-admin --username admin --firstname a --lastname b --email a@e.com --password admin123456 || true
  superset init
fi
. .venv/bin/activate
exec gunicorn -w 2 -b 0.0.0.0:3000 'superset.app:create_app()'
`,
  },

  // ── Node (this-session verified) ──────────────────────────────────────────
  {
    id: 'bluesky-pds',
    name: 'Bluesky PDS',
    blurb: 'Personal Data Server for the AT Protocol',
    category: 'network',
    effort: 'quick',
    modifiable: 'config',
    repo: 'https://github.com/bluesky-social/atproto',
    healthPath: '/',
    note: 'Dev-mode PDS on SQLite; admin password admin123456.',
    script:
      SH +
      `if [ ! -d node_modules ]; then
  npm init -y >/dev/null
  npm install @atproto/pds
  mkdir -p pdsdata/blobs
  cat > pds.mjs <<'MJSEOF'
import { PDS, envToCfg, envToSecrets } from "@atproto/pds"
const env = {
  port: 3000, hostname: "localhost",
  dataDirectory: "/home/sandbox/workspace/app/pdsdata",
  blobstoreDiskLocation: "/home/sandbox/workspace/app/pdsdata/blobs",
  devMode: true,
  jwtSecret: "x".repeat(32), adminPassword: "admin123456",
  plcRotationKeyK256PrivateKeyHex: "4f2b1c9d8e7a6b5c4d3e2f1a0b9c8d7e6f5a4b3c2d1e0f9a8b7c6d5e4f3a2b1c"
}
const pds = await PDS.create(envToCfg(env), envToSecrets(env))
await pds.start(); console.log("PDS running on 3000")
MJSEOF
fi
exec node pds.mjs
`,
  },
]
