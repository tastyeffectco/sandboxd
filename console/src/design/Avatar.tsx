import { CSSProperties, KeyboardEvent, MouseEvent, useSyncExternalStore } from 'react'
import { Bot, BotMood } from './Bot'

// An agent's or an app's face: its own logo when we ship one (src/logos/<id>.svg
// or .png), else its Boxbot. Clicking a big one (toggle) switches between the
// two; the choice is remembered on this computer for that agent or app.

const files = import.meta.glob('../logos/*.{svg,png}', {
  eager: true,
  query: '?url',
  import: 'default',
}) as Record<string, string>
const LOGOS: Record<string, string> = {}
for (const [file, url] of Object.entries(files)) LOGOS[file.split('/').pop()!.replace(/\.(svg|png)$/, '')] = url

export const logoFor = (id?: string) => (id ? LOGOS[id.toLowerCase()] : undefined)

const KEY = 'sbx-icon-style'
const EVENT = 'sbx-icon-style'
function readBots(): Record<string, true> {
  try {
    return JSON.parse(localStorage.getItem(KEY) || '{}') || {}
  } catch {
    return {}
  }
}
let bots = readBots()
const subscribe = (callback: () => void) => {
  window.addEventListener(EVENT, callback)
  return () => window.removeEventListener(EVENT, callback)
}
// true: show this one as a Boxbot, not its logo.
export function useAsBot(id: string): boolean {
  return useSyncExternalStore(
    subscribe,
    () => !!bots[id],
    () => false,
  )
}
export function setAsBot(id: string, asBot: boolean) {
  bots = { ...bots }
  if (asBot) bots[id] = true
  else delete bots[id]
  try {
    localStorage.setItem(KEY, JSON.stringify(bots))
  } catch {
    /* remembered for this visit only */
  }
  window.dispatchEvent(new Event(EVENT))
}

export function Avatar({
  who,
  logo,
  size = 40,
  mood,
  hue = 0,
  still,
  toggle = false,
  className,
  style,
}: {
  // The Boxbot's name (and the logo's id unless `logo` is given).
  who: string
  // The logo's id: an agent package, a coding agent, or a catalog app id.
  logo?: string
  size?: number
  mood?: BotMood
  // A second copy of the same agent: its bot turns colour, its logo gets a ring.
  hue?: number
  still?: boolean
  // Clicking switches between the logo and the Boxbot.
  toggle?: boolean
  className?: string
  style?: CSSProperties
}) {
  const id = (logo || who).toLowerCase()
  const url = LOGOS[id]
  const asBot = useAsBot(id)
  const face =
    url && !asBot ? (
      <span
        className={`logo-tile ${hue ? 'logo-copy' : ''} ${className || ''}`}
        style={{
          width: size,
          height: size,
          minWidth: size,
          maxWidth: size,
          // Pixels: a percentage would follow the parent's width.
          padding: Math.max(2, Math.round(size * 0.13)),
          ...(hue ? { ['--ring' as string]: `hsl(${(hue + 260) % 360} 85% 68%)` } : {}),
          ...style,
        }}
        aria-hidden="true"
      >
        <img src={url} alt="" draggable={false} />
      </span>
    ) : (
      <Bot who={who} mood={mood} size={size} hue={hue} still={still} className={className} style={style} />
    )
  if (!toggle || !url) return face
  const flip = (e: MouseEvent | KeyboardEvent) => {
    e.stopPropagation()
    e.preventDefault()
    setAsBot(id, !asBot)
  }
  return (
    <span
      role="button"
      tabIndex={0}
      className="avatar-toggle"
      title={asBot ? 'Show its own logo' : 'Show it as a sandboxd bot'}
      aria-label={asBot ? 'Show its own logo' : 'Show it as a sandboxd bot'}
      onClick={flip}
      onKeyDown={(e) => (e.key === 'Enter' || e.key === ' ') && flip(e)}
    >
      {face}
    </span>
  )
}
