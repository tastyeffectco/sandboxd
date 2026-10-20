import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { cleanup, fireEvent, render, screen, waitFor } from '@testing-library/react'
import { ModelProviders } from './ModelProviders'
import { api, ModelProvider } from './api'

vi.mock('./api', () => ({ api: {
  listModelProviders: vi.fn(),
  saveModelProvider: vi.fn(),
  deleteModelProvider: vi.fn(),
  testModelProvider: vi.fn(),
} }))

const saved: ModelProvider = {
  id: 'connection1', name: 'Local gateway', protocol: 'openai-completions', base_url: 'http://ollama:11434/v1',
  has_api_key: true, models: [{ id: 'team/model:latest', context_window: 32768, max_tokens: 4096 }],
}
const onError = vi.fn()
const toast = vi.fn()
beforeEach(() => {
  vi.resetAllMocks()
  vi.mocked(api.listModelProviders).mockResolvedValue([])
  vi.mocked(api.saveModelProvider).mockResolvedValue(saved)
  vi.mocked(api.testModelProvider).mockResolvedValue({ ok: true })
})
afterEach(cleanup)

describe('Model connections', () => {
  it('saves a compatible connection without rewriting its model alias or exposing its key', async () => {
    render(<ModelProviders onError={onError} toast={toast} />)
    fireEvent.click(await screen.findByText('Add connection'))
    fireEvent.change(screen.getByLabelText('Connection name'), { target: { value: 'Local gateway' } })
    fireEvent.change(screen.getByLabelText('API format'), { target: { value: 'anthropic-messages' } })
    fireEvent.change(screen.getByLabelText(/API base URL/), { target: { value: 'http://gateway:4000/v1' } })
    fireEvent.change(screen.getByLabelText(/API key \(optional/), { target: { value: 'secret-key' } })
    fireEvent.change(screen.getByLabelText('Model ID 1'), { target: { value: 'team/model:latest' } })
    fireEvent.submit(screen.getByText('Save connection').closest('form')!)
    await waitFor(() => expect(api.saveModelProvider).toHaveBeenCalledWith(undefined, {
      name: 'Local gateway', protocol: 'anthropic-messages', base_url: 'http://gateway:4000/v1',
      api_key: 'secret-key', models: saved.models,
    }))
    await waitFor(() => expect(screen.queryByDisplayValue('secret-key')).toBeNull())
    expect(onError).not.toHaveBeenCalled()
  })

  it('keeps an existing key when editing with a blank key field; tests only on explicit request', async () => {
    vi.mocked(api.listModelProviders).mockResolvedValue([saved])
    render(<ModelProviders onError={onError} toast={toast} />)
    await screen.findByText('Local gateway')
    expect(api.testModelProvider).not.toHaveBeenCalled()
    fireEvent.click(screen.getByText('Test model'))
    await screen.findByText('Model responded successfully.')
    expect(api.testModelProvider).toHaveBeenCalledWith(saved.id, saved.models[0].id)
    fireEvent.click(screen.getByText('Edit'))
    expect((screen.getByLabelText(/API key \(optional/) as HTMLInputElement).value).toBe('')
    fireEvent.change(screen.getByLabelText('Connection name'), { target: { value: 'Renamed' } })
    fireEvent.submit(screen.getByText('Save connection').closest('form')!)
    await waitFor(() => expect(api.saveModelProvider).toHaveBeenCalled())
    const [, input] = vi.mocked(api.saveModelProvider).mock.calls[0]
    expect(input.name).toBe('Renamed')
    expect(input).not.toHaveProperty('api_key')
  })

  it('removes a saved credential only when the operator explicitly selects removal', async () => {
    vi.mocked(api.listModelProviders).mockResolvedValue([saved])
    render(<ModelProviders onError={onError} toast={toast} />)
    fireEvent.click(await screen.findByText('Edit'))
    fireEvent.click(screen.getByLabelText('Remove the saved API key'))
    fireEvent.submit(screen.getByText('Save connection').closest('form')!)
    await waitFor(() => expect(api.saveModelProvider).toHaveBeenCalled())
    expect(vi.mocked(api.saveModelProvider).mock.calls[0][1].api_key).toBe('')
  })
})
