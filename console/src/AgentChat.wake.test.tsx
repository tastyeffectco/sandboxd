import { afterEach, describe, expect, it, vi } from 'vitest'
import { cleanup, fireEvent, render, screen, waitFor } from '@testing-library/react'
import { AgentChat } from './AgentChat'
import { api } from './api'

vi.mock('./api', () => ({
  api: {
    getAgents: vi.fn().mockResolvedValue([]),
    listModelProviders: vi.fn().mockResolvedValue([]),
    listTasks: vi.fn().mockResolvedValue([]),
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
})
