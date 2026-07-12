// Demo mode (VITE_DEMO=1): a fully static, read-only console backed by fixtures.
// Overrides global fetch so every /v1 call returns canned data — no backend, so
// it can't be abused and scales infinitely on static hosting (sandboxd.io/demo).
// Writes are blocked with a friendly "read-only demo" message.

export const IS_DEMO = import.meta.env.VITE_DEMO === '1'

const APP_ID = '01APPTODOAAAAAAAAAAAAAAAAA'
const SB_ID = '01SBTODOAAAAAAAAAAAAAAAAAA'
const TASK_ID = '01TASKAAAAAAAAAAAAAAAAAAAA'
// Static sample app served alongside the demo (see static/demo/sample-app.html).
const PREVIEW_URL = '/demo/sample-app'

const app = {
  id: APP_ID, name: 'Todo App', description: 'A demo app built by an agent', tags: ['demo'],
  runtime_preset: 'react-vite', current_sandbox_id: SB_ID,
  created_at: '2026-07-09T10:00:00Z', updated_at: '2026-07-09T10:12:00Z',
}

const sandbox = {
  id: SB_ID, status: 'running',
  preview: { url: PREVIEW_URL, status: 'ready' },
  processes: [{ name: 'web', kind: 'web', running: true, pid: 42, restarts: 0 }],
}

const presets = [
  { id: 'react-vite', label: 'React / Vite', description: 'React + Vite SPA with hot reload', template: 'react-standard', capabilities: ['node', 'pnpm'] },
  { id: 'nextjs', label: 'Next.js', description: 'Next.js app (App Router)', template: 'nextjs-standard', capabilities: ['node', 'pnpm'] },
  { id: 'node-express', label: 'Node / Express API', description: 'Express REST API', template: 'node-express-standard', capabilities: ['node'] },
  { id: 'fastapi', label: 'Python / FastAPI', description: 'FastAPI + uvicorn', template: 'fastapi-standard', capabilities: ['python3', 'python3-venv'] },
  { id: 'worker', label: 'Worker (no public endpoint)', description: 'Background worker', template: 'worker-standard', capabilities: [] },
]

const agents = [
  { id: 'opencode', label: 'OpenCode', installed_state: 'installed', status: 'connected', method: 'oauth', supports_oauth: true, supports_api_key: true, runnable: true },
  { id: 'claude-code', label: 'Claude Code', installed_state: 'installed', status: 'connected', method: 'api_key', supports_oauth: true, supports_api_key: true, runnable: true },
]

const settings = {
  version: 'v0.3.0', git_commit: 'demo',
  networking: { preview_domain: 'localhost', public_http_port: '80', preview_base: 'http://*.preview.localhost', preview_tls: false, preview_entrypoint: 'web' },
  auth: { enabled: false },
  runtime: { storage_mode: 'directory', base_image: 'sandboxd-base:0.3.0' },
  lifecycle: { idle_reap_enabled: true, idle_threshold_seconds: 2100, keepalive_max_seconds: 86400 },
  egress: { mode: 'disabled' },
  agents: { providers: ['opencode', 'claude-code'] },
  presets,
  capabilities: { snapshots: true, config_secrets: true, templates: false, forward_auth: true },
  editable: ['lifecycle.idle_reap_enabled', 'lifecycle.idle_threshold_seconds', 'lifecycle.keepalive_max_seconds'],
}

const tasks = [
  { id: TASK_ID, prompt: 'Build a todo list with add & remove', agent: 'opencode', status: 'succeeded',
    build_status: 'passed', preview_ok: true, app_healthy: true, can_revert: true,
    files_changed: ['src/App.tsx', 'src/todo.css', 'index.html'],
    created_at: '2026-07-09T10:10:00Z', finished_at: '2026-07-09T10:12:00Z' },
]

