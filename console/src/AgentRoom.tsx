import { useCallback, useEffect, useRef, useState } from 'react'
import { api, AgentInstance, AgentStreamEvent, streamAgentChat } from './api'
import { AgentMessage } from './AgentMessage'
import { Icon } from './design/Icon'
import { StatusPill } from './design/kit'

// One agent's own window: a chat like Telegram or Slack. Every agent uses this
// screen; the engine's adapter turns each agent's stream into the same events.

export type Step = { id: string; tool: string; label?: string; status: 'running' | 'done' | 'failed' }
export type Turn =
  | { role: 'user'; content: string; at: number }
  | {
      role: 'assistant'
      content: string
      thinking: string
      steps: Step[]
      at: number
      state: 'streaming' | 'done' | 'stopped' | 'error'
      error?: string
    }
type Saved = { conversation: string; turns: Turn[] }

const storageKey = (id: string) => `sandboxd.agent-chat.${id}`
const newConversation = () =>
  globalThis.crypto?.randomUUID?.() ?? `c-${Date.now()}-${Math.random().toString(36).slice(2)}`

function load(id: string): Saved {
  try {
    const raw = localStorage.getItem(storageKey(id))
    if (raw) {
      const saved = JSON.parse(raw) as Saved
      // A reply cut off by a reload is shown as stopped, not as still typing.
      saved.turns = saved.turns.map((t) => (t.role === 'assistant' && t.state === 'streaming' ? { ...t, state: 'stopped' } : t))
      return saved
    }
  } catch {
    /* storage unavailable: start fresh */
  }
  return { conversation: newConversation(), turns: [] }
}
function save(id: string, saved: Saved) {
  try {
    localStorage.setItem(storageKey(id), JSON.stringify({ ...saved, turns: saved.turns.slice(-200) }))
  } catch {
    /* storage full or unavailable */
  }
}

// Tool names in everyday words. Internal bookkeeping tools are not shown.
const HIDDEN = /^(tool_(describe|search|call)|todo|think|clarify)$/
export function stepLabel(step: Pick<Step, 'tool'>): string | null {
  const tool = step.tool.replace(/^mcp__/, '').replace(/^(sandboxd__|mcp:sandboxd:sandboxd__)/, '').replace(/^sandboxd__/, '')
  if (HIDDEN.test(tool)) return null
  const known: [RegExp, string][] = [
    [/create_app/, 'Creating your app'],
    [/ask_app/, 'Asking the app’s builder for a change'],
    [/list_apps/, 'Looking at your apps'],
    [/app_status/, 'Checking your app'],
    [/web_search|search_web|websearch/, 'Searching the web'],
    [/web_fetch|fetch_url|browser|browse|web_extract/, 'Reading a web page'],
    [/terminal|exec|shell|process|run_command|bash/, 'Running a command'],
    [/read_file|file_read|read$/, 'Reading a file'],
    [/write_file|file_write|edit|patch|apply_patch|write$/, 'Writing a file'],
    [/memory/, 'Updating its memory'],
    [/skill/, 'Using a skill'],
    [/cron|schedule/, 'Scheduling a task'],
    [/image/, 'Working on an image'],
    [/send_message|message/, 'Sending a message'],
  ]
  for (const [pattern, label] of known) if (pattern.test(tool)) return label
  const words = tool.replace(/[_.:-]+/g, ' ').trim()
  return words ? words[0].toUpperCase() + words.slice(1) : null
}

