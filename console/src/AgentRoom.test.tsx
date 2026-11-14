import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { cleanup, fireEvent, render, screen, waitFor } from '@testing-library/react'
import { AgentRoom, stepLabel } from './AgentRoom'
import { AgentInstance, AgentStreamEvent, streamAgentChat } from './api'

vi.mock('./api', () => ({
  api: { stopAgentInstance: vi.fn(), startAgentInstance: vi.fn(), deleteAgentInstance: vi.fn() },
  streamAgentChat: vi.fn(),
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
const room = (a = agent, reloadApps = () => {}) =>
  render(<AgentRoom agent={a} onChanged={() => {}} onRemoved={() => {}} onError={() => {}} reloadApps={reloadApps} />)

beforeEach(() => localStorage.clear())
afterEach(() => {
  cleanup()
  vi.mocked(streamAgentChat).mockReset()
})

describe('AgentRoom', () => {
  it('streams a reply with its steps and thinking, and opens links', async () => {
    const reloadApps = vi.fn()
    vi.mocked(streamAgentChat).mockImplementation(async (_id, body, onEvent) => {
      expect(body.messages).toEqual([{ role: 'user', content: 'Build me a tip calculator' }])
      expect(body.conversation_id).toBeTruthy()
      const send = (e: AgentStreamEvent) => onEvent(e)
      send({ type: 'thinking', delta: 'The person wants an app.' })
      send({ type: 'step', id: 's0', tool: 'tool_describe', status: 'running' })
      send({ type: 'step', id: 's1', tool: 'mcp__sandboxd__create_app', status: 'running' })
      send({ type: 'step', id: 's1', tool: 'mcp__sandboxd__create_app', status: 'done' })
      send({ type: 'text', delta: 'Done! **Tip Calculator** is ready: ' })
      send({ type: 'text', delta: 'http://s-01abc-3000.preview.localhost:8080' })
      send({ type: 'done' })
    })
    room(agent, reloadApps)
    fireEvent.change(screen.getByTestId('agent-input'), { target: { value: 'Build me a tip calculator' } })
    fireEvent.click(screen.getByTestId('agent-send'))
    expect(await screen.findByText('Tip Calculator')).toBeTruthy()
    expect(screen.getByTestId('agent-you').textContent).toBe('Build me a tip calculator')
    const steps = screen.getByTestId('agent-steps').textContent
    expect(steps).toContain('Creating your app')
    expect(steps).not.toContain('describe') // internal tools stay hidden
    expect(screen.getByText('How it thought about this')).toBeTruthy()
    const link = screen.getByTestId('agent-link') as HTMLAnchorElement
    expect(link.href).toBe('http://s-01abc-3000.preview.localhost:8080/')
    expect(link.textContent).toContain('Open the app preview')
    expect(reloadApps).toHaveBeenCalled() // the new app shows in the sidebar
  })

  it('keeps the conversation after reopening, and sends the history', async () => {
    vi.mocked(streamAgentChat).mockImplementation(async (_id, _body, onEvent) => {
      onEvent({ type: 'text', delta: 'Hi Abdel!' })
      onEvent({ type: 'done' })
    })
    const first = room()
    fireEvent.change(screen.getByTestId('agent-input'), { target: { value: 'Call me Abdel' } })
    fireEvent.click(screen.getByTestId('agent-send'))
    await screen.findByText('Hi Abdel!')
    first.unmount()
    room()
    expect(screen.getByText('Hi Abdel!')).toBeTruthy()
    fireEvent.change(screen.getByTestId('agent-input'), { target: { value: 'What is my name?' } })
    fireEvent.click(screen.getByTestId('agent-send'))
    await waitFor(() => expect(streamAgentChat).toHaveBeenCalledTimes(2))
    expect(vi.mocked(streamAgentChat).mock.calls[1][1].messages).toEqual([
      { role: 'user', content: 'Call me Abdel' },
      { role: 'assistant', content: 'Hi Abdel!' },
      { role: 'user', content: 'What is my name?' },
    ])
  })

  it('can stop a reply, and says errors plainly with a retry', async () => {
    vi.mocked(streamAgentChat).mockImplementation(
      (_id, _body, onEvent, signal) =>
        new Promise((_, reject) => {
          onEvent({ type: 'step', id: 's1', tool: 'web_search', status: 'running' })
          signal?.addEventListener('abort', () => reject(new DOMException('aborted', 'AbortError')))
        }),
    )
    room()
    fireEvent.change(screen.getByTestId('agent-input'), { target: { value: 'Search the news' } })
    fireEvent.click(screen.getByTestId('agent-send'))
    expect(await screen.findByText('Searching the web')).toBeTruthy()
    fireEvent.click(screen.getByTestId('agent-stop'))
    expect(await screen.findByText('Stopped.')).toBeTruthy()

    vi.mocked(streamAgentChat).mockImplementation(async (_id, _body, onEvent) => {
      onEvent({ type: 'error', message: "Hermes couldn't finish this reply. Try again." })
    })
    fireEvent.change(screen.getByTestId('agent-input'), { target: { value: 'Again' } })
    fireEvent.click(screen.getByTestId('agent-send'))
    expect((await screen.findByRole('alert')).textContent).toMatch(/couldn't finish this reply/)
    expect(screen.getByText('Try again')).toBeTruthy()
  })

  it('waits while the agent is getting ready', () => {
    room({ ...agent, status: 'starting' })
    const input = screen.getByTestId('agent-input') as HTMLTextAreaElement
    expect(input.disabled).toBe(true)
    expect(input.placeholder).toMatch(/getting ready/)
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
