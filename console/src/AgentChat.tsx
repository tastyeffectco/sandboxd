import { useCallback, useEffect, useRef, useState } from 'react'
import { api, Agent, App as TApp, ModelProvider, Sandbox, TaskSummary } from './api'
import { AgentPicker, BuildIntent, choiceBlocked, defaultChoice } from './AgentPicker'
import { ClaudeSignIn } from './ClaudeSignIn'
import { Btn } from './design/kit'
import { Icon } from './design/Icon'
import { Bot } from './design/Bot'
import { Avatar } from './design/Avatar'
import { ProjectIcon } from './design/ProjectIcon'
import { catalogIdOf } from './catalog'
import { AgentMessage } from './AgentMessage'
import { Dialog } from './design/Dialog'
import { attachmentPath, fileSize, MAX_FILE_BYTES, MAX_FILES, splitAttachments, withAttachments } from './chatAttachments'

// The workspace runs but its task runner is not answering yet (just started).
const notReadyYet = (e: unknown) =>
  (e as { status?: number }).status === 502 && /^runtimed:/.test((e as Error).message || '')
const terminal = (status: string) => ['succeeded', 'failed', 'cancelled', 'error'].includes(status)

export function AgentChat({
  sb,
  onError,
  toast,
  refresh,
  initialBuild,
  consumeBuild,
  goSettings,
  ready = true,
  draft,
  saveDraft,
  assistant,
  onOpenApp,
  onAppsChanged,
}: {
  // The sandboxd agent's chat: its own words and suggestions.
  assistant?: boolean
  // sandboxd agent: open an app it made, and tell the sidebar about it.
  onOpenApp?: (id: string) => void
  onAppsChanged?: () => void
  sb: Sandbox | null
  onError: (m: string) => void
  toast: (m: string) => void
  refresh: () => void
  initialBuild?: BuildIntent
  consumeBuild?: () => void
  goSettings: () => void
  ready?: boolean
  draft?: BuildIntent
  saveDraft?: (draft: BuildIntent) => void
}) {
  const [choice, setChoice] = useState(initialBuild || draft || defaultChoice)
  const [providers, setProviders] = useState<ModelProvider[]>([])
  const [agents, setAgents] = useState<Agent[]>([])
  const [loaded, setLoaded] = useState(false)
  const [text, setText] = useState(initialBuild?.prompt || draft?.prompt || '')
  const [history, setHistory] = useState<TaskSummary[]>([])
  const [active, setActive] = useState<TaskSummary | null>(null)
  const [response, setResponse] = useState('')
  const [resolved, setResolved] = useState('')
  const [tool, setTool] = useState('')
  const [cont, setCont] = useState(true)
  const [error, setError] = useState('')
  const [revertTarget, setRevertTarget] = useState<TaskSummary | null>(null)
  const [reverting, setReverting] = useState(false)
  // Attached files: uploaded into the project as soon as they are picked.
  const [files, setFiles] = useState<{ key: string; name: string; size: number; path?: string; preview?: string }[]>(
    [],
  )
  const picker = useRef<HTMLInputElement>(null)
  const uploading = files.some((f) => !f.path)
  const submitting = useRef(false)
  const initialSent = useRef(false)
  const saveDraftRef = useRef(saveDraft)
  saveDraftRef.current = saveDraft
  useEffect(() => {
    saveDraftRef.current?.({ ...choice, prompt: text })
  }, [choice, text])
  const scroll = useRef<HTMLDivElement>(null)
  // Apps the sandboxd agent made in this visit, shown as cards to open.
  const known = useRef<Set<string> | null>(null)
  const [made, setMade] = useState<TApp[]>([])
  const sbID = sb?.id
  const currentSandbox = useRef(sbID)
  currentSandbox.current = sbID
  useEffect(() => {
    setHistory([])
    setActive(null)
    setResponse('')
    setResolved('')
    setTool('')
  }, [sbID])
  const sandboxRunning = sb?.status === 'running' && ready
  // A resting project wakes when a task is sent, so sending stays available.
  const canSend = sandboxRunning || sb?.status === 'stopped'
  // Without a Claude API key, Claude Code uses a Claude subscription signed in
  // inside this project.
  const claudeSignIn =
    choice.agent === 'claude-code' &&
    !agents.some((a) => a.id === 'claude-code' && a.status === 'connected' && a.method === 'api_key')
  const [claudeSignedIn, setClaudeSignedIn] = useState(false)
  const blocked = choiceBlocked(choice, providers, agents) || (claudeSignIn && !claudeSignedIn)
  const loadOptions = useCallback(() => {
    Promise.all([api.getAgents(), api.listModelProviders()])
      .then(([a, p]) => {
        setAgents(a)
        setProviders(p)
        setLoaded(true)
      })
      .catch((e) => setError((e as Error).message))
  }, [])
  useEffect(loadOptions, [loadOptions])

  const loadHistory = useCallback(async () => {
    if (!sbID) return
    try {
      const tasks = await api.listTasks(sbID)
      if (currentSandbox.current !== sbID) return
      setHistory([...tasks].reverse())
      const inFlight = tasks.find((t) => !terminal(t.status))
      setActive((current) => {
        if (inFlight) return inFlight
        if (current && tasks.some((t) => t.id === current.id && terminal(t.status))) return null
        return current
      })
    } catch {
      /* Preserve the draft and current task across a transient outage. */
    }
  }, [sbID])
  useEffect(() => {
    void loadHistory()
    const timer = setInterval(() => {
      void loadHistory()
    }, 2500)
    return () => clearInterval(timer)
  }, [loadHistory])
  useEffect(() => {
    if (!sbID || !active?.id) return
    setResponse('')
    setResolved('')
    setTool('')
    let output = ''
    const source = new EventSource(api.taskEventsURL(sbID, active.id))
    source.addEventListener('message', (e) => {
      try {
        const m = JSON.parse((e as MessageEvent).data)
        if (m.role === 'agent' && m.text) {
          output += m.text
          setResponse(output)
        }
      } catch {
        /* invalid event */
      }
    })
    source.addEventListener('status', (e) => {
      try {
        const m = JSON.parse((e as MessageEvent).data)
        if (m.model) setResolved(m.model)
      } catch {
        /* invalid event */
      }
    })
    source.addEventListener('tool', (e) => {
      try {
        const m = JSON.parse((e as MessageEvent).data)
        setTool(`${m.name || 'Tool'}${m.path ? ` · ${m.path}` : ''}`)
      } catch {
        /* invalid event */
      }
    })
    source.addEventListener('done', () => {
      source.close()
      void loadHistory()
      refresh()
    })
    // Durable task polling remains active if streaming disconnects.
    source.onerror = () => source.close()
    return () => source.close()
  }, [sbID, active?.id, loadHistory, refresh])
  // While the sandboxd agent works, notice the apps it creates.
  const working = !!active
  useEffect(() => {
    if (!assistant) return
    const look = async () => {
      if (!known.current) return
      try {
        const list = await api.listApps()
        const fresh = list.filter((a) => !known.current!.has(a.id) && !(a.tags || []).includes('sandboxd:assistant'))
        if (!fresh.length) return
        for (const a of fresh) known.current.add(a.id)
        setMade((m) => [...m, ...fresh])
        onAppsChanged?.()
      } catch {
        /* the next look tries again */
      }
    }
    if (!working) {
      void look()
      return
    }
    const t = setInterval(() => void look(), 3000)
    return () => clearInterval(t)
  }, [assistant, working, onAppsChanged])

  // Stay at the end of the conversation while the person is there (a long
  // history, a reply growing as it renders); not when they scrolled up to read.
  const following = useRef(true)
  const [behind, setBehind] = useState(false)
  const toEnd = useCallback(() => {
    const el = scroll.current
    if (el) el.scrollTop = el.scrollHeight
  }, [])
  useEffect(() => {
    if (following.current) toEnd()
  }, [response, history.length, active?.id, toEnd])
  // A new conversation (another workspace) opens at its end.
  useEffect(() => {
    following.current = true
    setBehind(false)
  }, [sbID])
  useEffect(() => {
    const el = scroll.current
    if (!el || typeof MutationObserver === 'undefined') return
    const observer = new MutationObserver(() => {
      if (following.current) toEnd()
    })
    observer.observe(el, { childList: true, subtree: true, characterData: true })
    // Images and code blocks change size after they are added.
    const resize = typeof ResizeObserver === 'undefined' ? null : new ResizeObserver(() => following.current && toEnd())
    for (const child of Array.from(el.children)) resize?.observe(child)
    return () => {
      observer.disconnect()
      resize?.disconnect()
    }
  }, [toEnd, history.length])
  const onScroll = () => {
    const el = scroll.current
    if (!el) return
    following.current = el.scrollHeight - el.scrollTop - el.clientHeight < 80
    setBehind(!following.current)
  }
  // Attach: the paperclip, dropping files on the chat, or pasting.
  const attach = (list: FileList | File[] | null) => {
    const picked = Array.from(list || [])
    if (!picked.length || !sbID) return
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
      const path = attachmentPath(name)
      setFiles((prev) => [...prev, { key, name, size: f.size, preview }])
      api
        .putWorkspaceBlob(sbID, path, f)
        .then(() => setFiles((prev) => prev.map((p) => (p.key === key ? { ...p, path } : p))))
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

  const send = async (auto = false) => {
    if (!sbID || !canSend || (!text.trim() && !files.length) || uploading || active || blocked || !loaded || submitting.current)
      return
    submitting.current = true
    const attached = auto ? [] : files.filter((f) => f.path)
    const prompt = withAttachments(
      text,
      attached.map((f) => f.path!),
      assistant ? 'your workspace' : 'this project',
    )
    if (!auto) {
      for (const f of attached) if (f.preview) URL.revokeObjectURL(f.preview)
      setFiles([])
    }
    if (auto) {
      initialSent.current = true
      consumeBuild?.()
    }
    setError('')
    setText('')
    setResponse('')
    setActive({ id: '', prompt, status: 'starting', can_revert: false })
    if (assistant && !known.current)
      void api
        .listApps()
        .then((list) => (known.current ??= new Set(list.map((a) => a.id))))
        .catch(() => {})
    const submit = () =>
      api.submitTask(
        sbID,
        prompt,
        choice.agent,
        choice.model || undefined,
        auto ? false : cont,
        choice.provider || undefined,
      )
    try {
      let task
      // A workspace that has just started takes a few seconds before it
      // accepts work: wait for it quietly instead of failing the message.
      for (let waited = 0; ; waited += 2) {
        try {
          task = await submit()
          break
        } catch (e) {
          if (!notReadyYet(e) || waited >= 60 || currentSandbox.current !== sbID) throw e
          setTool(assistant ? 'sandboxd is waking up…' : 'Your workspace is waking up…')
          await new Promise((r) => setTimeout(r, 2000))
        }
      }
      setTool('')
      setActive({ id: task.id, prompt, status: 'running', can_revert: false })
      void loadHistory()
    } catch (e) {
      setTool('')
      setText(splitAttachments(prompt).text)
      setActive(null)
      setError(
        notReadyYet(e)
          ? `${assistant ? 'sandboxd' : 'Your workspace'} did not wake up. Your message is kept: try again in a moment, or restart the app.`
          : (e as Error).message,
      )
    } finally {
      submitting.current = false
    }
  }
  useEffect(() => {
    if (initialBuild && !initialSent.current && loaded && sandboxRunning && !blocked && !active)
      void send(true)
  }, [initialBuild, loaded, sandboxRunning, blocked, active]) // send is guarded synchronously against duplicate submissions.

  const cancel = async () => {
    if (!sbID || !active?.id) return
    try {
      await api.cancelTask(sbID, active.id)
      toast('Stopping the agent…')
      void loadHistory()
    } catch (e) {
      setError((e as Error).message)
    }
  }
  const revert = async () => {
    if (!sbID || !revertTarget) return
    setReverting(true)
    try {
      await api.revertTask(sbID, revertTarget.id)
      toast('Workspace restored')
      setRevertTarget(null)
      refresh()
      await loadHistory()
    } catch (e) {
      onError((e as Error).message)
    } finally {
      setReverting(false)
    }
  }
  return (
    <section className="agent-chat" aria-label="Build conversation">
      <header className="chat-heading">
        <span className="agent-avatar">
          <Avatar who={assistant ? 'sandboxd' : choice.agent || 'opencode'} mood={active ? 'working' : 'idle'} size={42} />
        </span>
        <div>
          <strong>
            {assistant ? 'Your sandboxd agent, on' : 'Build with'}{' '}
            {agentName(choice.agent)}
          </strong>
          <span>{active ? (assistant ? 'Working on it' : 'Working on your app') : assistant ? 'Pick any agent and model below' : 'Your coding partner'}</span>
        </div>
        <span className={`connection-dot ${active ? 'working' : ''}`} />
      </header>
      <details className="chat-options">
        <summary>
          <Icon name="settings" size={14} /> Agent & model{' '}
          <span>{choice.provider ? providers.find((p) => p.id === choice.provider)?.name : 'Built-in'}</span>
        </summary>
        <AgentPicker
          value={choice}
          onChange={setChoice}
          agents={agents}
          providers={providers}
          disabled={!!active}
        />
        <div className="chat-option-footer">
          <label>
            <input
              type="checkbox"
              checked={cont}
              disabled={!!active}
              onChange={(e) => setCont(e.target.checked)}
              data-testid="task-continue"
            />{' '}
            Continue conversation
          </label>
          <button className="text-button" onClick={goSettings}>
            Connections
          </button>
        </div>
      </details>
      <div className="chat-messages" ref={scroll} onScroll={onScroll} data-testid="chat-messages">
        {history.length === 0 && !active && (
          <div className={assistant ? 'chat-welcome home-welcome' : 'chat-welcome'}>
            {assistant ? (
              <div className="hero-crew" aria-hidden="true">
                <Bot who="hermes" size={46} className="crew-far crew-l2" />
                <Bot who="openmausbot" size={58} className="crew-l1" />
                <Bot who="sandboxd" size={104} mood={text.trim() ? 'thinking' : 'happy'} className="crew-lead" />
                <Bot who="openclaw" size={58} className="crew-r1" />
                <Bot who="opencode" size={46} className="crew-far crew-r2" />
                <span className="crew-floor" />
              </div>
            ) : (
              <span className="welcome-icon">
                <Bot who={choice.agent || 'opencode'} mood="happy" size={88} />
              </span>
            )}
            {assistant ? (
              <h1>
                What can I do <span className="sbx-grad-text">for you?</span>
              </h1>
            ) : (
              <h3>Let’s make it yours.</h3>
            )}
            <p>
              {assistant
                ? 'Ask me for an app, an open-source project from GitHub, a new agent, or a task for one of your agents. I build it, run it and show it to you, on your machine.'
                : 'Describe what to build or change. Your agent can edit files, install packages and run your app.'}
            </p>
            <div className="chat-suggestions">
              {(assistant
                ? [
                    'Build me a recipe app with a shopping list',
                    'Install Excalidraw for me',
                    'Add a Hermes agent to plan my week',
                    'What did my agents do today?',
                  ]
                : ['Improve the layout and typography', 'Explain this project’s structure', 'Find and fix bugs in this app']
              ).map((p) => (
                <button key={p} onClick={() => setText(p)}>
                  {p}
                  <Icon name="arrow" size={13} />
                </button>
              ))}
            </div>
          </div>
        )}
        {history
          .filter((t) => terminal(t.status))
          .map((t) => (
            <article className="chat-turn" key={t.id}>
              {t.prompt && <UserPrompt text={t.prompt} />}
              <div className="agent-message">
                <span className="message-label">
                  <Avatar
                    who={assistant ? 'sandboxd' : t.agent || 'opencode'}
                    size={22}
                    still
                    mood={t.status === 'failed' ? 'error' : 'idle'}
                  />
                  {assistant ? 'sandboxd' : agentName(t.agent)}
                </span>
                <AgentMessage text={plainTaskError(t.agent_message || t.error_message) || `Task ${t.status}`} />
              </div>
              {t.run_settings && (
                <p
                  className={
                    t.run_settings.status === 'applied' && t.run_settings.preview_ready
                      ? 'chat-notice'
                      : 'chat-notice warn'
                  }
                  role="status"
                  data-testid="run-settings"
                >
                  <Icon name={t.run_settings.status === 'kept_previous' ? 'history' : 'refresh'} size={14} />{' '}
                  {t.run_settings.message}
                </p>
              )}
              <div className="turn-meta">
                {t.status !== 'succeeded' && (
                  <span className={`task-status status-${t.status}`}>{statusLabel(t.status)}</span>
                )}
                {!!t.files_changed?.length && (
                  <details>
                    <summary>
                      {t.files_changed.length} {t.files_changed.length === 1 ? 'file' : 'files'} changed
                    </summary>
                    <ul>
                      {t.files_changed.map((f) => (
                        <li key={f}>{f}</li>
                      ))}
                    </ul>
                  </details>
                )}
                {t.can_revert && !assistant && (
                  <button
                    disabled={!sandboxRunning || !!active}
                    className="text-button"
                    onClick={() => setRevertTarget(t)}
                  >
                    <Icon name="history" size={12} /> Restore
                  </button>
                )}
              </div>
            </article>
          ))}
        {active && (
          <article className="chat-turn">
            <UserPrompt text={active.prompt || 'Task in progress'} />
            <div className="agent-message">
              <span className="message-label">
                <Avatar who={assistant ? 'sandboxd' : active.agent || choice.agent || 'opencode'} size={22} mood="working" />
                {assistant ? 'sandboxd' : agentName(active.agent || choice.agent)}
              </span>
              <AgentMessage text={response || 'Working on your request…'} />
            </div>
            <div role="status" className="agent-progress">
              <span className="loading-dot" />
              {tool || (assistant ? 'sandboxd is working on it…' : 'Working on your app…')}
            </div>
            {resolved && <p className="field-help">{resolved}</p>}
          </article>
        )}
        {made.map((a) => (
          <div className="made-app" key={a.id} data-testid="made-app">
            <ProjectIcon name={a.name} tags={a.tags} logo={catalogIdOf(a.tags)} size={40} mood="happy" />
            <span>
              <small>New app</small>
              <strong>{a.name}</strong>
            </span>
            {onOpenApp && (
              <Btn sm variant="primary" onClick={() => onOpenApp(a.id)}>
                Open
              </Btn>
            )}
          </div>
        ))}
        {behind && (
          <button
            className="chat-jump"
            onClick={() => {
              following.current = true
              setBehind(false)
              toEnd()
            }}
            data-testid="chat-jump"
          >
            <Icon name="arrow" size={13} /> Latest
          </button>
        )}
      </div>
      {error && (
        <div role="alert" className="chat-notice error">
          {error}
          {!loaded && (
            <button className="text-button" onClick={loadOptions}>
              Retry connection
            </button>
          )}
        </div>
      )}
      {claudeSignIn && sandboxRunning && sbID && (
        <ClaudeSignIn sandboxId={sbID} onSignedIn={setClaudeSignedIn} />
      )}
      {blocked && loaded && !claudeSignIn && (
        <div className="chat-notice" data-testid="agent-not-connected">
          Choose a model connection or connect your account.{' '}
          <button className="text-button" onClick={goSettings}>
            Open Settings
          </button>
        </div>
      )}
      {!sandboxRunning && !assistant && (
        <div className="chat-notice">
          {sb?.status === 'running'
            ? 'Preparing your project…'
            : !sb
              ? 'Your app is not set up yet. Your message is kept here.'
              : sb.status === 'stopped'
                ? 'Your app is asleep. Send a message and it wakes up.'
                : 'Your app is waking up…'}
        </div>
      )}
      <form
        className="chat-composer"
        onSubmit={(e) => {
          e.preventDefault()
          void send()
        }}
        onDragOver={(e) => {
          if (e.dataTransfer.types.includes('Files')) e.preventDefault()
        }}
        onDrop={(e) => {
          if (!e.dataTransfer.files.length) return
          e.preventDefault()
          attach(e.dataTransfer.files)
        }}
        onPaste={(e) => {
          if (e.clipboardData.files.length) {
            e.preventDefault()
            attach(e.clipboardData.files)
          }
        }}
      >
        {files.length > 0 && (
          <div className="agent-attachments" data-testid="chat-attachments">
            {files.map((f) => (
              <span key={f.key} className={f.path ? 'attachment' : 'attachment uploading'}>
                {f.preview ? <img src={f.preview} alt="" /> : <Icon name="book" size={14} />}
                <span className="attachment-name">{f.name}</span>
                <small>{f.path ? fileSize(f.size) : 'Attaching…'}</small>
                <button type="button" aria-label={`Remove ${f.name}`} onClick={() => unattach(f.key)}>
                  <Icon name="close" size={12} />
                </button>
              </span>
            ))}
          </div>
        )}
        <input
          ref={picker}
          type="file"
          multiple
          hidden
          onChange={(e) => {
            attach(e.target.files)
            e.target.value = ''
          }}
          data-testid="chat-file-input"
        />
        <textarea
          id="chat-prompt"
          aria-label="Message the agent"
          value={text}
          rows={3}
          onChange={(e) => setText(e.target.value)}
          onKeyDown={(e) => {
            if (e.key === 'Enter' && !e.shiftKey && !e.nativeEvent.isComposing) {
              e.preventDefault()
              void send()
            }
          }}
          placeholder={assistant ? "Ask for an app, an agent or a task…" : "What should we build next?"}
          data-testid="task-prompt"
        />
        <footer>
          <button
            type="button"
            className="composer-attach"
            disabled={!sbID}
            onClick={() => picker.current?.click()}
            aria-label="Attach files"
            title="Attach files"
            data-testid="chat-attach"
          >
            <Icon name="plus" size={16} />
          </button>
          <span>Enter to send · Shift + Enter for a new line</span>
          {active ? (
            <Btn disabled={!active.id} onClick={() => void cancel()} type="button">
              Stop
            </Btn>
          ) : (
            <button
              className="send-button"
              type="submit"
              disabled={!canSend || (!text.trim() && !files.length) || uploading || blocked || !loaded}
              data-testid="task-send"
              aria-label="Send message"
            >
              <Icon name="arrow" size={18} />
            </button>
          )}
        </footer>
      </form>
      {revertTarget && (
        <Dialog title="Restore this checkpoint?" close={() => !reverting && setRevertTarget(null)}>
          <p>
            Restore the workspace to before this task. File changes from this task onward will be discarded.
            Installed packages and build caches are kept.
          </p>
          <div className="dialog-actions">
            <Btn disabled={reverting} onClick={() => setRevertTarget(null)}>
              Keep current version
            </Btn>
            <Btn variant="danger" disabled={reverting} onClick={() => void revert()}>
              Restore checkpoint
            </Btn>
          </div>
        </Dialog>
      )}
    </section>
  )
}

// The engine's task errors are written for operators; say them plainly here.
export function plainTaskError(message?: string) {
  if (!message) return message
  if (/did not finish within the task timeout/i.test(message))
    return 'This took longer than the time limit, so the agent paused. Everything it made so far is saved: send “Please continue” to let it finish.'
  return message
}

// A free-app install sends the coding agent its exact setup; people see one
// line, with the details a click away.
export function UserPrompt({ text: sent }: { text: string }) {
  const { text, files } = splitAttachments(sent)
  if (files.length)
    return (
      <div className="user-message" data-testid="message-with-files">
        {text && <span>{text}</span>}
        <span className="message-files">
          {files.map((f) => (
            <span key={f} className="attachment">
              <Icon name="book" size={12} />
              <span className="attachment-name">{f}</span>
            </span>
          ))}
        </span>
      </div>
    )
  const catalog = /^Install (.+?) from sandboxd's app catalog/.exec(text)
  // An install the sandboxd agent started (install_project).
  const project = /^Install the open-source project https:\/\/github\.com\/[^/\s]+\/([^\s/]+?) so it runs/.exec(text)
  if (!catalog && !project) {
    // A long request, usually the brief sandboxd wrote for the builder: show
    // its start, with the rest one click away.
    if (text.length > 280)
      return (
        <div className="user-message" data-testid="long-request">
          <span>{text.slice(0, 160).replace(/\s+\S*$/, '')}…</span>
          <details className="install-details">
            <summary>Show the full request</summary>
            <pre>{text}</pre>
          </details>
        </div>
      )
    return <div className="user-message">{text}</div>
  }
  return (
    <div className="user-message" data-testid="catalog-install-message">
      <span>{catalog ? `Install ${catalog[1]} from Free apps` : `Install ${project![1]} from GitHub`}</span>
      <details className="install-details">
        <summary>Details</summary>
        <pre>{text}</pre>
      </details>
    </div>
  )
}

// A finished task's outcome, in plain words (success shows nothing).
function statusLabel(status: string) {
  return { failed: "Didn't finish", cancelled: 'Stopped', canceled: 'Stopped' }[status] || status
}

// A coding agent's id as people know it.
function agentName(id?: string) {
  return (
    { opencode: 'OpenCode', codex: 'Codex', 'claude-code': 'Claude Code', claude: 'Claude Code', pi: 'Pi' }[id || ''] ||
    (id ? id[0].toUpperCase() + id.slice(1) : 'Agent')
  )
}
