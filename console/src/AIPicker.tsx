import type React from 'react'
import { useEffect, useRef, useState } from 'react'
import type { Agent, ModelProvider } from './api'
import type { AgentChoice } from './AgentPicker'
import { aiOptions, describeChoice, type AIOption } from './aiOptions'
import { Icon } from './design/Icon'

const GROUPS: { id: AIOption['group']; title: string }[] = [
  { id: 'free', title: 'Free' },
  { id: 'connections', title: 'Your connections' },
  { id: 'subscriptions', title: 'Your subscriptions' },
]

// The AI a chat uses, in one button: "AI · Free AI ▾". Opening it lists every
// AI the person has, in plain words, and a way to connect another one. The
// engine's agent / connection / model pickers stay under "More options".
export function AIPicker({
  value,
  onChange,
  agents,
  providers,
  disabled,
  onConnect,
  more,
}: {
  value: AgentChoice
  onChange: (choice: AgentChoice) => void
  agents: Agent[]
  providers: ModelProvider[]
  disabled?: boolean
  // Settings → AI, to connect OpenRouter, CometAPI or a subscription.
  onConnect?: () => void
  // The detailed pickers, for people who want them.
  more?: React.ReactNode
}) {
  const [open, setOpen] = useState(false)
  const [showMore, setShowMore] = useState(false)
  const box = useRef<HTMLDivElement>(null)
  const options = aiOptions(agents, providers)
  const current = describeChoice(value, options)
  useEffect(() => {
    if (!open) return
    const away = (e: MouseEvent) => !box.current?.contains(e.target as Node) && setOpen(false)
    const esc = (e: KeyboardEvent) => e.key === 'Escape' && setOpen(false)
    document.addEventListener('mousedown', away)
    document.addEventListener('keydown', esc)
    return () => {
      document.removeEventListener('mousedown', away)
      document.removeEventListener('keydown', esc)
    }
  }, [open])
  const same = (o: AIOption) =>
    o.choice.agent === value.agent &&
    (o.choice.provider || '') === (value.provider || '') &&
    (o.choice.model || '') === (value.model || '')
  return (
    <div className="ai-picker" ref={box}>
      <button
        type="button"
        className="ai-chip"
        aria-haspopup="listbox"
        aria-expanded={open}
        disabled={disabled}
        onClick={() => setOpen((o) => !o)}
        data-testid="ai-chip"
        title={current.detail}
      >
        <Icon name="spark" size={14} />
        <span className="ai-chip-label">
          <small>AI</small>
          <strong>{current.label}</strong>
        </span>
        <Icon name="chevron" size={12} />
      </button>
      {open && (
        <div className="ai-menu" role="listbox" aria-label="Choose the AI" data-testid="ai-menu">
          {GROUPS.map((g) => {
            const list = options.filter((o) => o.group === g.id)
            if (!list.length) return null
            return (
              <div className="ai-group" key={g.id}>
                <p>{g.title}</p>
                {list.map((o) => (
                  <button
                    key={o.key}
                    type="button"
                    role="option"
                    aria-selected={same(o)}
                    className={same(o) ? 'ai-option active' : 'ai-option'}
                    onClick={() => {
                      onChange(o.choice)
                      setOpen(false)
                    }}
                    data-testid={`ai-option-${o.key}`}
                  >
                    <span>
                      <strong>{o.label}</strong>
                      <small>{o.detail}</small>
                    </span>
                    {same(o) && <Icon name="check" size={14} />}
                  </button>
                ))}
              </div>
            )
          })}
          <div className="ai-menu-footer">
            {onConnect && (
              <button
                type="button"
                className="text-button"
                onClick={() => {
                  setOpen(false)
                  onConnect()
                }}
                data-testid="ai-connect"
              >
                <Icon name="plus" size={12} /> Connect another AI
              </button>
            )}
            {more && (
              <button type="button" className="text-button" onClick={() => setShowMore((m) => !m)}>
                {showMore ? 'Fewer options' : 'More options'}
              </button>
            )}
          </div>
          {showMore && more && <div className="ai-more">{more}</div>}
        </div>
      )}
    </div>
  )
}
