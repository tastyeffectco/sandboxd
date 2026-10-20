import { afterEach, describe, expect, it, vi } from 'vitest'
import { cleanup, fireEvent, render, screen } from '@testing-library/react'
import { AppView } from './AppView'

vi.mock('./api', () => ({ api: {
  getApp: vi.fn().mockResolvedValue({ id: 'app', name: 'Model picker', created_at: '' }),
  appManifest: vi.fn().mockResolvedValue({ present: false }),
  runtimeInspect: vi.fn().mockResolvedValue({ suggestions: [] }),
  getAgents: vi.fn().mockResolvedValue([]),
  listModelProviders: vi.fn().mockResolvedValue([]),
} }))

afterEach(cleanup)

describe('OpenCode model picker', () => {
  it('offers Flash before M3, preserves other choices, and leaves the saved default in control', async () => {
    const noop = () => {}
    render(<AppView appId="app" onError={noop} toast={noop} goApps={noop} goSettings={noop} />)
    const picker = await screen.findByTestId('task-model') as HTMLSelectElement
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
