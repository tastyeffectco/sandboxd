import { describe, it, expect, beforeEach, vi } from 'vitest'
import { render, screen, fireEvent, waitFor } from '@testing-library/react'
import App from './App'
import {
  installFetch,
  appsFixture,
  presetsFixture,
  webSandboxFixture,
  gitCredentialsFixture,
} from './test/fixtures'

describe('console — app list', () => {
  beforeEach(() => {
    installFetch((m, p) => {
      if (m === 'GET' && p.startsWith('/v1/git-credentials')) return { credentials: gitCredentialsFixture }
      if (m === 'GET' && p.startsWith('/v1/apps') && !/\/v1\/apps\//.test(p)) return { apps: appsFixture }
      if (p.startsWith('/v1/presets')) return { presets: presetsFixture }
      if (/\/v1\/sandboxes\//.test(p)) return webSandboxFixture // app-card status badge
      return undefined
    })
  })

  it('imports from a Git URL: mode toggle + credential dropdown + posts git{}', async () => {
    let posted: { name: string; git?: { repo_url: string; credential_id: string } } | null = null
    installFetch((m, p) => {
      if (m === 'GET' && p.startsWith('/v1/git-credentials')) return { credentials: gitCredentialsFixture }
      if (m === 'GET' && p.startsWith('/v1/apps') && !/\/v1\/apps\//.test(p)) return { apps: appsFixture }
      if (p.startsWith('/v1/presets')) return { presets: presetsFixture }
      if (/\/v1\/sandboxes\//.test(p)) return webSandboxFixture
      if (m === 'POST' && p === '/v1/apps') return { ...appsFixture[0], id: 'newapp' }
      return undefined
    })
    const realFetch = globalThis.fetch
    globalThis.fetch = vi.fn(async (input: unknown, init?: { method?: string; body?: string }) => {
      if ((init?.method || 'GET').toUpperCase() === 'POST' && String(input).endsWith('/v1/apps')) {
        posted = init?.body ? JSON.parse(init.body) : null
      }
      return realFetch(input as never, init as never)
    }) as unknown as typeof fetch

    render(<App />)
    fireEvent.click(await screen.findByTestId('mode-git'))
    fireEvent.change(screen.getByTestId('app-name'), { target: { value: 'imp' } })
    fireEvent.change(screen.getByTestId('git-repo-url'), {
      target: { value: 'https://github.com/org/repo.git' },
    })
    fireEvent.change(screen.getByTestId('git-credential'), { target: { value: gitCredentialsFixture[0].id } })
    fireEvent.click(screen.getByTestId('create-app'))

    await waitFor(() => expect(posted).not.toBeNull())
    expect(posted!.git?.repo_url).toBe('https://github.com/org/repo.git')
    expect(posted!.git?.credential_id).toBe(gitCredentialsFixture[0].id)
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

  it('create form shows one path at a time: template picker vs git import', async () => {
    render(<App />)
    // default (template) mode: preset picker + App Store hint; no git fields
    expect(await screen.findByTestId('app-preset')).toBeTruthy()
    expect(screen.getByTestId('blank-hint').textContent).toMatch(/app store/i)
    expect(screen.queryByTestId('git-import-fields')).toBeNull()
    // switch to git: preset picker hidden, git fields + auto-detect note shown
    fireEvent.click(screen.getByTestId('mode-git'))
    expect(screen.queryByTestId('app-preset')).toBeNull()
    expect(screen.getByTestId('git-import-fields')).toBeTruthy()
    expect(screen.getByTestId('git-autodetect-note').textContent).toMatch(/auto-detected/i)
  })
})
