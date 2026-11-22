import { afterEach, describe, expect, it, vi } from 'vitest'
import { cleanup, fireEvent, render, screen } from '@testing-library/react'
import { MyApps } from './MyApps'
import type { App } from './api'

vi.mock('./api', () => ({ api: { getSandbox: vi.fn().mockResolvedValue({ status: 'running' }) } }))
afterEach(cleanup)

const app = (id: string, name: string, description = ''): App =>
  ({ id, name, description, tags: [], current_sandbox_id: `sb-${id}` }) as unknown as App

describe('My apps', () => {
  it('with no apps, sends people to the sandboxd agent instead of a second build box', () => {
    const onAsk = vi.fn()
    render(<MyApps apps={[]} onOpen={vi.fn()} onAsk={onAsk} />)
    expect(screen.getByText('No apps yet')).toBeTruthy()
    expect(screen.queryByRole('textbox')).toBeNull()
    fireEvent.click(screen.getByTestId('apps-ask'))
    expect(onAsk).toHaveBeenCalledTimes(1)
  })

  it('finds an app by name or description and opens it', () => {
    const onOpen = vi.fn()
    render(
      <MyApps
        apps={[app('a1', 'Recipe Box', 'recipes and a shopping list'), app('a2', 'Excalidraw', 'whiteboard')]}
        onOpen={onOpen}
        onAsk={vi.fn()}
      />,
    )
    fireEvent.change(screen.getByLabelText('Search apps'), { target: { value: 'shopping' } })
    const cards = screen.getAllByTestId('app-card')
    expect(cards).toHaveLength(1)
    fireEvent.click(cards[0])
    expect(onOpen).toHaveBeenCalledWith('a1')
    fireEvent.change(screen.getByLabelText('Search apps'), { target: { value: 'nothing like this' } })
    expect(screen.getByText(/No apps match/)).toBeTruthy()
  })
})
