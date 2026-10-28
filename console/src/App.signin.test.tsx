import { describe, it, expect, vi } from 'vitest'
import { render, screen, waitFor } from '@testing-library/react'
import App from './App'
import { installFetch, settingsFixture } from './test/fixtures'

// The engine reports signed out until the host restores a session.
function engine() {
  const state = { authenticated: false }
  installFetch((method, path) => {
    if (path === '/v1/auth/status')
      return { enabled: true, authenticated: state.authenticated, password_set: true }
    if (path === '/v1/settings') return settingsFixture
    if (path === '/v1/apps') return { apps: [] }
    if (['/v1/agents', '/v1/model-providers'].includes(path)) return { providers: [] }
    if (path === '/v1/presets') return { presets: [] }
    if (path === '/v1/git-credentials') return { credentials: [] }
    return method === 'GET' ? {} : undefined
  })
  return state
}

describe('App with a host-managed login', () => {
  it('restores the session without a password prompt or sign-out', async () => {
    const state = engine()
    const signIn = vi.fn(async () => {
      state.authenticated = true
    })
    render(<App signIn={signIn} />)
    await screen.findByText('What do you want to build?')
    expect(signIn).toHaveBeenCalledTimes(1)
    expect(screen.queryByTestId('login-password')).toBeNull()
    expect(screen.queryByTestId('nav-logout')).toBeNull()
  })

  it('falls back to the password form when the host cannot sign in', async () => {
    engine()
    const signIn = vi.fn(async () => {
      throw new Error('password changed')
    })
    render(<App signIn={signIn} />)
    await waitFor(() => expect(screen.getByTestId('login-password')).toBeTruthy())
    expect(signIn).toHaveBeenCalledTimes(1)
  })

  it('keeps the password form for every other engine', async () => {
    engine()
    render(<App />)
    await waitFor(() => expect(screen.getByTestId('login-password')).toBeTruthy())
  })
})

describe('App network-sharing hint', () => {
  const signedIn = () =>
    installFetch((method, path) => {
      if (path === '/v1/auth/status') return { enabled: true, authenticated: true, password_set: true }
      if (path === '/v1/settings') return settingsFixture
      if (path === '/v1/apps') return { apps: [] }
      if (['/v1/agents', '/v1/model-providers'].includes(path)) return { providers: [] }
      if (path === '/v1/presets') return { presets: [] }
      if (path === '/v1/git-credentials') return { credentials: [] }
      return method === 'GET' ? {} : undefined
    })

  it('explains LAN-only previews on a server engine', async () => {
    signedIn()
    render(<App />)
    expect(await screen.findByTestId('public-url-hint')).toBeTruthy()
  })

  it('stays out of the way when the engine runs on this computer', async () => {
    signedIn()
    render(<App localEngine />)
    await screen.findByText('What do you want to build?')
    await new Promise((r) => setTimeout(r, 50))
    expect(screen.queryByTestId('public-url-hint')).toBeNull()
  })
})
