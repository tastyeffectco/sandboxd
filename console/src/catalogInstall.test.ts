import { describe, expect, it, vi } from 'vitest'
import { installCatalogApp } from './catalogInstall'
import { api } from './api'
import { CATALOG, catalogManifest } from './catalog'

vi.mock('./api', () => ({
  api: {
    createApp: vi.fn(),
    createAppSandbox: vi.fn(),
    putWorkspaceFile: vi.fn(),
    getSandbox: vi.fn(),
    stopSandbox: vi.fn(),
    startSandbox: vi.fn(),
  },
}))

describe('installCatalogApp', () => {
  it('waits for a source app copy, writes sandbox.yaml, then restarts the workspace', async () => {
    const excalidraw = CATALOG.find((a) => a.id === 'excalidraw')!
    const order: string[] = []
    vi.mocked(api.createApp).mockResolvedValue({ id: 'a1' } as never)
    vi.mocked(api.createAppSandbox).mockResolvedValue({ id: 's1', status: 'creating' } as never)
    vi.mocked(api.getSandbox)
      .mockResolvedValueOnce({ id: 's1', status: 'creating' } as never)
      .mockResolvedValueOnce({ id: 's1', status: 'running' } as never)
    vi.mocked(api.putWorkspaceFile).mockImplementation(async (_s, path) => void order.push('put ' + path))
    vi.mocked(api.stopSandbox).mockImplementation(async () => (order.push('stop'), {}) as never)
    vi.mocked(api.startSandbox).mockImplementation(async () => (order.push('start'), {}) as never)
    const id = await installCatalogApp(excalidraw, { wait: async () => {} })
    expect(id).toBe('a1')
    expect(vi.mocked(api.createApp).mock.calls[0][0].git).toEqual(excalidraw.git)
    expect(api.createAppSandbox).toHaveBeenCalledWith('a1', {})
    expect(api.getSandbox).toHaveBeenCalledTimes(2)
    expect(order).toEqual(['put sandbox.yaml', 'stop', 'start'])
    expect(vi.mocked(api.putWorkspaceFile).mock.calls[0][2]).toBe(catalogManifest(excalidraw))
  })

  it('says what to do when the workspace never starts', async () => {
    vi.mocked(api.createApp).mockResolvedValue({ id: 'a2' } as never)
    vi.mocked(api.createAppSandbox).mockResolvedValue({ id: 's2', status: 'creating' } as never)
    vi.mocked(api.getSandbox).mockResolvedValue({ id: 's2', status: 'creating' } as never)
    await expect(installCatalogApp(CATALOG[0], { wait: async () => {}, readyWithin: -1 })).rejects.toThrow(
      /did not start\. Open it and try again/,
    )
  })
})
