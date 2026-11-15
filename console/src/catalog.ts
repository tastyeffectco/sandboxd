// Free open-source apps people can install in one click. Each one runs in its
// own project on the person's engine: a start command in sandbox.yaml
// downloads (or builds) the app on first start and serves it on port 3000.
// Every entry here was installed on a real Windows PC before it was added;
// pin exact versions so an install tomorrow is the install we tested.

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
  // Built from its source code, so the coding agent can change it.
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
  // Minutes the first start takes on a normal connection.
  minutes: number
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
    minutes: 3,
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
    source: true,
    firstRun: 'On first open, choose SQLite, then create your account.',
    git: { repo_url: 'https://github.com/louislam/uptime-kuma', branch: '2.5.11' },
    start:
      '[ -d dist ] || (npm ci --omit dev --no-audit && npm run download-dist); export UPTIME_KUMA_PORT=3000; exec node server/server.js',
    minutes: 2,
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
]

export const catalogTag = (id: string) => `catalog:${id}`

// sandbox.yaml for an entry. JSON strings are valid YAML double-quoted scalars.
export function catalogManifest(app: CatalogApp): string {
  return `version: 1\nweb:\n  command: ${JSON.stringify(app.start)}\n  port: 3000\n  health_path: ${JSON.stringify(app.health || '/')}\n`
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
