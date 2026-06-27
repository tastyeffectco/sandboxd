// Fixtures that MIRROR sandboxd's /v1 responses. sandboxd is the contract — the
// JSON shapes here match the Go response-shape tests
// (control-plane/internal/api/v1_client_contract_test.go and friends). If a
// server shape changes, update these to match (and the Go test should fail first).
import { vi } from 'vitest'
import type { App, Preset, Sandbox, AppEvent, ConfigItem, Settings, GitCredential } from '../api'

// Git credential metadata (mirrors GET /v1/git-credentials). No token field.
export const gitCredentialsFixture: GitCredential[] = [
  { id: '01GITCREDAAAAAAAAAAAAAAAAA', name: 'github', host: 'github.com', username: 'x-access-token', token_set: true, created_at: '2026-06-26T00:00:00Z' },
]

export const presetsFixture: Preset[] = [
  { id: 'react-vite', label: 'React / Vite', description: 'React + Vite SPA', template: 'react-standard', capabilities: ['node', 'pnpm'] },
  { id: 'nextjs', label: 'Next.js', description: 'Next.js app', template: 'nextjs-standard', capabilities: ['node', 'pnpm'] },
  { id: 'node-express', label: 'Node / Express API', description: 'Express REST API', template: 'node-express-standard', capabilities: ['node'] },
  { id: 'fastapi', label: 'Python / FastAPI', description: 'FastAPI REST API', template: 'fastapi-standard', capabilities: ['python3', 'python3-venv'] },
  { id: 'worker', label: 'Worker (no public endpoint)', description: 'Background worker', template: 'worker-standard', capabilities: [] },
]

export const appsFixture: App[] = [
  {
    id: '01APPAAAAAAAAAAAAAAAAAAAAA', name: 'My App', description: '', tags: [],
    runtime_preset: 'react-vite', current_sandbox_id: '01SBWEBAAAAAAAAAAAAAAAAAAA',
    created_at: '2026-06-23T00:00:00Z', updated_at: '2026-06-23T00:00:00Z',
  },
]

// Web app sandbox: serving preview + a running web process.
export const webSandboxFixture: Sandbox = {
  id: '01SBWEBAAAAAAAAAAAAAAAAAAA', status: 'running',
  preview: { url: 'http://app.preview.localhost', status: 'ready' },
  processes: [{ name: 'web', kind: 'web', running: true, pid: 42, restarts: 0 }],
}

// Worker-only sandbox: no public endpoint (preview status 'none', no URL).
export const workerSandboxFixture: Sandbox = {
  id: '01SBWRKAAAAAAAAAAAAAAAAAAA', status: 'running',
  preview: { url: '', status: 'none' },
  processes: [{ name: 'worker', kind: 'worker', running: true, pid: 50, restarts: 0 }],
}

// Unhealthy sandbox: stopped, preview down, web process NOT running (and churning
// on restarts). The UI must not present this as healthy/running.
export const unhealthySandboxFixture: Sandbox = {
  id: '01SBBADAAAAAAAAAAAAAAAAAAA', status: 'stopped',
  preview: { url: '', status: 'down' },
  processes: [{ name: 'web', kind: 'web', running: false, pid: 0, restarts: 3 }],
}

export const eventsFixture: AppEvent[] = [
  { id: 'ev1', type: 'app.created', severity: 'info', message: 'App created: My App', created_at: '2026-06-23T00:00:00Z' },
]

// Config: a sensitive value is REDACTED (no `value` field, value_set true) and a
// non-sensitive value is returned — mirrors sandboxd's write-only secret model.
export const configFixture: ConfigItem[] = [
  { key: 'API_KEY', sensitive: true, value_set: true, access_policy: 'agent_access', created_at: '2026-06-23T00:00:00Z', updated_at: '2026-06-23T00:00:00Z' },
  { key: 'LOG_LEVEL', sensitive: false, value_set: true, value: 'debug', access_policy: 'runtime_access', created_at: '2026-06-23T00:00:00Z', updated_at: '2026-06-23T00:00:00Z' },
]

// Instance settings summary — safe metadata only (mirrors GET /v1/settings).
export const settingsFixture: Settings = {
  version: 'v0.4.0',
  git_commit: 'abc1234',
  networking: {
    preview_domain: 'localhost',
    public_http_port: '18080',
    preview_base: 'http://*.preview.localhost:18080',
    preview_tls: false,
    preview_entrypoint: 'web',
  },
  auth: { enabled: false },
  runtime: { storage_mode: 'directory', base_image: 'sandboxd-base:1.0.0' },
  lifecycle: { idle_reap_enabled: true, idle_threshold_seconds: 2100, keepalive_max_seconds: 86400 },
  egress: { mode: 'disabled' },
  agents: { providers: ['opencode'] },
  presets: presetsFixture,
  capabilities: { snapshots: true, config_secrets: true, templates: false, forward_auth: true },
  editable: ['lifecycle.idle_reap_enabled', 'lifecycle.idle_threshold_seconds', 'lifecycle.keepalive_max_seconds'],
}

// --- fetch mock ------------------------------------------------------

function jsonResponse(data: unknown, status = 200): Response {
  return {
    ok: status >= 200 && status < 300,
    status,
    statusText: 'OK',
    headers: { get: (k: string) => (k.toLowerCase() === 'content-type' ? 'application/json' : null) },
    json: async () => data,
    text: async () => JSON.stringify(data),
  } as unknown as Response
}

// installFetch stubs global.fetch, routing (method, path) -> data via handler.
// Returning undefined yields a 404 so a missing mock is obvious.
export function installFetch(handler: (method: string, path: string) => unknown) {
  globalThis.fetch = vi.fn(async (input: unknown, init?: { method?: string }) => {
    const path = typeof input === 'string' ? input : String((input as { url: string }).url)
    const method = (init?.method || 'GET').toUpperCase()
    const data = handler(method, path)
    if (data === undefined) return jsonResponse({ error: { message: `no mock: ${method} ${path}` } }, 404)
    return jsonResponse(data)
  }) as unknown as typeof fetch
}

// appDetailRoutes wires the GETs AppDetail issues on mount for a given sandbox.
export const runtimeInspectFixture = {
  existing_manifest: { present: false },
  suggestions: [
    { preset: 'nextjs', runnable: true, confidence: 'high', reasons: ['next is a dependency'] },
    {
      preset: 'astro',
      runnable: false,
      confidence: 'high',
      reasons: ['astro is a dependency'],
      warnings: ['Astro dev defaults to port 4321 and blocks unknown hosts; there is no built-in Astro preset yet'],
    },
  ],
  default_suggestion: 'nextjs',
  alternatives: ['astro'],
}

export function appDetailRoutes(sandbox: Sandbox) {
  return (_m: string, p: string): unknown => {
    if (/\/v1\/apps\/[^/]+\/runtime-inspect/.test(p)) return runtimeInspectFixture
    if (/\/v1\/apps\/[^/]+\/config/.test(p)) return { config: configFixture }
    if (/\/v1\/apps\/[^/]+\/events/.test(p)) return { events: eventsFixture }
    if (/\/v1\/apps\/[^/]+\/snapshots/.test(p)) return { snapshots: [] }
    if (/\/v1\/sandboxes\/[^/]+$/.test(p)) return sandbox
    if (/\/v1\/apps\/[^/]+$/.test(p)) return appsFixture[0]
    return undefined
  }
}
