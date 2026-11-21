import { afterEach, describe, expect, it, vi } from 'vitest'
import { cleanup, render, screen, waitFor } from '@testing-library/react'
import { AgentChat } from './AgentChat'

const brief = 'A personal recipe app. ' + 'Users can add, edit and save recipes with ingredients and steps. '.repeat(6)
vi.mock('./api', () => ({
  api: {
    getAgents: vi.fn().mockResolvedValue([]),
    listModelProviders: vi.fn().mockResolvedValue([]),
    listTasks: vi.fn().mockResolvedValue([
      { id: 't2', prompt: 'hello', agent: 'opencode', status: 'failed', can_revert: false, error_message: 'Out of time' },
      {
        id: 't1',
        prompt:
          'A personal recipe app. ' + 'Users can add, edit and save recipes with ingredients and steps. '.repeat(6),
        agent: 'opencode',
        status: 'succeeded',
        can_revert: false,
        agent_message: 'Built it.',
      },
    ]),
    listApps: vi.fn().mockResolvedValue([]),
    taskEventsURL: () => '/events',
  },
}))

afterEach(cleanup)
const noop = () => {}
const chat = (assistant: boolean, status = 'running') => (
  <AgentChat assistant={assistant} sb={{ id: 'sb', status }} onError={noop} toast={noop} refresh={noop} goSettings={noop} />
)

describe('chat in plain words', () => {
  it('signs the sandboxd agent replies as sandboxd, not its coding agent', async () => {
    render(chat(true))
    await waitFor(() => expect(screen.getByText('Built it.')).toBeTruthy())
    const labels = [...document.querySelectorAll('.message-label')].map((e) => e.textContent?.trim())
    expect(labels.length).toBeGreaterThan(0)
    expect(labels.every((l) => l === 'sandboxd')).toBe(true)
  })

  it('shows no badge for success, and says when something did not finish', async () => {
    render(chat(false))
    await waitFor(() => expect(screen.getByText('Built it.')).toBeTruthy())
    expect(screen.queryByText('succeeded')).toBeNull()
    expect(screen.getByText("Didn't finish")).toBeTruthy()
  })

  it('folds a long brief to its start, with the rest one click away', async () => {
    render(chat(false))
    await waitFor(() => expect(screen.getByTestId('long-request')).toBeTruthy())
    expect(screen.getByText('Show the full request')).toBeTruthy()
    expect(brief.length).toBeGreaterThan(280)
  })

  it('does not tell people on Home that a workspace is resting', async () => {
    render(chat(true, 'stopped'))
    await waitFor(() => expect(screen.getByText('Built it.')).toBeTruthy())
    expect(screen.queryByText(/asleep|resting/)).toBeNull()
  })
})
