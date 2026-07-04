import { CSSProperties, ReactNode } from 'react'

// Exact palette from the design.
export const c = {
  bg: '#fafafa', panel: '#ffffff', panel2: '#f4f4f5', panel3: '#fcfcfc',
  fg: '#09090b', fg2: '#3f3f46', muted: '#52525b', muted2: '#71717a', faint: '#a1a1aa',
  border: '#e4e4e7', border2: '#d4d4d8', ink: '#18181b', ink2: '#3f3f46', link: '#171717',
  good: '#15803d', warn: '#b45309', bad: '#dc2626',
}
export const font = {
  sans: "'IBM Plex Sans',system-ui,sans-serif",
  display: "'Space Grotesk','IBM Plex Sans',sans-serif",
  mono: "'IBM Plex Mono',ui-monospace,monospace",
}
export const mono: CSSProperties = { fontFamily: font.mono }

export function Card({ children, style, pad }: { children: ReactNode; style?: CSSProperties; pad?: number | boolean }) {
  return (
    <div style={{ background: c.panel, border: `1px solid ${c.border}`, borderRadius: 10, ...(pad ? { padding: pad === true ? 16 : pad } : {}), ...style }}>
      {children}
    </div>
  )
}

export function H({ children, size = 15, style }: { children: ReactNode; size?: number; style?: CSSProperties }) {
  return <div style={{ fontFamily: font.display, fontSize: size, fontWeight: 600, ...style }}>{children}</div>
}

type BtnVariant = 'primary' | 'outline' | 'ghost' | 'danger'
export function Btn({
  children, onClick, variant = 'outline', disabled, title, style, sm,
}: { children: ReactNode; onClick?: () => void; variant?: BtnVariant; disabled?: boolean; title?: string; style?: CSSProperties; sm?: boolean }) {
  const base: CSSProperties = {
    borderRadius: 7, fontSize: sm ? 12 : 12.5, fontFamily: font.sans, fontWeight: 500,
    padding: sm ? '5px 12px' : '7px 14px', cursor: disabled ? 'not-allowed' : 'pointer',
    opacity: disabled ? 0.55 : 1, whiteSpace: 'nowrap',
  }
  const v: Record<BtnVariant, CSSProperties> = {
    primary: { background: c.ink, color: '#fff', fontWeight: 600, border: 'none' },
    outline: { background: c.panel2, border: `1px solid ${c.border2}`, color: c.fg },
    ghost: { background: 'transparent', border: `1px solid ${c.border2}`, color: c.muted },
    danger: { background: 'transparent', border: '1px solid rgba(220,38,38,.3)', color: c.bad },
  }
  return (
    <button title={title} disabled={disabled} onClick={onClick} className="dc-hoverborder" style={{ ...base, ...v[variant], ...style }}>
      {children}
    </button>
  )
}

type Tone = 'good' | 'warn' | 'bad' | 'neutral'
const toneColor: Record<Tone, string> = { good: c.good, warn: c.warn, bad: c.bad, neutral: c.muted2 }
export function Pill({ tone = 'neutral', dot, children }: { tone?: Tone; dot?: boolean; children: ReactNode }) {
  const col = toneColor[tone]
  return (
    <span style={{
      display: 'inline-flex', alignItems: 'center', gap: 6, fontFamily: font.mono, fontSize: 10.5,
      color: col, background: `${col}14`, border: `1px solid ${col}40`, borderRadius: 5, padding: '1px 8px',
    }}>
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
  return <Pill tone={statusTone(status)} dot>{s}</Pill>
}

export function Input(props: React.InputHTMLAttributes<HTMLInputElement> & { mono?: boolean }) {
  const { mono: m, style, ...rest } = props
  return (
    <input
      {...rest}
      style={{
        background: c.bg, border: `1px solid ${c.border2}`, borderRadius: 7, padding: '8px 11px',
        color: c.fg, fontSize: 12.5, fontFamily: m ? font.mono : font.sans, ...style,
      }}
    />
  )
}

export function tab(active: boolean): CSSProperties {
  return {
    padding: '8px 12px', fontSize: 13, fontWeight: 500, cursor: 'pointer',
    color: active ? c.fg : c.muted2,
    borderBottom: `2px solid ${active ? c.fg : 'transparent'}`, marginBottom: -1,
  }
}
export function navItem(active: boolean): CSSProperties {
  return { padding: '5px 10px', fontSize: 12.5, cursor: 'pointer', borderRadius: 6, color: active ? c.fg : c.muted, background: active ? c.panel2 : 'transparent' }
}
