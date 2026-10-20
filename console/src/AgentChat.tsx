import { useCallback, useEffect, useRef, useState } from 'react'
import { api, Agent, ModelProvider, Sandbox, TaskSummary } from './api'
import { AgentPicker, BuildIntent, choiceBlocked, defaultChoice } from './AgentPicker'
import { Btn } from './design/kit'
import { Icon } from './design/Icon'
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
}: {
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
  const blocked = choiceBlocked(choice, providers, agents)
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
  useEffect(() => {
    scroll.current?.scrollTo?.({ top: scroll.current.scrollHeight, behavior: 'smooth' })
  }, [response, history.length, active?.id])
  const send = async (auto = false) => {
    if (!sbID || !sandboxRunning || !text.trim() || active || blocked || !loaded || submitting.current) return
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
          <Icon name="spark" size={17} />
        </span>
        <div>
          <strong>
            Build with {choice.agent === 'opencode' ? 'OpenCode' : choice.agent === 'pi' ? 'Pi' : 'Claude'}
          </strong>
          <span>{active ? 'Working on your app' : 'Your coding partner'}</span>
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
      <div className="chat-messages" ref={scroll}>
        {history.length === 0 && !active && (
          <div className="chat-welcome">
            <span className="welcome-icon">
              <Icon name="spark" size={28} />
            </span>
            <h3>Let’s make it yours.</h3>
            <p>
              Describe what to build or change. Your agent can edit files, install packages and run your app.
            </p>
            <div className="chat-suggestions">
              {[
                'Improve the layout and typography',
                'Explain this project’s structure',
                'Find and fix bugs in this app',
              ].map((p) => (
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
              {t.prompt && <div className="user-message">{t.prompt}</div>}
              <div className="agent-message">
                <span className="message-label">
                  <Icon name="spark" size={13} />
                  {t.agent || 'Agent'}
                </span>
                <AgentMessage text={t.agent_message || t.error_message || `Task ${t.status}`} />
              </div>
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
            <div className="user-message">{active.prompt || 'Task in progress'}</div>
            <div className="agent-message">
              <span className="message-label">
                <Icon name="spark" size={13} />
                Agent
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
      {blocked && loaded && (
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
                ? 'Start the workspace to continue building.'
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
          placeholder="What should we build next?"
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
              disabled={!sandboxRunning || !text.trim() || blocked || !loaded}
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
