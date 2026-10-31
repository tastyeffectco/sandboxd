import { afterEach, describe, expect, it, vi } from 'vitest'
import { cleanup, fireEvent, render, screen, waitFor } from '@testing-library/react'
import { HelpersView } from './HelpersView'
import { api } from './api'

vi.mock('./api', () => ({
  api: {
    listHelperPackages: vi.fn().mockResolvedValue([
      { id: 'hermes', label: 'Hermes', description: 'A personal assistant.', download: 'about 3 GB' },
    ]),
    listHelpers: vi.fn(),
    listModelProviders: vi.fn(),
    createHelper: vi.fn(),
    chatWithHelper: vi.fn(),
    startHelper: vi.fn(),
    stopHelper: vi.fn(),
    deleteHelper: vi.fn(),
  },
}))

afterEach(cleanup)

const view = () => render(<HelpersView onError={() => {}} reloadApps={() => {}} goModels={() => {}} />)
const connection = {
  id: 'c1',
  name: 'OpenRouter',
  protocol: 'openai-completions',
  base_url: 'https://openrouter.ai/api/v1',
  models: [{ id: 'm1', context_window: 1, max_tokens: 1 }],
  has_api_key: true,
}

describe('HelpersView', () => {
  it('asks for an AI model before Hermes can be added', async () => {
    vi.mocked(api.listHelpers).mockResolvedValue([])
    vi.mocked(api.listModelProviders).mockResolvedValue([])
    view()
    expect(await screen.findByText(/needs an AI model to think with/)).toBeTruthy()
    expect(screen.queryByTestId('add-helper')).toBeNull()
  })

  it('adds Hermes with the model connection and an optional Telegram bot', async () => {
    vi.mocked(api.listHelpers).mockResolvedValue([])
    vi.mocked(api.listModelProviders).mockResolvedValue([connection] as never)
    vi.mocked(api.createHelper).mockResolvedValue({ helper: {} as never })
    view()
    fireEvent.click(await screen.findByTestId('add-helper'))
    await waitFor(() =>
      expect(api.createHelper).toHaveBeenCalledWith({
        package: 'hermes',
        connection_id: 'c1',
        telegram_bot_token: undefined,
        telegram_allowed_users: undefined,
      }),
    )
  })

  it('chats with a running helper', async () => {
    vi.mocked(api.listHelpers).mockResolvedValue([
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
    vi.mocked(api.chatWithHelper).mockResolvedValue({ reply: 'Created **Recipe Box**.' })
    view()
    fireEvent.change(await screen.findByTestId('helper-message'), { target: { value: 'Build a recipe app' } })
    fireEvent.submit(screen.getByTestId('helper-message').closest('form')!)
    expect(await screen.findByText('Recipe Box')).toBeTruthy()
    expect(api.chatWithHelper).toHaveBeenCalledWith('h1', [{ role: 'user', content: 'Build a recipe app' }])
  })
})
