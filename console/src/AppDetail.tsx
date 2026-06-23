import { useCallback, useEffect, useRef, useState } from 'react'
import { api, App as TApp, Sandbox } from './api'
import { StatusBadge } from './ui'

export function AppDetail({ appId, onError }: { appId: string; onError: (m: string) => void }) {
  const [app, setApp] = useState<TApp | null>(null)
  const [sb, setSb] = useState<Sandbox | null>(null)
  const [busy, setBusy] = useState(false)

  const refresh = useCallback(async () => {
    try {
      const a = await api.getApp(appId)
      setApp(a)
      setSb(a.current_sandbox_id ? await api.getSandbox(a.current_sandbox_id) : null)
    } catch (e) {
      onError((e as Error).message)
    }
  }, [appId, onError])

  useEffect(() => {
    refresh()
  }, [refresh])
  useEffect(() => {
    const t = setInterval(refresh, 4000) // reflect status/preview changes
    return () => clearInterval(t)
  }, [refresh])

  const act = async (fn: () => Promise<unknown>) => {
    setBusy(true)
    try {
      await fn()
      await refresh()
    } catch (e) {
      onError((e as Error).message)
    } finally {
      setBusy(false)
    }
  }

  if (!app) return <p className="muted">Loading…</p>

  const status = sb?.status
  const previewURL = sb?.preview?.url

  return (
    <div className="stack">
      <div className="row">
        <div>
          <h1>{app.name}</h1>
          <div className="muted mono" style={{ fontSize: 12, marginTop: 4 }}>{app.id}</div>
        </div>
        <div className="spacer" />
        {sb && <StatusBadge status={status} />}
      </div>

      <div className="row" data-testid="controls">
        {!sb && (
          <button
            className="btn btn-primary"
            disabled={busy}
            data-testid="create-sandbox"
            onClick={() => act(() => api.createAppSandbox(appId))}
          >
            Create sandbox
          </button>
        )}
        {sb && status === 'stopped' && (
          <button className="btn btn-primary" disabled={busy} data-testid="start" onClick={() => act(() => api.startSandbox(sb.id))}>
            Start
          </button>
        )}
        {sb && status === 'running' && (
          <button className="btn btn-outline" disabled={busy} data-testid="stop" onClick={() => act(() => api.stopSandbox(sb.id))}>
            Stop
          </button>
        )}
        {sb && (
          <button
            className="btn btn-outline"
            disabled={busy}
            data-testid="snapshot"
            title="Snapshots capture a stopped sandbox"
            onClick={() => act(() => api.createSnapshot(sb.id, `${app.name}-${Date.now()}`))}
          >
            Snapshot
          </button>
        )}
        {sb && (
          <button className="btn btn-ghost" disabled={busy} data-testid="delete-sandbox" onClick={() => act(() => api.deleteSandbox(sb.id))}>
            Delete sandbox
          </button>
        )}
      </div>

      <div className="detail">
        <div>
          <h2>Preview</h2>
          {previewURL && status === 'running' ? (
            <iframe className="preview-frame" src={previewURL} title="preview" data-testid="preview" />
          ) : (
            <div className="preview-empty">{sb ? 'Sandbox not running' : 'No sandbox yet'}</div>
          )}
          {previewURL && (
            <div className="mono" style={{ fontSize: 12, marginTop: 8 }}>
              <a href={previewURL} target="_blank" rel="noreferrer" className="linklike">
                {previewURL} ↗
              </a>
            </div>
          )}
        </div>

        <TaskPanel sandboxId={sb?.id} running={status === 'running'} onError={onError} />
      </div>
    </div>
  )
}

function TaskPanel({
  sandboxId,
  running,
  onError,
}: {
  sandboxId?: string
  running: boolean
  onError: (m: string) => void
}) {
  const [prompt, setPrompt] = useState('')
  const [status, setStatus] = useState<string | null>(null)
  const [log, setLog] = useState<string[]>([])
  const esRef = useRef<EventSource | null>(null)
  const logRef = useRef<HTMLDivElement | null>(null)

  useEffect(() => () => esRef.current?.close(), [])
  useEffect(() => {
    if (logRef.current) logRef.current.scrollTop = logRef.current.scrollHeight
  }, [log])

  const run = async () => {
    if (!sandboxId || !prompt.trim()) return
    setLog([])
    setStatus('running')
    try {
      const t = await api.submitTask(sandboxId, prompt.trim())
      esRef.current?.close()
      const es = new EventSource(api.taskEventsURL(sandboxId, t.id))
      esRef.current = es
      for (const type of ['status', 'message', 'tool', 'build']) {
        es.addEventListener(type, (m) => setLog((l) => [...l, `[${type}] ${(m as MessageEvent).data}`]))
      }
      es.addEventListener('done', () => {
        es.close()
        api
          .getTask(sandboxId, t.id)
          .then((r) => setStatus(r.status + (r.build_ok ? ' · build ok' : '')))
          .catch(() => setStatus('done'))
      })
      es.onerror = () => es.close()
    } catch (e) {
      onError((e as Error).message)
      setStatus(null)
    }
  }

  return (
    <div className="card">
      <h2>Task</h2>
      <textarea
        className="textarea"
        placeholder="Describe a change — e.g. “add a dark-mode toggle”"
        value={prompt}
        onChange={(e) => setPrompt(e.target.value)}
        data-testid="task-prompt"
      />
      <div className="row" style={{ marginTop: 10 }}>
        <button className="btn btn-primary" disabled={!running || !prompt.trim()} onClick={run} data-testid="run-task">
          Run task
        </button>
        <div className="spacer" />
        {status && (
          <span className="badge" data-testid="task-status">
            {status}
          </span>
        )}
      </div>
      {!running && <div className="muted" style={{ fontSize: 12, marginTop: 8 }}>Start the sandbox to run a task.</div>}
      {log.length > 0 && (
        <div className="log mono" ref={logRef} data-testid="task-log" style={{ marginTop: 12 }}>
          {log.map((l, i) => (
            <div key={i} className="ev">
              {l}
            </div>
          ))}
        </div>
      )}
    </div>
  )
}
