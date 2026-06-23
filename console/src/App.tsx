import { useCallback, useEffect, useState } from 'react'
import { api, App as TApp } from './api'
import { AppDetail } from './AppDetail'
import { StatusBadge } from './ui'

export default function App() {
  const [appId, setAppId] = useState<string | null>(null)
  const [error, setError] = useState<string | null>(null)
  const [info, setInfo] = useState<string | null>(null)

  useEffect(() => {
    if (!error) return
    const t = setTimeout(() => setError(null), 4500)
    return () => clearTimeout(t)
  }, [error])
  useEffect(() => {
    if (!info) return
    const t = setTimeout(() => setInfo(null), 5000)
    return () => clearTimeout(t)
  }, [info])

  return (
    <>
      <div className="topbar">
        <div className="brand" style={{ cursor: 'pointer' }} onClick={() => setAppId(null)}>
          sandboxd <span className="dot">/</span> console
        </div>
        <div className="spacer" />
        {appId && (
          <button className="btn btn-ghost btn-sm" onClick={() => setAppId(null)}>
            ← Apps
          </button>
        )}
      </div>
      <div className="container">
        {appId ? (
          <AppDetail appId={appId} onError={setError} onInfo={setInfo} />
        ) : (
          <AppList onOpen={setAppId} onError={setError} />
        )}
      </div>
      {error && (
        <div className="toast" onClick={() => setError(null)} data-testid="toast">
          {error}
        </div>
      )}
      {info && (
        <div className="toast toast-info" onClick={() => setInfo(null)} data-testid="toast-info">
          {info}
        </div>
      )}
    </>
  )
}

function AppList({ onOpen, onError }: { onOpen: (id: string) => void; onError: (m: string) => void }) {
  const [apps, setApps] = useState<TApp[] | null>(null)
  // Real status of each app's current sandbox, keyed by app id. A sandbox
  // row exists in many states (creating/running/stopped/error), so presence
  // alone must not read as "running" — we show the actual status.
  const [sbStatus, setSbStatus] = useState<Record<string, string>>({})
  const [name, setName] = useState('')
  const [busy, setBusy] = useState(false)

  const load = useCallback(() => {
    api
      .listApps()
      .then(async (list) => {
        setApps(list)
        const pairs = await Promise.all(
          list
            .filter((a) => a.current_sandbox_id)
            .map(async (a) => {
              try {
                const s = await api.getSandbox(a.current_sandbox_id as string)
                return [a.id, s.status] as const
              } catch {
                return [a.id, 'unknown'] as const
              }
            }),
        )
        setSbStatus(Object.fromEntries(pairs))
      })
      .catch((e) => onError(e.message))
  }, [onError])
  useEffect(load, [load])

  const create = async () => {
    if (!name.trim()) return
    setBusy(true)
    try {
      const a = await api.createApp({ name: name.trim() })
      setName('')
      onOpen(a.id)
    } catch (e) {
      onError((e as Error).message)
    } finally {
      setBusy(false)
    }
  }

  return (
    <div className="stack">
      <h1>Apps</h1>
      <div className="card">
        <div className="row">
          <input
            className="input"
            placeholder="New app name…"
            value={name}
            onChange={(e) => setName(e.target.value)}
            onKeyDown={(e) => e.key === 'Enter' && create()}
            data-testid="app-name"
          />
          <button
            className="btn btn-primary"
            onClick={create}
            disabled={busy || !name.trim()}
            data-testid="create-app"
          >
            Create app
          </button>
        </div>
      </div>

      {apps === null ? (
        <p className="muted">Loading…</p>
      ) : apps.length === 0 ? (
        <p className="muted">No apps yet — create one above to get started.</p>
      ) : (
        <div className="grid" data-testid="app-list">
          {apps.map((a) => (
            <div key={a.id} className="card click" onClick={() => onOpen(a.id)} data-testid="app-card">
              <div className="card-title">{a.name}</div>
              <div className="muted mono" style={{ fontSize: 12, marginBottom: 12 }}>
                {a.description || a.id}
              </div>
              <div className="row">
                {a.current_sandbox_id ? (
                  <StatusBadge status={sbStatus[a.id]} />
                ) : (
                  <span className="badge">
                    <span className="dot-i" />
                    no sandbox
                  </span>
                )}
                <div className="spacer" />
                {a.tags?.slice(0, 3).map((t) => (
                  <span key={t} className="tag">
                    {t}
                  </span>
                ))}
              </div>
            </div>
          ))}
        </div>
      )}
    </div>
  )
}
