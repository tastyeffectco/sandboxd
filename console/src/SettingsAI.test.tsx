import { afterEach, describe, expect, it, vi } from 'vitest'
import { cleanup, fireEvent, render, screen, waitFor } from '@testing-library/react'
import { SettingsAI, COMETAPI_SIGNUP } from './SettingsAI'
import { AIPicker } from './AIPicker'
import { api, type ModelProvider } from './api'

const openrouter = {
  id: 'c1',
  name: 'OpenRouter',
  protocol: 'openai-completions',
  base_url: 'https://openrouter.ai/api/v1',
  has_api_key: true,
  models: [{ id: 'deepseek/deepseek-v4-pro', context_window: 1, max_tokens: 1 }],
} as ModelProvider

vi.mock('./api', () => ({
  api: {
    getAgents: vi.fn().mockResolvedValue([]),
    listModelProviders: vi.fn(),
    getSettings: vi.fn().mockResolvedValue({ agents: { default_ai: { agent: '' } } }),
    patchSettings: vi.fn().mockResolvedValue({}),
    saveModelProvider: vi.fn(),
  },
}))

afterEach(() => {
  cleanup()
  vi.clearAllMocks()
})

describe('Settings → AI', () => {
  it('saves the AI new chats start with, in one click', async () => {
    vi.mocked(api.listModelProviders).mockResolvedValue([openrouter])
    const toast = vi.fn()
    render(<SettingsAI onError={vi.fn()} toast={toast} goSection={vi.fn()} />)
    await waitFor(() => expect(screen.getByTestId('default-ai-free').getAttribute('aria-checked')).toBe('true'))
    fireEvent.click(screen.getByTestId('default-ai-c1:deepseek/deepseek-v4-pro'))
    await waitFor(() =>
      expect(api.patchSettings).toHaveBeenCalledWith({
        agents: { default_ai: { agent: 'opencode', provider: 'c1', model: 'deepseek/deepseek-v4-pro' } },
      }),
    )
    expect(toast).toHaveBeenCalledWith('New chats now use DeepSeek V4 Pro')
  })

  it('offers CometAPI through the referral link, always saying so', async () => {
    vi.mocked(api.listModelProviders).mockResolvedValue([])
    const open = vi.spyOn(window, 'open').mockReturnValue(null)
    render(<SettingsAI onError={vi.fn()} toast={vi.fn()} goSection={vi.fn()} />)
    await screen.findByTestId('cometapi-card')
    expect(screen.getByTestId('cometapi-disclosure').textContent).toMatch(/Affiliate link: sandboxd earns a commission/)
    fireEvent.click(screen.getByTestId('cometapi-signup'))
    expect(open).toHaveBeenCalledWith(COMETAPI_SIGNUP, '_blank', 'noopener')
    expect(COMETAPI_SIGNUP).toContain('ref=abdelouahed')
  })

  it('does not save a CometAPI key that CometAPI refuses', async () => {
    vi.mocked(api.listModelProviders).mockResolvedValue([])
    globalThis.fetch = vi.fn().mockResolvedValue({ status: 401 }) as unknown as typeof fetch
    const onError = vi.fn()
    render(<SettingsAI onError={onError} toast={vi.fn()} goSection={vi.fn()} />)
    fireEvent.click(await screen.findByText('I have a key'))
    fireEvent.change(screen.getByTestId('cometapi-key'), { target: { value: 'wrong' } })
    fireEvent.click(screen.getByText('Connect'))
    await waitFor(() => expect(onError).toHaveBeenCalledWith(expect.stringMatching(/did not work/)))
    expect(api.saveModelProvider).not.toHaveBeenCalled()
  })
})

describe('the AI button in a chat', () => {
  it('shows the AI in plain words and switches it from a short list', () => {
    const onChange = vi.fn()
    const onConnect = vi.fn()
    render(
      <AIPicker
        value={{ agent: 'opencode', provider: '', model: '' }}
        onChange={onChange}
        agents={[]}
        providers={[openrouter]}
        onConnect={onConnect}
      />,
    )
    expect(screen.getByTestId('ai-chip').textContent).toContain('Free AI')
    fireEvent.click(screen.getByTestId('ai-chip'))
    fireEvent.click(screen.getByTestId('ai-option-c1:deepseek/deepseek-v4-pro'))
    expect(onChange).toHaveBeenCalledWith({ agent: 'opencode', provider: 'c1', model: 'deepseek/deepseek-v4-pro' })
    fireEvent.click(screen.getByTestId('ai-chip'))
    fireEvent.click(screen.getByTestId('ai-connect'))
    expect(onConnect).toHaveBeenCalled()
  })
})
