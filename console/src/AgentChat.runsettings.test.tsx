import { afterEach, describe, expect, it, vi } from 'vitest'
import { cleanup, render, screen } from '@testing-library/react'
import { AgentChat } from './AgentChat'
import { api } from './api'

vi.mock('./api', () => ({
  api: {
    getAgents: vi.fn().mockResolvedValue([]),
    listModelProviders: vi.fn().mockResolvedValue([]),
    listTasks: vi.fn(),
    taskEventsURL: () => '/events',
  },
}))

afterEach(cleanup)

const chat = () => {
  const noop = () => {}
  render(
    <AgentChat
      sb={{ id: 'sb', status: 'running' }}
      onError={noop}
      toast={noop}
      refresh={noop}
      goSettings={noop}
    />,
  )
}

describe('AgentChat run settings', () => {
  it('tells the user the app now starts differently', async () => {
    vi.mocked(api.listTasks).mockResolvedValue([
      {
        id: 't1',
        prompt: 'Make it an Astro blog',
        status: 'succeeded',
        agent_message: 'Done.',
        can_revert: true,
        run_settings: {
          status: 'applied',
          preview_ready: true,
          message: 'Your app now starts with new settings, and the preview is running.',
        },
      },
    ])
    chat()
    const note = await screen.findByTestId('run-settings')
    expect(note.textContent).toContain('preview is running')
    expect(note.className).not.toContain('warn')
  })

  it('warns when a change was undone', async () => {
    vi.mocked(api.listTasks).mockResolvedValue([
      {
        id: 't2',
        prompt: 'Use port 5173',
        status: 'succeeded',
        can_revert: true,
        run_settings: {
          status: 'kept_previous',
          message:
            'The agent tried to change how your app starts, but they use port 5173. The preview only works on port 3000. Your app keeps running as before.',
        },
      },
    ])
    chat()
    const note = await screen.findByTestId('run-settings')
    expect(note.className).toContain('warn')
    expect(note.textContent).toContain('keeps running as before')
  })
})
