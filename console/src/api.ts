// Thin client for the public sandboxd /v1 API. The console talks ONLY
// to /v1 (same-origin; proxied to sandboxd by vite in dev and nginx in
// the container). No Go imports, no DB, no workspace access.

export interface App {
  id: string
  name: string
  description: string
  tags: string[]
  current_sandbox_id?: string
  created_at: string
  updated_at: string
}

export interface Preview {
  url: string
  status: string
}

export interface Sandbox {
  id: string
  status: string
  preview?: Preview
}

export interface TaskResult {
  id: string
  status: string
  build_ok?: boolean
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
    throw new Error(message)
  }
  const ct = res.headers.get('content-type') || ''
  return (ct.includes('application/json') ? res.json() : (res.text() as unknown)) as Promise<T>
}

export const api = {
  listApps: () => req<{ apps: App[] }>('GET', '/v1/apps').then((r) => r.apps || []),
  createApp: (b: { name: string; description?: string; tags?: string[] }) =>
    req<App>('POST', '/v1/apps', b),
  getApp: (id: string) => req<App>('GET', `/v1/apps/${id}`),
  createAppSandbox: (id: string, body: { template?: string } = {}) =>
    req<Sandbox>('POST', `/v1/apps/${id}/sandbox`, body),

  getSandbox: (id: string) => req<Sandbox>('GET', `/v1/sandboxes/${id}`),
  startSandbox: (id: string) => req<Sandbox>('POST', `/v1/sandboxes/${id}/start`),
  stopSandbox: (id: string) => req<Sandbox>('POST', `/v1/sandboxes/${id}/stop`),
  deleteSandbox: (id: string) => req<unknown>('DELETE', `/v1/sandboxes/${id}`),

  submitTask: (id: string, prompt: string) =>
    req<{ id: string }>('POST', `/v1/sandboxes/${id}/tasks`, { prompt, agent: 'opencode' }),
  getTask: (id: string, taskId: string) =>
    req<TaskResult>('GET', `/v1/sandboxes/${id}/tasks/${taskId}`),
  taskEventsURL: (id: string, taskId: string) =>
    `/v1/sandboxes/${id}/tasks/${taskId}/events`,

  createSnapshot: (sandboxId: string, name: string) =>
    req<{ id: string }>('POST', '/v1/snapshots', { source_sandbox_id: sandboxId, name }),
}
