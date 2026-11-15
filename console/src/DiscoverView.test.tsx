import { afterEach, describe, expect, it, vi } from 'vitest'
import { cleanup, fireEvent, render, screen, waitFor } from '@testing-library/react'
import { DiscoverView } from './DiscoverView'
import { api } from './api'
import { CATALOG, catalogManifest, catalogTag, installPrompt } from './catalog'

vi.mock('./api', () => ({ api: { createApp: vi.fn(), createAppSandbox: vi.fn() } }))
afterEach(cleanup)

const view = (over: Partial<Parameters<typeof DiscoverView>[0]> = {}) => {
  const props = { apps: [], onInstalled: vi.fn(), onOpen: vi.fn(), goAgents: vi.fn(), onError: vi.fn() }
  render(<DiscoverView {...props} {...over} />)
  return props
}

describe('DiscoverView (free apps)', () => {
  it('installs a downloaded app into an empty project and hands its install to the coding agent', async () => {
    vi.mocked(api.createApp).mockResolvedValue({ id: 'a1' } as never)
    vi.mocked(api.createAppSandbox).mockResolvedValue({} as never)
    const props = view()
    fireEvent.click(screen.getByTestId('catalog-memos').querySelector('[data-testid=catalog-install]')!)
    await waitFor(() => expect(props.onInstalled).toHaveBeenCalled())
    expect(vi.mocked(api.createApp).mock.calls[0][0]).toMatchObject({ name: 'Memos', tags: ['catalog:memos'] })
    expect(vi.mocked(api.createApp).mock.calls[0][0].git).toBeUndefined()
    expect(api.createAppSandbox).toHaveBeenCalledWith('a1', { template: 'blank' })
    const [id, intent] = props.onInstalled.mock.calls[0]
    expect(id).toBe('a1')
    expect(intent.agent).toBe('opencode')
    expect(intent.prompt).toContain('sandbox.yaml')
  })

  it('copies a source app from its repository at the tested version', async () => {
    vi.mocked(api.createApp).mockClear()
    vi.mocked(api.createApp).mockResolvedValue({ id: 'a2' } as never)
    view()
    fireEvent.click(screen.getByTestId('catalog-uptime-kuma').querySelector('[data-testid=catalog-install]')!)
    await waitFor(() => expect(api.createAppSandbox).toHaveBeenCalledWith('a2', {}))
    expect(vi.mocked(api.createApp).mock.calls[0][0].git).toEqual({
      repo_url: 'https://github.com/louislam/uptime-kuma',
      branch: '2.5.11',
    })
  })

  it('opens an app that is already installed', () => {
    const props = view({ apps: [{ id: 'x', name: 'Memos', tags: [catalogTag('memos')] } as never] })
    fireEvent.click(screen.getByTestId('catalog-memos').querySelector('[data-testid=catalog-open]')!)
    expect(props.onOpen).toHaveBeenCalledWith('x')
  })

  it('filters by category and links to agents', () => {
    const props = view()
    fireEvent.click(screen.getByRole('tab', { name: 'Finance' }))
    expect(screen.getAllByRole('article').length).toBe(CATALOG.filter((a) => a.category === 'Finance').length)
    fireEvent.click(screen.getByTestId('discover-agents'))
    expect(props.goAgents).toHaveBeenCalled()
  })
})

describe('catalog', () => {
  it('has unique ids and pinned, port-3000 start commands', () => {
    expect(new Set(CATALOG.map((a) => a.id)).size).toBe(CATALOG.length)
    for (const a of CATALOG) {
      expect(a.start).toContain('3000')
      expect(a.start).not.toMatch(/\/latest\//) // pinned versions only
      if (a.git) expect(a.git.branch).toBeTruthy()
    }
  })

  it('writes a sandbox.yaml whose command is exactly the start command', () => {
    for (const a of CATALOG) {
      const yaml = catalogManifest(a)
      const line = yaml.split('\n').find((l) => l.startsWith('  command: '))!
      // JSON strings are valid YAML double-quoted scalars: decode the same way.
      expect(JSON.parse(line.slice('  command: '.length))).toBe(a.start)
      expect(yaml).toContain('port: 3000')
      expect(installPrompt(a)).toContain(yaml)
    }
  })
})
