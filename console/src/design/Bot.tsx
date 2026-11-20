import { CSSProperties, useId } from 'react'

// Boxbots: sandboxd's crew. Every agent, coding agent and project gets one: a
// soft rounded cube (the sandbox) with a glass visor, glowing eyes and an
// antenna. Colour says who it is; the face says what it is doing.

export type BotMood = 'idle' | 'thinking' | 'working' | 'happy' | 'sleeping' | 'error'

export type BotLook = {
  // Body light and shade, eye glow.
  hi: string
  lo: string
  glow: string
  // A signature shape: who it is at a glance, not only its colour.
  trait?: 'star' | 'wings' | 'claws' | 'ears'
}

// The named crew.
export const BOTS: Record<string, BotLook> = {
  sandboxd: { hi: '#b9a2ff', lo: '#5b3fd6', glow: '#ffd9f4', trait: 'star' },
  hermes: { hi: '#ffd57a', lo: '#d9821c', glow: '#fff4d1', trait: 'wings' },
  openclaw: { hi: '#ff9a7d', lo: '#d6384a', glow: '#ffe1d6', trait: 'claws' },
  openmausbot: { hi: '#86dcff', lo: '#2f6fe0', glow: '#e1f6ff', trait: 'ears' },
  opencode: { hi: '#7ff0c4', lo: '#14a074', glow: '#dcfff1' },
  codex: { hi: '#c9d2e6', lo: '#5a6684', glow: '#ffffff' },
  'claude-code': { hi: '#ffb08a', lo: '#c9572c', glow: '#fff0e6' },
  claude: { hi: '#ffb08a', lo: '#c9572c', glow: '#fff0e6' },
  pi: { hi: '#ff9be6', lo: '#b8389b', glow: '#ffe6f8' },
  rakazo: { hi: '#a8f07a', lo: '#3f9a2c', glow: '#efffe0' },
}

// A project's bot: a stable colour from its name.
export function lookFor(key: string): BotLook {
  const k = key.toLowerCase()
  if (BOTS[k]) return BOTS[k]
  let h = 0
  for (const ch of k) h = (h * 31 + ch.charCodeAt(0)) >>> 0
  const hue = h % 360
  return {
    hi: `hsl(${hue} 90% 76%)`,
    lo: `hsl(${(hue + 18) % 360} 62% 42%)`,
    glow: `hsl(${hue} 100% 94%)`,
  }
}

