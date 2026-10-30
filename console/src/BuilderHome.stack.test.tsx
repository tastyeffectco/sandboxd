import { afterEach, describe, expect, it, vi } from 'vitest'
import { cleanup, fireEvent, render, screen, waitFor } from '@testing-library/react'
import { BuilderHome } from './BuilderHome'
import { api } from './api'

vi.mock('./api', () => ({
  api: {
    listPresets: vi.fn().mockResolvedValue([
      { id: 'react-vite', label: 'React / Vite', description: '' },
      { id: 'nextjs', label: 'Next.js', description: '' },
    ]),
    getAgents: vi.fn().mockResolvedValue([]),
    listModelProviders: vi.fn().mockResolvedValue([]),
    getSandbox: vi.fn(),
    createApp: vi.fn().mockResolvedValue({ id: 'app1' }),
    createAppSandbox: vi.fn().mockResolvedValue({}),
  },
}))

afterEach(() => {
  cleanup()
  vi.mocked(api.createApp).mockClear()
})

const home = () =>
  render(
    <BuilderHome
      apps={[]}
      reload={() => {}}
      onOpen={() => {}}
      onCreated={() => {}}
      goSettings={() => {}}
      saveDraft={() => {}}
    />,
  )

describe('BuilderHome stack from the idea', () => {
  it('starts a Next.js idea on Next.js and says why', async () => {
    home()
    fireEvent.change(screen.getByTestId('build-prompt'), { target: { value: 'A blog with Next.js' } })
    expect(screen.getByTestId('stack-toggle').textContent).toContain('Next.js · Auto')
    expect(screen.getByTestId('stack-note').textContent).toMatch(/because your idea mentions Next\.js/)
    await waitFor(() => expect((screen.getByTestId('build-app') as HTMLButtonElement).disabled).toBe(false))
    fireEvent.click(screen.getByTestId('build-app'))
    await waitFor(() => expect(api.createApp).toHaveBeenCalled())
    expect(vi.mocked(api.createApp).mock.calls[0][0]).toMatchObject({ runtime_preset: 'nextjs' })
  })

  it('warns before building with a language the workspace does not have', () => {
    home()
    fireEvent.change(screen.getByTestId('build-prompt'), { target: { value: 'A Laravel blog' } })
    const note = screen.getByTestId('stack-note')
    expect(note.className).toContain('warn')
    expect(note.textContent).toMatch(/PHP isn't available/)
  })

  it('keeps the default starter for plain ideas and a stack chosen by hand', async () => {
    home()
    fireEvent.change(screen.getByTestId('build-prompt'), { target: { value: 'A habit tracker' } })
    expect(screen.queryByTestId('stack-note')).toBeNull()
    fireEvent.click(screen.getByTestId('stack-toggle'))
    fireEvent.click(await screen.findByTestId('preset-nextjs'))
    fireEvent.change(screen.getByTestId('build-prompt'), { target: { value: 'A FastAPI service' } })
    expect(screen.getByTestId('stack-toggle').textContent).toContain('Next.js')
    expect(screen.queryByTestId('stack-note')).toBeNull()
  })
})