const files = {
  path: '', recursive: false, entries: [
    { name: 'src', path: 'src', type: 'dir', dir: true },
    { name: 'index.html', path: 'index.html', type: 'file', dir: false, size: 412 },
    { name: 'package.json', path: 'package.json', type: 'file', dir: false, size: 336 },
    { name: 'sandbox.yaml', path: 'sandbox.yaml', type: 'file', dir: false, size: 128 },
  ],
}
const fileContents: Record<string, string> = {
  'src/App.tsx': `import { useState } from 'react'\n\nexport default function App() {\n  const [items, setItems] = useState<string[]>(['Try sandboxd'])\n  const [text, setText] = useState('')\n  return (\n    <main>\n      <h1>Todo</h1>\n      <input value={text} onChange={e => setText(e.target.value)} />\n      <button onClick={() => { setItems([...items, text]); setText('') }}>Add</button>\n      <ul>{items.map((t, i) => <li key={i} onClick={() => setItems(items.filter((_, j) => j !== i))}>{t}</li>)}</ul>\n    </main>\n  )\n}\n`,
  'sandbox.yaml': `version: 1\nweb:\n  command: "[ -x node_modules/.bin/vite ] || pnpm install; pnpm exec vite --host 0.0.0.0 --port 3000"\n  port: 3000\n  health_path: "/"\n`,
  'package.json': `{\n  "name": "todo-app",\n  "private": true,\n  "scripts": { "dev": "vite", "build": "vite build" }\n}\n`,
}

const gitStatus = { available: true, branch: 'main', head_sha: 'a1b2c3d', clean: false, user_clean: false, ahead: 0, behind: 0,
  files: [{ path: 'src/App.tsx', status: 'modified', staged: false }, { path: 'src/todo.css', status: 'untracked', staged: false }],
  runtime_files: [{ path: 'sandbox.yaml', status: 'modified', staged: false }] }
const gitDiff = { available: true, truncated: false,
  diff: 'diff --git a/src/App.tsx b/src/App.tsx\n@@\n+const [items, setItems] = useState<string[]>([])\n+<button onClick={add}>Add</button>\n' }
const config = [
  { key: 'PUBLIC_TITLE', sensitive: false, value_set: true, value: 'My Todos', access_policy: 'runtime_access', created_at: '2026-07-09T10:00:00Z', updated_at: '2026-07-09T10:00:00Z' },
  { key: 'API_KEY', sensitive: true, value_set: true, access_policy: 'agent_access', created_at: '2026-07-09T10:00:00Z', updated_at: '2026-07-09T10:00:00Z' },
]
const events = [
  { id: 'e3', type: 'task.succeeded', severity: 'info', message: 'Task finished: Build a todo list', created_at: '2026-07-09T10:12:00Z' },
  { id: 'e2', type: 'sandbox.ready', severity: 'info', message: 'Preview ready', created_at: '2026-07-09T10:01:00Z' },
  { id: 'e1', type: 'app.created', severity: 'info', message: 'App created: Todo App', created_at: '2026-07-09T10:00:00Z' },
]
const manifest = {
  present: true, source: 'sandbox.yaml',
  manifest: fileContents['sandbox.yaml'],
  validation: { valid: true, errors: [], warnings: [], effective: { web: { command: 'pnpm exec vite --host 0.0.0.0 --port 3000', port: 3000, health_path: '/' }, workers: [] } },
  effective: { web: { command: 'pnpm exec vite --host 0.0.0.0 --port 3000', port: 3000, health_path: '/' }, workers: [] },
}

const READ_ONLY = { error: { message: 'This is a read-only demo — install sandboxd to run it for real:  curl -fsSL https://raw.githubusercontent.com/tastyeffectco/sandboxd/main/install.sh | bash' } }

function match(p: string, re: RegExp) { return re.test(p) }

