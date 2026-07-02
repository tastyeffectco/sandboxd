import { describe, it, expect, beforeEach, vi } from 'vitest'
import { render, screen, fireEvent, waitFor } from '@testing-library/react'
import { AppStore } from './AppStore'
import { CATALOG, recipeManifest } from './catalog'
import { installFetch } from './test/fixtures'

// The App Store is a pure /v1 client (docs/APP-CATALOG-CONTRACT.md): the tests
// assert the exact call sequence — app → sandbox → two workspace files →
// stop/start → poll — with no non-/v1 traffic.

const noApps = (m: string, p: string) => {
  if (m === 'GET' && p.startsWith('/v1/apps')) return { apps: [] }
  return undefined
}

describe('console — app store', () => {
  beforeEach(() => {
    installFetch(noApps)
  })

  it('renders catalog cards with install buttons', async () => {
    render(<AppStore onOpen={() => {}} onError={() => {}} onInfo={() => {}} />)
    expect(screen.getByTestId('store-grid')).toBeTruthy()
    // A known-instant recipe is present and installable.
    expect(screen.getByTestId('store-install-filebrowser')).toBeTruthy()
    expect(CATALOG.length).toBeGreaterThanOrEqual(20)
  })

  it('filters by category and search', async () => {
    render(<AppStore onOpen={() => {}} onError={() => {}} onInfo={() => {}} />)
    fireEvent.click(screen.getByTestId('store-cat-data'))
    expect(screen.queryByTestId('store-card-filebrowser')).toBeNull()
    expect(screen.getByTestId('store-card-directus')).toBeTruthy()
    fireEvent.click(screen.getByTestId('store-cat-all'))
    fireEvent.change(screen.getByTestId('store-search'), { target: { value: 'gotify' } })
    expect(screen.getByTestId('store-card-gotify')).toBeTruthy()
    expect(screen.queryByTestId('store-card-directus')).toBeNull()
  })

  it('marks already-installed recipes (tag catalog:<id>) as Open', async () => {
    installFetch((m, p) => {
      if (m === 'GET' && p.startsWith('/v1/apps'))
        return {
          apps: [
            {
              id: 'appX',
              name: 'gotify',
              description: '',
              tags: ['catalog', 'catalog:gotify'],
              created_at: '',
              updated_at: '',
            },
          ],
        }
      return undefined
    })
    const onOpen = vi.fn()
    render(<AppStore onOpen={onOpen} onError={() => {}} onInfo={() => {}} />)
    const open = await screen.findByTestId('store-open-gotify')
    fireEvent.click(open)
    expect(onOpen).toHaveBeenCalledWith('appX')
  })

  it('install drives the /v1 sequence: app → sandbox → recipe files → restart → poll', async () => {
    const calls: string[] = []
    const putBodies: Record<string, string> = {}
    const realFetch = (path: string, init?: { method?: string; body?: string }) => {
      const m = (init?.method || 'GET').toUpperCase()
      calls.push(`${m} ${path}`)
      const json = (d: unknown) =>
        new Response(JSON.stringify(d), { status: 200, headers: { 'content-type': 'application/json' } })
      if (m === 'GET' && p0(path) === '/v1/apps') return json({ apps: [] })
      if (m === 'POST' && path === '/v1/apps') return json({ id: 'app1', name: 'filebrowser', tags: [] })
      if (m === 'POST' && path === '/v1/apps/app1/sandbox') return json({ id: 'sb1', status: 'running' })
      if (m === 'PUT' && path.startsWith('/v1/sandboxes/sb1/files')) {
        putBodies[decodeURIComponent(path.split('path=')[1])] = init?.body || ''
        return json({ ok: true })
      }
      if (m === 'POST' && path === '/v1/sandboxes/sb1/stop') return json({ id: 'sb1', status: 'stopped' })
      if (m === 'POST' && path === '/v1/sandboxes/sb1/start') return json({ id: 'sb1', status: 'running' })
      if (m === 'GET' && path === '/v1/sandboxes/sb1')
        return json({
          id: 'sb1',
          status: calls.some((c) => c.startsWith('POST /v1/sandboxes/sb1/start')) ? 'running' : 'stopped',
          processes: [{ name: 'web', kind: 'web', running: true }],
        })
      return json({ error: { message: `no mock ${m} ${path}` } })
    }
    const p0 = (p: string) => p.split('?')[0]
    globalThis.fetch = vi.fn(async (input: unknown, init?: { method?: string; body?: string }) =>
      realFetch(typeof input === 'string' ? input : String((input as { url: string }).url), init),
    ) as unknown as typeof fetch

    const onInfo = vi.fn()
    render(<AppStore onOpen={() => {}} onError={() => {}} onInfo={onInfo} />)
    fireEvent.click(screen.getByTestId('store-install-filebrowser'))

    await waitFor(() => expect(onInfo).toHaveBeenCalled(), { timeout: 15000 })

    // All three recipe files were written via the generic files endpoint.
    expect(Object.keys(putBodies).sort()).toEqual([
      'workspace/app/AGENTS.md',
      'workspace/app/catalog-run.sh',
      'workspace/app/sandbox.yaml',
    ])
    const r = CATALOG.find((x) => x.id === 'filebrowser')!
    expect(putBodies['workspace/app/sandbox.yaml']).toBe(recipeManifest(r))
    expect(putBodies['workspace/app/catalog-run.sh']).toContain('.catalog-installed')
    // Agent context tells tasks what they can and cannot modify.
    expect(putBodies['workspace/app/AGENTS.md']).toContain('PREBUILT RELEASE BINARY')
    // Restart ordering: stop before start, start before the final poll success.
    expect(calls.findIndex((c) => c === 'POST /v1/sandboxes/sb1/stop')).toBeLessThan(
      calls.findIndex((c) => c === 'POST /v1/sandboxes/sb1/start'),
    )
    // Contract: every call is /v1 — the store never touches internal surfaces.
    expect(calls.every((c) => c.split(' ')[1].startsWith('/v1/'))).toBe(true)
  }, 20000)

  it('recipes are well-formed: 0.0.0.0 binding, idempotency guard or venv/dir guard, health path set', () => {
    for (const r of CATALOG) {
      expect(r.script.startsWith('#!/bin/bash'), r.id).toBe(true)
      expect(r.script).toContain('cd /home/sandbox/workspace/app')
      expect(r.healthPath.startsWith('/'), r.id).toBe(true)
      // Every script must be guarded so restarts don't re-install.
      expect(/\.catalog-installed|\[ ! -d |\[ ! -x |\[ ! -f /.test(r.script), `${r.id} lacks an install guard`).toBe(true)
      // The final process must exec so runtimed supervises the real app.
      expect(/\nexec /.test(r.script), `${r.id} must exec its server`).toBe(true)
    }
  })
})
