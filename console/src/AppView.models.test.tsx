import { afterEach, describe, expect, it, vi } from 'vitest'
import { cleanup, fireEvent, render, screen, waitFor } from '@testing-library/react'
import { AppView } from './AppView'
import { api } from './api'

vi.mock('./api', () => ({
  api: {
    getApp: vi.fn().mockResolvedValue({ id: 'app', name: 'Model picker', created_at: '' }),
    appManifest: vi.fn().mockResolvedValue({ present: false }),
    runtimeInspect: vi.fn().mockResolvedValue({ suggestions: [] }),
    getAgents: vi.fn().mockResolvedValue([]),
    listModelProviders: vi.fn().mockResolvedValue([]),
    getAgentModels: vi.fn(),
  },
}))

afterEach(cleanup)

describe('account model picker', () => {
  it('shows exactly the provider catalog and reloads it when switching agents', async () => {
    vi.mocked(api.getAgentModels).mockImplementation(async (agent) => ({
      source: 'provider',
      models: [{ id: `${agent}-account-model`, name: `${agent} account model` }],
    }))
    const noop = () => {}
    render(<AppView appId="app" onError={noop} toast={noop} goApps={noop} goSettings={noop} />)
    const picker = (await screen.findByTestId('task-model')) as HTMLSelectElement
    await waitFor(() =>
      expect(Array.from(picker.options).map((o) => o.value)).toEqual(['', 'opencode-account-model']),
    )
    fireEvent.change(picker, { target: { value: 'opencode-account-model' } })
    expect(picker.value).toBe('opencode-account-model')
    fireEvent.change(screen.getByTestId('task-agent'), { target: { value: 'codex' } })
    await waitFor(() =>
      expect(Array.from(picker.options).map((o) => o.value)).toEqual(['', 'codex-account-model']),
    )
    expect(picker.value).toBe('')
    fireEvent.change(screen.getByTestId('task-agent'), { target: { value: 'claude-code' } })
    await waitFor(() =>
      expect(Array.from(picker.options).map((o) => o.value)).toEqual(['', 'claude-code-account-model']),
    )
  })
  it('shows discovery failures without inventing fallback model names', async () => {
    vi.mocked(api.getAgentModels).mockRejectedValue(new Error('Reconnect this account'))
    const noop = () => {}
    render(<AppView appId="app" onError={noop} toast={noop} goApps={noop} goSettings={noop} />)
    await screen.findByText(/Reconnect this account/)
    expect(
      Array.from((screen.getByTestId('task-model') as HTMLSelectElement).options).map((o) => o.value),
    ).toEqual([''])
  })
})
