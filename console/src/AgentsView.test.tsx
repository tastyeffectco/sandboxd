import { afterEach, describe, expect, it, vi } from 'vitest'
import { cleanup, fireEvent, render, screen, waitFor } from '@testing-library/react'
import { AgentsView } from './AgentsView'
import { api, AgentInstance } from './api'

vi.mock('./api', () => ({
  api: {
    listAgentCatalogEntrys: vi.fn().mockResolvedValue([
      {
        id: 'hermes',
        label: 'Hermes',
        description: 'A personal assistant.',
        download: 'about 3 GB',
        memory: 'up to 2 GB',
        telegram: true,
      },
    ]),
    listModelProviders: vi.fn(),
    createAgentInstance: vi.fn(),
    saveModelProvider: vi.fn(),
    getSettings: vi.fn().mockResolvedValue({
      editable: ['agents.time_zone'],
      agents: { time_zone: 'Europe/Paris' },
    }),
    patchSettings: vi.fn().mockResolvedValue({ agents: { time_zone: 'Asia/Tokyo' } }),
  },
}))

afterEach(cleanup)

const connection = {
  id: 'c1',
  name: 'OpenRouter',
  protocol: 'openai-completions',
  base_url: 'https://openrouter.ai/api/v1',
  models: [{ id: 'm1', context_window: 1, max_tokens: 1 }],
  has_api_key: true,
}
const hermes: AgentInstance = {
  id: 'h1',
  package: 'hermes',
  name: 'Hermes',
  connection_id: 'c1',
  model: 'm1',
  status: 'running',
  telegram: false,
  apps_today: 0,
  apps_per_day: 10,
  created_at: '',
}

