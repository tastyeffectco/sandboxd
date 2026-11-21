import { afterEach, describe, expect, it, vi } from 'vitest'
import { installTheme, resolvedTheme, setThemePref, themePref } from './theme'

const mq = (dark: boolean) =>
  vi.fn().mockReturnValue({ matches: dark, addEventListener: vi.fn() }) as unknown as typeof matchMedia

afterEach(() => {
  localStorage.clear()
  delete document.documentElement.dataset.theme
})

describe('appearance', () => {
  it('follows the computer until the person chooses', () => {
    globalThis.matchMedia = mq(true)
    expect(themePref()).toBe('system')
    installTheme()
    expect(document.documentElement.dataset.theme).toBe('dark')
    globalThis.matchMedia = mq(false)
    expect(resolvedTheme()).toBe('light')
  })

  it('remembers Light or Dark and applies it at once', () => {
    globalThis.matchMedia = mq(true)
    setThemePref('light')
    expect(themePref()).toBe('light')
    expect(document.documentElement.dataset.theme).toBe('light')
    setThemePref('dark')
    expect(document.documentElement.dataset.theme).toBe('dark')
  })

  it('tells the desktop app, so its title bar matches', () => {
    globalThis.matchMedia = mq(false)
    const setTheme = vi.fn().mockResolvedValue(undefined)
    ;(window as unknown as { sandboxdDesktop: unknown }).sandboxdDesktop = { setTheme }
    setThemePref('light')
    expect(setTheme).toHaveBeenCalledWith('light')
    delete (window as unknown as { sandboxdDesktop?: unknown }).sandboxdDesktop
  })
})
