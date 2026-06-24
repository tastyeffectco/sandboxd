import { describe, it, expect, beforeEach } from 'vitest'
import { render, screen } from '@testing-library/react'
import { Settings } from './Settings'
import { installFetch, settingsFixture } from './test/fixtures'

const noop = () => {}

describe('console — Settings page', () => {
  beforeEach(() => {
    installFetch((m, p) => {
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
    // no api-key-looking value, and no password input anywhere on the page
    const page = screen.getByTestId('settings-page')
    expect(page.textContent || '').not.toMatch(/sk-[A-Za-z0-9]{8,}/)
    expect(page.querySelector('input[type="password"]')).toBeNull()
  })
})
