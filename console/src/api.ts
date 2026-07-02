// Thin client for the public sandboxd /v1 API. The console talks ONLY
// to /v1 (same-origin; proxied to sandboxd by vite in dev and nginx in
// the container). No Go imports, no DB, no workspace access.

export interface App {
  id: string
  name: string
  description: string
  tags: string[]
  runtime_preset?: string
  current_sandbox_id?: string
  created_at: string
  updated_at: string
}

export interface Preset {
  id: string
  label: string
  description: string
  template?: string
  capabilities?: string[]
}

// Read-only instance/settings summary (GET /v1/settings). Safe metadata only —
// the server never includes secrets/tokens/keys here.
export interface Settings {
  version: string
  git_commit?: string
  networking: {
    preview_domain: string
    public_http_port?: string
    preview_base: string
    preview_tls: boolean
    preview_entrypoint?: string
  }
  auth: { enabled: boolean }
  runtime: { storage_mode: string; base_image: string }
  lifecycle: { idle_reap_enabled: boolean; idle_threshold_seconds: number; keepalive_max_seconds: number }
  egress: { mode: string }
  agents: { providers: string[] }
  presets: Preset[]
  capabilities: Record<string, boolean>
  editable?: string[] // field paths the client may PATCH (e.g. lifecycle.*)
}

// Read-only AI Agents status (GET /v1/agents). No tokens are ever returned.
// `runnable` = runtimed has a task adapter for this provider; a connected but
// not-runnable provider means "credentials imported, runner not enabled yet".
export interface Agent {
  id: string
  label: string
  installed_state: 'installed' | 'not_installed' | 'unknown'
  status: 'connected' | 'needs_login'
  // How the provider is currently connected. '' when not connected.
  method: 'oauth' | 'api_key' | ''
  supports_oauth: boolean
  supports_api_key: boolean
  runnable: boolean
}

export interface SettingsPatch {
  lifecycle?: {
    idle_reap_enabled?: boolean
    idle_threshold_seconds?: number
    keepalive_max_seconds?: number
  }
}

export interface Preview {
  url: string
  status: string
}

export type AccessPolicy = 'control_plane_only' | 'agent_access' | 'runtime_access' | 'both'

export interface ConfigItem {
  key: string
  sensitive: boolean
  access_policy: AccessPolicy
  value_set: boolean
  value?: string // non-sensitive entries only; secrets are never returned
  created_at: string
  updated_at: string
}

export interface Process {
  name: string
  kind: string // "web" | "worker"
  running: boolean
  pid?: number
  restarts: number
}

export interface Sandbox {
  id: string
  status: string
  preview?: Preview
  processes?: Process[]
}

export interface AppEvent {
  id: string
  type: string
  severity: string
  message: string
  app_id?: string
  sandbox_id?: string
  task_id?: string
  snapshot_id?: string
  payload?: Record<string, unknown>
  created_at: string
}

export interface Snapshot {
  id: string
  name: string
  status: string
  source_app_id?: string
  size_bytes?: number
  created_at: string
}

export interface TaskResult {
  id: string
  status: string
  build_ok?: boolean
  build_status?: 'passed' | 'failed' | 'skipped'
  preview_ok?: boolean // omitted for worker-only apps (no public endpoint)
  app_healthy?: boolean
  files_changed?: string[]
  error_message?: string
  preview_status_after?: string
}

async function req<T>(method: string, path: string, body?: unknown): Promise<T> {
  const res = await fetch(path, {
    method,
    headers: body !== undefined ? { 'content-type': 'application/json' } : undefined,
    body: body !== undefined ? JSON.stringify(body) : undefined,
  })
  if (!res.ok) {
    let message = `${res.status} ${res.statusText}`
    try {
      const e = await res.json()
      if (e?.error?.message) message = e.error.message
    } catch {
      /* non-JSON error body */
    }
    const err = new Error(message) as Error & { status?: number }
    err.status = res.status
    throw err
  }
  const ct = res.headers.get('content-type') || ''
  return (ct.includes('application/json') ? res.json() : (res.text() as unknown)) as Promise<T>
}

