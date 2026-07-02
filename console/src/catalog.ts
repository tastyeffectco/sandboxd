// App Catalog (v1) — curated one-click recipes for the App Store view.
//
// Contract: docs/APP-CATALOG-CONTRACT.md. Every recipe is a self-bootstrapping
// `catalog-run.sh` (install-once guard + exec) written into the sandbox via the
// public /v1 files API, plus a standard sandbox.yaml pointing at it. The console
// talks ONLY to /v1 — no core changes, no new endpoints.
//
// Every script here was live-verified on sandboxd (arm64, SQLite/on-disk state)
// during the self-hosted catalog QA sweep (qa-reports/selfhosted/CHUNK-17..25.md).

import { CATALOG2 } from './catalog2'

export type CatalogCategory = 'dev' | 'productivity' | 'media' | 'data' | 'network' | 'ai' | 'other'
export type CatalogEffort = 'instant' | 'quick' | 'build'

// How agent tasks can change an installed app:
//  - 'source': the app's source is cloned into the workspace — tasks can edit
//    code, rebuild, and restart (full sandboxd experience).
//  - 'config': the app runs from a prebuilt release binary (black box); tasks
//    can edit its config files, catalog-run.sh flags/env, plugins, and data —
//    but not the app's own code.
export type CatalogModifiable = 'source' | 'config'

export interface CatalogRecipe {
  id: string
  name: string
  blurb: string
  category: CatalogCategory
  effort: CatalogEffort
  modifiable: CatalogModifiable
  repo: string // upstream source repo — agents read it to understand the app
  script: string // catalog-run.sh content (idempotent install + exec run)
  healthPath: string // must be a 200 route
  entryPath?: string // UI path when not '/'
  note?: string
  // Agent-task context: what an agent should know to modify this app
  // (config file paths, restart semantics). Written to workspace/app/AGENTS.md
  // at install so tasks land with real context instead of a mystery binary.
  agentNotes?: string
  // Optional per-app skills: small how-to docs written to workspace/app/skills/
  // that teach agents to OPERATE the running app (create an n8n workflow via
  // its API, send a gotify message, …) — not just edit files.
  skills?: { name: string; content: string }[]
}

// AGENTS.md written into the workspace at install — the contract between the
// catalog and agent tasks (`POST /v1/sandboxes/{id}/tasks`).
export function recipeAgentsMd(r: CatalogRecipe): string {
  const lines = [
    `# ${r.name} (installed from the sandboxd App Store)`,
    '',
    `This workspace runs **${r.name}** — ${r.blurb}.`,
    '',
    `- Upstream source: ${r.repo} — read it (or its docs) to understand the app before non-trivial changes.`,
    '- Supervision: `sandbox.yaml` → `web.command` → `catalog-run.sh` (install-once guard, then `exec` the server on 0.0.0.0:3000).',
    '- The app listens on http://127.0.0.1:3000 inside this sandbox — you can drive its HTTP API directly with curl.',
    '- To apply changes: edit files, then restart the web process (stop/start the sandbox; runtimed restarts it).',
    '- Keep the server binding to 0.0.0.0:3000 and all state inside the workspace.',
    r.modifiable === 'source'
      ? "- The app's FULL SOURCE lives in this workspace — you may edit code and rebuild (see catalog-run.sh for the build steps its install phase used)."
      : "- The app runs from a PREBUILT RELEASE BINARY: do NOT try to edit the app's own code. You CAN edit its configuration, catalog-run.sh flags/env, plugins, and data.",
  ]
  if (r.agentNotes) lines.push('', r.agentNotes)
  if (r.skills?.length) {
    lines.push('', '## Skills', '', 'Task-oriented how-tos for operating this app live in `skills/`:')
    for (const sk of r.skills) lines.push(`- skills/${sk.name}.md`)
  }
  lines.push('')
  return lines.join('\n')
}

export function recipeManifest(r: CatalogRecipe): string {
  return [
    'version: 1',
    'web:',
    '  command: "exec bash /home/sandbox/workspace/app/catalog-run.sh"',
    '  port: 3000',
    `  health_path: "${r.healthPath}"`,
    'build:',
    '  command: ""',
    '',
  ].join('\n')
}

// Shared script prelude: workspace cwd, writable TMPDIR (host /tmp is noexec).
const SH = `#!/bin/bash
set -e
cd /home/sandbox/workspace/app
export TMPDIR=/home/sandbox/workspace/tmp
mkdir -p "$TMPDIR"
`

// Latest-release asset lookup via the GitHub API. Works today; note the shared
// 60/hr unauthenticated rate limit (contract §5) — pinned URLs are preferred
// when asset names are stable.
const ghAsset = (repo: string, match: string, exclude = 'sha|sig|asc') =>
  `U=$(curl -s https://api.github.com/repos/${repo}/releases/latest | grep browser_download_url | grep -iE '${match}' | grep -viE '${exclude}' | cut -d'"' -f4 | head -1)`

