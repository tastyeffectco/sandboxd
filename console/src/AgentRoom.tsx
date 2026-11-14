import { useCallback, useEffect, useRef, useState } from 'react'
import { api, AgentAttachment, AgentInstance, AgentMessageRow, AgentStep } from './api'
import { AgentMessage } from './AgentMessage'
import { Icon } from './design/Icon'
import { StatusPill } from './design/kit'

// One agent's own window: one continuous conversation, like a chat in Telegram.
// The engine keeps the thread and writes replies itself, so closing this
// window or the app never loses a message or stops a reply.

// The agent's own app (OpenClaw's Control panel) opens inside this window in
// the desktop app, on a private address only that agent's app can use. The
// engine signs the person in: no token to paste, nothing to pair.
export function agentAppUrl(agent: Pick<AgentInstance, 'id' | 'ui_label'>): string {
  if (!agent.ui_label || !window.sandboxdDesktop || !/^[0-9a-z]{6,40}$/i.test(agent.id)) return ''
  return `http://ui-${agent.id.toLowerCase()}.localhost:${location.port}/`
}

type View = 'chat' | 'app'
const viewKey = (id: string) => `sandboxd.agent-view.${id}`
function savedView(id: string): View {
  try {
    return localStorage.getItem(viewKey(id)) === 'app' ? 'app' : 'chat'
  } catch {
    return 'chat'
  }
}

