import { describe, it, expect, vi, beforeEach } from 'vitest'
import { render, fireEvent, screen, waitFor } from '@testing-library/react'
import type { ConfigItem } from './api'

const api = vi.hoisted(() => ({
  listConfig: vi.fn(),
  createConfig: vi.fn(),
  patchConfig: vi.fn(),
  deleteConfig: vi.fn(),
}))
vi.mock('./api', () => ({ api }))

import { SecretsPanel } from './SecretsPanel'

const secret: ConfigItem = {
  key: 'STRIPE_KEY',
  sensitive: true,
  access_policy: 'agent_access',
  value_set: true,
  created_at: '2026-11-01T00:00:00Z',
  updated_at: '2026-11-01T00:00:00Z',
}
const variable: ConfigItem = { ...secret, key: 'SITE_NAME', sensitive: false, value: 'My shop' }

const open = () => render(<SecretsPanel appId="a1" onError={() => {}} toast={() => {}} />)

describe('SecretsPanel', () => {
  beforeEach(() => {
    vi.clearAllMocks()
    // jsdom has no modal dialogs; open them as plain dialogs.
    HTMLDialogElement.prototype.showModal ??= function (this: HTMLDialogElement) {
      this.open = true
    }
    HTMLDialogElement.prototype.close ??= function (this: HTMLDialogElement) {
      this.open = false
    }
    api.createConfig.mockResolvedValue({})
    api.patchConfig.mockResolvedValue({})
    api.deleteConfig.mockResolvedValue({})
  })

  it('never shows a secret value, but shows a variable', async () => {
    api.listConfig.mockResolvedValue([secret, variable])
    open()
    expect(await screen.findByText('STRIPE_KEY')).toBeTruthy()
    expect(screen.getByText('•••••••• · Secret')).toBeTruthy()
    expect(screen.getByText('My shop')).toBeTruthy()
  })

  it('adds a new value with a trimmed name', async () => {
    api.listConfig.mockResolvedValue([])
    open()
    fireEvent.click(await screen.findByText('Add your first value'))
    fireEvent.change(screen.getByPlaceholderText('MY_API_KEY'), { target: { value: '  OPENAI_KEY ' } })
    fireEvent.change(screen.getByPlaceholderText('Enter the new secret'), { target: { value: 'sk-1' } })
    fireEvent.click(screen.getByText('Save value'))
    await waitFor(() =>
      expect(api.createConfig).toHaveBeenCalledWith('a1', {
        key: 'OPENAI_KEY',
        value: 'sk-1',
        sensitive: true,
        access_policy: 'control_plane_only',
      }),
    )
  })

  it('keeps the saved value when editing only the access policy', async () => {
    api.listConfig.mockResolvedValue([secret])
    open()
    await screen.findByText('STRIPE_KEY')
    fireEvent.click(screen.getByText('Edit'))
    expect(screen.queryByPlaceholderText('Enter the new secret')).toBeNull()
    fireEvent.click(screen.getByText('Save value'))
    await waitFor(() =>
      expect(api.patchConfig).toHaveBeenCalledWith('a1', 'STRIPE_KEY', { access_policy: 'agent_access' }),
    )
  })

  it('removes a value only after confirming', async () => {
    api.listConfig.mockResolvedValue([secret])
    open()
    fireEvent.click(await screen.findByLabelText('Remove STRIPE_KEY'))
    expect(api.deleteConfig).not.toHaveBeenCalled()
    fireEvent.click(screen.getByText('Remove value'))
    await waitFor(() => expect(api.deleteConfig).toHaveBeenCalledWith('a1', 'STRIPE_KEY'))
  })
})
