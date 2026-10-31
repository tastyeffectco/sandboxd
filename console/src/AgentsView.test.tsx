import { afterEach, describe, expect, it, vi } from 'vitest'
import { cleanup, fireEvent, render, screen, waitFor } from '@testing-library/react'
import { AgentsView } from './AgentsView'
import { api } from './api'

vi.mock('./api', () => ({
  api: {
    listAgentCatalogEntrys: vi.fn().mockResolvedValue([
      { id: 'hermes', label: 'Hermes', description: 'A personal assistant.', download: 'about 3 GB' },
    ]),
    listAgentInstances: vi.fn(),
    listModelProviders: vi.fn(),
    createAgentInstance: vi.fn(),
    chatWithAgent: vi.fn(),
    startAgentInstance: vi.fn(),
    stopAgentInstance: vi.fn(),
    deleteAgentInstance: vi.fn(),
  },
}))

afterEach(cleanup)

const view = () => render(<AgentsView onError={() => {}} reloadApps={() => {}} goModels={() => {}} />)
const connection = {
  id: 'c1',
  name: 'OpenRouter',
  protocol: 'openai-completions',
  base_url: 'https://openrouter.ai/api/v1',
  models: [{ id: 'm1', context_window: 1, max_tokens: 1 }],
  has_api_key: true,
}

describe('AgentsView', () => {
  it('asks for an AI model before Hermes can be added', async () => {
    vi.mocked(api.listAgentInstances).mockResolvedValue([])
    vi.mocked(api.listModelProviders).mockResolvedValue([])
    view()
    expect(await screen.findByText(/needs an AI model to think with/)).toBeTruthy()
    expect(screen.queryByTestId('add-agent')).toBeNull()
  })

  it('adds Hermes with the model connection and an optional Telegram bot', async () => {
    vi.mocked(api.listAgentInstances).mockResolvedValue([])
    vi.mocked(api.listModelProviders).mockResolvedValue([connection] as never)
    vi.mocked(api.createAgentInstance).mockResolvedValue({ agent: {} as never })
    view()
    fireEvent.click(await screen.findByTestId('add-agent'))
    await waitFor(() =>
      expect(api.createAgentInstance).toHaveBeenCalledWith({
        package: 'hermes',
        connection_id: 'c1',
        telegram_bot_token: undefined,
        telegram_allowed_users: undefined,
      }),
    )
  })

  it('chats with a running agent', async () => {
    vi.mocked(api.listAgentInstances).mockResolvedValue([
      {
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
      },
    ])
    vi.mocked(api.listModelProviders).mockResolvedValue([connection] as never)
    vi.mocked(api.chatWithAgent).mockResolvedValue({ reply: 'Created **Recipe Box**.' })
    view()
    fireEvent.change(await screen.findByTestId('agent-chat-input'), { target: { value: 'Build a recipe app' } })
    fireEvent.submit(screen.getByTestId('agent-chat-input').closest('form')!)
    expect(await screen.findByText('Recipe Box')).toBeTruthy()
    expect(api.chatWithAgent).toHaveBeenCalledWith('h1', [{ role: 'user', content: 'Build a recipe app' }])
  })
})
