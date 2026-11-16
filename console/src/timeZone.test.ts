import { describe, expect, it, vi } from 'vitest'
import { api } from './api'
import { syncAgentTimeZone } from './timeZone'

vi.mock('./api', () => ({ api: { getSettings: vi.fn(), patchSettings: vi.fn() } }))

describe('syncAgentTimeZone', () => {
  it('fills an empty time zone from the browser', async () => {
    vi.mocked(api.getSettings).mockResolvedValue({ editable: ['agents.time_zone'], agents: { time_zone: '' } } as never)
    vi.mocked(api.patchSettings).mockResolvedValue({ agents: { time_zone: 'Europe/Paris' } } as never)
    expect(await syncAgentTimeZone('Europe/Paris')).toBe('Europe/Paris')
    expect(api.patchSettings).toHaveBeenCalledWith({ agents: { time_zone: 'Europe/Paris' } })
  })
  it('never overrides a zone the person chose', async () => {
    vi.mocked(api.patchSettings).mockClear()
    vi.mocked(api.getSettings).mockResolvedValue({ editable: ['agents.time_zone'], agents: { time_zone: 'Asia/Tokyo' } } as never)
    expect(await syncAgentTimeZone('Europe/Paris')).toBe('Asia/Tokyo')
    expect(api.patchSettings).not.toHaveBeenCalled()
  })
  it('leaves engines that cannot store it alone', async () => {
    vi.mocked(api.patchSettings).mockClear()
    vi.mocked(api.getSettings).mockResolvedValue({ editable: [], agents: {} } as never)
    await syncAgentTimeZone('Europe/Paris')
    expect(api.patchSettings).not.toHaveBeenCalled()
  })
})