export const api = {
  listApps: () => req<{ apps: App[] }>('GET', '/v1/apps').then((r) => r.apps || []),
  listPresets: () => req<{ presets: Preset[] }>('GET', '/v1/presets').then((r) => r.presets || []),
  getSettings: () => req<Settings>('GET', '/v1/settings'),
  getAgents: () => req<{ providers: Agent[] }>('GET', '/v1/agents').then((r) => r.providers || []),

  // Connect an agent provider by subscription (paste the credential bundle the
  // owner's `<cli> login` produced) — stored opaquely, never parsed.
  importAgentCredential: (provider: string, credentials: string) =>
    req<{ provider: string; status: string; method: string }>('POST', `/v1/agents/${provider}/import`, {
      credentials,
    }),
  // Connect an agent provider by API key — stored opaquely; injected as the
  // provider's key env var at task time.
  setAgentApiKey: (provider: string, api_key: string) =>
    req<{ provider: string; status: string; method: string }>('POST', `/v1/agents/${provider}/api-key`, {
      api_key,
    }),
  disconnectAgent: (provider: string) => req<unknown>('POST', `/v1/agents/${provider}/disconnect`),
  patchSettings: (body: SettingsPatch) => req<Settings>('PATCH', '/v1/settings', body),
  createApp: (b: { name: string; description?: string; tags?: string[]; runtime_preset?: string }) =>
    req<App>('POST', '/v1/apps', b),
  getApp: (id: string) => req<App>('GET', `/v1/apps/${id}`),
  createAppSandbox: (id: string, body: { template?: string; runtime_preset?: string } = {}) =>
    req<Sandbox>('POST', `/v1/apps/${id}/sandbox`, body),

  // App config & secrets. Sensitive values are write-only: the server
  // never returns them, so the UI shows metadata (value_set) only.
  listConfig: (appId: string) =>
    req<{ config: ConfigItem[] }>('GET', `/v1/apps/${appId}/config`).then((r) => r.config || []),
  createConfig: (
    appId: string,
    body: { key: string; value: string; sensitive: boolean; access_policy: AccessPolicy },
  ) => req<ConfigItem>('POST', `/v1/apps/${appId}/config`, body),
  patchConfig: (
    appId: string,
    key: string,
    body: { value?: string; sensitive?: boolean; access_policy?: AccessPolicy },
  ) => req<ConfigItem>('PATCH', `/v1/apps/${appId}/config/${encodeURIComponent(key)}`, body),
  deleteConfig: (appId: string, key: string) =>
    req<unknown>('DELETE', `/v1/apps/${appId}/config/${encodeURIComponent(key)}`),

  getSandbox: (id: string) => req<Sandbox>('GET', `/v1/sandboxes/${id}`),
  getProcessLogs: (id: string, name: string, tail = 200) =>
    req<{ process: string; lines: string[] }>(
      'GET',
      `/v1/sandboxes/${id}/processes/${encodeURIComponent(name)}/logs?tail=${tail}`,
    ),
  startSandbox: (id: string) => req<Sandbox>('POST', `/v1/sandboxes/${id}/start`),
  stopSandbox: (id: string) => req<Sandbox>('POST', `/v1/sandboxes/${id}/stop`),
  deleteSandbox: (id: string) => req<unknown>('DELETE', `/v1/sandboxes/${id}`),

  submitTask: (id: string, prompt: string, agent: string = 'opencode') =>
    req<{ id: string }>('POST', `/v1/sandboxes/${id}/tasks`, { prompt, agent }),
  getTask: (id: string, taskId: string) =>
    req<TaskResult>('GET', `/v1/sandboxes/${id}/tasks/${taskId}`),
  taskEventsURL: (id: string, taskId: string) =>
    `/v1/sandboxes/${id}/tasks/${taskId}/events`,

  createSnapshot: (sandboxId: string, name: string) =>
    req<{ id: string }>('POST', '/v1/snapshots', { source_sandbox_id: sandboxId, name }),

  // Phase 4 — app-scoped snapshot history, restore, fork.
  listAppSnapshots: (appId: string) =>
    req<{ snapshots: Snapshot[] }>('GET', `/v1/apps/${appId}/snapshots`).then((r) => r.snapshots || []),
  restoreApp: (appId: string, snapshotId: string) =>
    req<Sandbox>('POST', `/v1/apps/${appId}/restore`, { snapshot_id: snapshotId }),
  forkApp: (appId: string, snapshotId: string, name: string) =>
    req<{ app: App }>('POST', `/v1/apps/${appId}/fork`, { snapshot_id: snapshotId, name }),

  // Phase 5 — durable activity timeline (newest-first).
  listAppEvents: (appId: string, limit = 50) =>
    req<{ events: AppEvent[]; next_before?: string }>(
      'GET',
      `/v1/apps/${appId}/events?limit=${limit}`,
    ).then((r) => r.events || []),
}
