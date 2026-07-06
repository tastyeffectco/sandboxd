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
  git?: { repo_url: string; branch?: string; credential_id?: string }
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
  agents: { providers: string[]; system_prompt?: string }
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

// Advisory runtime detection (GET /v1/apps/{id}/runtime-inspect). Suggestions
// only — never applied; the console renders, it owns no detection logic.
export interface ConfigSnippet {
  file: string
  note: string
}
export interface RuntimeSuggestion {
  preset: string
  runnable: boolean
  confidence: 'high' | 'medium' | 'low'
  reasons: string[]
  warnings?: string[]
  suggested_manifest?: string // advisory sandbox.yaml text (detect-only stacks)
  notes?: string[]
  config_snippets?: ConfigSnippet[]
}

// Manifest validation (POST /v1/runtime/manifest/validate) + effective view.
export interface ManifestEffective {
  web?: { command: string; port: number; health_path: string }
  workers: { name: string; command: string }[]
}
export interface ManifestValidation {
  valid: boolean
  errors: string[]
  warnings: string[]
  effective?: ManifestEffective
}
// GET /v1/apps/{id}/runtime/manifest
export interface AppManifest {
  present: boolean
  reason?: string
  source?: 'sandbox.yaml' | 'preset' | 'default'
  manifest?: string
  validation?: ManifestValidation
  effective?: ManifestEffective
}
export interface RuntimeManifestSummary {
  present: boolean
  authoritative?: boolean
  web_command?: string
  web_port?: number
  health_path?: string
}
export interface RuntimeInspect {
  existing_manifest: RuntimeManifestSummary
  suggestions: RuntimeSuggestion[]
  alternatives?: string[]
  default_suggestion?: string
  warnings?: string[]
}

// Read-only Git status/diff (A2). Runs in-sandbox; no network/credentials.
export interface GitFile {
  path: string
  status: string // modified|added|deleted|renamed|copied|untracked|unmerged
  staged: boolean
}
export interface GitStatus {
  available: boolean
  reason?: string
  branch?: string
  head_sha?: string
  clean?: boolean // raw repo clean (no changes at all)
  user_clean?: boolean // clean ignoring runtime-generated files
  ahead?: number | null
  behind?: number | null
  files?: GitFile[] // user/repo changes
  runtime_files?: GitFile[] // sandboxd/runtime-generated (sandbox.yaml, lockfiles, caches)
}
export interface GitDiff {
  available: boolean
  reason?: string
  diff?: string
  truncated?: boolean
}
export interface GitCommitResult {
  committed: boolean
  reason?: string
  sha?: string
  branch?: string
  files_committed?: string[]
}
export interface GitPushResult {
  pushed: boolean
  reason?: string
  branch?: string
  remote_url?: string
  commits?: number
  head_detached?: boolean
}

// Git credential metadata (GET /v1/git-credentials). The token is write-only —
// it is sent on create and never returned.
export interface GitCredential {
  id: string
  name: string
  host: string
  username: string
  token_set: boolean
  created_at: string
}

export interface Preview {
  url: string
  status: string
  // The resolved web port the preview URL routes to (Traefik target). Fixed at
  // sandbox create; compare against the effective manifest port to detect drift.
  port?: number
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
  // Guided Claude subscription login: get the authorize link, then submit the
  // code the user pastes back. Tokens are exchanged + stored + refreshed host-side.
  oauthStart: (provider: string) =>
    req<{ authorize_url: string }>('POST', `/v1/agents/${provider}/oauth/start`),
  oauthFinish: (provider: string, code: string) =>
    req<{ provider: string; status: string; method: string }>('POST', `/v1/agents/${provider}/oauth/finish`, {
      code,
    }),
  patchSettings: (body: SettingsPatch) => req<Settings>('PATCH', '/v1/settings', body),

  // Git credentials (for importing private repos in a later v0.4.x release).
  // The token is write-only: sent on create, never returned.
  listGitCredentials: () =>
    req<{ credentials: GitCredential[] }>('GET', '/v1/git-credentials').then((r) => r.credentials || []),
  createGitCredential: (b: { name: string; host?: string; username?: string; token: string }) =>
    req<GitCredential>('POST', '/v1/git-credentials', b),
  deleteGitCredential: (id: string) => req<unknown>('DELETE', `/v1/git-credentials/${id}`),
  runtimeInspect: (appId: string) => req<RuntimeInspect>('GET', `/v1/apps/${appId}/runtime-inspect`),
  appManifest: (appId: string) => req<AppManifest>('GET', `/v1/apps/${appId}/runtime/manifest`),
  validateManifest: (mfst: string) =>
    req<ManifestValidation>('POST', '/v1/runtime/manifest/validate', { manifest: mfst }),
  // Write one workspace file via the existing generic PUT endpoint (raw body).
  // Used by the explicit "Apply sandbox.yaml" CTA — console-only, no new endpoint.
  // Read a workspace file. NOTE the path asymmetry: GET content is relative to
  // the APP dir (e.g. "AGENTS.md"), while putWorkspaceFile is relative to the
  // workspace MOUNT (e.g. "workspace/app/AGENTS.md"). Returns null on 404.
  getWorkspaceFile: async (sandboxId: string, appRelPath: string): Promise<string | null> => {
    const res = await fetch(`/v1/sandboxes/${sandboxId}/files/content?path=${encodeURIComponent(appRelPath)}`)
    if (res.status === 404) return null
    if (!res.ok) throw new Error(`${res.status} ${res.statusText}`)
    return res.text()
  },
  putWorkspaceFile: async (sandboxId: string, path: string, content: string): Promise<void> => {
    const res = await fetch(`/v1/sandboxes/${sandboxId}/files?path=${encodeURIComponent(path)}`, {
      method: 'PUT',
      headers: { 'content-type': 'text/plain' },
      body: content,
    })
    if (!res.ok) {
      let message = `${res.status} ${res.statusText}`
      try {
        const e = await res.json()
        if (e?.error?.message) message = e.error.message
      } catch {
        /* non-JSON */
      }
      throw new Error(message)
    }
  },
  gitStatus: (appId: string) => req<GitStatus>('GET', `/v1/apps/${appId}/git/status`),
  gitDiff: (appId: string, path?: string) =>
    req<GitDiff>('GET', `/v1/apps/${appId}/git/diff${path ? `?path=${encodeURIComponent(path)}` : ''}`),
  gitCommit: (
    appId: string,
    body: { message: string; paths?: string[]; runtime_paths?: string[]; author_name?: string; author_email?: string },
  ) => req<GitCommitResult>('POST', `/v1/apps/${appId}/git/commit`, body),
  gitPush: (appId: string, body: { branch?: string }) =>
    req<GitPushResult>('POST', `/v1/apps/${appId}/git/push`, body),
  createApp: (b: {
    name: string
    description?: string
    tags?: string[]
    runtime_preset?: string
    git?: { repo_url: string; branch?: string; credential_id?: string } // credential_id omitted → public tokenless clone
  }) => req<App>('POST', '/v1/apps', b),
  getApp: (id: string) => req<App>('GET', `/v1/apps/${id}`),
  // Full delete: the app AND everything it owns (sandbox container, workspace
  // image, snapshots, and all app rows). Irreversible.
  deleteApp: (id: string) => req<unknown>('DELETE', `/v1/apps/${id}`),
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

  submitTask: (id: string, prompt: string, agent: string = 'opencode', model?: string) =>
    req<{ id: string }>('POST', `/v1/sandboxes/${id}/tasks`, {
      prompt,
      agent,
      ...(model && model.trim() ? { model: model.trim() } : {}),
    }),
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
