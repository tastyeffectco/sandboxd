import { describe, it, expect, beforeEach, vi } from 'vitest'
import { render, screen, fireEvent, waitFor } from '@testing-library/react'
import { Settings } from './Settings'
import { installFetch, settingsFixture, gitCredentialsFixture, agentsFixture } from './test/fixtures'

const noop = () => {}

describe('console — Settings page', () => {
  beforeEach(() => {
    installFetch((m, p) => {
      if (m === 'GET' && p.startsWith('/v1/git-credentials')) return { credentials: gitCredentialsFixture }
      if (m === 'GET' && p.startsWith('/v1/agents')) return { providers: agentsFixture }
      if (m === 'GET' && p.startsWith('/v1/settings')) return settingsFixture
      return undefined
    })
  })

  it('renders all settings sections from /v1/settings', async () => {
    render(<Settings onError={noop} />)
    expect(await screen.findByTestId('settings-system')).toBeTruthy()
    for (const id of [
      'settings-networking',
      'settings-runtime',
      'settings-agents',
      'settings-security',
      'settings-egress',
      'settings-capabilities',
    ]) {
      expect(screen.getByTestId(id)).toBeTruthy()
    }
    // safe values surfaced (unique strings)
    expect(screen.getByText('v0.4.0')).toBeTruthy()
    expect(screen.getByText('http://*.preview.localhost:18080')).toBeTruthy()
    expect(screen.getByTestId('settings-agents-list')).toBeTruthy() // AI Agents from /v1/agents
    expect(screen.getByTestId('settings-presets')).toBeTruthy()
    // egress mode rendered as a mode word
    expect(screen.getByTestId('settings-egress').textContent).toMatch(/disabled/i)
  })

  it('shows auth mode as a boolean state, never a secret/token', async () => {
    render(<Settings onError={noop} />)
    await screen.findByTestId('settings-security')
    // auth rendered as enabled/disabled, not a value
    const sec = screen.getByTestId('settings-security')
    expect(sec.textContent).toMatch(/API auth/i)
    expect(sec.textContent).toMatch(/disabled/i)
    // no api-key-looking value anywhere, and the security/auth section itself
    // exposes no secret/password input (the Git-credential token field is a
    // separate, intentional write-only input elsewhere on the page).
    const page = screen.getByTestId('settings-page')
    expect(page.textContent || '').not.toMatch(/sk-[A-Za-z0-9]{8,}/)
    expect(sec.querySelector('input')).toBeNull()
  })

  it('AI Agents: every provider offers subscription + API-key connect; no token rendered', async () => {
    render(<Settings onError={noop} />)
    expect(await screen.findByTestId('settings-agents-list')).toBeTruthy()
    for (const a of agentsFixture) expect(screen.getByTestId(`agent-${a.id}`)).toBeTruthy()
    expect(screen.getByTestId('agent-codex').textContent).toMatch(/not installed/i)
    // Each not-connected provider shows both connect actions.
    for (const id of ['claude-code', 'codex']) {
      const card = screen.getByTestId(`agent-${id}`)
      expect(card.querySelector('[data-testid="agent-connect-oauth"]')).toBeTruthy()
      expect(card.querySelector('[data-testid="agent-connect-apikey"]')).toBeTruthy()
    }
    // opencode is connected (oauth) → shows Disconnect, not connect actions.
    const oc = screen.getByTestId('agent-opencode')
    expect(oc.querySelector('[data-testid="agent-disconnect"]')).toBeTruthy()
    expect(oc.querySelector('[data-testid="agent-connect-oauth"]')).toBeNull()
    expect(oc.textContent).toMatch(/via subscription/i)
    expect(screen.getByTestId('settings-page').textContent || '').not.toMatch(/sk-[A-Za-z0-9]{8,}/)
  })

  it('shows "runner not enabled yet" when claude-code is connected but not runnable', async () => {
    const connectedNotRunnable = agentsFixture.map((a) =>
      a.id === 'claude-code' ? { ...a, status: 'connected', runnable: false } : a,
    )
    installFetch((m, p) => {
      if (m === 'GET' && p.startsWith('/v1/agents')) return { providers: connectedNotRunnable }
      if (m === 'GET' && p.startsWith('/v1/settings')) return settingsFixture
      return undefined
    })
    render(<Settings onError={noop} />)
    expect(await screen.findByTestId('agent-runner-disabled')).toBeTruthy()
    expect(screen.getByTestId('agent-runner-disabled').textContent).toMatch(/runner not enabled yet/i)
    // disconnect is offered on the claude-code card; no token shown
    const card = screen.getByTestId('agent-claude-code')
    expect(card.querySelector('[data-testid="agent-disconnect"]')).toBeTruthy()
  })

  it('Connect subscription: posts the pasted credential bundle opaquely', async () => {
    let posted: unknown = null
    let postedPath = ''
    installFetch((m, p) => {
      if (m === 'POST' && p === '/v1/agents/claude-code/import')
        return { provider: 'claude-code', status: 'connected', method: 'oauth' }
      if (m === 'GET' && p.startsWith('/v1/agents')) return { providers: agentsFixture }
      if (m === 'GET' && p.startsWith('/v1/settings')) return settingsFixture
      return undefined
    })
    const realFetch = globalThis.fetch
    globalThis.fetch = vi.fn(async (input: unknown, init?: { method?: string; body?: string }) => {
      if ((init?.method || 'GET').toUpperCase() === 'POST' && String(input).includes('/agents/')) {
        posted = init?.body ? JSON.parse(init.body) : null
        postedPath = String(input)
      }
      return realFetch(input as never, init as never)
    }) as unknown as typeof fetch

    render(<Settings onError={noop} />)
    const card = await screen.findByTestId('agent-claude-code')
    fireEvent.click(card.querySelector('[data-testid="agent-connect-oauth"]') as Element)
    const ta = await screen.findByTestId('agent-connect-input')
    fireEvent.change(ta, { target: { value: '{"claudeAiOauth":{"x":1}}' } })
    fireEvent.click(screen.getByTestId('agent-connect-submit'))
    await waitFor(() => expect(posted).not.toBeNull())
    expect(postedPath).toContain('/v1/agents/claude-code/import')
    expect((posted as { credentials: string }).credentials).toContain('claudeAiOauth')
    await waitFor(() => expect(screen.queryByTestId('agent-connect-modal')).toBeNull())
  })

  it('Use API key: posts the key to the provider api-key endpoint', async () => {
    let posted: unknown = null
    let postedPath = ''
    installFetch((m, p) => {
      if (m === 'POST' && p === '/v1/agents/codex/api-key')
        return { provider: 'codex', status: 'connected', method: 'api_key' }
      if (m === 'GET' && p.startsWith('/v1/agents')) return { providers: agentsFixture }
      if (m === 'GET' && p.startsWith('/v1/settings')) return settingsFixture
      return undefined
    })
    const realFetch = globalThis.fetch
    globalThis.fetch = vi.fn(async (input: unknown, init?: { method?: string; body?: string }) => {
      if ((init?.method || 'GET').toUpperCase() === 'POST' && String(input).includes('/agents/')) {
        posted = init?.body ? JSON.parse(init.body) : null
        postedPath = String(input)
      }
      return realFetch(input as never, init as never)
    }) as unknown as typeof fetch

    render(<Settings onError={noop} />)
    const card = await screen.findByTestId('agent-codex')
    fireEvent.click(card.querySelector('[data-testid="agent-connect-apikey"]') as Element)
    const ta = await screen.findByTestId('agent-connect-input')
    fireEvent.change(ta, { target: { value: 'sk-secret-key' } })
    fireEvent.click(screen.getByTestId('agent-connect-submit'))
    await waitFor(() => expect(posted).not.toBeNull())
    expect(postedPath).toContain('/v1/agents/codex/api-key')
    expect((posted as { api_key: string }).api_key).toBe('sk-secret-key')
    await waitFor(() => expect(screen.queryByTestId('agent-connect-modal')).toBeNull())
  })

  it('lifecycle is editable; protected sections have no inputs', async () => {
    render(<Settings onError={noop} />)
    // lifecycle section exposes editable inputs (server marked them editable)
    expect(await screen.findByTestId('settings-idle-threshold')).toBeTruthy()
    expect(screen.getByTestId('settings-keepalive')).toBeTruthy()
    expect(screen.getByTestId('settings-idle-enabled')).toBeTruthy()
    // read-only sections must NOT contain inputs
    for (const id of ['settings-networking', 'settings-security', 'settings-egress', 'settings-agents']) {
      expect(screen.getByTestId(id).querySelector('input')).toBeNull()
    }
  })

  it('Save sends a PATCH with only the lifecycle fields', async () => {
    let patched: { method: string; body: unknown } | null = null
    installFetch((m, p) => {
      if (m === 'GET' && p.startsWith('/v1/git-credentials')) return { credentials: gitCredentialsFixture }
      if (m === 'GET' && p.startsWith('/v1/agents')) return { providers: agentsFixture }
      if (m === 'GET' && p.startsWith('/v1/settings')) return settingsFixture
      if (m === 'PATCH' && p.startsWith('/v1/settings')) return settingsFixture
      return undefined
    })
    // capture the PATCH body
    const realFetch = globalThis.fetch
    globalThis.fetch = vi.fn(async (input: unknown, init?: { method?: string; body?: string }) => {
      if ((init?.method || 'GET').toUpperCase() === 'PATCH') {
        patched = { method: 'PATCH', body: init?.body ? JSON.parse(init.body) : null }
      }
      return realFetch(input as never, init as never)
    }) as unknown as typeof fetch

    render(<Settings onError={noop} />)
    const idle = await screen.findByTestId('settings-idle-threshold')
    fireEvent.change(idle, { target: { value: '600' } })
    fireEvent.click(screen.getByTestId('settings-save'))

    await waitFor(() => expect(patched).not.toBeNull())
    expect(patched!.body).toHaveProperty('lifecycle')
    expect((patched!.body as { lifecycle: { idle_threshold_seconds: number } }).lifecycle.idle_threshold_seconds).toBe(600)
    // body carries ONLY lifecycle (no protected keys)
    expect(Object.keys(patched!.body as object)).toEqual(['lifecycle'])
  })

  it('Git credentials: lists, adds (token write-only, cleared, never rendered), deletes', async () => {
    let posted: { name: string; token: string } | null = null
    let deleted = false
    installFetch((m, p) => {
      if (m === 'GET' && p.startsWith('/v1/git-credentials')) return { credentials: gitCredentialsFixture }
      if (m === 'POST' && p === '/v1/git-credentials') return { id: 'newid', name: 'gl', host: '', username: '', token_set: true, created_at: 'x' }
      if (m === 'DELETE' && p.startsWith('/v1/git-credentials/')) return {}
      if (m === 'GET' && p.startsWith('/v1/settings')) return settingsFixture
      return undefined
    })
    const realFetch = globalThis.fetch
    globalThis.fetch = vi.fn(async (input: unknown, init?: { method?: string; body?: string }) => {
      const method = (init?.method || 'GET').toUpperCase()
      if (method === 'POST' && String(input).endsWith('/git-credentials')) posted = init?.body ? JSON.parse(init.body) : null
      if (method === 'DELETE' && String(input).includes('/git-credentials/')) deleted = true
      return realFetch(input as never, init as never)
    }) as unknown as typeof fetch

    render(<Settings onError={noop} />)
    // existing credential is listed; the token is never present anywhere on the page
    expect(await screen.findByTestId('git-cred-01GITCREDAAAAAAAAAAAAAAAAA')).toBeTruthy()
    const SECRET = 'ghp_secret_never_render'
    // add a credential
    fireEvent.change(screen.getByTestId('git-cred-name'), { target: { value: 'gl' } })
    const tokenInput = screen.getByTestId('git-cred-token') as HTMLInputElement
    expect(tokenInput.type).toBe('password') // write-only field
    fireEvent.change(tokenInput, { target: { value: SECRET } })
    fireEvent.click(screen.getByTestId('git-cred-add'))

    await waitFor(() => expect(posted).not.toBeNull())
    expect(posted!.token).toBe(SECRET) // token IS sent in the request body
    // …but cleared from the field afterwards and never rendered back
    await waitFor(() => expect((screen.getByTestId('git-cred-token') as HTMLInputElement).value).toBe(''))
    expect(screen.getByTestId('settings-page').textContent || '').not.toContain(SECRET)

    // delete
    fireEvent.click(screen.getByTestId('git-cred-delete-01GITCREDAAAAAAAAAAAAAAAAA'))
    await waitFor(() => expect(deleted).toBe(true))
  })
})
