import { afterEach, describe, expect, it, vi } from 'vitest'
import { cleanup, fireEvent, render, screen, waitFor } from '@testing-library/react'
import { AssistantView } from './AssistantView'
import { api } from './api'

vi.mock('./api', () => ({ api: { ensureAssistant: vi.fn() } }))
vi.mock('./AppView', () => ({
  AppView: (p: { appId: string; assistant?: boolean }) => (
    <div data-testid="appview" data-app={p.appId} data-assistant={String(!!p.assistant)} />
  ),
}))
afterEach(() => {
  cleanup()
  vi.clearAllMocks()
})

const props = { onError: vi.fn(), toast: vi.fn(), goApps: vi.fn(), goSettings: vi.fn() }

describe('AssistantView (Ask sandboxd)', () => {
  it('makes or refreshes the sandboxd agent, then opens its chat only', async () => {
    vi.mocked(api.ensureAssistant).mockResolvedValue({ app_id: 'a9' })
    render(<AssistantView {...props} />)
    expect(screen.getByTestId('assistant-loading')).toBeTruthy()
    await waitFor(() => expect(screen.getByTestId('appview').dataset.app).toBe('a9'))
    expect(screen.getByTestId('appview').dataset.assistant).toBe('true')
    expect(api.ensureAssistant).toHaveBeenCalledTimes(1)
  })

  it('says so in plain words when it cannot start', async () => {
    vi.mocked(api.ensureAssistant).mockRejectedValue(new Error('The engine is not running.'))
    render(<AssistantView {...props} />)
    await waitFor(() => expect(screen.getByTestId('assistant-failed').textContent).toContain('The engine is not running.'))
  })

  it('offers to try again or build an app instead', async () => {
    vi.mocked(api.ensureAssistant)
      .mockRejectedValueOnce(new Error('The engine is not running.'))
      .mockResolvedValueOnce({ app_id: 'a9' })
    render(<AssistantView {...props} />)
    await waitFor(() => expect(screen.getByTestId('assistant-failed')).toBeTruthy())
    fireEvent.click(screen.getByText('Build an app instead'))
    expect(props.goApps).toHaveBeenCalledTimes(1)
    fireEvent.click(screen.getByText('Try again'))
    await waitFor(() => expect(screen.getByTestId('appview').dataset.app).toBe('a9'))
    expect(api.ensureAssistant).toHaveBeenCalledTimes(2)
  })

  it('opens the projects page when the engine has no sandboxd agent yet', async () => {
    vi.mocked(api.ensureAssistant).mockRejectedValue(Object.assign(new Error('404 Not Found'), { status: 404 }))
    render(<AssistantView {...props} />)
    await waitFor(() => expect(props.goApps).toHaveBeenCalledTimes(1))
    expect(screen.queryByTestId('assistant-failed')).toBeNull()
  })
})
