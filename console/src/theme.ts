// Appearance: Light, Dark, or like this computer (System). The choice is a
// per-computer convenience kept in localStorage; the theme itself is the
// <html data-theme> attribute that design/theme.css reads.
export type ThemePref = 'system' | 'light' | 'dark'
const KEY = 'sandboxd.theme'
const dark = () => typeof matchMedia === 'function' && matchMedia('(prefers-color-scheme: dark)').matches

export function themePref(): ThemePref {
  try {
    const v = localStorage.getItem(KEY)
    if (v === 'light' || v === 'dark' || v === 'system') return v
  } catch {
    /* storage unavailable: follow the computer */
  }
  return 'system'
}

export function resolvedTheme(pref = themePref()): 'light' | 'dark' {
  return pref === 'system' ? (dark() ? 'dark' : 'light') : pref
}

function paint() {
  const t = resolvedTheme()
  document.documentElement.dataset.theme = t
  document.querySelector('meta[name="theme-color"]')?.setAttribute('content', t === 'dark' ? '#0a0a11' : '#f6f5f1')
  // The desktop app matches its window and title bar buttons.
  window.sandboxdDesktop?.setTheme?.(t)?.catch(() => {})
}

export function setThemePref(pref: ThemePref) {
  try {
    localStorage.setItem(KEY, pref)
  } catch {
    /* still applied for this session */
  }
  paint()
}

// Apply now and follow the computer when it switches between light and dark.
export function installTheme() {
  paint()
  if (typeof matchMedia === 'function') {
    matchMedia('(prefers-color-scheme: dark)').addEventListener?.('change', () => themePref() === 'system' && paint())
  }
}