// Tool names in everyday words. Internal bookkeeping tools are not shown.
const HIDDEN = /^(tool_(describe|search|call)|todo|think|clarify)$/
export function stepLabel(step: Pick<AgentStep, 'tool'>): string | null {
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
  const [messages, setMessages] = useState<AgentMessageRow[] | null>(null)
  const [files, setFiles] = useState<Pending[]>([])
  const [dragging, setDragging] = useState(false)
  const picker = useRef<HTMLInputElement>(null)
  const uploading = files.some((f) => !f.file)
  const [draft, setDraft] = useState('')
  const [busy, setBusy] = useState(false)
  const [sending, setSending] = useState(false)
  const end = useRef<HTMLDivElement>(null)
  const seen = useRef<Record<string, string>>({})
  const appUrl = agentAppUrl(agent)
  const [view, setView] = useState<View>(() => (appUrl ? savedView(agent.id) : 'chat'))
  const [appOpened, setAppOpened] = useState(view === 'app')
  const choose = (next: View) => {
    setView(next)
    if (next === 'app') setAppOpened(true)
    try {
      localStorage.setItem(viewKey(agent.id), next)
    } catch {
      /* remembered for this visit only */
    }
  }
  const running = agent.status === 'running'
  const working = sending || !!messages?.some((m) => m.state === 'working')

  const refresh = useCallback(async () => {
    try {
      const r = await api.listAgentMessages(agent.id)
      // A reply that just finished building or changing an app: show it in the sidebar.
      let appChanged = false
      for (const m of r.messages) {
        if (seen.current[m.id] === 'working' && m.state !== 'working')
          appChanged ||= (m.steps || []).some((s) => /create_app|ask_app/.test(s.tool) && s.status === 'done')
        seen.current[m.id] = m.state
      }
      if (appChanged) reloadApps()
      setMessages(r.messages)
    } catch {
      /* the next refresh tries again */
    }
  }, [agent.id, reloadApps])

  useEffect(() => {
    setMessages(null)
    seen.current = {}
    void refresh()
  }, [refresh])
  // Live while the agent writes; slower when idle (other channels add messages too).
  useEffect(() => {
    const t = setInterval(() => void refresh(), working ? 800 : 4000)
    return () => clearInterval(t)
  }, [refresh, working])
  const count = messages?.length ?? 0
  const lastLength = messages?.[count - 1]?.content.length ?? 0
  // Braces: Chromium's scrollIntoView now returns a Promise, which React would
  // take for a clean-up function.
  useEffect(() => {
    end.current?.scrollIntoView?.({ block: 'end' })
  }, [count, lastLength])

  // Attach: the paperclip, dropping files on the window, or pasting.
  const attach = (list: FileList | File[] | null) => {
    const picked = Array.from(list || [])
    if (!picked.length) return
    if (files.length + picked.length > MAX_FILES) {
      onError(`You can send up to ${MAX_FILES} files at a time.`)
      return
    }
    for (const f of picked) {
      if (f.size > MAX_FILE_BYTES) {
        onError(`${f.name} is too big. Files can be up to 25 MB.`)
        continue
      }
      if (!f.size) continue
      const key = `${Date.now()}-${Math.random()}`
      const name = f.name || (f.type.startsWith('image/') ? `pasted-image.${f.type.slice(6) || 'png'}` : 'file')
      const preview = f.type.startsWith('image/') ? URL.createObjectURL(f) : undefined
      setFiles((prev) => [...prev, { key, name, size: f.size, preview }])
      api
        .uploadAgentFile(agent.id, f, name)
        .then((file) => setFiles((prev) => prev.map((p) => (p.key === key ? { ...p, file } : p))))
        .catch((e) => {
          setFiles((prev) => prev.filter((p) => p.key !== key))
          if (preview) URL.revokeObjectURL(preview)
          onError(`${name} could not be attached: ${(e as Error).message}`)
        })
    }
  }
  const unattach = (key: string) =>
    setFiles((prev) => {
      const gone = prev.find((p) => p.key === key)
      if (gone?.preview) URL.revokeObjectURL(gone.preview)
      return prev.filter((p) => p.key !== key)
    })

  const send = async (text: string, withFiles = true) => {
    const content = text.trim()
    const attached = withFiles ? files.filter((f) => f.file) : []
    if ((!content && !attached.length) || working || !running || (withFiles && uploading)) return
    setDraft('')
    if (withFiles) setFiles([])
    setSending(true)
    try {
      const r = await api.sendAgentMessage(
        agent.id,
        content,
        attached.map((f) => f.file!.id),
      )
      for (const m of r.messages) seen.current[m.id] = m.state
      setMessages((prev) => [...(prev || []), ...r.messages])
      for (const f of attached) if (f.preview) URL.revokeObjectURL(f.preview)
    } catch (e) {
      setDraft(content)
      if (withFiles) setFiles(attached)
      onError((e as Error).message)
    } finally {
      setSending(false)
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

  const lastUser = [...(messages || [])].reverse().find((m) => m.role === 'user')
  const initial = agent.name.slice(0, 1).toUpperCase()
  return (
    <section
      className={dragging ? 'agent-room dragging' : 'agent-room'}
      data-testid="agent-room"
      onDragOver={(e) => {
        if (view !== 'chat' || !running || !e.dataTransfer.types.includes('Files')) return
        e.preventDefault()
        setDragging(true)
      }}
      onDragLeave={(e) => {
        if (e.currentTarget === e.target) setDragging(false)
      }}
      onDrop={(e) => {
        if (view !== 'chat' || !running || !e.dataTransfer.files.length) return
        e.preventDefault()
        setDragging(false)
        attach(e.dataTransfer.files)
      }}
    >
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
            className="text-button"
            disabled={busy || working || !count}
            onClick={() => {
              if (window.confirm(`Clear this conversation? ${agent.name} keeps what it has learned about you.`))
                void act(async () => {
                  await api.clearAgentMessages(agent.id)
                  await refresh()
                })
            }}
            data-testid="agent-clear"
          >
            Clear conversation
          </button>
          <button
            className="text-button danger"
            disabled={busy}
            onClick={() => {
              if (window.confirm(`Remove ${agent.name}? Its memory, skills and this conversation are deleted. Your apps stay.`))
                void act(async () => {
                  await api.deleteAgentInstance(agent.id)
                  onRemoved()
                })
            }}
          >
            Remove
          </button>
        </div>
      </header>

      {appUrl && (
        <nav className="agent-views" role="tablist" aria-label={`${agent.name} views`}>
          <button role="tab" aria-selected={view === 'chat'} className={view === 'chat' ? 'active' : ''} onClick={() => choose('chat')} data-testid="agent-view-chat">
            <Icon name="spark" size={14} /> Chat
          </button>
          <button role="tab" aria-selected={view === 'app'} className={view === 'app' ? 'active' : ''} onClick={() => choose('app')} data-testid="agent-view-app">
            <Icon name="grid" size={14} /> {agent.ui_label}
          </button>
        </nav>
      )}

      {appUrl && appOpened && (
        <div className="agent-app" hidden={view !== 'app'} data-testid="agent-app">
          {running ? (
            <iframe
              src={appUrl}
              title={`${agent.name} ${agent.ui_label}`}
              sandbox="allow-scripts allow-same-origin allow-forms allow-popups allow-downloads allow-modals"
              allow="clipboard-read; clipboard-write"
            />
          ) : (
            <p className="bubble-note" style={{ margin: 'auto' }}>
              {agent.status === 'starting'
                ? `${agent.name} is getting ready… its ${agent.ui_label} opens in about a minute.`
                : `${agent.name} is paused. Start it to open its ${agent.ui_label}.`}
            </p>
          )}
        </div>
      )}

      <div className="agent-room-log" hidden={view !== 'chat'} role="log" aria-live="polite" data-testid="agent-log">
        {messages === null && <p className="bubble-note" style={{ margin: 'auto' }}>Loading the conversation…</p>}
        {messages?.length === 0 && (
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
        {messages?.map((m, i) =>
          m.role === 'user' ? (
            <div key={m.id} className="bubble-row mine">
              <div className="bubble mine" data-testid="agent-you">
                {(m.attachments || []).length > 0 && (
                  <div className="bubble-files" data-testid="agent-files">
                    {(m.attachments || []).map((f) => (
                      <AttachedFile key={f.id} agentId={agent.id} file={f} />
                    ))}
                  </div>
                )}
                {m.content}
              </div>
            </div>
          ) : (
            <div key={m.id} className="bubble-row">
              <span className="agent-avatar small" aria-hidden="true">
                {initial}
              </span>
              <div className="bubble theirs" data-testid="agent-reply">
                {m.thinking && (
                  <details className="bubble-thinking">
                    <summary>{m.state === 'working' && !m.content ? 'Thinking…' : 'How it thought about this'}</summary>
                    <p>{m.thinking}</p>
                  </details>
                )}
                {(m.steps || []).some((s) => stepLabel(s)) && (
                  <ul className="bubble-steps" data-testid="agent-steps">
                    {(m.steps || [])
                      .filter((s) => stepLabel(s))
                      .map((s, k) => (
                        <li key={s.id || k} className={s.status}>
                          {s.status === 'running' ? (
                            <span className="spinner" aria-label="working" />
                          ) : s.status === 'done' ? (
                            <Icon name="check" size={12} />
                          ) : (
                            <Icon name="close" size={12} />
                          )}
                          {stepLabel(s)}
                        </li>
                      ))}
                  </ul>
                )}
                {m.content ? (
                  <div className="bubble-text">
                    <AgentMessage text={m.content} />
                  </div>
                ) : (
                  m.state === 'working' &&
                  !m.thinking &&
                  !(m.steps || []).length && (
                    <span className="typing" aria-label={`${agent.name} is typing`}>
                      <i />
                      <i />
                      <i />
                    </span>
                  )
                )}
                {m.state !== 'working' && links(m.content).length > 0 && (
                  <div className="bubble-links">
                    {links(m.content).map((url) => (
                      <a key={url} href={url} target="_blank" rel="noreferrer" data-testid="agent-link">
                        <Icon name="external" size={13} /> {linkTitle(url)}
                      </a>
                    ))}
                  </div>
                )}
                {m.state === 'stopped' && <p className="bubble-note">Stopped.</p>}
                {m.state === 'error' && (
                  <p className="bubble-note error" role="alert">
                    {m.error || `${agent.name} couldn't answer.`}{' '}
                    {lastUser && i === messages.length - 1 && running && (
                      <button className="text-button" onClick={() => void send(lastUser.content, false)}>
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

      {files.length > 0 && (
        <div className="agent-attachments" hidden={view !== 'chat'} data-testid="agent-attachments">
          {files.map((f) => (
            <span key={f.key} className={f.file ? 'attachment' : 'attachment uploading'}>
              {f.preview ? <img src={f.preview} alt="" /> : <Icon name="book" size={14} />}
              <span className="attachment-name">{f.name}</span>
              <small>{f.file ? fileSize(f.size) : 'Attaching…'}</small>
              <button type="button" aria-label={`Remove ${f.name}`} onClick={() => unattach(f.key)}>
                <Icon name="close" size={12} />
              </button>
            </span>
          ))}
        </div>
      )}

      <form
        hidden={view !== 'chat'}
        className="agent-composer"
        onSubmit={(e) => {
          e.preventDefault()
          void send(draft)
        }}
      >
        <input
          ref={picker}
          type="file"
          multiple
          hidden
          onChange={(e) => {
            attach(e.target.files)
            e.target.value = ''
          }}
          data-testid="agent-file-input"
        />
        <button
          type="button"
          className="composer-attach"
          disabled={!running}
          onClick={() => picker.current?.click()}
          title="Attach files"
          data-testid="agent-attach"
        >
          <Icon name="plus" size={18} />
          <span className="sr-only">Attach files</span>
        </button>
        <textarea
          value={draft}
          rows={1}
          onPaste={(e) => {
            if (e.clipboardData.files.length) {
              e.preventDefault()
              attach(e.clipboardData.files)
            }
          }}
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
          <button
            type="button"
            className="composer-send stop"
            onClick={() => void api.stopAgentMessage(agent.id).then(refresh)}
            data-testid="agent-stop"
          >
            Stop
          </button>
        ) : (
          <button
            type="submit"
            className="composer-send"
            disabled={!running || uploading || (!draft.trim() && !files.length)}
            data-testid="agent-send"
          >
            <Icon name="arrow" size={16} />
            <span className="sr-only">Send</span>
          </button>
        )}
      </form>
    </section>
  )
}

const MAX_FILES = 10
const MAX_FILE_BYTES = 25 * 1024 * 1024
type Pending = { key: string; name: string; size: number; preview?: string; file?: AgentAttachment }

export function fileSize(n: number): string {
  if (n >= 1024 * 1024) return `${(n / 1024 / 1024).toFixed(1)} MB`
  if (n >= 1024) return `${Math.round(n / 1024)} KB`
  return `${n} bytes`
}

// A file in the conversation: pictures show, anything else downloads.
function AttachedFile({ agentId, file }: { agentId: string; file: AgentAttachment }) {
  const url = api.agentFileUrl(agentId, file.id)
  if (file.type.startsWith('image/') && file.type !== 'image/svg+xml')
    return (
      <a href={url} download={file.name} className="bubble-image" title={file.name}>
        <img src={url} alt={file.name} loading="lazy" />
      </a>
    )
  return (
    <a href={url} download={file.name} className="bubble-file">
      <Icon name="book" size={14} />
      <span>{file.name}</span>
      <small>{fileSize(file.size)}</small>
    </a>
  )
}
