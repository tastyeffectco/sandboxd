import { afterEach, describe, expect, it, vi } from 'vitest'
import { cleanup, fireEvent, render, screen, waitFor } from '@testing-library/react'
import { TrendingStrip } from './TrendingStrip'
import { api } from './api'
import { CATALOG, TRENDING, starCount } from './catalog'

vi.mock('./api', () => ({
  api: { createApp: vi.fn(), createAppSandbox: vi.fn(), putWorkspaceFile: vi.fn() },
}))
afterEach(cleanup)

const strip = (over: Partial<Parameters<typeof TrendingStrip>[0]> = {}) => {
  const props = { apps: [], onInstalled: vi.fn(), onOpen: vi.fn(), onError: vi.fn() }
  render(<TrendingStrip {...props} {...over} />)
  return props
}

describe('TrendingStrip (home page)', () => {
  it('lists the most-starred trending entries, and links to the rest', () => {
    strip({ onSeeAll: vi.fn() })
    expect(TRENDING.length).toBeGreaterThan(6)
    for (const e of TRENDING.slice(0, 6)) expect(screen.getByTestId(`trending-${e.id}`).textContent).toContain(e.pitch)
    expect(screen.queryByTestId(`trending-${TRENDING[6].id}`)).toBeNull()
    expect(starCount(TRENDING[0].stars)).toBeGreaterThanOrEqual(starCount(TRENDING[1].stars))
    expect(screen.getByTestId('trending-all').textContent).toContain(String(TRENDING.length))
  })

  it('writes the entry files into the new project before the install task', async () => {
    vi.mocked(api.createApp).mockResolvedValue({ id: 'a1' } as never)
    vi.mocked(api.createAppSandbox).mockResolvedValue({ id: 's1' } as never)
    vi.mocked(api.putWorkspaceFile).mockResolvedValue()
    const props = strip()
    fireEvent.click(screen.getByTestId('trending-sherlock').querySelector('[data-testid=trending-try]')!)
    await waitFor(() => expect(props.onInstalled).toHaveBeenCalled())
    expect(api.createAppSandbox).toHaveBeenCalledWith('a1', { template: 'blank' })
    const [sandbox, path, content] = vi.mocked(api.putWorkspaceFile).mock.calls[0]
    expect([sandbox, path]).toEqual(['s1', 'app.py'])
    expect(content).toContain('sherlock')
    expect(props.onInstalled.mock.calls[0][1].prompt).toContain('sherlock-project==0.16.2')
  })

  it('opens an entry that is already installed', () => {
    const props = strip({ apps: [{ id: 'x', name: 'Sherlock', tags: ['catalog:sherlock'] } as never] })
    fireEvent.click(screen.getByTestId('trending-sherlock').querySelector('[data-testid=trending-open]')!)
    expect(props.onOpen).toHaveBeenCalledWith('x')
  })

  it('shows nothing when no entry is trending', () => {
    strip({ items: [] })
    expect(screen.queryByTestId('trending')).toBeNull()
  })

  it('keeps entry files small and pinned like the rest of the catalog', () => {
    for (const e of CATALOG.filter((a) => a.files)) {
      for (const [path, content] of Object.entries(e.files!)) {
        expect(path).not.toMatch(/^\/|\.\./)
        expect(content.length).toBeLessThan(64 * 1024)
      }
    }
  })
})
