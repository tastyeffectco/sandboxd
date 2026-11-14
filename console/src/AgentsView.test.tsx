import { afterEach, describe, expect, it, vi } from 'vitest'
import { cleanup, fireEvent, render, screen, waitFor } from '@testing-library/react'
import { AgentsView } from './AgentsView'
import { api, AgentInstance } from './api'

vi.mock('./api', () => ({
  api: {
    listAgentCatalogEntrys: vi.fn().mockResolvedValue([
      { id: 'hermes', label: 'Hermes', description: 'A personal assistant.', download: 'about 3 GB', memory: 'up to 2 GB', telegram: true },
    ]),
    listModelProviders: vi.fn(),
    createAgentInstance: vi.fn(),
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
})
