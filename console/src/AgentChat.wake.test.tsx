import { afterEach, describe, expect, it, vi } from 'vitest'
import { cleanup, fireEvent, render, screen, waitFor } from '@testing-library/react'
import { AgentChat } from './AgentChat'
import { api } from './api'

vi.mock('./api', () => ({
  api: {
    getAgents: vi.fn().mockResolvedValue([]),
    listModelProviders: vi.fn().mockResolvedValue([]),
    listTasks: vi.fn().mockResolvedValue([]),
    listApps: vi.fn().mockResolvedValue([]),
    submitTask: vi.fn().mockResolvedValue({ id: 'task' }),
    taskEventsURL: () => '/events',
  },
}))

afterEach(cleanup)

describe('AgentChat on a resting project', () => {
  it('sends the message, which wakes the project', async () => {
    globalThis.EventSource = class {
      addEventListener() {}
      close() {}
      onerror = null
    } as unknown as typeof EventSource
    const noop = () => {}
    render(
      <AgentChat
        sb={{ id: 'sb', status: 'stopped' }}
        onError={noop}
        toast={noop}
        refresh={noop}
        goSettings={noop}
      />,
    )
    expect(screen.getByText('Your workspace is resting. Send a message to wake it.')).toBeTruthy()
    fireEvent.change(screen.getByTestId('task-prompt'), { target: { value: 'Add a footer' } })
    await waitFor(() => expect((screen.getByTestId('task-send') as HTMLButtonElement).disabled).toBe(false))
    fireEvent.click(screen.getByTestId('task-send'))
    await waitFor(() =>
      expect(api.submitTask).toHaveBeenCalledWith(
        'sb',
        'Add a footer',
        'opencode',
        undefined,
        true,
        undefined,
      ),
    )
  })

  it('waits quietly while a just-started workspace gets ready, then sends', async () => {
    globalThis.EventSource = class {
      addEventListener() {}
      close() {}
      onerror = null
    } as unknown as typeof EventSource
    const notReady = Object.assign(new Error('runtimed: dial unix /run/sandboxd/runtimed.sock: no such file'), {
      status: 502,
    })
    vi.mocked(api.submitTask).mockReset()
    vi.mocked(api.submitTask).mockRejectedValueOnce(notReady).mockResolvedValueOnce({ id: 'task' } as never)
    const noop = () => {}
    render(
      <AgentChat assistant sb={{ id: 'sb', status: 'running' }} onError={noop} toast={noop} refresh={noop} goSettings={noop} />,
    )
    fireEvent.change(screen.getByTestId('task-prompt'), { target: { value: 'hello' } })
    await waitFor(() => expect((screen.getByTestId('task-send') as HTMLButtonElement).disabled).toBe(false))
    fireEvent.click(screen.getByTestId('task-send'))
    expect(await screen.findByText('sandboxd is waking up…')).toBeTruthy()
    await waitFor(() => expect(api.submitTask).toHaveBeenCalledTimes(2), { timeout: 4000 })
    expect(screen.queryByRole('alert')).toBeNull()
  })
})
