import { afterEach, describe, expect, it, vi } from 'vitest'
import { cleanup, render, screen, waitFor } from '@testing-library/react'
import { AgentChat } from './AgentChat'

vi.mock('./api', () => ({
  api: {
    getAgents: vi.fn().mockResolvedValue([]),
    listModelProviders: vi.fn().mockResolvedValue([]),
    listTasks: vi.fn().mockResolvedValue([{ id: 't1', prompt: 'hello', status: 'succeeded', can_revert: true, result: 'Hi!' }]),
    listApps: vi.fn().mockResolvedValue([]),
    taskEventsURL: () => '/events',
  },
}))

afterEach(cleanup)

const noop = () => {}
const chat = (assistant: boolean) => (
  <AgentChat
    assistant={assistant}
    sb={{ id: 'sb', status: 'running' }}
    onError={noop}
    toast={noop}
    refresh={noop}
    goSettings={noop}
  />
)

describe('Restore after a task', () => {
  it('is offered in a project chat, to undo file changes', async () => {
    render(chat(false))
    await waitFor(() => expect(screen.getByText('Restore')).toBeTruthy())
  })

  it('is not offered in the sandboxd agent chat, which is not an app', async () => {
    render(chat(true))
    await waitFor(() => expect(screen.getByText('hello')).toBeTruthy())
    expect(screen.queryByText('Restore')).toBeNull()
  })
})
