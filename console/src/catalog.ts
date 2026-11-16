// Free open-source apps people can install in one click. Each one runs in its
// own project on the person's engine: a start command in sandbox.yaml
// downloads (or builds) the app on first start and serves it on port 3000.
// Every entry here was installed on a real Windows PC before it was added;
// pin exact versions so an install tomorrow is the install we tested.

import mujocoPage from './catalog-files/mujoco.html?raw'
import sherlockPage from './catalog-files/sherlock.py?raw'

export interface CatalogApp {
  id: string
  name: string
  // One line in plain words: what a person does with it.
  pitch: string
  category: string
  license: string
  // Approximate GitHub stars, as shown on the card ("41k").
  stars: string
  repo: string
  color: string
  // The coding agent can change it: its source code is in the project (or,
  // for Glance, everything lives in one settings file it reloads).
  source: boolean
  // Shown after installing: what to do on first open.
  firstRun?: string
  // Upstream code to copy into the project (otherwise it starts empty).
  git?: { repo_url: string; branch: string }
  // sandbox.yaml web.command: install on first start, then serve on :3000.
  start: string
  // A page that answers 200 once the app is up (default "/"); for apps whose
  // home page redirects or rate-limits the preview's frequent checks.
  health?: string
  // Rebuilds the app after each change the coding agent makes (source apps
  // served from a production build), so the change shows in the preview.
  build?: string
  // Minutes the first start takes on a normal connection.
  minutes: number
  // Hyped on GitHub right now: shown under "Trending on GitHub" on the home page.
  trending?: boolean
  // Small files (path in the project -> content) written before the install,
  // e.g. a one-page web front for a command-line tool.
  files?: Record<string, string>
}

const SERVE = 'npx --yes serve@14.2.4'
const ARCH = "$(uname -m | sed 's/x86_64/amd64/;s/aarch64/arm64/')"

