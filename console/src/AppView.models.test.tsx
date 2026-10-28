import { afterEach, describe, expect, it, vi } from 'vitest'
import { cleanup, fireEvent, render, screen } from '@testing-library/react'
import { AppView } from './AppView'
import { api, type Agent } from './api'

vi.mock('./api', () => ({ api: {
  getApp: vi.fn().mockResolvedValue({ id: 'app', name: 'Model picker', created_at: '' }),
  appManifest: vi.fn().mockResolvedValue({ present: false }),
  runtimeInspect: vi.fn().mockResolvedValue({ suggestions: [] }),
  getAgents: vi.fn().mockResolvedValue([]),
  listModelProviders: vi.fn().mockResolvedValue([]),
} }))

afterEach(cleanup)

const connected = (id: string): Agent => ({
  id, label: id, installed_state: 'installed', status: 'connected', method: 'api_key',
  supports_oauth: false, supports_api_key: true, runnable: id !== 'minimax',
})
const renderPicker = async () => {
  const noop = () => {}
  render(<AppView appId="app" onError={noop} toast={noop} goApps={noop} goSettings={noop} />)
  return (await screen.findByTestId('task-model')) as HTMLSelectElement
}

describe('OpenCode model picker', () => {
  it('offers only the free model when nothing is connected', async () => {
    vi.mocked(api.getAgents).mockResolvedValue([])
    const picker = await renderPicker()
    await screen.findByText('Free to use. No account needed.')
    expect(Array.from(picker.options).map((o) => [o.value, o.text])).toEqual([['', 'Free model']])
  })

  it('offers paid models once accounts are connected, Flash before M3', async () => {
    vi.mocked(api.getAgents).mockResolvedValue([connected('opencode'), connected('minimax')])
    const picker = await renderPicker()
    await screen.findByRole('option', { name: 'glm-5' })
    expect(picker.value).toBe('')
    expect(Array.from(picker.options).map((o) => o.value)).toEqual([
      '', 'opencode/glm-5', 'opencode/kimi-k2.6', 'opencode/deepseek-v4-pro',
      'opencode/MiniMax-M3.1-Flash', 'opencode/MiniMax-M3',
    ])
    fireEvent.change(picker, { target: { value: 'opencode/MiniMax-M3.1-Flash' } })
    expect(picker.value).toBe('opencode/MiniMax-M3.1-Flash')
    fireEvent.change(screen.getByTestId('task-agent'), { target: { value: 'claude-code' } })
    expect(Array.from(picker.options).map((o) => o.value)).toEqual(['', 'sonnet', 'opus', 'haiku'])
  })
})