// Returns { status, body } | { status, text } for a given request.
function route(method: string, url: string): { status: number; body?: unknown; text?: string } {
  const u = url.split('?')[0]
  const q = url.includes('?') ? url.slice(url.indexOf('?') + 1) : ''
  const isWrite = method !== 'GET' && method !== 'HEAD'

  // File content (raw text).
  if (match(u, /\/v1\/sandboxes\/[^/]+\/files\/content$/)) {
    const rel = decodeURIComponent((q.match(/path=([^&]*)/)?.[1] || '')).replace(/^workspace\/app\//, '')
    const c = fileContents[rel]
    return c !== undefined ? { status: 200, text: c } : { status: 404, body: { error: { message: 'not found' } } }
  }
  if (isWrite) return { status: 403, body: READ_ONLY }

  // Auth is disabled in the demo, but report authenticated so the gate passes.
  if (match(u, /\/v1\/auth\/status$/)) return { status: 200, body: { enabled: false, authenticated: true, password_set: true } }
  if (match(u, /\/v1\/apps$/)) return { status: 200, body: { apps: [app] } }
  if (match(u, /\/v1\/presets$/)) return { status: 200, body: { presets } }
  if (match(u, /\/v1\/settings$/)) return { status: 200, body: settings }
  if (match(u, /\/v1\/agents$/)) return { status: 200, body: { providers: agents } }
  if (match(u, /\/v1\/git-credentials$/)) return { status: 200, body: { credentials: [{ id: 'gc1', name: 'github', host: 'github.com', username: 'x-access-token', token_set: true, created_at: '2026-07-09T10:00:00Z' }] } }
  if (match(u, /\/v1\/apps\/[^/]+\/runtime\/manifest$/)) return { status: 200, body: manifest }
  if (match(u, /\/v1\/apps\/[^/]+\/runtime-inspect$/)) return { status: 200, body: { existing_manifest: { present: true }, suggestions: [], default_suggestion: 'react-vite', alternatives: [] } }
  if (match(u, /\/v1\/apps\/[^/]+\/git\/status$/)) return { status: 200, body: gitStatus }
  if (match(u, /\/v1\/apps\/[^/]+\/git\/diff$/)) return { status: 200, body: gitDiff }
  if (match(u, /\/v1\/apps\/[^/]+\/config$/)) return { status: 200, body: { config } }
  if (match(u, /\/v1\/apps\/[^/]+\/events$/)) return { status: 200, body: { events } }
  if (match(u, /\/v1\/apps\/[^/]+\/snapshots$/)) return { status: 200, body: { snapshots: [{ id: 'snap1', name: 'before-deploy', created_at: '2026-07-09T10:05:00Z' }] } }
  if (match(u, /\/v1\/sandboxes\/[^/]+\/tasks\/[^/]+$/)) return { status: 200, body: tasks[0] }
  if (match(u, /\/v1\/sandboxes\/[^/]+\/tasks$/)) return { status: 200, body: { tasks } }
  if (match(u, /\/v1\/sandboxes\/[^/]+\/files$/)) return { status: 200, body: files }
  if (match(u, /\/v1\/sandboxes\/[^/]+\/processes\/[^/]+\/logs$/)) return { status: 200, body: { process: 'web', lines: ['VITE ready in 312 ms', '➜  Local:   http://localhost:3000/'] } }
  if (match(u, /\/v1\/sandboxes\/[^/]+$/)) return { status: 200, body: sandbox }
  if (match(u, /\/v1\/apps\/[^/]+$/)) return { status: 200, body: app }
  return { status: 200, body: {} }
}

function fakeResponse(r: { status: number; body?: unknown; text?: string }): Response {
  const text = r.text !== undefined ? r.text : JSON.stringify(r.body ?? {})
  return {
    ok: r.status >= 200 && r.status < 300,
    status: r.status,
    statusText: r.status === 200 ? 'OK' : 'Error',
    headers: { get: (k: string) => (k.toLowerCase() === 'content-type' ? (r.text !== undefined ? 'text/plain' : 'application/json') : null) },
    json: async () => (r.body ?? {}),
    text: async () => text,
  } as unknown as Response
}

export function installDemo() {
  const realFetch = globalThis.fetch
  globalThis.fetch = (async (input: unknown, init?: { method?: string }) => {
    const url = typeof input === 'string' ? input : String((input as { url?: string })?.url || '')
    if (!url.startsWith('/v1/')) return realFetch(input as RequestInfo, init as RequestInit)
    const method = (init?.method || 'GET').toUpperCase()
    return fakeResponse(route(method, url))
  }) as typeof fetch
}
