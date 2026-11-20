import { CSSProperties, ReactNode } from 'react'

// The palette is the design system's CSS variables (design/theme.css), so
// inline styles follow the theme. Use alpha() for a see-through tint.
export const c = {
  bg: 'var(--bg)',
  panel: 'var(--panel)',
  panel2: 'var(--panel-2)',
  panel3: 'var(--panel-3)',
  fg: 'var(--fg)',
  fg2: 'var(--fg-2)',
  muted: 'var(--muted)',
  muted2: 'var(--muted-2)',
  faint: 'var(--faint)',
  border: 'var(--border)',
  border2: 'var(--border-2)',
  ink: 'var(--ink)',
  ink2: 'var(--ink-2)',
  link: 'var(--link)',
  accent: 'var(--accent)',
  good: 'var(--good)',
  warn: 'var(--warn)',
  bad: 'var(--bad)',
}
// alpha(c.good, 8) -> the colour at 8% over transparent.
export const alpha = (color: string, percent: number) => `color-mix(in srgb, ${color} ${percent}%, transparent)`
export const font = {
  sans: 'var(--sans)',
  display: 'var(--display)',
  mono: 'var(--mono)',
}
export const mono: CSSProperties = { fontFamily: font.mono }

export function Card({
  children,
  style,
  pad,
  onClick,
  ...rest
}: React.HTMLAttributes<HTMLDivElement> & { pad?: number | boolean }) {
  return (
    <div
      {...rest}
      onClick={onClick}
      style={{
        background: c.panel,
        border: `1px solid ${c.border}`,
        borderRadius: 10,
        ...(pad ? { padding: pad === true ? 16 : pad } : {}),
        ...style,
      }}
    >
      {children}
    </div>
  )
}

export function H({
  children,
  size = 15,
  style,
}: {
  children: ReactNode
  size?: number
  style?: CSSProperties
}) {
  return <div style={{ fontFamily: font.display, fontSize: size, fontWeight: 600, ...style }}>{children}</div>
}

type BtnVariant = 'primary' | 'outline' | 'ghost' | 'danger'
export function Btn({
  children,
  onClick,
  variant = 'outline',
  disabled,
  title,
  style,
  sm,
  type = 'button',
  ...rest
}: React.ButtonHTMLAttributes<HTMLButtonElement> & { variant?: BtnVariant; sm?: boolean }) {
  const base: CSSProperties = {
    display: 'inline-flex',
    alignItems: 'center',
    justifyContent: 'center',
    gap: 7,
    borderRadius: 9,
    minHeight: sm ? 30 : 36,
    fontSize: sm ? 12 : 13,
    fontFamily: font.sans,
    fontWeight: 500,
    padding: sm ? '5px 12px' : '7px 14px',
    cursor: disabled ? 'not-allowed' : 'pointer',
    opacity: disabled ? 0.55 : 1,
    whiteSpace: 'nowrap',
  }
  const v: Record<BtnVariant, CSSProperties> = {
    primary: { background: 'var(--brand-grad)', color: 'var(--on-accent)', fontWeight: 600, border: 'none', boxShadow: '0 6px 20px -8px rgba(155,123,255,.7)' },
    outline: { background: c.panel2, border: `1px solid ${c.border2}`, color: c.fg },
    ghost: { background: 'transparent', border: `1px solid ${c.border2}`, color: c.muted },
    danger: { background: 'transparent', border: `1px solid ${alpha(c.bad, 35)}`, color: c.bad },
  }
  return (
    <button
      {...rest}
      type={type}
      title={title}
      disabled={disabled}
      onClick={onClick}
      className="dc-hoverborder"
      style={{ ...base, ...v[variant], ...style }}
    >
      {children}
    </button>
  )
}

type Tone = 'good' | 'warn' | 'bad' | 'neutral'
const toneColor: Record<Tone, string> = { good: c.good, warn: c.warn, bad: c.bad, neutral: c.muted2 }
export function Pill({
  tone = 'neutral',
  dot,
  children,
}: {
  tone?: Tone
  dot?: boolean
  children: ReactNode
}) {
  const col = toneColor[tone]
  return (
    <span
      style={{
        display: 'inline-flex',
        alignItems: 'center',
        gap: 6,
        fontFamily: font.mono,
        fontSize: 10.5,
        color: col,
        background: alpha(col, 10),
        border: `1px solid ${alpha(col, 30)}`,
        borderRadius: 5,
        padding: '1px 8px',
      }}
    >
      {dot && <span style={{ width: 6, height: 6, borderRadius: '50%', background: col }} />}
      {children}
    </span>
  )
}

// status -> tone/label for sandbox/process/app status strings.
export function statusTone(status?: string): Tone {
  if (status === 'running') return 'good'
  if (status === 'error') return 'bad'
  if (status === 'creating' || status === 'starting') return 'warn'
  return 'neutral'
}
export function StatusPill({ status }: { status?: string }) {
  const s = status || 'no sandbox'
  return (
    <Pill tone={statusTone(status)} dot>
      {s}
    </Pill>
  )
}

export function Input(props: React.InputHTMLAttributes<HTMLInputElement> & { mono?: boolean }) {
  const { mono: m, style, ...rest } = props
  return (
    <input
      {...rest}
      style={{
        background: c.bg,
        border: `1px solid ${c.border2}`,
        borderRadius: 7,
        padding: '8px 11px',
        color: c.fg,
        fontSize: 12.5,
        fontFamily: m ? font.mono : font.sans,
        ...style,
      }}
    />
  )
}

export function tab(active: boolean): CSSProperties {
  return {
    padding: '8px 12px',
    fontSize: 13,
    fontWeight: 500,
    cursor: 'pointer',
    color: active ? c.fg : c.muted2,
    borderBottom: `2px solid ${active ? c.fg : 'transparent'}`,
    marginBottom: -1,
  }
}
export function navItem(active: boolean): CSSProperties {
  return {
    padding: '5px 10px',
    fontSize: 12.5,
    cursor: 'pointer',
    borderRadius: 6,
    color: active ? c.fg : c.muted,
    background: active ? c.panel2 : 'transparent',
  }
}