export const CATALOG: CatalogApp[] = [
  {
    id: 'excalidraw',
    name: 'Excalidraw',
    pitch: 'A whiteboard for hand-drawn sketches, diagrams and wireframes.',
    category: 'Whiteboard',
    license: 'MIT',
    stars: '136k',
    repo: 'https://github.com/excalidraw/excalidraw',
    color: '#6965db',
    source: true,
    git: { repo_url: 'https://github.com/excalidraw/excalidraw', branch: 'master' },
    start:
      '[ -f excalidraw-app/build/index.html ] || (mkdir -p "$HOME/.local/bin" && corepack enable --install-directory "$HOME/.local/bin" && export PATH="$HOME/.local/bin:$PATH" && yarn install --frozen-lockfile && yarn build:app:docker); exec ' +
      SERVE +
      ' -s excalidraw-app/build -l tcp://0.0.0.0:3000',
    build: 'export PATH="$HOME/.local/bin:$PATH" && yarn build:app:docker',
    minutes: 3,
  },
  {
    id: 'drawio',
    name: 'draw.io',
    pitch: 'Flowcharts, org charts and diagrams with drag and drop.',
    category: 'Diagrams',
    license: 'Apache-2.0',
    stars: '9k',
    repo: 'https://github.com/jgraph/drawio',
    color: '#f08705',
    source: false,
    start:
      '[ -f site/index.html ] || (curl -fsSL -o draw.war https://github.com/jgraph/drawio/releases/download/v32.6.10/draw.war && python3 -m zipfile -e draw.war site && rm draw.war); exec ' +
      SERVE +
      ' site -l tcp://0.0.0.0:3000',
    minutes: 1,
  },
  {
    id: 'uptime-kuma',
    name: 'Uptime Kuma',
    pitch: 'Watches your websites and tells you the moment one goes down.',
    category: 'Monitoring',
    license: 'MIT',
    stars: '94k',
    repo: 'https://github.com/louislam/uptime-kuma',
    color: '#5cdd8b',
    // Its web page comes prebuilt (download-dist): not changeable as is.
    source: false,
    firstRun: 'On first open, choose SQLite, then create your account.',
    git: { repo_url: 'https://github.com/louislam/uptime-kuma', branch: '2.5.11' },
    start:
      '[ -d dist ] || (npm ci --omit dev --no-audit && npm run download-dist); export UPTIME_KUMA_PORT=3000; exec node server/server.js',
    minutes: 2,
  },
  {
    id: 'changedetection',
    name: 'changedetection.io',
    pitch: 'Get told when a web page changes: prices, back in stock, news.',
    category: 'Monitoring',
    license: 'Apache-2.0',
    stars: '36k',
    repo: 'https://github.com/dgtlmoon/changedetection.io',
    color: '#4c63d2',
    source: false,
    firstRun: 'Paste the address of a page to watch at the top, then click Watch.',
    start:
      '[ -x .venv/bin/changedetection.io ] || (uv venv -q .venv && uv pip install -q --prerelease=allow --python .venv/bin/python changedetection.io==0.70.3); mkdir -p data; exec .venv/bin/changedetection.io -d ./data -h 0.0.0.0 -p 3000',
    minutes: 1,
  },
  {
    id: 'memos',
    name: 'Memos',
    pitch: 'Quick private notes, like your own little Twitter only you can read.',
    category: 'Notes',
    license: 'MIT',
    stars: '64k',
    repo: 'https://github.com/usememos/memos',
    color: '#0d9488',
    source: false,
    firstRun: 'On first open, create your account (the first one is the admin).',
    start:
      '[ -x ./memos ] || curl -fsSL https://github.com/usememos/memos/releases/download/26.11.0/memos_26.11.0_linux_' +
      ARCH +
      '.tar.gz | tar xz memos; mkdir -p data; exec ./memos --port 3000 --data ./data',
    minutes: 1,
  },
  {
    id: 'glance',
    name: 'Glance',
    pitch: 'Your own start page: news, weather, videos and links on one screen.',
    category: 'Dashboard',
    license: 'AGPL-3.0',
    stars: '38k',
    repo: 'https://github.com/glanceapp/glance',
    color: '#c8a35c',
    source: true,
    firstRun: 'Ask me to make it yours: your city\'s weather, your favourite sites.',
    start:
      '[ -x ./glance ] || curl -fsSL https://github.com/glanceapp/glance/releases/download/v0.8.8/glance-linux-' +
      ARCH +
      ".tar.gz | tar xz glance; [ -f glance.yml ] || { printf 'server:\\n  port: 3000\\n\\n' > glance.yml && curl -fsSL https://raw.githubusercontent.com/glanceapp/glance/v0.8.8/docs/glance.yml >> glance.yml; }; exec ./glance --config glance.yml",
    minutes: 1,
  },
  {
    id: 'squoosh',
    name: 'Squoosh',
    pitch: 'Shrink photos for the web or email, and compare quality side by side.',
    category: 'Images',
    license: 'Apache-2.0',
    stars: '26k',
    repo: 'https://github.com/GoogleChromeLabs/squoosh',
    color: '#ff3385',
    source: true,
    git: { repo_url: 'https://github.com/GoogleChromeLabs/squoosh', branch: 'dev' },
    start:
      '[ -f build/index.html ] || (npm ci --no-audit --no-fund && npm run build); exec ' + SERVE + ' build -l tcp://0.0.0.0:3000',
    build: 'npm run build',
    minutes: 2,
  },
  {
    id: 'it-tools',
    name: 'IT Tools',
    pitch: '80+ handy tools: QR codes, passwords, unit and date converters, and more.',
    category: 'Utilities',
    license: 'GPL-3.0',
    stars: '41k',
    repo: 'https://github.com/CorentinTh/it-tools',
    color: '#18a058',
    source: false,
    start:
      '[ -f dist/index.html ] || (curl -fsSL -o it-tools.zip https://github.com/CorentinTh/it-tools/releases/download/v2024.10.22-7ca5933/it-tools-2024.10.22-7ca5933.zip && python3 -m zipfile -e it-tools.zip . && rm it-tools.zip); exec ' +
      SERVE +
      ' -s dist -l tcp://0.0.0.0:3000',
    minutes: 1,
  },
  {
    id: 'cyberchef',
    name: 'CyberChef',
    pitch: 'Decode, convert and analyse any data: Base64, hashes, dates, encodings.',
    category: 'Utilities',
    license: 'Apache-2.0',
    stars: '36k',
    repo: 'https://github.com/gchq/CyberChef',
    color: '#4a8a2c',
    source: false,
    start:
      '[ -f site/index.html ] || (curl -fsSL -o cc.zip https://github.com/gchq/CyberChef/releases/download/v11.5.0/CyberChef_8cd426dd4f40f1423912d5fad91b578a86a65112.zip && python3 -m zipfile -e cc.zip site && cp site/CyberChef_v11.5.0.html site/index.html && rm cc.zip); exec ' +
      SERVE +
      ' site -l tcp://0.0.0.0:3000',
    minutes: 1,
  },
  {
    id: 'omni-tools',
    name: 'Omni Tools',
    pitch: 'Edit images, PDFs, text and lists right in your browser, nothing uploaded.',
    category: 'Utilities',
    license: 'MIT',
    stars: '11k',
    repo: 'https://github.com/iib0011/omni-tools',
    color: '#1c7ed6',
    source: true,
    git: { repo_url: 'https://github.com/iib0011/omni-tools', branch: 'v0.7.0' },
    start:
      '[ -f dist/index.html ] || (npm ci --no-audit --no-fund && npm run build); exec ' +
      SERVE +
      ' -s dist -l tcp://0.0.0.0:3000',
    build: 'npm run build',
    minutes: 4,
  },
  {
    id: 'wikijs',
    name: 'Wiki.js',
    pitch: 'A beautiful wiki for your notes, your family or your team.',
    category: 'Notes',
    license: 'AGPL-3.0',
    stars: '29k',
    repo: 'https://github.com/requarks/wiki',
    color: '#1976d2',
    source: false,
    firstRun: 'On first open, enter your email and a password, then click Install.',
    start:
      "[ -f wiki/server/index.js ] || (mkdir -p wiki && curl -fsSL https://github.com/requarks/wiki/releases/download/v2.5.315/wiki-js.tar.gz | tar xz -C wiki && printf 'port: 3000\\nbindIP: 0.0.0.0\\ndb:\\n  type: sqlite\\n  storage: data/db.sqlite\\nlogLevel: info\\ndataPath: ./data\\n' > wiki/config.yml && (cd wiki && npm rebuild sqlite3)); cd wiki && exec node server",
    minutes: 1,
  },
  {
    id: 'shiori',
    name: 'Shiori',
    pitch: 'Save web pages to read later, with a copy kept even if the page disappears.',
    category: 'Reading',
    license: 'MIT',
    stars: '12k',
    repo: 'https://github.com/go-shiori/shiori',
    color: '#f44336',
    source: false,
    firstRun: 'Sign in as shiori with the password gopher, then change it in Settings.',
    start:
      '[ -x ./shiori ] || curl -fsSL https://github.com/go-shiori/shiori/releases/download/v1.8.0/shiori_Linux_$(uname -m)_1.8.0.tar.gz | tar xz shiori; mkdir -p data; export SHIORI_DIR=$PWD/data; exec ./shiori server -a 0.0.0.0 -p 3000',
    minutes: 1,
  },
  {
    id: 'vikunja',
    name: 'Vikunja',
    pitch: 'To-do lists and Kanban boards for your projects, shopping and chores.',
    category: 'Tasks',
    license: 'AGPL-3.0',
    stars: '6k',
    repo: 'https://github.com/go-vikunja/vikunja',
    color: '#1973ff',
    source: false,
    firstRun: 'On first open, click Create account to make yours.',
    start:
      '[ -x ./vikunja ] || (curl -fsSL -o v.zip https://github.com/go-vikunja/vikunja/releases/download/v2.8.0/vikunja-v2.8.0-linux-' +
      ARCH +
      '-full.zip && python3 -m zipfile -e v.zip vk && mv vk/vikunja-v2.8.0-linux-' +
      ARCH +
      ' vikunja && chmod +x vikunja && rm -rf v.zip vk); mkdir -p data/files; export VIKUNJA_SERVICE_INTERFACE=:3000 VIKUNJA_CORS_ENABLE=false VIKUNJA_SERVICE_ROOTPATH=$PWD/data VIKUNJA_DATABASE_TYPE=sqlite VIKUNJA_DATABASE_PATH=$PWD/data/vikunja.db VIKUNJA_FILES_BASEPATH=$PWD/data/files; exec ./vikunja',
    minutes: 1,
  },
  {
    id: 'tiddlywiki',
    name: 'TiddlyWiki',
    pitch: 'A personal notebook where every note links to the others.',
    category: 'Notes',
    license: 'BSD-3-Clause',
    stars: '9k',
    repo: 'https://github.com/TiddlyWiki/TiddlyWiki5',
    color: '#5778d8',
    source: false,
    start:
      '[ -f node_modules/tiddlywiki/tiddlywiki.js ] || npm i --no-audit --no-fund tiddlywiki@5.4.1; [ -d wiki ] || node node_modules/tiddlywiki/tiddlywiki.js wiki --init server; exec node node_modules/tiddlywiki/tiddlywiki.js wiki --listen host=0.0.0.0 port=3000',
    minutes: 1,
  },
  {
    id: 'silverbullet',
    name: 'SilverBullet',
    pitch: 'Notes you write in plain text, with links, quick notes and a daily journal.',
    category: 'Notes',
    license: 'MIT',
    stars: '6.5k',
    repo: 'https://github.com/silverbulletmd/silverbullet',
    color: '#4f6bed',
    source: false,
    start:
      '[ -x ./silverbullet ] || (curl -fsSL -o sb.zip https://github.com/silverbulletmd/silverbullet/releases/download/2.12.1/silverbullet-server-linux-$(uname -m).zip && python3 -m zipfile -e sb.zip . && chmod +x silverbullet && rm sb.zip); mkdir -p notes; export SB_HOSTNAME=0.0.0.0 SB_PORT=3000; exec ./silverbullet --single notes',
    minutes: 1,
  },
  {
    id: 'bentopdf',
    name: 'BentoPDF',
    pitch: 'Merge, split, sign and compress PDFs, without uploading them anywhere.',
    category: 'Documents',
    license: 'AGPL-3.0',
    stars: '16k',
    repo: 'https://github.com/alam00000/bentopdf',
    color: '#e8590c',
    source: false,
    start:
      '[ -f site/index.html ] || (curl -fsSL -o bento.zip https://github.com/alam00000/bentopdf/releases/download/v2.8.11/dist-2.8.11.zip && python3 -m zipfile -e bento.zip site && rm bento.zip); exec ' +
      SERVE +
      ' site -l tcp://0.0.0.0:3000',
    minutes: 2,
  },
  {
    id: 'sherlock',
    name: 'Sherlock',
    pitch: 'Type a username and see which of 400+ websites have an account with it.',
    category: 'Privacy',
    license: 'MIT',
    stars: '113k',
    repo: 'https://github.com/sherlock-project/sherlock',
    color: '#334e68',
    source: false,
    trending: true,
    files: { 'app.py': sherlockPage },
    start:
      '[ -x .venv/bin/sherlock ] || (uv venv -q .venv && uv pip install -q --python .venv/bin/python sherlock-project==0.16.2); export PORT=3000; exec .venv/bin/python app.py',
    minutes: 1,
  },
  {
    id: 'anydoc',
    name: 'anydoc',
    pitch: 'Turn any Word, PowerPoint, Excel or PDF file into clean text you can paste into an AI chat. Files never leave this computer.',
    category: 'Documents',
    license: 'MIT',
    stars: '24k',
    repo: 'https://github.com/firecrawl/anydoc',
    color: '#f2542d',
    source: false,
    trending: true,
    start:
      '[ -f site/pkg/anydoc_wasm_bg.wasm ] || (mkdir -p site/pkg site/assets/fonts && cd site && for f in index.html assets/logo.svg assets/logo-light.svg assets/fonts/GeistMono-400.woff2 assets/fonts/GeistMono-500.woff2 assets/fonts/SuisseIntl-400.woff2 assets/fonts/SuisseIntl-450.woff2 assets/fonts/SuisseIntl-500.woff2; do curl -fsSL -o $f https://raw.githubusercontent.com/firecrawl/anydoc/v0.2.4/wasm/www/$f || exit 1; done && npm pack -q @firecrawl/anydoc-wasm@0.2.4 >/dev/null && tar xzf firecrawl-anydoc-wasm-0.2.4.tgz && cp package/anydoc_wasm.js package/anydoc_wasm_bg.wasm pkg/ && rm -rf package firecrawl-anydoc-wasm-0.2.4.tgz); exec npx --yes serve@14.2.4 site -l tcp://0.0.0.0:3000',
    minutes: 1,
  },
  {
    id: 'pixelfx',
    name: 'pixelfx',
    pitch: '244 animated pixel-art effects (fire, lightning, explosions, magic) to browse and drop into your own game.',
    category: 'Games',
    license: 'MIT',
    stars: '8k',
    repo: 'https://github.com/patch-gg/pixelfx',
    color: '#d6336c',
    source: false,
    trending: true,
    start:
      '[ -f fx/tools/serve.mjs ] || (mkdir -p fx && curl -fsSL https://codeload.github.com/patch-gg/pixelfx/tar.gz/5294518ce7c72e48b6100a744596e0acf57f2826 | tar xz --strip-components=1 -C fx); cd fx && exec node tools/serve.mjs 3000',
    health: '/gallery/',
    minutes: 1,
  },
  {
    id: 'mujoco',
    name: 'MuJoCo',
    pitch: 'Google DeepMind\'s robot physics engine, as a playground: push a robot, drop it, watch it fall in slow motion.',
    category: 'Science',
    license: 'Apache-2.0',
    stars: '18k',
    repo: 'https://github.com/google-deepmind/mujoco',
    color: '#2f6fde',
    source: false,
    trending: true,
    files: { 'index.html': mujocoPage },
    start:
      '[ -f humanoid.xml ] || curl -fsSL -o humanoid.xml https://raw.githubusercontent.com/google-deepmind/mujoco/4.0.1/model/humanoid/humanoid.xml; [ -f node_modules/@mujoco/mujoco/mujoco.wasm ] || (echo \'{"private":true}\' > package.json && npm i --no-audit --no-fund @mujoco/mujoco@4.0.1 three@0.178.0); exec npx --yes serve@14.2.4 . -l tcp://0.0.0.0:3000',
    minutes: 1,
  },
  {
    id: 'photocraft',
    name: 'PhotoCraft',
    pitch: 'A full photo editor with layers, filters and Photoshop files, running in your browser. Nothing to install.',
    category: 'Images',
    license: 'Apache-2.0',
    stars: '80k',
    repo: 'https://github.com/storytold/photocraft',
    color: '#2f7bf5',
    source: false,
    trending: true,
    start:
      '[ -f photocraft-web-0.23.1/index.html ] || (curl -fsSL -o pc.zip https://github.com/storytold/photocraft/releases/download/v0.23.1/photocraft-web-0.23.1.zip && python3 -m zipfile -e pc.zip . && cp -r photocraft-web-0.23.1/snippets photocraft-web-0.23.1/app/ && rm pc.zip); exec npx --yes serve@14.2.4 photocraft-web-0.23.1 -l tcp://0.0.0.0:3000',
    minutes: 1,
  },
  {
    id: 'pdfcraft',
    name: 'PdfCraft',
    pitch: 'Edit, sign, combine and compress PDFs, privately on this computer. Reads scanned pages too.',
    category: 'Documents',
    license: 'Apache-2.0',
    stars: '21k',
    repo: 'https://github.com/storytold/pdfcraft',
    color: '#e8590c',
    source: false,
    trending: true,
    start:
      '[ -f pdfcraft-web-0.21.1/index.html ] || (curl -fsSL -o pc.zip https://github.com/storytold/pdfcraft/releases/download/v0.21.1/pdfcraft-web-0.21.1.zip && python3 -m zipfile -e pc.zip . && rm pc.zip); exec npx --yes serve@14.2.4 pdfcraft-web-0.21.1 -l tcp://0.0.0.0:3000',
    minutes: 1,
  },
  {
    id: 'filmcraft',
    name: 'FilmCraft',
    pitch: 'A video editor with a timeline, titles and audio mixing. Opens with a demo project to play with.',
    category: 'Video',
    license: 'Apache-2.0',
    stars: '18k',
    repo: 'https://github.com/storytold/filmcraft',
    color: '#7048e8',
    source: false,
    trending: true,
    start:
      '[ -f filmcraft-web-0.20.1/index.html ] || (curl -fsSL -o w.zip https://github.com/storytold/filmcraft/releases/download/v0.20.1/filmcraft-web-0.20.1.zip && python3 -m zipfile -e w.zip . && rm w.zip && if [ -d filmcraft-web-0.20.1/app ] && [ ! -d filmcraft-web-0.20.1/app/snippets ]; then cp -r filmcraft-web-0.20.1/snippets filmcraft-web-0.20.1/app/; fi); exec npx --yes serve@14.2.4 filmcraft-web-0.20.1 -l tcp://0.0.0.0:3000',
    minutes: 1,
  },
  {
    id: 'gridcraft',
    name: 'GridCraft',
    pitch: 'A spreadsheet with formulas, charts and Excel files, free and private.',
    category: 'Documents',
    license: 'Apache-2.0',
    stars: '8k',
    repo: 'https://github.com/storytold/gridcraft',
    color: '#2b8a3e',
    source: false,
    trending: true,
    start:
      '[ -f gridcraft-web-0.19.1/index.html ] || (curl -fsSL -o w.zip https://github.com/storytold/gridcraft/releases/download/v0.19.1/gridcraft-web-0.19.1.zip && python3 -m zipfile -e w.zip . && rm w.zip && if [ -d gridcraft-web-0.19.1/app ] && [ ! -d gridcraft-web-0.19.1/app/snippets ]; then cp -r gridcraft-web-0.19.1/snippets gridcraft-web-0.19.1/app/; fi); exec npx --yes serve@14.2.4 gridcraft-web-0.19.1 -l tcp://0.0.0.0:3000',
    minutes: 1,
  },
  {
    id: 'deckcraft',
    name: 'DeckCraft',
    pitch: 'Make slide presentations with transitions and present them, like PowerPoint but free.',
    category: 'Documents',
    license: 'Apache-2.0',
    stars: '5k',
    repo: 'https://github.com/storytold/deckcraft',
    color: '#f76707',
    source: false,
    trending: true,
    start:
      '[ -f deckcraft-web-0.18.1/index.html ] || (curl -fsSL -o w.zip https://github.com/storytold/deckcraft/releases/download/v0.18.1/deckcraft-web-0.18.1.zip && python3 -m zipfile -e w.zip . && rm w.zip && if [ -d deckcraft-web-0.18.1/app ] && [ ! -d deckcraft-web-0.18.1/app/snippets ]; then cp -r deckcraft-web-0.18.1/snippets deckcraft-web-0.18.1/app/; fi); exec npx --yes serve@14.2.4 deckcraft-web-0.18.1 -l tcp://0.0.0.0:3000',
    minutes: 1,
  },
]

