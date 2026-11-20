import { useCallback, useEffect, useRef, useState } from 'react'
import { api, Agent, App as TApp, ModelProvider, Sandbox, TaskSummary } from './api'
import { AgentPicker, BuildIntent, choiceBlocked, defaultChoice } from './AgentPicker'
import { ClaudeSignIn } from './ClaudeSignIn'
import { Btn } from './design/kit'
import { Icon } from './design/Icon'
import { Bot } from './design/Bot'
import { Avatar } from './design/Avatar'
import { catalogIdOf } from './catalog'
import { AgentMessage } from './AgentMessage'
import { Dialog } from './design/Dialog'

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
  const send = async (auto = false) => {
    if (!sbID || !canSend || !text.trim() || active || blocked || !loaded || submitting.current) return
    submitting.current = true
    const prompt = text.trim()
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
    try {
      const task = await api.submitTask(
        sbID,
        prompt,
        choice.agent,
        choice.model || undefined,
        auto ? false : cont,
        choice.provider || undefined,
      )
      setActive({ id: task.id, prompt, status: 'running', can_revert: false })
      void loadHistory()
    } catch (e) {
      setText(prompt)
      setActive(null)
      setError((e as Error).message)
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
                  <Avatar who={t.agent || 'opencode'} size={22} still mood={t.status === 'failed' ? 'error' : 'idle'} />
                  {agentName(t.agent)}
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
                <span className={`task-status status-${t.status}`}>{t.status}</span>
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
                {t.can_revert && (
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
                <Avatar who={active.agent || choice.agent || 'opencode'} size={22} mood="working" />
                {agentName(active.agent || choice.agent)}
              </span>
              <AgentMessage text={response || 'Working on your request…'} />
            </div>
            <div role="status" className="agent-progress">
              <span className="loading-dot" />
              {tool || 'Building in your workspace…'}
            </div>
            {resolved && <p className="field-help">{resolved}</p>}
          </article>
        )}
        {made.map((a) => (
          <div className="made-app" key={a.id} data-testid="made-app">
            <Avatar who={a.name} logo={catalogIdOf(a.tags)} size={40} mood="happy" />
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
      {!sandboxRunning && (
        <div className="chat-notice">
          {sb?.status === 'running'
            ? 'Preparing your project…'
            : !sb
              ? 'Create a workspace to start building. Your prompt is kept here.'
              : sb.status === 'stopped'
                ? 'Your workspace is resting. Send a message to wake it.'
                : 'Your workspace is starting…'}
        </div>
      )}
      <form
        className="chat-composer"
        onSubmit={(e) => {
          e.preventDefault()
          void send()
        }}
      >
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
          <span>Enter to send · Shift + Enter for a new line</span>
          {active ? (
            <Btn disabled={!active.id} onClick={() => void cancel()} type="button">
              Stop
            </Btn>
          ) : (
            <button
              className="send-button"
              type="submit"
              disabled={!canSend || !text.trim() || blocked || !loaded}
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
export function UserPrompt({ text }: { text: string }) {
  const catalog = /^Install (.+?) from sandboxd's app catalog/.exec(text)
  // An install the sandboxd agent started (install_project).
  const project = /^Install the open-source project https:\/\/github\.com\/[^/\s]+\/([^\s/]+?) so it runs/.exec(text)
  if (!catalog && !project) return <div className="user-message">{text}</div>
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

// A coding agent's id as people know it.
function agentName(id?: string) {
  return (
    { opencode: 'OpenCode', codex: 'Codex', 'claude-code': 'Claude Code', claude: 'Claude Code', pi: 'Pi' }[id || ''] ||
    (id ? id[0].toUpperCase() + id.slice(1) : 'Agent')
  )
}