export const CATALOG: CatalogRecipe[] = [
  // ───────────────────────── instant: prebuilt binaries ─────────────────────────
  {
    id: 'filebrowser',
    name: 'File Browser',
    blurb: 'Web file manager for the sandbox workspace',
    category: 'productivity',
    effort: 'instant',
    modifiable: 'config',
    repo: 'https://github.com/filebrowser/filebrowser',
    agentNotes: 'Settings live in `fb.db` — manage via `./filebrowser config set ... -d fb.db` and `./filebrowser users ... -d fb.db`. Files served from `root/`.',
    healthPath: '/',
    note: 'Create users via the CLI; first-run DB is initialized automatically.',
    script:
      SH +
      `if [ ! -f .catalog-installed ]; then
  curl -sL https://github.com/filebrowser/filebrowser/releases/latest/download/linux-arm64-filebrowser.tar.gz -o fb.tgz
  tar xzf fb.tgz filebrowser && chmod +x filebrowser && mkdir -p root
  ./filebrowser config init -d fb.db || true
  touch .catalog-installed
fi
exec ./filebrowser -a 0.0.0.0 -p 3000 -d fb.db -r root
`,
  },
  {
    id: 'gotify',
    name: 'Gotify',
    blurb: 'Self-hosted push notification server',
    category: 'network',
    effort: 'instant',
    modifiable: 'config',
    repo: 'https://github.com/gotify/server',
    skills: [{ name: 'send-notification', content: `# Gotify: send a notification
1. Login admin/admin → create an application: \`curl -s -u admin:admin -X POST 127.0.0.1:3000/application -H 'content-type: application/json' -d '{"name":"agent"}'\` → take \`.token\`.
2. Send: \`curl -s -X POST '127.0.0.1:3000/message?token=APPTOKEN' -F title=Hello -F message='It works' -F priority=5\`
3. Read messages: \`curl -s -u admin:admin 127.0.0.1:3000/message\`` }],
    agentNotes: 'Config via GOTIFY_* env in `catalog-run.sh`; plugins dir supported. Admin login admin/admin. DB: gotify.db (SQLite).',
    healthPath: '/',
    note: 'Default login admin/admin.',
    script:
      SH +
      `if [ ! -f .catalog-installed ]; then
  curl -sL https://github.com/gotify/server/releases/latest/download/gotify-linux-arm64.zip -o gotify.zip
  unzip -o gotify.zip >/dev/null && chmod +x gotify-linux-arm64
  touch .catalog-installed
fi
export GOTIFY_SERVER_PORT=3000 GOTIFY_DATABASE__DIALECT=sqlite3 GOTIFY_DATABASE__CONNECTION=/home/sandbox/workspace/app/gotify.db
exec ./gotify-linux-arm64
`,
  },
  {
    id: 'beszel',
    name: 'Beszel',
    blurb: 'Lightweight server monitoring hub',
    category: 'dev',
    effort: 'instant',
    modifiable: 'config',
    repo: 'https://github.com/henrygd/beszel',
    healthPath: '/',
    script:
      SH +
      `if [ ! -f .catalog-installed ]; then
  ${ghAsset('henrygd/beszel', 'beszel_linux_arm64', 'agent|sha|sig')}
  curl -sL "$U" -o beszel.tgz && tar xzf beszel.tgz beszel && chmod +x beszel
  touch .catalog-installed
fi
exec ./beszel serve --http 0.0.0.0:3000
`,
  },
  {
    id: 'glance',
    name: 'Glance',
    blurb: 'Personal dashboard with widgets',
    category: 'productivity',
    effort: 'instant',
    modifiable: 'config',
    repo: 'https://github.com/glanceapp/glance',
    skills: [{ name: 'add-widgets', content: `# Glance: customize the dashboard
Everything is \`glance.yml\`. Add widgets under pages[].columns[].widgets, e.g.:
\`\`\`yaml
- type: rss
  feeds:
    - url: https://news.ycombinator.com/rss
- type: weather
  location: Berlin, Germany
\`\`\`
Then restart the web process. Widget list: https://github.com/glanceapp/glance/blob/main/docs/configuration.md` }],
    agentNotes: 'Config: `glance.yml` (pages/columns/widgets — clock, weather, rss, bookmarks…). Edit it and restart to change the dashboard.',
    healthPath: '/',
    script:
      SH +
      `if [ ! -f .catalog-installed ]; then
  ${ghAsset('glanceapp/glance', 'linux-arm64')}
  curl -sL "$U" -o glance.tgz && tar xzf glance.tgz glance && chmod +x glance
  printf 'server:\\n  host: 0.0.0.0\\n  port: 3000\\npages:\\n  - name: Home\\n    columns:\\n      - size: full\\n        widgets:\\n          - type: clock\\n' > glance.yml
  touch .catalog-installed
fi
exec ./glance --config glance.yml
`,
  },
  {
    id: 'mailpit',
    name: 'Mailpit',
    blurb: 'Email testing tool with web UI (SMTP catch-all)',
    category: 'dev',
    effort: 'instant',
    modifiable: 'config',
    repo: 'https://github.com/axllent/mailpit',
    healthPath: '/',
    script:
      SH +
      `if [ ! -f .catalog-installed ]; then
  ${ghAsset('axllent/mailpit', 'linux-arm64')}
  curl -sL "$U" -o mailpit.tgz && tar xzf mailpit.tgz mailpit && chmod +x mailpit
  touch .catalog-installed
fi
exec ./mailpit --listen 0.0.0.0:3000 --database /home/sandbox/workspace/app/mailpit.db
`,
  },
  {
    id: 'syncthing',
    name: 'Syncthing',
    blurb: 'Continuous peer-to-peer file synchronization',
    category: 'network',
    effort: 'instant',
    modifiable: 'config',
    repo: 'https://github.com/syncthing/syncthing',
    healthPath: '/',
    script:
      SH +
      `if [ ! -f .catalog-installed ]; then
  ${ghAsset('syncthing/syncthing', 'linux-arm64')}
  curl -sL "$U" -o st.tgz && tar xzf st.tgz
  mv syncthing-linux-arm64-*/syncthing . && chmod +x syncthing
  touch .catalog-installed
fi
exec ./syncthing --gui-address=0.0.0.0:3000 --no-browser --home=/home/sandbox/workspace/app/stconfig --no-restart
`,
  },
  {
    id: 'sftpgo',
    name: 'SFTPGo',
    blurb: 'SFTP/WebDAV server with a full admin web UI',
    category: 'network',
    effort: 'instant',
    modifiable: 'config',
    repo: 'https://github.com/drakkan/sftpgo',
    healthPath: '/',
    script:
      SH +
      `if [ ! -f .catalog-installed ]; then
  ${ghAsset('drakkan/sftpgo', 'linux_arm64.*tar.xz')}
  curl -sL "$U" -o sftpgo.txz && tar xf sftpgo.txz && chmod +x sftpgo
  touch .catalog-installed
fi
export SFTPGO_HTTPD__BINDINGS__0__ADDRESS=0.0.0.0 SFTPGO_HTTPD__BINDINGS__0__PORT=3000
export SFTPGO_HTTPD__TEMPLATES_PATH=/home/sandbox/workspace/app/templates SFTPGO_HTTPD__STATIC_FILES_PATH=/home/sandbox/workspace/app/static SFTPGO_HTTPD__OPENAPI_PATH=/home/sandbox/workspace/app/openapi
export SFTPGO_DATA_PROVIDER__DRIVER=sqlite SFTPGO_DATA_PROVIDER__NAME=/home/sandbox/workspace/app/sftpgo.db
exec ./sftpgo serve
`,
  },
  {
    id: 'memos',
    name: 'Memos',
    blurb: 'Lightweight self-hosted note taking',
    category: 'productivity',
    effort: 'instant',
    modifiable: 'config',
    repo: 'https://github.com/usememos/memos',
    healthPath: '/',
    script:
      SH +
      `if [ ! -f .catalog-installed ]; then
  ${ghAsset('usememos/memos', 'linux-arm64|linux_arm64')}
  curl -sL "$U" -o memos.tgz && tar xzf memos.tgz && chmod +x memos && mkdir -p memosdata
  touch .catalog-installed
fi
exec ./memos --addr 0.0.0.0 --port 3000 --data /home/sandbox/workspace/app/memosdata
`,
  },
  {
    id: 'wakapi',
    name: 'Wakapi',
    blurb: 'WakaTime-compatible coding statistics',
    category: 'dev',
    effort: 'instant',
    modifiable: 'config',
    repo: 'https://github.com/muety/wakapi',
    healthPath: '/',
    note: 'IPv6 listener disabled (upstream dual-stack bug #860).',
    script:
      SH +
      `if [ ! -f .catalog-installed ]; then
  ${ghAsset('muety/wakapi', 'linux_arm64|linux-arm64')}
  curl -sL "$U" -o wakapi.zip
  (unzip -o wakapi.zip >/dev/null 2>&1 || tar xzf wakapi.zip) && chmod +x wakapi
  touch .catalog-installed
fi
export WAKAPI_DB_TYPE=sqlite3 WAKAPI_DB_NAME=/home/sandbox/workspace/app/wakapi.db
export WAKAPI_PORT=3000 WAKAPI_LISTEN_IPV4=0.0.0.0 WAKAPI_LISTEN_IPV6=- WAKAPI_INSECURE_COOKIES=true
exec ./wakapi
`,
  },
  {
    id: 'trailbase',
    name: 'TrailBase',
    blurb: 'Single-binary Firebase alternative on SQLite',
    category: 'data',
    effort: 'instant',
    modifiable: 'config',
    repo: 'https://github.com/trailbaseio/trailbase',
    healthPath: '/_/admin/',
    entryPath: '/_/admin/',
    note: 'Admin UI at /_/admin/ (root path serves the API, not a page).',
    script:
      SH +
      `if [ ! -f .catalog-installed ]; then
  ${ghAsset('trailbaseio/trailbase', 'linux.*arm64|arm64.*linux')}
  curl -sL "$U" -o tb.zip
  (unzip -o tb.zip >/dev/null 2>&1 || tar xzf tb.zip) && chmod +x trail
  touch .catalog-installed
fi
exec ./trail --data-dir /home/sandbox/workspace/app/traildepot run --address 0.0.0.0:3000
`,
  },
  {
    id: 'code-server',
    name: 'code-server',
    blurb: 'VS Code in the browser',
    category: 'dev',
    effort: 'instant',
    modifiable: 'config',
    repo: 'https://github.com/coder/code-server',
    healthPath: '/',
    script:
      SH +
      `if [ ! -f .catalog-installed ]; then
  ${ghAsset('coder/code-server', 'linux-arm64.tar.gz')}
  curl -sL "$U" -o cs.tgz && tar xzf cs.tgz && mv code-server-*-linux-arm64 cs
  touch .catalog-installed
fi
exec ./cs/bin/code-server --bind-addr 0.0.0.0:3000 --auth none --disable-telemetry
`,
  },
  {
    id: 'cyberchef',
    name: 'CyberChef',
    blurb: 'The cyber Swiss-army knife (encode/decode/analyse)',
    category: 'dev',
    effort: 'instant',
    modifiable: 'config',
    repo: 'https://github.com/gchq/CyberChef',
    healthPath: '/',
    script:
      SH +
      `if [ ! -f .catalog-installed ]; then
  ${ghAsset('gchq/CyberChef', '.zip')}
  mkdir -p cc && curl -sL "$U" -o cc.zip
  (cd cc && unzip -o ../cc.zip >/dev/null && cp CyberChef_*.html index.html)
  touch .catalog-installed
fi
cd cc
exec python3 -m http.server 3000 --bind 0.0.0.0
`,
  },
  {
    id: 'gowa',
    name: 'WhatsApp Web API (gowa)',
    blurb: 'Multi-device WhatsApp Web API gateway',
    category: 'network',
    effort: 'instant',
    modifiable: 'config',
    repo: 'https://github.com/aldinokemal/go-whatsapp-web-multidevice',
    healthPath: '/',
    script:
      SH +
      `if [ ! -f .catalog-installed ]; then
  ${ghAsset('aldinokemal/go-whatsapp-web-multidevice', 'linux.*arm64|arm64.*linux')}
  curl -sL "$U" -o gowa.zip
  (unzip -o gowa.zip >/dev/null 2>&1 || tar xzf gowa.zip)
  chmod +x linux-arm64
  touch .catalog-installed
fi
exec ./linux-arm64 rest --port 3000 --db-uri "file:/home/sandbox/workspace/app/gowa.db?_foreign_keys=on"
`,
  },
  {
    id: 'homebox',
    name: 'Homebox',
    blurb: 'Inventory and organization for your home',
    category: 'productivity',
    effort: 'instant',
    modifiable: 'config',
    repo: 'https://github.com/sysadminsmedia/homebox',
    healthPath: '/',
    script:
      SH +
      `if [ ! -f .catalog-installed ]; then
  ${ghAsset('sysadminsmedia/homebox', 'linux_arm64|linux-arm64')}
  curl -sL "$U" -o hb.tgz
  (tar xzf hb.tgz 2>/dev/null || mv hb.tgz homebox) && chmod +x homebox && mkdir -p hbdata
  touch .catalog-installed
fi
export HBOX_AUTH_API_KEY_PEPPER=catalog_pepper_at_least_32_bytes_long_0000
export HBOX_WEB_PORT=3000 HBOX_WEB_HOST=0.0.0.0 HBOX_STORAGE_DATA=/home/sandbox/workspace/app/hbdata
export HBOX_STORAGE_SQLITE_URL='/home/sandbox/workspace/app/hbdata/homebox.db?_pragma=busy_timeout=999999&_fk=1'
exec ./homebox
`,
  },
  {
    id: 'readeck',
    name: 'Readeck',
    blurb: 'Read-later bookmarks and article archiving',
    category: 'productivity',
    effort: 'instant',
    modifiable: 'config',
    repo: 'https://codeberg.org/readeck/readeck',
    healthPath: '/login',
    entryPath: '/login',
    note: 'UI lives at /login — the root path is not routed.',
    script:
      SH +
      `if [ ! -f .catalog-installed ]; then
  U=$(curl -s https://codeberg.org/api/v1/repos/readeck/readeck/releases/latest | grep -oE '"browser_download_url":"[^"]+"' | cut -d'"' -f4 | grep -iE 'linux-arm64' | grep -viE 'sha|sig' | head -1)
  curl -sL "$U" -o readeck && chmod +x readeck
  touch .catalog-installed
fi
export READECK_SERVER_HOST=0.0.0.0 READECK_SERVER_PORT=3000
exec ./readeck serve
`,
  },
  {
    id: 'triliumnext',
    name: 'Trilium Notes (Next)',
    blurb: 'Hierarchical note taking with rich features',
    category: 'productivity',
    effort: 'instant',
    modifiable: 'config',
    repo: 'https://github.com/TriliumNext/Notes',
    healthPath: '/',
    script:
      SH +
      `if [ ! -f .catalog-installed ]; then
  ${ghAsset('TriliumNext/Notes', 'server.*linux.*arm64|linux.*arm64.*server')}
  curl -sL "$U" -o tn.tar
  (tar xJf tn.tar 2>/dev/null || tar xzf tn.tar)
  touch .catalog-installed
fi
D=$(ls -d TriliumNext*/ Trilium*/ 2>/dev/null | head -1)
cd "$D"
export TRILIUM_DATA_DIR=/home/sandbox/workspace/app/tndata TRILIUM_NETWORK_HOST=0.0.0.0 TRILIUM_NETWORK_PORT=3000 TRILIUM_PORT=3000
exec ./trilium.sh
`,
  },
  {
    id: 'garage',
    name: 'Garage',
    blurb: 'Lightweight S3-compatible object storage',
    category: 'data',
    effort: 'instant',
    modifiable: 'config',
    repo: 'https://git.deuxfleurs.fr/Deuxfleurs/garage',
    agentNotes: 'Config: `garage.toml` (S3 on :3000). Manage buckets/keys with `./garage -c garage.toml bucket|key ...`.',
    healthPath: '/',
    note: 'S3 API on the preview port; responses are S3 XML (no HTML UI).',
    script:
      SH +
      `if [ ! -f .catalog-installed ]; then
  curl -sL https://garagehq.deuxfleurs.fr/_releases/v2.1.0/aarch64-unknown-linux-musl/garage -o garage
  chmod +x garage
  SECRET=$(od -An -tx1 -N32 /dev/urandom | tr -d ' \\n')
  printf 'metadata_dir = "/home/sandbox/workspace/app/meta"\\ndata_dir = "/home/sandbox/workspace/app/data"\\ndb_engine = "sqlite"\\nreplication_factor = 1\\nrpc_bind_addr = "127.0.0.1:3901"\\nrpc_secret = "%s"\\n[s3_api]\\ns3_region = "garage"\\napi_bind_addr = "0.0.0.0:3000"\\nroot_domain = ".s3.garage.localhost"\\n[admin]\\napi_bind_addr = "127.0.0.1:3903"\\n' "$SECRET" > garage.toml
  mkdir -p meta data
  touch .catalog-installed
fi
exec ./garage -c /home/sandbox/workspace/app/garage.toml server
`,
  },
  {
    id: 'martin',
    name: 'Martin',
    blurb: 'Blazing-fast vector tile server (MBTiles/PMTiles)',
    category: 'data',
    effort: 'instant',
    modifiable: 'config',
    repo: 'https://github.com/maplibre/martin',
    agentNotes: 'Tile sources are the .mbtiles/.pmtiles files passed in `catalog-run.sh`. Add files to the workspace and append them to the martin command.',
    healthPath: '/catalog',
    entryPath: '/catalog',
    note: 'Ships with a generated demo tileset; add .mbtiles files to the workspace.',
    script:
      SH +
      `if [ ! -f .catalog-installed ]; then
  ${ghAsset('maplibre/martin', 'aarch64-unknown-linux-musl', 'sha|debug')}
  curl -sL "$U" -o martin.tgz && tar xzf martin.tgz && chmod +x martin
  python3 - <<'PYEOF'
import sqlite3
png = bytes.fromhex("89504e470d0a1a0a0000000d4948445200000001000000010806000000" + "1f15c4890000000d4944415478da63f8ffff3f0005fe02fea72d1e480000000049454e44ae426082")
con = sqlite3.connect("demo.mbtiles")
con.execute("create table metadata (name text, value text)")
con.execute("create table tiles (zoom_level int, tile_column int, tile_row int, tile_data blob)")
for k, v in [("name","demo"),("format","png"),("minzoom","0"),("maxzoom","0"),("bounds","-180,-85,180,85")]:
    con.execute("insert into metadata values (?,?)",(k,v))
con.execute("insert into tiles values (0,0,0,?)",(png,))
con.commit(); con.close()
PYEOF
  touch .catalog-installed
fi
exec ./martin --listen-addresses 0.0.0.0:3000 --webui enable-for-all /home/sandbox/workspace/app/demo.mbtiles
`,
  },

  // ───────────────────────── quick: pip / npm one-liners ─────────────────────────
  {
    id: 'glances',
    name: 'Glances',
    blurb: 'System monitoring dashboard',
    category: 'dev',
    effort: 'quick',
    modifiable: 'config',
    repo: 'https://github.com/nicolargo/glances',
    healthPath: '/',
    script:
      SH +
      `if [ ! -d .venv ]; then
  uv venv --python-preference only-managed --python 3.12 >/dev/null
  . .venv/bin/activate && uv pip install 'glances[web]'
fi
. .venv/bin/activate
exec glances -w --bind 0.0.0.0 -p 3000
`,
  },
  {
    id: 'changedetection',
    name: 'changedetection.io',
    blurb: 'Website change detection and alerts',
    category: 'productivity',
    effort: 'quick',
    modifiable: 'config',
    repo: 'https://github.com/dgtlmoon/changedetection.io',
    healthPath: '/',
    note: 'Runs the changedetection.io console script (not python -m).',
    script:
      SH +
      `if [ ! -d .venv ]; then
  uv venv --python-preference only-managed --python 3.12 >/dev/null
  . .venv/bin/activate && uv pip install changedetection.io 'setuptools<81'
  mkdir -p cddata
fi
. .venv/bin/activate
exec changedetection.io -d /home/sandbox/workspace/app/cddata -p 3000 -h 0.0.0.0
`,
  },
  {
    id: 'whoogle',
    name: 'Whoogle Search',
    blurb: 'Ad-free, tracking-free Google proxy',
    category: 'network',
    effort: 'quick',
    modifiable: 'config',
    repo: 'https://github.com/benbusby/whoogle-search',
    healthPath: '/',
    script:
      SH +
      `if [ ! -d .venv ]; then
  uv venv --python-preference only-managed --python 3.12 >/dev/null
  . .venv/bin/activate && uv pip install whoogle-search cachetools
fi
. .venv/bin/activate
exec whoogle-search --host 0.0.0.0 --port 3000
`,
  },
  {
    id: 'esphome',
    name: 'ESPHome',
    blurb: 'ESP microcontroller firmware dashboard',
    category: 'other',
    effort: 'quick',
    modifiable: 'config',
    repo: 'https://github.com/esphome/esphome',
    healthPath: '/',
    script:
      SH +
      `if [ ! -d .venv ]; then
  uv venv --python-preference only-managed --python 3.12 >/dev/null
  . .venv/bin/activate && uv pip install esphome
  mkdir -p espconfig
fi
. .venv/bin/activate
exec esphome dashboard --address 0.0.0.0 --port 3000 espconfig
`,
  },
  {
    id: 'libretranslate',
    name: 'LibreTranslate',
    blurb: 'Self-hosted machine translation API + UI',
    category: 'ai',
    effort: 'quick',
    modifiable: 'config',
    repo: 'https://github.com/LibreTranslate/LibreTranslate',
    healthPath: '/',
    note: 'First start downloads the en/es language models (~1 min).',
    script:
      SH +
      `if [ ! -d .venv ]; then
  uv venv --python-preference only-managed --python 3.12 >/dev/null
  . .venv/bin/activate && uv pip install libretranslate
fi
. .venv/bin/activate
export LT_LOAD_ONLY=en,es
exec libretranslate --host 0.0.0.0 --port 3000
`,
  },
  {
    id: 'pgadmin',
    name: 'pgAdmin 4',
    blurb: 'PostgreSQL administration UI (connect to external DBs)',
    category: 'data',
    effort: 'quick',
    modifiable: 'config',
    repo: 'https://github.com/pgadmin-org/pgadmin4',
    healthPath: '/',
    note: 'Login admin@example.com / admin123456.',
    script:
      SH +
      `if [ ! -d .venv ]; then
  uv venv --python-preference only-managed --python 3.12 >/dev/null
  . .venv/bin/activate && uv pip install pgadmin4
  mkdir -p pgdata
  P=$(ls -d .venv/lib/python3.12/site-packages/pgadmin4 | head -1)
  cat > "$P/config_local.py" <<'CFGEOF'
import os
DATA_DIR = "/home/sandbox/workspace/app/pgdata"
LOG_FILE = os.path.join(DATA_DIR, "pgadmin4.log")
SQLITE_PATH = os.path.join(DATA_DIR, "pgadmin4.db")
SESSION_DB_PATH = os.path.join(DATA_DIR, "sessions")
STORAGE_DIR = os.path.join(DATA_DIR, "storage")
AZURE_CREDENTIAL_CACHE_DIR = os.path.join(DATA_DIR, "azurecredentialcache")
KERBEROS_CCACHE_DIR = os.path.join(DATA_DIR, "krbccache")
DEFAULT_SERVER = "0.0.0.0"
DEFAULT_SERVER_PORT = 3000
SERVER_MODE = False
MASTER_PASSWORD_REQUIRED = False
CFGEOF
fi
. .venv/bin/activate
export PGADMIN_SETUP_EMAIL=admin@example.com PGADMIN_SETUP_PASSWORD=admin123456
exec pgadmin4
`,
  },
  {
    id: 'actualbudget',
    name: 'Actual Budget',
    blurb: 'Private local-first budgeting',
    category: 'productivity',
    effort: 'quick',
    modifiable: 'config',
    repo: 'https://github.com/actualbudget/actual-server',
    healthPath: '/',
    script:
      SH +
      `if [ ! -d node_modules ]; then
  npm init -y >/dev/null
  npm install @actual-app/sync-server
fi
export ACTUAL_PORT=3000 ACTUAL_HOSTNAME=0.0.0.0
exec npx @actual-app/sync-server
`,
  },
  {
    id: 'directus',
    name: 'Directus',
    blurb: 'Instant headless CMS / data platform on SQLite',
    category: 'data',
    effort: 'quick',
    modifiable: 'config',
    repo: 'https://github.com/directus/directus',
    skills: [{ name: 'manage-items', content: `# Directus: create collections & items via API
1. Login: \`curl -s -X POST 127.0.0.1:3000/auth/login -H 'content-type: application/json' -d '{"email":"admin@example.com","password":"admin123456"}'\` → \`.data.access_token\`.
2. Create collection: POST /collections with {"collection":"articles","fields":[{"field":"title","type":"string"}]}.
3. CRUD items: POST/GET /items/articles (Authorization: Bearer TOKEN).
Docs: https://docs.directus.io/reference/introduction.html` }],
    healthPath: '/admin/login',
    entryPath: '/admin/login',
    note: 'Login admin@example.com / admin123456. Install takes ~1–2 min.',
    script:
      SH +
      `if [ ! -d node_modules ]; then
  npm init -y >/dev/null
  npm install directus
  DB_CLIENT=sqlite3 DB_FILENAME=/home/sandbox/workspace/app/data.db KEY=k SECRET=s ADMIN_EMAIL=admin@example.com ADMIN_PASSWORD=admin123456 npx directus bootstrap
fi
export HOST=0.0.0.0 PORT=3000 DB_CLIENT=sqlite3 DB_FILENAME=/home/sandbox/workspace/app/data.db KEY=k SECRET=s
exec npx directus start
`,
  },
  {
    id: 'n8n',
    name: 'n8n',
    blurb: 'Workflow automation platform (SQLite)',
    category: 'ai',
    effort: 'quick',
    modifiable: 'config',
    repo: 'https://github.com/n8n-io/n8n',
    skills: [{ name: 'create-workflow', content: `# n8n: create a workflow via the REST API
1. First boot requires an owner: POST /rest/owner/setup with {email, firstName, lastName, password} (or complete it once in the UI).
2. Login: \`curl -s -c /tmp/n8n.jar -X POST 127.0.0.1:3000/rest/login -H 'content-type: application/json' -d '{"emailOrLdapLoginId":"EMAIL","password":"PASS"}'\`
3. Create workflow: \`curl -s -b /tmp/n8n.jar -X POST 127.0.0.1:3000/rest/workflows -H 'content-type: application/json' -d '{"name":"My flow","nodes":[...],"connections":{},"settings":{}}'\`
   Node objects need: name, type (e.g. n8n-nodes-base.scheduleTrigger / .httpRequest), typeVersion, position, parameters.
4. Activate: PATCH /rest/workflows/ID with {"active":true}.
Alternative: the public API (X-N8N-API-KEY) once an API key is created in Settings.` }],
    agentNotes: 'Workflows/data under `n8ndata/`. n8n itself is an npm dist (pinned 1.68.1) — configure via N8N_* env in `catalog-run.sh`; do not edit node_modules.',
    healthPath: '/',
    note: 'Pinned to 1.68.1 — the latest npm release ships a broken @langchain/core dep tree. Install ~3 min.',
    script:
      SH +
      `export NPMG=/home/sandbox/workspace/app/.npmg
if [ ! -x "$NPMG/bin/n8n" ]; then
  npm config set prefix "$NPMG"
  npm install -g n8n@1.68.1
fi
export PATH="$NPMG/bin:$PATH"
export N8N_PORT=3000 N8N_LISTEN_ADDRESS=0.0.0.0 N8N_HOST=0.0.0.0 N8N_SECURE_COOKIE=false
export N8N_USER_FOLDER=/home/sandbox/workspace/app/n8ndata
exec n8n start
`,
  },
  {
    id: 'wikijs',
    name: 'Wiki.js',
    blurb: 'Modern wiki on SQLite',
    category: 'productivity',
    effort: 'quick',
    modifiable: 'config',
    repo: 'https://github.com/requarks/wiki',
    agentNotes: 'Config: `wiki/config.yml`. Content lives in the SQLite DB (`wiki/db.sqlite`).',
    healthPath: '/',
    script:
      SH +
      `if [ ! -d wiki ]; then
  curl -sL https://github.com/requarks/wiki/releases/latest/download/wiki-js.tar.gz -o w.tgz
  mkdir -p wiki && tar xzf w.tgz -C wiki
  (cd wiki && npm rebuild sqlite3 --build-from-source || npm install sqlite3)
  printf 'port: 3000\\nbindIP: 0.0.0.0\\ndb:\\n  type: sqlite\\n  storage: /home/sandbox/workspace/app/wiki/db.sqlite\\n' > wiki/config.yml
fi
cd wiki
exec node server
`,
  },
  {
    id: 'silverbullet',
    name: 'SilverBullet',
    blurb: 'Hackable markdown knowledge base',
    category: 'productivity',
    effort: 'quick',
    modifiable: 'config',
    repo: 'https://github.com/silverbulletmd/silverbullet',
    agentNotes: 'Your notes live in `space/` (markdown files — edit freely). Server config via flags in `catalog-run.sh`.',
    healthPath: '/',
    note: 'Runs on pinned Deno 1.46 (2.x breaks upstream); needs --unstable-kv.',
    script:
      SH +
      `export DENO_DIR=/home/sandbox/workspace/app/.deno
if [ ! -x d1/deno ]; then
  curl -sL https://github.com/denoland/deno/releases/download/v1.46.3/deno-aarch64-unknown-linux-gnu.zip -o deno1.zip
  mkdir -p d1 space && (cd d1 && unzip -qo ../deno1.zip && chmod +x deno)
fi
exec ./d1/deno run -A --unstable-kv https://get.silverbullet.md --hostname 0.0.0.0 --port 3000 --db /home/sandbox/workspace/app/sb.db ./space
`,
  },
  {
    id: 'shlink',
    name: 'Shlink',
    blurb: 'Self-hosted URL shortener (REST + SQLite)',
    category: 'network',
    effort: 'quick',
    modifiable: 'config',
    repo: 'https://github.com/shlinkio/shlink',
    healthPath: '/rest/health',
    entryPath: '/rest/health',
    note: 'API-first: /rest/health shows status; manage via shlink CLI or web client.',
    script:
      SH +
      `if [ ! -f .catalog-installed ]; then
  P=$(curl -s https://dl.static-php.dev/static-php-cli/bulk/ | grep -oE 'php-8.4.[0-9]+-cli-linux-aarch64.tar.gz' | sort -V | tail -1)
  curl -sL "https://dl.static-php.dev/static-php-cli/bulk/$P" -o php.tgz && tar xzf php.tgz && chmod +x php
  V=$(curl -s https://api.github.com/repos/shlinkio/shlink/releases/latest | grep '"tag_name"' | cut -d'"' -f4)
  VN=$(echo "$V" | tr -d v)
  curl -sL "https://github.com/shlinkio/shlink/releases/download/$V/shlink$VN""_php8.4_dist.zip" -o shlink.zip
  unzip -qo shlink.zip
  D=$(ls -d shlink*dist | head -1)
  (cd "$D" && DB_DRIVER=sqlite ../php vendor/bin/shlink-installer init --no-interaction || true)
  touch .catalog-installed
fi
D=$(ls -d shlink*dist | head -1)
cd "$D"
export DB_DRIVER=sqlite
exec /home/sandbox/workspace/app/php -S 0.0.0.0:3000 -t public
`,
  },
  {
    id: 'ghost',
    name: 'Ghost',
    blurb: 'Professional publishing platform (SQLite)',
    category: 'productivity',
    effort: 'build',
    modifiable: 'config',
    repo: 'https://github.com/TryGhost/Ghost',
    healthPath: '/ghost/',
    entryPath: '/ghost/',
    note: 'Install ~3–5 min (ghost-cli local install).',
    script:
      SH +
      `export NPMG=/home/sandbox/workspace/app/.npmg
if [ ! -d ghostdir/current ]; then
  npm config set prefix "$NPMG"
  npm install -g ghost-cli
  export PATH="$NPMG/bin:$PATH"
  mkdir -p ghostdir && cd ghostdir
  ghost install local --no-start --no-setup || true
  cat > config.development.json <<'CFGEOF'
{
  "url": "http://localhost:3000",
  "server": { "port": 3000, "host": "0.0.0.0" },
  "database": { "client": "sqlite3", "connection": { "filename": "/home/sandbox/workspace/app/ghostdir/content/data/ghost-local.db" } },
  "mail": { "transport": "Direct" },
  "logging": { "transports": ["stdout"] },
  "process": "local"
}
CFGEOF
  cd ..
fi
cd ghostdir
export NODE_ENV=development
exec node current/index.js
`,
  },

  // ───────────────────────── build: clone + build ─────────────────────────
  {
    id: 'homepage',
    name: 'Homepage',
    blurb: 'Highly configurable application dashboard',
    category: 'productivity',
    effort: 'build',
    modifiable: 'source',
    repo: 'https://github.com/gethomepage/homepage',
    agentNotes: 'Source app (Next.js). User config in `hp/config/*.yaml` (services, widgets, bookmarks); code in `hp/src`. Rebuild with `pnpm build` after code changes.',
    healthPath: '/',
    note: 'Build takes several minutes (pnpm install + next build).',
    script:
      SH +
      `R=/home/sandbox/workspace/app/hp
if [ ! -d "$R/.next" ]; then
  rm -rf "$R" && git clone --depth 1 https://github.com/gethomepage/homepage "$R"
  cd "$R" && pnpm install && pnpm build && mkdir -p config
fi
cd "$R"
export HOMEPAGE_ALLOWED_HOSTS='*' PORT=3000 HOSTNAME=0.0.0.0
exec pnpm start
`,
  },
  {
    id: 'dashy',
    name: 'Dashy',
    blurb: 'Feature-rich self-hosted dashboard',
    category: 'productivity',
    effort: 'build',
    modifiable: 'source',
    repo: 'https://github.com/Lissy93/dashy',
    agentNotes: 'Source app (Vue). User config: `dy/user-data/conf.yml`; code in `dy/src`. Rebuild with `corepack yarn build` after code changes.',
    healthPath: '/',
    note: 'Yarn-guarded project — installed via corepack yarn. Build ~2–3 min.',
    script:
      SH +
      `R=/home/sandbox/workspace/app/dy
if [ ! -d "$R/dist" ]; then
  rm -rf "$R" && git clone --depth 1 https://github.com/Lissy93/dashy "$R"
  cd "$R" && corepack yarn install --network-timeout 600000 && corepack yarn build
fi
cd "$R"
export PORT=3000 HOST=0.0.0.0 NODE_ENV=production
exec node server
`,
  },
  {
    id: 'it-tools',
    name: 'IT Tools',
    blurb: 'Handy online tools for developers',
    category: 'dev',
    effort: 'build',
    modifiable: 'source',
    repo: 'https://github.com/CorentinTh/it-tools',
    healthPath: '/',
    script:
      SH +
      `R=/home/sandbox/workspace/app/it
if [ ! -d "$R/dist" ]; then
  rm -rf "$R" && git clone --depth 1 https://github.com/CorentinTh/it-tools "$R"
  cd "$R" && pnpm install && pnpm build
fi
cd "$R/dist"
exec python3 -m http.server 3000 --bind 0.0.0.0
`,
  },
  {
    id: 'convertx',
    name: 'ConvertX',
    blurb: 'Self-hosted file converter (Bun + SQLite)',
    category: 'media',
    effort: 'build',
    modifiable: 'source',
    repo: 'https://github.com/C4illin/ConvertX',
    healthPath: '/',
    note: 'Runs from source under Bun; CSS generated at install.',
    script:
      SH +
      `R=/home/sandbox/workspace/app/cx
if [ ! -d "$R/node_modules" ]; then
  rm -rf "$R" && git clone --depth 1 https://github.com/C4illin/ConvertX "$R"
  cd "$R" && bun install
  bun x @tailwindcss/cli -i ./src/main.css -o ./public/generated.css || true
fi
cd "$R"
export JWT_SECRET=catalog-changeme PORT=3000 HTTP_ALLOWED=true
exec bun src/index.tsx
`,
  },
  {
    id: 'metube',
    name: 'MeTube',
    blurb: 'yt-dlp web UI for video downloads',
    category: 'media',
    effort: 'build',
    modifiable: 'source',
    repo: 'https://github.com/alexta69/metube',
    healthPath: '/',
    note: 'Angular UI build ~3–4 min; requires Python 3.13.',
    script:
      SH +
      `R=/home/sandbox/workspace/app/mt
if [ ! -d "$R/ui/dist" ]; then
  rm -rf "$R" && git clone --depth 1 https://github.com/alexta69/metube "$R"
  cd "$R" && uv venv --python-preference only-managed --python 3.13
  . .venv/bin/activate && uv pip install aiohttp 'python-socketio>=5.0,<6.0' yt-dlp mutagen watchfiles
  cd ui && npm install --legacy-peer-deps && npx ng build
fi
cd "$R" && . .venv/bin/activate
mkdir -p dl st
export DOWNLOAD_DIR="$R/dl" STATE_DIR="$R/st" PORT=3000 HOST=0.0.0.0
exec python3 app/main.py
`,
  },
  {
    id: 'healthchecks',
    name: 'Healthchecks',
    blurb: 'Cron job monitoring with alerts',
    category: 'dev',
    effort: 'build',
    modifiable: 'source',
    repo: 'https://github.com/healthchecks/healthchecks',
    healthPath: '/accounts/login/',
    entryPath: '/accounts/login/',
    script:
      SH +
      `R=/home/sandbox/workspace/app/hc
if [ ! -d "$R/.venv" ]; then
  rm -rf "$R" && git clone --depth 1 https://github.com/healthchecks/healthchecks "$R"
  cd "$R" && uv venv --python-preference only-managed --python 3.12
  . .venv/bin/activate && uv pip install wheel && uv pip install -r requirements.txt
  SECRET_KEY=x DEBUG=True DB=sqlite python manage.py migrate --noinput
fi
cd "$R" && . .venv/bin/activate
export SECRET_KEY=catalog-changeme DEBUG=True DB=sqlite ALLOWED_HOSTS='*'
exec python manage.py runserver 0.0.0.0:3000
`,
  },
]

export const CATEGORIES: { id: CatalogCategory | 'all'; label: string }[] = [
  { id: 'all', label: 'All' },
  { id: 'dev', label: 'Developer' },
  { id: 'productivity', label: 'Productivity' },
  { id: 'data', label: 'Data' },
  { id: 'network', label: 'Network' },
  { id: 'media', label: 'Media' },
  { id: 'ai', label: 'AI & Automation' },
  { id: 'other', label: 'Other' },
]

// Expansion set (exhaustive verified list) — see catalog2.ts.
CATALOG.push(...CATALOG2)
