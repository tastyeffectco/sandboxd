import { afterEach, expect, it } from 'vitest'
import { cleanup, render, screen } from '@testing-library/react'
import { UserPrompt } from './AgentChat'
import { CATALOG, installPrompt } from './catalog'

afterEach(cleanup)

it('shows a free-app install as one line, with the setup folded away', () => {
  render(<UserPrompt text={installPrompt(CATALOG[0])} />)
  const bubble = screen.getByTestId('catalog-install-message')
  expect(bubble.firstChild?.textContent).toBe(`Install ${CATALOG[0].name} from Free apps`)
  expect(bubble.querySelector('details')?.open).toBe(false)
})

it('shows any other prompt as written', () => {
  render(<UserPrompt text="Build me a todo app" />)
  expect(screen.getByText('Build me a todo app')).toBeTruthy()
  expect(screen.queryByTestId('catalog-install-message')).toBeNull()
})
