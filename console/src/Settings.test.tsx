import { describe, it, expect, beforeEach, vi } from 'vitest'
import { render, screen, fireEvent, waitFor } from '@testing-library/react'
import { Settings } from './Settings'
import { installFetch, settingsFixture, agentsFixture } from './test/fixtures'

const noop = () => {}

describe('console — Settings page', () => {
  beforeEach(() => {
    installFetch((m, p) => {
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
    // no api-key-looking value, and no password input anywhere on the page
    const page = screen.getByTestId('settings-page')
    expect(page.textContent || '').not.toMatch(/sk-[A-Za-z0-9]{8,}/)
    expect(page.querySelector('input[type="password"]')).toBeNull()
  })

  it('AI Agents: only Claude Code is connectable; no token rendered', async () => {
    render(<Settings onError={noop} />)
    expect(await screen.findByTestId('settings-agents-list')).toBeTruthy()
    for (const a of agentsFixture) expect(screen.getByTestId(`agent-${a.id}`)).toBeTruthy()
    expect(screen.getByTestId('agent-codex').textContent).toMatch(/not installed/i)
    // claude-code (needs_login) shows the subscription Connect button…
    const connect = screen.getByTestId('agent-connect')
    expect(connect.textContent).toMatch(/use your claude subscription/i)
    // …but opencode/codex are NOT connectable in A2 (claude-code only).
    expect(screen.getByTestId('agent-opencode').querySelector('button')).toBeNull()
    expect(screen.getByTestId('agent-codex').querySelector('button')).toBeNull()
    // no token-looking value anywhere on the page
    expect(screen.getByTestId('settings-page').textContent || '').not.toMatch(/sk-[A-Za-z0-9]{8,}/)
  })

  it('Connect Claude Code: shows login URL, accepts pasted code, connects', async () => {
    const url = 'https://claude.ai/oauth/authorize?response_type=code&state=x'
    installFetch((m, p) => {
      if (p.startsWith('/v1/agents/claude-code/connect')) {
        if (m === 'POST' && p.endsWith('/code')) return { session_id: 's1', status: 'connected' }
        if (m === 'POST') return { session_id: 's1', status: 'awaiting_code', url }
        if (m === 'GET') return { session_id: 's1', status: 'awaiting_code', url }
      }
      if (m === 'GET' && p.startsWith('/v1/agents')) return { providers: agentsFixture }
      if (m === 'GET' && p.startsWith('/v1/settings')) return settingsFixture
      return undefined
    })
    render(<Settings onError={noop} />)
    fireEvent.click(await screen.findByTestId('agent-connect'))
    // modal opens and surfaces the login URL (not a token)
    const link = await screen.findByTestId('claude-connect-url')
    expect((link as HTMLAnchorElement).href).toContain('response_type=code')
    // paste code + submit
    fireEvent.change(screen.getByTestId('claude-code-input'), { target: { value: 'THECODE' } })
    fireEvent.click(screen.getByTestId('claude-code-submit'))
    // on success the modal closes
    await waitFor(() => expect(screen.queryByTestId('claude-connect-modal')).toBeNull())
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
})
