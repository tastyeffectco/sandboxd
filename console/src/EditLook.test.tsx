import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { cleanup, fireEvent, render, screen, waitFor } from '@testing-library/react'
import { EditLook } from './EditLook'
import { api, AgentInstance, App as TApp } from './api'
import { iconChoice, withIcon } from './design/ProjectIcon'

vi.mock('./api', () => ({
  api: { updateApp: vi.fn(), renameAgent: vi.fn() },
}))
beforeEach(() => {
  // jsdom has no modal dialogs; open them as plain dialogs.
  HTMLDialogElement.prototype.showModal ??= function (this: HTMLDialogElement) {
    this.open = true
  }
  HTMLDialogElement.prototype.close ??= function (this: HTMLDialogElement) {
    this.open = false
  }
})
afterEach(() => {
  cleanup()
  localStorage.clear()
})

const app = { id: 'a1', name: 'Recipe box', tags: ['created-by:me'] } as TApp
const hermes = { id: 'h1', package: 'hermes', name: 'Hermes' } as AgentInstance

describe('project icons', () => {
  it('keeps the choice in the tags, and a free app shows its logo by default', () => {
    expect(iconChoice([], 'excalidraw')).toEqual({ kind: 'logo' })
    expect(iconChoice(['icon:bot'], 'excalidraw')).toEqual({ kind: 'bot' })
    expect(iconChoice(['icon:#ff7a59'])).toEqual({ kind: 'color', color: '#ff7a59' })
    expect(withIcon(['x', 'icon:bot'], { kind: 'color', color: '#3ee6a8' })).toEqual(['x', 'icon:#3ee6a8'])
    expect(withIcon(['x', 'icon:bot'], { kind: 'logo' })).toEqual(['x'])
  })
})

describe('EditLook', () => {
  it('renames a project and saves its colour', async () => {
    const onSaved = vi.fn()
    vi.mocked(api.updateApp).mockResolvedValue({ ...app, name: 'Recipes' })
    render(<EditLook app={app} onSaved={onSaved} onClose={() => {}} onError={() => {}} />)
    fireEvent.change(screen.getByTestId('look-name'), { target: { value: 'Recipes' } })
    fireEvent.click(screen.getByRole('button', { name: 'Colour #ff7a59' }))
    fireEvent.click(screen.getByTestId('look-save'))
    await waitFor(() => expect(onSaved).toHaveBeenCalled())
    expect(api.updateApp).toHaveBeenCalledWith('a1', { name: 'Recipes', tags: ['created-by:me', 'icon:#ff7a59'] })
  })

  it('renames an agent and shows it as a Boxbot instead of its logo', async () => {
    const onSaved = vi.fn()
    vi.mocked(api.renameAgent).mockResolvedValue({ ...hermes, name: 'Hermes for work' })
    render(<EditLook agent={hermes} onSaved={onSaved} onClose={() => {}} onError={() => {}} />)
    expect(screen.getByRole('button', { name: 'Its own logo' }).getAttribute('aria-pressed')).toBe('true')
    fireEvent.change(screen.getByTestId('look-name'), { target: { value: 'Hermes for work' } })
    fireEvent.click(screen.getByRole('button', { name: 'A sandboxd bot' }))
    fireEvent.click(screen.getByTestId('look-save'))
    await waitFor(() => expect(onSaved).toHaveBeenCalled())
    expect(api.renameAgent).toHaveBeenCalledWith('h1', 'Hermes for work')
    expect(localStorage.getItem('sbx-icon-style')).toContain('hermes')
  })
})
