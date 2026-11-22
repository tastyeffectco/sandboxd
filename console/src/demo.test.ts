import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { installDemo } from './demo'

// The static demo (sandboxd.io/demo) answers every /v1 call from fixtures and
// refuses writes, so it can never reach or change a real engine.
describe('demo mode fetch', () => {
  const realFetch = globalThis.fetch
  const passthrough = vi.fn(async () => new Response('real'))

  beforeEach(() => {
    passthrough.mockClear()
    globalThis.fetch = passthrough as unknown as typeof fetch
    installDemo()
  })
  afterEach(() => {
    globalThis.fetch = realFetch
  })

  it('answers reads from sample data', async () => {
    const res = await fetch('/v1/apps')
    expect(res.ok).toBe(true)
    const body = (await res.json()) as { apps: { name: string }[] }
    expect(body.apps.map((a) => a.name)).toEqual(['Recipe Box', 'Todo App'])
    expect(passthrough).not.toHaveBeenCalled()
  })

  it('opens Home on the sample sandboxd agent, with its conversation', async () => {
    const res = await fetch('/v1/assistant', { method: 'POST' })
    expect(res.ok).toBe(true)
    const { app_id } = (await res.json()) as { app_id: string }
    const home = (await (await fetch(`/v1/apps/${app_id}`)).json()) as { tags: string[]; current_sandbox_id: string }
    expect(home.tags).toContain('sandboxd:assistant')
    const { tasks } = (await (await fetch(`/v1/sandboxes/${home.current_sandbox_id}/tasks`)).json()) as {
      tasks: unknown[]
    }
    expect(tasks.length).toBeGreaterThan(0)
  })

  it('shows a crew of agents and a conversation with Hermes', async () => {
    const { agents } = (await (await fetch('/v1/agent-instances')).json()) as { agents: { name: string }[] }
    expect(agents.map((a) => a.name)).toEqual(['Hermes', 'OpenClaw', 'OpenMausBot'])
    const { messages } = (await (await fetch('/v1/agent-instances/demo-hermes/messages')).json()) as {
      messages: unknown[]
    }
    expect(messages.length).toBe(5)
  })

  it('reports signed in so the console opens without a password', async () => {
    const body = await (await fetch('/v1/auth/status')).json()
    expect(body).toMatchObject({ authenticated: true })
  })

  it('refuses every write with a read-only message that says how to install', async () => {
    for (const method of ['POST', 'PATCH', 'PUT', 'DELETE']) {
      const res = await fetch('/v1/apps', { method })
      expect(res.status).toBe(403)
      expect(res.ok).toBe(false)
      const body = (await res.json()) as { error: { message: string } }
      expect(body.error.message).toMatch(/read-only demo/)
      expect(body.error.message).toContain('install.sh')
    }
    expect(passthrough).not.toHaveBeenCalled()
  })

  it('serves sample file contents as text and 404s unknown files', async () => {
    const ok = await fetch('/v1/sandboxes/x/files/content?path=workspace/app/sandbox.yaml')
    expect(ok.headers.get('content-type')).toBe('text/plain')
    expect(await ok.text()).toContain('web:')
    const missing = await fetch('/v1/sandboxes/x/files/content?path=nope.txt')
    expect(missing.status).toBe(404)
  })

  it('leaves requests outside /v1 to the real network', async () => {
    const res = await fetch('/demo/sample-app')
    expect(await res.text()).toBe('real')
    expect(passthrough).toHaveBeenCalledTimes(1)
  })
})
