import { describe, it, expect, beforeEach, vi } from 'vitest'
import { render, screen, fireEvent, waitFor } from '@testing-library/react'
import { Settings } from './Settings'
import { installFetch, settingsFixture, gitCredentialsFixture } from './test/fixtures'

const noop = () => {}

describe('console — Settings page', () => {
  beforeEach(() => {
    installFetch((m, p) => {
      if (m === 'GET' && p.startsWith('/v1/git-credentials')) return { credentials: gitCredentialsFixture }
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
    expect(screen.getByText('opencode')).toBeTruthy()
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
