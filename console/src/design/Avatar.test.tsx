import { afterEach, describe, expect, it } from 'vitest'
import { act, cleanup, fireEvent, render } from '@testing-library/react'
import { Avatar, logoFor, setAsBot } from './Avatar'

afterEach(() => {
  cleanup()
  act(() => setAsBot('hermes', false))
})

describe('Avatar', () => {
  it('shows an agent or app by its own logo, and a Boxbot when there is none', () => {
    expect(logoFor('hermes')).toBeTruthy()
    expect(logoFor('excalidraw')).toBeTruthy()
    const { container } = render(
      <>
        <Avatar who="hermes" />
        <Avatar who="Recipe box" />
      </>,
    )
    expect(container.querySelectorAll('.logo-tile img')).toHaveLength(1)
    expect(container.querySelectorAll('svg.bot')).toHaveLength(1)
  })

  it('switches between the logo and the Boxbot on click, everywhere and remembered', () => {
    const { container, getByRole } = render(
      <>
        <Avatar who="hermes" size={100} toggle />
        <Avatar who="hermes" size={20} />
      </>,
    )
    fireEvent.click(getByRole('button', { name: 'Show it as a sandboxd bot' }))
    expect(container.querySelectorAll('.logo-tile')).toHaveLength(0)
    expect(container.querySelectorAll('svg.bot')).toHaveLength(2)
    expect(localStorage.getItem('sbx-icon-style')).toContain('hermes')
    fireEvent.click(getByRole('button', { name: 'Show its own logo' }))
    expect(container.querySelectorAll('.logo-tile')).toHaveLength(2)
  })

  it('does not swallow clicks on things without a logo', () => {
    const { queryByRole } = render(<Avatar who="Recipe box" toggle />)
    expect(queryByRole('button')).toBeNull()
  })
})
