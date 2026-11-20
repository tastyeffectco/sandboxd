import { CSSProperties } from 'react'
import { Bot, BotMood } from './Bot'
import { Avatar, logoFor } from './Avatar'

// A project's face, like an app icon on a phone: a coloured tile with its
// initials. A free app shows its own logo. The person can pick the colour,
// the logo or a Boxbot (Rename & icon); the choice is saved on the project as
// a tag, so every screen and computer shows the same.

export const APP_COLORS = [
  '#8f6bff',
  '#ff7a59',
  '#ff6bd5',
  '#3ee6a8',
  '#5cc8ff',
  '#ffc35c',
  '#ff5c7a',
  '#a8f07a',
  '#5b6cff',
  '#9aa3b8',
]
const ICON_TAG = 'icon:'
export type IconChoice = { kind: 'logo' } | { kind: 'bot' } | { kind: 'color'; color: string }

export function iconChoice(tags: string[] | undefined, logo?: string): IconChoice {
  const tag = tags?.find((t) => t.startsWith(ICON_TAG))?.slice(ICON_TAG.length)
  if (tag === 'bot') return { kind: 'bot' }
  if (tag && /^#[0-9a-f]{6}$/i.test(tag)) return { kind: 'color', color: tag }
  if (logo && logoFor(logo)) return { kind: 'logo' }
  return { kind: 'color', color: '' }
}
// The project's tags with this choice saved (the default needs no tag).
export function withIcon(tags: string[] | undefined, choice: IconChoice): string[] {
  const rest = (tags || []).filter((t) => !t.startsWith(ICON_TAG))
  if (choice.kind === 'bot') return [...rest, 'icon:bot']
  if (choice.kind === 'color' && choice.color) return [...rest, ICON_TAG + choice.color]
  return rest
}
export function defaultColor(name: string) {
  let h = 0
  for (const ch of name.toLowerCase()) h = (h * 31 + ch.charCodeAt(0)) >>> 0
  return APP_COLORS[h % APP_COLORS.length]
}
const initials = (name: string) => {
  const words = name.trim().split(/[\s\-_.]+/).filter(Boolean)
  return ((words[0]?.[0] || '?') + (words.length > 1 ? words[1][0] : '')).toUpperCase()
}

export function AppTile({ name, color, size = 40, style }: { name: string; color?: string; size?: number; style?: CSSProperties }) {
  return (
    <span
      className="app-tile"
      aria-hidden="true"
      style={{
        width: size,
        height: size,
        minWidth: size,
        fontSize: Math.round(size * (initials(name).length > 1 ? 0.36 : 0.46)),
        ['--tile' as string]: color || defaultColor(name),
        ...style,
      }}
    >
      {initials(name)}
    </span>
  )
}

export function ProjectIcon({
  name,
  tags,
  logo,
  size = 40,
  mood,
  still,
  style,
}: {
  name: string
  tags?: string[]
  // The free app it was installed from (its logo).
  logo?: string
  size?: number
  mood?: BotMood
  still?: boolean
  style?: CSSProperties
}) {
  const choice = iconChoice(tags, logo)
  if (choice.kind === 'bot') return <Bot who={name} size={size} mood={mood} still={still} style={style} />
  if (choice.kind === 'logo') return <Avatar who={name} logo={logo} size={size} still={still} style={style} />
  return <AppTile name={name} color={choice.color} size={size} style={style} />
}
