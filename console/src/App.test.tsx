import { describe, it, expect, beforeEach } from 'vitest'
import { render, screen } from '@testing-library/react'
import App from './App'
import { installFetch, appsFixture, presetsFixture, webSandboxFixture } from './test/fixtures'

describe('console — app list', () => {
  beforeEach(() => {
    installFetch((m, p) => {
      if (m === 'GET' && p.startsWith('/v1/apps') && !/\/v1\/apps\//.test(p)) return { apps: appsFixture }
      if (p.startsWith('/v1/presets')) return { presets: presetsFixture }
      if (/\/v1\/sandboxes\//.test(p)) return webSandboxFixture // app-card status badge
      return undefined
    })
  })

  it('loads and renders the app list', async () => {
    render(<App />)
    expect(await screen.findByTestId('app-list')).toBeTruthy()
    expect(await screen.findByText('My App')).toBeTruthy()
  })

  it('renders the create row with a preset dropdown populated from /v1/presets', async () => {
    render(<App />)
    expect(await screen.findByTestId('app-name')).toBeTruthy()
    expect(await screen.findByTestId('create-app')).toBeTruthy()
    const select = await screen.findByTestId('app-preset')
    expect(select).toBeTruthy()
    // every preset label from /v1/presets is an option
    for (const p of presetsFixture) {
      expect(screen.getByRole('option', { name: p.label })).toBeTruthy()
    }
  })
})
