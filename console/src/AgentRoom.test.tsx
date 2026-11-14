import { afterEach, describe, expect, it, vi } from 'vitest'
import { cleanup, fireEvent, render, screen, waitFor } from '@testing-library/react'
import { AgentRoom, stepLabel } from './AgentRoom'
import { api, AgentInstance, AgentMessageRow } from './api'

vi.mock('./api', () => ({
  api: {
    stopAgentInstance: vi.fn(),
    startAgentInstance: vi.fn(),
    deleteAgentInstance: vi.fn(),
    listAgentMessages: vi.fn(),
    sendAgentMessage: vi.fn(),
    stopAgentMessage: vi.fn(),
    clearAgentMessages: vi.fn(),
  },
}))

const agent: AgentInstance = {
  id: 'h1',
  package: 'hermes',
  name: 'Hermes',
  connection_id: 'c1',
  model: 'deepseek/deepseek-v4.1-flash',
  status: 'running',
  telegram: false,
  apps_today: 0,
  apps_per_day: 10,
  created_at: '',
}
const msg = (m: Partial<AgentMessageRow> & Pick<AgentMessageRow, 'id' | 'role'>): AgentMessageRow => ({
  content: '',
  steps: [],
  state: 'done',
  channel: 'app',
  created_at: '',
  ...m,
})
const room = (a = agent, reloadApps = () => {}) =>
  render(<AgentRoom agent={a} onChanged={() => {}} onRemoved={() => {}} onError={() => {}} reloadApps={reloadApps} />)

afterEach(() => {
  cleanup()
  vi.clearAllMocks()
})

describe('AgentRoom', () => {
  it('shows the whole conversation kept by the engine', async () => {
    vi.mocked(api.listAgentMessages).mockResolvedValue({
      working: false,
      messages: [
        msg({ id: '1', role: 'user', content: 'Call me Abdel' }),
        msg({ id: '2', role: 'assistant', content: 'Hi **Abdel**!' }),
      ],
    })
    room()
    expect(await screen.findByText('Abdel')).toBeTruthy()
    expect(screen.getByTestId('agent-you').textContent).toBe('Call me Abdel')
  })

  it('sends, follows the reply as the engine writes it, and refreshes the sidebar after an app is built', async () => {
    const reloadApps = vi.fn()
    const you = msg({ id: '1', role: 'user', content: 'Build me a tip calculator' })
    const working = msg({
      id: '2',
      role: 'assistant',
      state: 'working',
      thinking: 'The person wants an app.',
      steps: [
        { id: 's0', tool: 'tool_describe', status: 'done' },
        { id: 's1', tool: 'mcp__sandboxd__create_app', status: 'running' },
      ],
    })
    const done = msg({
      ...working,
      state: 'done',
      steps: [{ id: 's1', tool: 'mcp__sandboxd__create_app', status: 'done' }],
      content: 'Done! **Tip Calculator**: http://s-01abc-3000.preview.localhost:8080',
    })
    vi.mocked(api.listAgentMessages)
      .mockResolvedValueOnce({ working: false, messages: [] })
      .mockResolvedValueOnce({ working: true, messages: [you, working] })
      .mockResolvedValue({ working: false, messages: [you, done] })
    vi.mocked(api.sendAgentMessage).mockResolvedValue({ messages: [you, msg({ id: '2', role: 'assistant', state: 'working' })] })
    room(agent, reloadApps)
    await screen.findByText('Chat with Hermes')
    fireEvent.change(screen.getByTestId('agent-input'), { target: { value: 'Build me a tip calculator' } })
    fireEvent.click(screen.getByTestId('agent-send'))
    await waitFor(() => expect(api.sendAgentMessage).toHaveBeenCalledWith('h1', 'Build me a tip calculator'))
    expect(await screen.findByText('Creating your app', {}, { timeout: 3000 })).toBeTruthy()
    expect(screen.getByTestId('agent-stop')).toBeTruthy()
    expect(screen.getByTestId('agent-steps').textContent).not.toContain('describe')
    expect(await screen.findByText('Tip Calculator', {}, { timeout: 3000 })).toBeTruthy()
    const link = screen.getByTestId('agent-link') as HTMLAnchorElement
    expect(link.textContent).toContain('Open the app preview')
    expect(reloadApps).toHaveBeenCalled()
  })

  it('stops a reply, says errors plainly and offers to try again', async () => {
    vi.mocked(api.listAgentMessages).mockResolvedValue({
      working: true,
      messages: [msg({ id: '1', role: 'user', content: 'Search the news' }), msg({ id: '2', role: 'assistant', state: 'working' })],
    })
    vi.mocked(api.stopAgentMessage).mockResolvedValue(undefined)
    room()
    fireEvent.click(await screen.findByTestId('agent-stop'))
    expect(api.stopAgentMessage).toHaveBeenCalledWith('h1')
    cleanup()

    vi.mocked(api.listAgentMessages).mockResolvedValue({
      working: false,
      messages: [
        msg({ id: '1', role: 'user', content: 'Again' }),
        msg({ id: '2', role: 'assistant', state: 'error', error: "Hermes couldn't finish this reply. Try again." }),
      ],
    })
    vi.mocked(api.sendAgentMessage).mockResolvedValue({ messages: [] })
    room()
    expect((await screen.findByRole('alert')).textContent).toMatch(/couldn't finish this reply/)
    fireEvent.click(screen.getByText('Try again'))
    await waitFor(() => expect(api.sendAgentMessage).toHaveBeenCalledWith('h1', 'Again'))
  })

  it('waits while the agent is getting ready', async () => {
    vi.mocked(api.listAgentMessages).mockResolvedValue({ working: false, messages: [] })
    room({ ...agent, status: 'starting' })
    const input = screen.getByTestId('agent-input') as HTMLTextAreaElement
    expect(input.disabled).toBe(true)
    expect(input.placeholder).toMatch(/getting ready/)
    await screen.findByText('Chat with Hermes')
  })
})

describe('stepLabel', () => {
  it('says what tools do in everyday words', () => {
    expect(stepLabel({ tool: 'mcp__sandboxd__list_apps' })).toBe('Looking at your apps')
    expect(stepLabel({ tool: 'sandboxd__ask_app' })).toBe('Asking the app’s builder for a change')
    expect(stepLabel({ tool: 'terminal' })).toBe('Running a command')
    expect(stepLabel({ tool: 'tool_search' })).toBeNull()
    expect(stepLabel({ tool: 'weather_lookup' })).toBe('Weather lookup')
  })
})