export const catalogTag = (id: string) => `catalog:${id}`

// "113k" -> 113000, for ordering by popularity.
export const starCount = (stars: string) => parseFloat(stars) * (/k$/i.test(stars) ? 1000 : 1)

// Most-starred first.
export const TRENDING = CATALOG.filter((a) => a.trending).sort((a, b) => starCount(b.stars) - starCount(a.stars))

// sandbox.yaml for an entry. JSON strings are valid YAML double-quoted scalars.
export function catalogManifest(app: CatalogApp): string {
  const web = `version: 1\nweb:\n  command: ${JSON.stringify(app.start)}\n  port: 3000\n  health_path: ${JSON.stringify(app.health || '/')}\n`
  return app.build ? `${web}build:\n  command: ${JSON.stringify(app.build)}\n  timeout_seconds: 600\n` : web
}

// The first task: the coding agent only writes sandbox.yaml; the start
// command does the install, so this works the same on any model.
export function installPrompt(app: CatalogApp): string {
  return (
    `Install ${app.name} from sandboxd's app catalog (open source, ${app.repo.replace('https://', '')}).\n` +
    'Create the file sandbox.yaml with exactly this content, and change nothing else:\n\n' +
    catalogManifest(app) +
    `\nsandboxd then downloads and starts the app by itself (about ${app.minutes} min the first time). ` +
    'Reply with one plain sentence' +
    (app.firstRun ? ` that ends with: "${app.firstRun}"` : '') +
    '.'
  )
}
