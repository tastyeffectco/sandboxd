import { useCallback, useEffect, useRef, useState } from 'react'
import { api, App as TApp, Sandbox, ConfigItem, AccessPolicy, Snapshot, AppEvent } from './api'
import { StatusBadge } from './ui'

const ACCESS_POLICIES: AccessPolicy[] = ['control_plane_only', 'agent_access', 'runtime_access', 'both']
// Only control_plane_only is enforced today. The others describe delivery to
// agents/app runtimes that the secrets broker (Slice 2) has not implemented
// yet, so they are shown but not selectable — picking one must not imply a
// secret is being delivered anywhere.
const ACTIVE_POLICIES: AccessPolicy[] = ['control_plane_only']
const policyReserved = (p: AccessPolicy) => !ACTIVE_POLICIES.includes(p)
const policyLabel = (p: AccessPolicy) => (policyReserved(p) ? `${p} — reserved (broker)` : p)

export function AppDetail({
  appId,
  onError,
  onInfo,
}: {
  appId: string
  onError: (m: string) => void
  onInfo: (m: string) => void
}) {
  const [app, setApp] = useState<TApp | null>(null)
  const [sb, setSb] = useState<Sandbox | null>(null)
  const [busy, setBusy] = useState(false)
  const [snapReload, setSnapReload] = useState(0) // bump to refresh snapshot history

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

  // Capture a snapshot with explicit feedback. A running source returns 409.
  // On success, refresh the history list below.
  const snapshot = async () => {
    if (!sb || !app) return
    setBusy(true)
    try {
      await api.createSnapshot(sb.id, `${app.name}-${Date.now()}`)
      onInfo('Snapshot captured.')
      setSnapReload((n) => n + 1)
    } catch (e) {
      const err = e as Error & { status?: number }
      onError(err.status === 409 ? 'Stop the sandbox before capturing a snapshot.' : err.message)
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
            title="Capture a snapshot (stop the sandbox first)"
            onClick={snapshot}
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
          <h2>Preview / endpoint</h2>
          {previewURL && status === 'running' ? (
            <iframe className="preview-frame" src={previewURL} title="preview" data-testid="preview" />
          ) : (
            <div className="preview-empty" data-testid="preview-empty">
              {!sb
                ? 'No sandbox yet'
                : sb.preview?.status === 'none'
                  ? 'No public endpoint — worker process running'
                  : 'Sandbox not running'}
            </div>
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

      <ProcessesPanel sandbox={sb} onError={onError} />

      <SnapshotsPanel
        appId={appId}
        appName={app.name}
        reloadKey={snapReload}
        onError={onError}
        onInfo={onInfo}
        onChanged={refresh}
      />

      <ConfigPanel appId={appId} onError={onError} />

      <ActivityPanel appId={appId} reloadKey={snapReload} onError={onError} />
    </div>
  )
}

// ProcessesPanel shows the sandbox's supervised processes (web + workers) from
// the runtime manifest, and lets you tail each process's recent logs. A
// worker-only app (no web) is valid here — its worker simply shows as running
// with no public endpoint.
function ProcessesPanel({ sandbox, onError }: { sandbox: Sandbox | null; onError: (m: string) => void }) {
  const [logsFor, setLogsFor] = useState<string | null>(null)
  const [logLines, setLogLines] = useState<string[]>([])
  const [busy, setBusy] = useState(false)
  if (!sandbox) return null
  const procs = sandbox.processes ?? []

  const viewLogs = async (name: string) => {
    setBusy(true)
    setLogsFor(name)
    setLogLines([])
    try {
      const r = await api.getProcessLogs(sandbox.id, name, 200)
      setLogLines(r.lines)
    } catch (e) {
      onError((e as Error).message)
    } finally {
      setBusy(false)
    }
  }

  return (
    <div className="card" data-testid="processes-panel">
      <h2 className="card-title">Processes</h2>
      {procs.length === 0 ? (
        <p className="muted" data-testid="processes-empty">
          No processes reported (sandbox stopped or still starting).
        </p>
      ) : (
        <table className="config-table" data-testid="processes-list">
          <thead>
            <tr>
              <th>Name</th>
              <th>Kind</th>
              <th>Status</th>
              <th>PID</th>
              <th>Restarts</th>
              <th />
            </tr>
          </thead>
          <tbody>
            {procs.map((p) => (
              <tr key={p.name} data-testid={`process-${p.name}`}>
                <td className="mono">{p.name}</td>
                <td>{p.kind}</td>
                <td>
                  <span className={`badge ${p.running ? 'running' : 'stopped'}`}>
                    {p.running ? 'running' : 'stopped'}
                  </span>
                </td>
                <td className="muted mono">{p.pid || '—'}</td>
                <td className="muted">{p.restarts}</td>
                <td>
                  <button
                    className="btn btn-ghost btn-sm"
                    disabled={busy}
                    data-testid={`process-logs-${p.name}`}
                    onClick={() => viewLogs(p.name)}
                  >
                    Logs
                  </button>
                </td>
              </tr>
            ))}
          </tbody>
        </table>
      )}
      {logsFor && (
        <div className="log mono" data-testid="process-log-output" style={{ marginTop: 10 }}>
          <div className="row">
            <strong>{logsFor} — recent logs</strong>
            <div className="spacer" />
            <button className="btn btn-ghost btn-sm" onClick={() => setLogsFor(null)}>
              close
            </button>
          </div>
          {logLines.length === 0 ? (
            <div className="muted">{busy ? 'Loading…' : '(no output)'}</div>
          ) : (
            logLines.map((l, i) => (
              <div key={i} className="ev">
                {l}
              </div>
            ))
          )}
        </div>
      )}
    </div>
  )
}

// ActivityPanel renders the durable event timeline (newest-first). It is
// backed by SQLite, so it survives page refresh and server restart. Failed
// events are flagged by severity. Read-only.
function ActivityPanel({
  appId,
  reloadKey,
  onError,
}: {
  appId: string
  reloadKey: number
  onError: (m: string) => void
}) {
  const [evts, setEvts] = useState<AppEvent[] | null>(null)

  const load = useCallback(() => {
    api
      .listAppEvents(appId)
      .then(setEvts)
      .catch((e) => onError((e as Error).message))
  }, [appId, onError])
  useEffect(load, [load, reloadKey])
  useEffect(() => {
    const t = setInterval(load, 6000) // reflect new activity
    return () => clearInterval(t)
  }, [load])

  const sev = (s: string) => (s === 'error' ? 'ev-error' : s === 'warning' ? 'ev-warn' : 'ev-info')

  return (
    <div className="card" data-testid="activity-panel">
      <div className="row">
        <h2 className="card-title">Activity</h2>
        <div className="spacer" />
        <span className="muted" style={{ fontSize: 12 }}>Durable timeline — survives restarts.</span>
      </div>
      {evts === null ? (
        <p className="muted">Loading…</p>
      ) : evts.length === 0 ? (
        <p className="muted" data-testid="activity-empty">
          No activity yet.
        </p>
      ) : (
        <div className="timeline" data-testid="activity-list">
          {evts.map((e) => (
            <div key={e.id} className={`tl-row ${sev(e.severity)}`} data-testid={`event-${e.type}`}>
              <span className="tl-time muted mono">{new Date(e.created_at).toLocaleString()}</span>
              <span className="tl-type mono">{e.type}</span>
              <span className="tl-msg">{e.message}</span>
              {(e.task_id || e.sandbox_id || e.snapshot_id) && (
                <span className="tl-ids muted mono">
                  {e.task_id ? `task:${e.task_id.slice(0, 8)} ` : ''}
                  {e.sandbox_id ? `sb:${e.sandbox_id.slice(0, 8)} ` : ''}
                  {e.snapshot_id ? `snap:${e.snapshot_id.slice(0, 8)}` : ''}
                </span>
              )}
            </div>
          ))}
        </div>
      )}
    </div>
  )
}

// SnapshotsPanel shows an app's snapshot history and the two recovery
// actions. Restore is destructive (replaces the current sandbox), so it
// confirms first. Fork spins a brand-new app from the snapshot.
function SnapshotsPanel({
  appId,
  appName,
  reloadKey,
  onError,
  onInfo,
  onChanged,
}: {
  appId: string
  appName: string
  reloadKey: number
  onError: (m: string) => void
  onInfo: (m: string) => void
  onChanged: () => void
}) {
  const [snaps, setSnaps] = useState<Snapshot[] | null>(null)
  const [busy, setBusy] = useState(false)

  const load = useCallback(() => {
    api
      .listAppSnapshots(appId)
      .then(setSnaps)
      .catch((e) => onError((e as Error).message))
  }, [appId, onError])
  useEffect(load, [load, reloadKey])

  const restore = async (snap: Snapshot) => {
    if (
      !window.confirm(
        `Restore "${snap.name}"?\n\nThis REPLACES the app's current sandbox and permanently discards any work that has not been snapshotted. Continue?`,
      )
    ) {
      return
    }
    setBusy(true)
    try {
      await api.restoreApp(appId, snap.id)
      onInfo('Restored from snapshot — a fresh sandbox was created.')
      onChanged()
    } catch (e) {
      onError((e as Error).message)
    } finally {
      setBusy(false)
    }
  }

  const fork = async (snap: Snapshot) => {
    const name = window.prompt('Fork into a new app named:', `${appName} fork`)
    if (name === null || !name.trim()) return
    setBusy(true)
    try {
      const res = await api.forkApp(appId, snap.id, name.trim())
      onInfo(`Forked into new app "${res.app?.name ?? name.trim()}".`)
    } catch (e) {
      onError((e as Error).message)
    } finally {
      setBusy(false)
    }
  }

  return (
    <div className="card" data-testid="snapshots-panel">
      <div className="row">
        <h2 className="card-title">Snapshots</h2>
        <div className="spacer" />
        <span className="muted" style={{ fontSize: 12 }}>
          Capture from the controls above (stop the sandbox first). Restore replaces the
          current sandbox; fork creates a new app.
        </span>
      </div>
      {snaps === null ? (
        <p className="muted">Loading…</p>
      ) : snaps.length === 0 ? (
        <p className="muted" data-testid="snapshots-empty">
          No snapshots yet.
        </p>
      ) : (
        <table className="config-table" data-testid="snapshots-list">
          <thead>
            <tr>
              <th>Name</th>
              <th>Captured</th>
              <th>Size</th>
              <th />
            </tr>
          </thead>
          <tbody>
            {snaps.map((s) => (
              <tr key={s.id} data-testid={`snapshot-row-${s.id}`}>
                <td className="mono">{s.name}</td>
                <td className="muted">{new Date(s.created_at).toLocaleString()}</td>
                <td className="muted">{s.size_bytes ? `${Math.round(s.size_bytes / 1024)} KB` : '—'}</td>
                <td className="row" style={{ gap: 4 }}>
                  <button
                    className="btn btn-outline btn-sm"
                    disabled={busy || s.status !== 'ready'}
                    data-testid={`snapshot-restore-${s.id}`}
                    onClick={() => restore(s)}
                  >
                    Restore
                  </button>
                  <button
                    className="btn btn-ghost btn-sm"
                    disabled={busy || s.status !== 'ready'}
                    data-testid={`snapshot-fork-${s.id}`}
                    onClick={() => fork(s)}
                  >
                    Fork
                  </button>
                </td>
              </tr>
            ))}
          </tbody>
        </table>
      )}
    </div>
  )
}

// ConfigPanel manages an app's config & secrets. Secrets are write-only:
// the API never returns a sensitive value, so a stored secret shows as
// "•••• set" and can only be replaced, never read back.
function ConfigPanel({ appId, onError }: { appId: string; onError: (m: string) => void }) {
  const [items, setItems] = useState<ConfigItem[] | null>(null)
  const [busy, setBusy] = useState(false)
  const [key, setKey] = useState('')
  const [value, setValue] = useState('')
  const [sensitive, setSensitive] = useState(true)
  const [policy, setPolicy] = useState<AccessPolicy>('control_plane_only')
  const [editKey, setEditKey] = useState<string | null>(null) // row whose value is being replaced
  const [editValue, setEditValue] = useState('')

  const load = useCallback(() => {
    api
      .listConfig(appId)
      .then(setItems)
      .catch((e) => onError((e as Error).message))
  }, [appId, onError])
  useEffect(load, [load])

  const act = async (fn: () => Promise<unknown>) => {
    setBusy(true)
    try {
      await fn()
      load()
    } catch (e) {
      onError((e as Error).message)
    } finally {
      setBusy(false)
    }
  }

  const add = async () => {
    if (!key.trim()) return
    await act(async () => {
      await api.createConfig(appId, { key: key.trim(), value, sensitive, access_policy: policy })
      setKey('')
      setValue('')
    })
  }

  return (
    <div className="card" data-testid="config-panel">
      <div className="row">
        <h2 className="card-title">Config &amp; Secrets</h2>
        <div className="spacer" />
        <span className="muted" style={{ fontSize: 12 }}>
          Secrets are encrypted at rest and never shown again.
        </span>
      </div>
      <p className="muted" style={{ fontSize: 12, marginTop: 4 }} data-testid="config-broker-note">
        Stored in sandboxd only. <code>control_plane_only</code> is the one policy
        enforced today — delivery to agents and app runtimes arrives with the
        secrets broker (not yet implemented), so the other policies are reserved.
      </p>

      {items === null ? (
        <p className="muted">Loading…</p>
      ) : items.length === 0 ? (
        <p className="muted" data-testid="config-empty">
          No config yet. Add a key below.
        </p>
      ) : (
        <table className="config-table" data-testid="config-list">
          <thead>
            <tr>
              <th>Key</th>
              <th>Value</th>
              <th>Access policy</th>
              <th />
            </tr>
          </thead>
          <tbody>
            {items.map((it) => (
              <tr key={it.key} data-testid={`config-row-${it.key}`}>
                <td className="mono">{it.key}</td>
                <td className="mono">
                  {editKey === it.key ? (
                    <span className="row" style={{ gap: 6, flexWrap: 'nowrap' }}>
                      <input
                        className="input mono"
                        type={it.sensitive ? 'password' : 'text'}
                        placeholder={it.sensitive ? 'new secret value' : 'new value'}
                        value={editValue}
                        autoFocus
                        disabled={busy}
                        onChange={(e) => setEditValue(e.target.value)}
                        data-testid={`config-edit-value-${it.key}`}
                      />
                      <button
                        className="btn btn-primary btn-sm"
                        disabled={busy}
                        data-testid={`config-save-${it.key}`}
                        onClick={() =>
                          act(async () => {
                            await api.patchConfig(appId, it.key, { value: editValue })
                            setEditKey(null)
                            setEditValue('')
                          })
                        }
                      >
                        Save
                      </button>
                      <button
                        className="btn btn-ghost btn-sm"
                        disabled={busy}
                        onClick={() => {
                          setEditKey(null)
                          setEditValue('')
                        }}
                      >
                        Cancel
                      </button>
                    </span>
                  ) : it.sensitive ? (
                    <span className="tag" title="Encrypted at rest; write-only">
                      •••• {it.value_set ? 'set' : 'empty'}
                    </span>
                  ) : (
                    <span>{it.value}</span>
                  )}
                </td>
                <td>
                  <select
                    className="input"
                    value={it.access_policy}
                    disabled={busy}
                    data-testid={`config-policy-${it.key}`}
                    onChange={(e) =>
                      act(() => api.patchConfig(appId, it.key, { access_policy: e.target.value as AccessPolicy }))
                    }
                  >
                    {ACCESS_POLICIES.map((p) => (
                      <option key={p} value={p} disabled={policyReserved(p) && it.access_policy !== p}>
                        {policyLabel(p)}
                      </option>
                    ))}
                  </select>
                </td>
                <td className="row" style={{ gap: 4 }}>
                  <button
                    className="btn btn-ghost btn-sm"
                    disabled={busy || editKey === it.key}
                    data-testid={`config-replace-${it.key}`}
                    onClick={() => {
                      setEditKey(it.key)
                      setEditValue('')
                    }}
                  >
                    {it.sensitive ? 'Replace' : 'Edit'}
                  </button>
                  <button
                    className="btn btn-ghost btn-sm"
                    disabled={busy}
                    data-testid={`config-delete-${it.key}`}
                    onClick={() => act(() => api.deleteConfig(appId, it.key))}
                  >
                    Delete
                  </button>
                </td>
              </tr>
            ))}
          </tbody>
        </table>
      )}

      <div className="row config-add" style={{ marginTop: 12, flexWrap: 'wrap', gap: 8 }}>
        <input
          className="input mono"
          placeholder="KEY"
          value={key}
          onChange={(e) => setKey(e.target.value)}
          data-testid="config-key"
        />
        <input
          className="input mono"
          type={sensitive ? 'password' : 'text'}
          placeholder={sensitive ? 'secret value' : 'value'}
          value={value}
          onChange={(e) => setValue(e.target.value)}
          data-testid="config-value"
        />
        <select
          className="input"
          value={policy}
          onChange={(e) => setPolicy(e.target.value as AccessPolicy)}
          data-testid="config-new-policy"
        >
          {ACCESS_POLICIES.map((p) => (
            <option key={p} value={p} disabled={policyReserved(p)}>
              {policyLabel(p)}
            </option>
          ))}
        </select>
        <label className="muted" style={{ display: 'flex', alignItems: 'center', gap: 6, fontSize: 13 }}>
          <input
            type="checkbox"
            checked={sensitive}
            onChange={(e) => setSensitive(e.target.checked)}
            data-testid="config-sensitive"
          />
          secret
        </label>
        <button
          className="btn btn-primary"
          disabled={busy || !key.trim()}
          onClick={add}
          data-testid="config-add"
        >
          Add
        </button>
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