export function Bot({
  who = 'sandboxd',
  look,
  mood = 'idle',
  size = 40,
  still = false,
  title,
  hue = 0,
  style,
  className,
}: {
  who?: string
  look?: BotLook
  mood?: BotMood
  size?: number
  // No idle motion (lists with many bots).
  still?: boolean
  // Announce the bot to screen readers with this text; otherwise it is decorative.
  title?: string
  // Turn the colour wheel (degrees): a second Hermes is the same bot in another colour.
  hue?: number
  style?: CSSProperties
  className?: string
}) {
  const id = useId().replace(/:/g, '')
  const l = look || lookFor(who)
  const asleep = mood === 'sleeping'
  const eye = asleep ? 'rgba(255,255,255,.35)' : l.glow
  return (
    <svg
      viewBox="0 0 120 120"
      width={size}
      height={size}
      // Decorative next to a name (the usual case); a titled bot is announced.
      role={title ? 'img' : undefined}
      aria-label={title}
      aria-hidden={title ? undefined : true}
      className={`bot bot-${mood} ${still ? 'bot-still' : ''} ${className || ''}`}
      style={hue ? { ...style, filter: `hue-rotate(${hue}deg)` } : style}
    >
      <defs>
        <linearGradient id={`b${id}`} x1="0" y1="0" x2="1" y2="1">
          <stop offset="0" stopColor={l.hi} />
          <stop offset="1" stopColor={l.lo} />
        </linearGradient>
        <linearGradient id={`v${id}`} x1="0" y1="0" x2="0" y2="1">
          <stop offset="0" stopColor="#1d1b2e" />
          <stop offset="1" stopColor="#07070d" />
        </linearGradient>
        <radialGradient id={`s${id}`} cx=".3" cy=".22" r=".55">
          <stop offset="0" stopColor="#fff" stopOpacity=".75" />
          <stop offset="1" stopColor="#fff" stopOpacity="0" />
        </radialGradient>
        <filter id={`g${id}`} x="-50%" y="-50%" width="200%" height="200%">
          <feGaussianBlur stdDeviation="2.4" result="b" />
          <feMerge>
            <feMergeNode in="b" />
            <feMergeNode in="SourceGraphic" />
          </feMerge>
        </filter>
      </defs>
      {/* floor shadow */}
      <ellipse className="bot-shadow" cx="60" cy="110" rx="30" ry="5" fill="#000" opacity=".45" />
      <g className="bot-body">
        {/* antenna */}
        <path d="M60 22 V10" stroke={l.lo} strokeWidth="4" strokeLinecap="round" />
        {l.trait === 'star' ? (
          <path
            className="bot-bulb"
            d="M60 -1 L63 6 L70 9 L63 12 L60 19 L57 12 L50 9 L57 6 Z"
            fill={l.hi}
            filter={`url(#g${id})`}
          />
        ) : (
          <circle className="bot-bulb" cx="60" cy="9" r="6" fill={l.hi} filter={`url(#g${id})`} />
        )}
        {l.trait === 'ears' && (
          <g fill={l.lo}>
            <circle cx="26" cy="26" r="15" />
            <circle cx="94" cy="26" r="15" />
            <circle cx="26" cy="26" r="8" fill={l.hi} opacity=".55" />
            <circle cx="94" cy="26" r="8" fill={l.hi} opacity=".55" />
          </g>
        )}
        {/* sides: ears, wings or claws */}
        {l.trait === 'wings' ? (
          <g fill={l.hi} stroke={l.lo} strokeWidth="2" strokeLinejoin="round">
            <path d="M17 52 C2 44 -2 34 1 28 C6 36 11 38 17 40 Z" />
            <path d="M17 64 C4 60 -1 52 0 46 C6 52 11 54 17 55 Z" />
            <path d="M103 52 C118 44 122 34 119 28 C114 36 109 38 103 40 Z" />
            <path d="M103 64 C116 60 121 52 120 46 C114 52 109 54 103 55 Z" />
          </g>
        ) : l.trait === 'claws' ? (
          <g fill={l.lo}>
            <path d="M16 54 C2 50 -1 66 6 74 C8 68 12 66 16 68 Z" />
            <path d="M6 74 C2 80 8 86 14 82 C12 78 13 76 16 74 Z" />
            <path d="M104 54 C118 50 121 66 114 74 C112 68 108 66 104 68 Z" />
            <path d="M114 74 C118 80 112 86 106 82 C108 78 107 76 104 74 Z" />
          </g>
        ) : (
          <>
            <rect x="9" y="50" width="10" height="24" rx="5" fill={l.lo} />
            <rect x="101" y="50" width="10" height="24" rx="5" fill={l.lo} />
          </>
        )}
        {/* head: the box */}
        <rect x="15" y="22" width="90" height="80" rx="26" fill={`url(#b${id})`} />
        <rect x="15" y="22" width="90" height="80" rx="26" fill={`url(#s${id})`} />
        <rect x="15.5" y="22.5" width="89" height="79" rx="25.5" fill="none" stroke="#fff" strokeOpacity=".28" />
        {/* visor */}
        <rect x="26" y="40" width="68" height="44" rx="17" fill={`url(#v${id})`} />
        <path
          d="M33 47 Q44 42 60 43"
          stroke="#fff"
          strokeOpacity=".16"
          strokeWidth="3"
          strokeLinecap="round"
          fill="none"
        />
        {/* face */}
        <g className="bot-face" filter={`url(#g${id})`} fill={eye} stroke={eye}>
          {mood === 'happy' && (
            <g fill="none" strokeWidth="5" strokeLinecap="round">
              <path d="M40 66 Q47 57 54 66" />
              <path d="M66 66 Q73 57 80 66" />
            </g>
          )}
          {asleep && (
            <g fill="none" strokeWidth="4.5" strokeLinecap="round">
              <path d="M40 63 Q47 68 54 63" />
              <path d="M66 63 Q73 68 80 63" />
            </g>
          )}
          {mood === 'error' && (
            <g fill="none" strokeWidth="4.5" strokeLinecap="round">
              <path d="M41 57 L53 65 M53 57 L41 65" />
              <path d="M67 57 L79 65 M79 57 L67 65" />
            </g>
          )}
          {mood === 'working' && (
            <g className="bot-scan" stroke="none">
              <rect x="38" y="58" width="16" height="7" rx="3.5" />
              <rect x="66" y="58" width="16" height="7" rx="3.5" />
            </g>
          )}
          {(mood === 'idle' || mood === 'thinking') && (
            <g className={mood === 'thinking' ? 'bot-look' : 'bot-blink'} stroke="none">
              <rect x="40" y="52" width="12" height="20" rx="6" />
              <rect x="68" y="52" width="12" height="20" rx="6" />
            </g>
          )}
        </g>
        {mood === 'thinking' && (
          <g className="bot-dots" fill={l.hi}>
            <circle cx="96" cy="18" r="3.4" />
            <circle cx="106" cy="10" r="2.6" />
            <circle cx="113" cy="3" r="1.8" />
          </g>
        )}
        {asleep && (
          <text className="bot-z" x="94" y="22" fontSize="16" fontWeight="700" fill={l.hi} fontFamily="var(--display)">
            z
          </text>
        )}
      </g>
    </svg>
  )
}

// An agent's or workspace's status as a face.
export function moodFor(status?: string, working = false): BotMood {
  if (working) return 'working'
  if (status === 'running' || status === 'ready') return 'idle'
  if (status === 'starting' || status === 'creating' || status === 'waking' || status === 'pulling') return 'thinking'
  if (status === 'error' || status === 'failed') return 'error'
  return 'sleeping'
}

// Several agents of one kind (two Hermes) look alike but not the same: the
// first keeps its colours, each next one turns the colour wheel further.
export function agentHue(agents: { id: string; package: string }[], agent: { id: string; package: string }): number {
  const same = agents
    .filter((a) => a.package === agent.package)
    .map((a) => a.id)
    .sort()
  const i = same.indexOf(agent.id)
  return i > 0 ? (i * 67) % 360 : 0
}