const LINK = /https?:\/\/[^\s<>()[\]"'`]+[^\s<>()[\]"'`.,;:!?]/g
function links(text: string) {
  return [...new Set(text.match(LINK) || [])].slice(0, 4)
}
function linkTitle(url: string) {
  try {
    const u = new URL(url)
    if (/\.preview\.localhost$/.test(u.hostname) || /^s-[0-9a-z]+-\d+\./i.test(u.hostname)) return 'Open the app preview'
    return u.hostname.replace(/^www\./, '')
  } catch {
    return url
  }
}

export function AgentRoom({
  agent,
  onChanged,
  onRemoved,
  onError,
  reloadApps,
}: {
  agent: AgentInstance
  onChanged: () => void
  onRemoved: () => void
  onError: (message: string) => void
  reloadApps: () => void
}) {
  const [saved, setSaved] = useState<Saved>(() => load(agent.id))
  const [draft, setDraft] = useState('')
  const [busy, setBusy] = useState(false)
  const abort = useRef<AbortController | null>(null)
  const end = useRef<HTMLDivElement>(null)
  const running = agent.status === 'running'
  const working = saved.turns.some((t) => t.role === 'assistant' && t.state === 'streaming')

  useEffect(() => {
    setSaved(load(agent.id))
  }, [agent.id])
  useEffect(() => {
    save(agent.id, saved)
  }, [agent.id, saved])
  // Braces: Chromium's scrollIntoView now returns a Promise, which React would
  // take for a clean-up function.
  useEffect(() => {
    end.current?.scrollIntoView?.({ block: 'end' })
  }, [saved.turns])
  useEffect(() => () => abort.current?.abort(), [agent.id])

  const update = useCallback((fn: (last: Extract<Turn, { role: 'assistant' }>) => Turn) => {
    setSaved((s) => {
      const turns = [...s.turns]
      const last = turns[turns.length - 1]
      if (last?.role === 'assistant') turns[turns.length - 1] = fn(last)
      return { ...s, turns }
    })
  }, [])

  const send = async (text: string) => {
    const content = text.trim()
    if (!content || working || !running) return
    setDraft('')
    const history = saved.turns
      .filter((t) => t.role === 'user' || (t.state === 'done' && t.content))
      .map((t) => ({ role: t.role, content: t.content }))
    const messages = [...history, { role: 'user' as const, content }]
    setSaved((s) => ({
      ...s,
      turns: [
        ...s.turns,
        { role: 'user', content, at: Date.now() },
        { role: 'assistant', content: '', thinking: '', steps: [], at: Date.now(), state: 'streaming' },
      ],
    }))
    const controller = new AbortController()
    abort.current = controller
    let failed = ''
    let createdApp = false
    try {
      await streamAgentChat(
        agent.id,
        { messages, conversation_id: saved.conversation },
        (e: AgentStreamEvent) => {
          if (e.type === 'text') update((t) => ({ ...t, content: t.content + e.delta }))
          else if (e.type === 'thinking') update((t) => ({ ...t, thinking: t.thinking + e.delta }))
          else if (e.type === 'step') {
            if (/create_app|ask_app/.test(e.tool) && e.status === 'done') createdApp = true
            update((t) => {
              const steps = [...t.steps]
              const i = steps.findIndex((s) => s.id === e.id && e.id)
              const step = { id: e.id || `${e.tool}-${steps.length}`, tool: e.tool, label: e.label, status: e.status }
              if (i >= 0) steps[i] = { ...steps[i], status: e.status }
              else steps.push(step)
              return { ...t, steps }
            })
          } else if (e.type === 'error') failed = e.message
        },
        controller.signal,
      )
      update((t) => ({
        ...t,
        state: failed ? 'error' : 'done',
        error: failed || undefined,
        steps: t.steps.map((s) => (s.status === 'running' ? { ...s, status: failed ? 'failed' : 'done' } : s)),
      }))
    } catch (e) {
      const stopped = controller.signal.aborted
      update((t) => ({
        ...t,
        state: stopped ? 'stopped' : 'error',
        error: stopped ? undefined : (e as Error).message,
        steps: t.steps.map((s) => (s.status === 'running' ? { ...s, status: 'failed' } : s)),
      }))
    } finally {
      abort.current = null
      if (createdApp) reloadApps()
    }
  }

  const act = async (fn: () => Promise<unknown>) => {
    setBusy(true)
    try {
      await fn()
      onChanged()
    } catch (e) {
      onError((e as Error).message)
    } finally {
      setBusy(false)
    }
  }

  const lastUser = [...saved.turns].reverse().find((t) => t.role === 'user')
  const initial = agent.name.slice(0, 1).toUpperCase()
  return (
    <section className="agent-room" data-testid="agent-room">
      <header className="agent-room-header">
        <span className="agent-avatar" aria-hidden="true">
          {initial}
        </span>
        <div className="agent-room-title">
          <h1>{agent.name}</h1>
          <span>
            <StatusPill status={agent.status === 'missing' ? 'not started' : agent.status} />
            <small title="AI model">{agent.model}</small>
          </span>
        </div>
        <div className="agent-room-actions">
          <button
            className="text-button"
            disabled={working || !saved.turns.length}
            onClick={() => setSaved({ conversation: newConversation(), turns: [] })}
            data-testid="agent-new-chat"
          >
            <Icon name="plus" size={14} /> New chat
          </button>
          {running || agent.status === 'starting' ? (
            <button className="text-button" disabled={busy} onClick={() => void act(() => api.stopAgentInstance(agent.id))}>
              Pause
            </button>
          ) : (
            <button className="text-button" disabled={busy} onClick={() => void act(() => api.startAgentInstance(agent.id))}>
              Start
            </button>
          )}
          <button
            className="text-button danger"
            disabled={busy}
            onClick={() => {
              if (window.confirm(`Remove ${agent.name}? Its memory and skills are deleted. Your apps stay.`))
                void act(async () => {
                  await api.deleteAgentInstance(agent.id)
                  try {
                    localStorage.removeItem(storageKey(agent.id))
                  } catch {
                    /* ignore */
                  }
                  onRemoved()
                })
            }}
          >
            Remove
          </button>
        </div>
      </header>

      <div className="agent-room-log" role="log" aria-live="polite" data-testid="agent-log">
        {!saved.turns.length && (
          <div className="agent-room-empty">
            <span className="agent-avatar large" aria-hidden="true">
              {initial}
            </span>
            <h2>Chat with {agent.name}</h2>
            <p>It remembers what you tell it, uses tools, and can build and change apps on this computer.</p>
            <div className="agent-suggestions">
              {['What can you do for me?', 'Build me a recipe app with a shopping list', 'Which apps do I have?'].map((s) => (
                <button key={s} disabled={!running} onClick={() => void send(s)} data-testid="agent-suggestion">
                  {s}
                </button>
              ))}
            </div>
          </div>
        )}
        {saved.turns.map((t, i) =>
          t.role === 'user' ? (
            <div key={i} className="bubble-row mine">
              <div className="bubble mine" data-testid="agent-you">
                {t.content}
              </div>
            </div>
          ) : (
            <div key={i} className="bubble-row">
              <span className="agent-avatar small" aria-hidden="true">
                {initial}
              </span>
              <div className="bubble theirs" data-testid="agent-reply">
                {t.thinking && (
                  <details className="bubble-thinking">
                    <summary>{t.state === 'streaming' && !t.content ? 'Thinking…' : 'How it thought about this'}</summary>
                    <p>{t.thinking}</p>
                  </details>
                )}
                {t.steps.some((s) => stepLabel(s)) && (
                  <ul className="bubble-steps" data-testid="agent-steps">
                    {t.steps
                      .filter((s) => stepLabel(s))
                      .map((s) => (
                        <li key={s.id} className={s.status}>
                          {s.status === 'running' ? <span className="spinner" aria-label="working" /> : s.status === 'done' ? <Icon name="check" size={12} /> : <Icon name="close" size={12} />}
                          {stepLabel(s)}
                        </li>
                      ))}
                  </ul>
                )}
                {t.content ? (
                  <div className="bubble-text">
                    <AgentMessage text={t.content} />
                  </div>
                ) : (
                  t.state === 'streaming' && !t.thinking && !t.steps.length && <span className="typing" aria-label={`${agent.name} is typing`}><i /><i /><i /></span>
                )}
                {links(t.content).length > 0 && t.state !== 'streaming' && (
                  <div className="bubble-links">
                    {links(t.content).map((url) => (
                      <a key={url} href={url} target="_blank" rel="noreferrer" data-testid="agent-link">
                        <Icon name="external" size={13} /> {linkTitle(url)}
                      </a>
                    ))}
                  </div>
                )}
                {t.state === 'stopped' && <p className="bubble-note">Stopped.</p>}
                {t.state === 'error' && (
                  <p className="bubble-note error" role="alert">
                    {t.error || `${agent.name} couldn't answer.`}{' '}
                    {lastUser && i === saved.turns.length - 1 && (
                      <button
                        className="text-button"
                        onClick={() => {
                          setSaved((s) => ({ ...s, turns: s.turns.slice(0, -2) }))
                          void send(lastUser.content)
                        }}
                      >
                        Try again
                      </button>
                    )}
                  </p>
                )}
              </div>
            </div>
          ),
        )}
        <div ref={end} />
      </div>

      <form
        className="agent-composer"
        onSubmit={(e) => {
          e.preventDefault()
          void send(draft)
        }}
      >
        <textarea
          value={draft}
          rows={1}
          onChange={(e) => setDraft(e.target.value)}
          onKeyDown={(e) => {
            if (e.key === 'Enter' && !e.shiftKey && !e.nativeEvent.isComposing) {
              e.preventDefault()
              void send(draft)
            }
          }}
          placeholder={
            running
              ? `Message ${agent.name}`
              : agent.status === 'starting'
                ? `${agent.name} is getting ready… this takes about a minute`
                : `${agent.name} is paused. Start it to chat.`
          }
          disabled={!running}
          maxLength={8000}
          data-testid="agent-input"
        />
        {working ? (
          <button type="button" className="composer-send stop" onClick={() => abort.current?.abort()} data-testid="agent-stop">
            Stop
          </button>
        ) : (
          <button type="submit" className="composer-send" disabled={!running || !draft.trim()} data-testid="agent-send">
            <Icon name="arrow" size={16} />
            <span className="sr-only">Send</span>
          </button>
        )}
      </form>
    </section>
  )
}