describe('AgentsView (catalog)', () => {
  it('asks for an AI model before Hermes can be added', async () => {
    vi.mocked(api.listModelProviders).mockResolvedValue([])
    render(<AgentsView onError={() => {}} agents={[]} goModels={() => {}} onOpen={() => {}} />)
    expect(await screen.findByText(/needs an AI model to think with/)).toBeTruthy()
    expect(screen.queryByTestId('add-agent')).toBeNull()
  })

  it('connects OpenRouter in one click from the desktop app, free models first for a new account', async () => {
    vi.mocked(api.listModelProviders).mockResolvedValue([])
    vi.mocked(api.saveModelProvider).mockResolvedValue(connection as never)
    const openRouterSignIn = vi.fn().mockResolvedValue({ key: 'sk-or-1', free: true })
    window.sandboxdDesktop = { openRouterSignIn } as never
    try {
      render(<AgentsView onError={() => {}} agents={[]} goModels={() => {}} onOpen={() => {}} />)
      fireEvent.click(await screen.findByTestId('connect-openrouter'))
      expect(await screen.findByTestId('add-agent')).toBeTruthy()
      const input = vi.mocked(api.saveModelProvider).mock.calls[0][1]
      expect(input.api_key).toBe('sk-or-1')
      expect(input.models.map((m) => m.id)).toEqual(['openrouter/free', 'deepseek/deepseek-v4-pro'])
      expect(screen.getByTestId('openrouter-connected').textContent).toMatch(/free models/)
    } finally {
      delete window.sandboxdDesktop
    }
  })

  it('shows the time zone agents use and lets the person change it', async () => {
    vi.mocked(api.listModelProviders).mockResolvedValue([connection] as never)
    render(<AgentsView onError={() => {}} agents={[]} goModels={() => {}} onOpen={() => {}} />)
    const select = (await screen.findByTestId('agent-time-zone')) as HTMLSelectElement
    expect(select.value).toBe('Europe/Paris')
    fireEvent.change(select, { target: { value: 'Asia/Tokyo' } })
    await waitFor(() =>
      expect(api.patchSettings).toHaveBeenCalledWith({
        agents: { time_zone: 'Asia/Tokyo' },
      }),
    )
    await waitFor(() => expect(select.value).toBe('Asia/Tokyo'))
  })

  it('adds Hermes and opens its own window', async () => {
    vi.mocked(api.listModelProviders).mockResolvedValue([connection] as never)
    vi.mocked(api.createAgentInstance).mockResolvedValue({ agent: hermes })
    const onOpen = vi.fn()
    render(<AgentsView onError={() => {}} agents={[]} goModels={() => {}} onOpen={onOpen} />)
    fireEvent.click(await screen.findByTestId('add-agent'))
    await waitFor(() => expect(onOpen).toHaveBeenCalledWith('h1'))
    expect(api.createAgentInstance).toHaveBeenCalledWith({
      package: 'hermes',
      connection_id: 'c1',
      telegram_bot_token: undefined,
      telegram_allowed_users: undefined,
    })
  })

  it('offers to open an agent that is already added', async () => {
    vi.mocked(api.listModelProviders).mockResolvedValue([connection] as never)
    const onOpen = vi.fn()
    render(<AgentsView onError={() => {}} agents={[hermes]} goModels={() => {}} onOpen={onOpen} />)
    fireEvent.click(await screen.findByTestId('open-agent'))
    expect(onOpen).toHaveBeenCalledWith('h1')
  })

  it('adds a second Hermes with its own name', async () => {
    vi.mocked(api.listModelProviders).mockResolvedValue([connection] as never)
    vi.mocked(api.createAgentInstance).mockResolvedValue({
      agent: { ...hermes, id: 'h2', name: 'Hermes for work' },
    })
    const onOpen = vi.fn()
    render(<AgentsView onError={() => {}} agents={[hermes]} goModels={() => {}} onOpen={onOpen} />)
    fireEvent.click(await screen.findByTestId('add-another-hermes'))
    const name = screen.getByTestId('agent-name') as HTMLInputElement
    expect(name.value).toBe('Hermes 2')
    fireEvent.change(name, { target: { value: 'Hermes for work' } })
    fireEvent.click(screen.getByTestId('add-agent'))
    await waitFor(() => expect(onOpen).toHaveBeenCalledWith('h2'))
    expect(vi.mocked(api.createAgentInstance).mock.calls.slice(-1)[0][0]).toMatchObject({
      package: 'hermes',
      name: 'Hermes for work',
    })
  })

  it('does not add a second agent with a name already in use', async () => {
    vi.mocked(api.listModelProviders).mockResolvedValue([connection] as never)
    render(<AgentsView onError={() => {}} agents={[hermes]} goModels={() => {}} onOpen={() => {}} />)
    fireEvent.click(await screen.findByTestId('add-another-hermes'))
    fireEvent.change(screen.getByTestId('agent-name'), {
      target: { value: ' hermes ' },
    })
    expect(screen.getByRole('alert').textContent).toMatch(/already have an agent called hermes/)
    expect((screen.getByTestId('add-agent') as HTMLButtonElement).disabled).toBe(true)
  })

  it('lists every agent of a kind, each opening its own window', async () => {
    vi.mocked(api.listModelProviders).mockResolvedValue([connection] as never)
    const onOpen = vi.fn()
    const second = {
      ...hermes,
      id: 'h2',
      name: 'Hermes 2',
      status: 'stopped' as const,
    }
    render(<AgentsView onError={() => {}} agents={[hermes, second]} goModels={() => {}} onOpen={onOpen} />)
    expect(await screen.findByText('2 on your crew')).toBeTruthy()
    const open = screen.getAllByTestId('open-agent')
    expect(open).toHaveLength(2)
    fireEvent.click(open[1])
    expect(onOpen).toHaveBeenCalledWith('h2')
  })
})

describe('agentHue', () => {
  it('keeps the first agent of a kind in its own colours and turns the next ones', async () => {
    const { agentHue } = await import('./design/Bot')
    const list = [
      { id: '01b', package: 'hermes' },
      { id: '01a', package: 'hermes' },
      { id: '01c', package: 'openclaw' },
    ]
    expect(agentHue(list, list[1])).toBe(0)
    expect(agentHue(list, list[0])).toBe(67)
    expect(agentHue(list, list[2])).toBe(0)
  })
})
