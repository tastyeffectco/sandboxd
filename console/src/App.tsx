import { useCallback, useEffect, useState } from 'react'
import { api, App as TApp } from './api'
import { AppDetail } from './AppDetail'

export default function App() {
  const [appId, setAppId] = useState<string | null>(null)
  const [error, setError] = useState<string | null>(null)

  useEffect(() => {
    if (!error) return
    const t = setTimeout(() => setError(null), 4500)
    return () => clearTimeout(t)
  }, [error])

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
          <AppDetail appId={appId} onError={setError} />
        ) : (
          <AppList onOpen={setAppId} onError={setError} />
        )}
      </div>
      {error && (
        <div className="toast" onClick={() => setError(null)} data-testid="toast">
          {error}
        </div>
      )}
    </>
  )
}

function AppList({ onOpen, onError }: { onOpen: (id: string) => void; onError: (m: string) => void }) {
  const [apps, setApps] = useState<TApp[] | null>(null)
  const [name, setName] = useState('')
  const [busy, setBusy] = useState(false)

  const load = useCallback(() => {
    api.listApps().then(setApps).catch((e) => onError(e.message))
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
                <span className={`badge ${a.current_sandbox_id ? 'running' : ''}`}>
                  <span className="dot-i" />
                  {a.current_sandbox_id ? 'sandbox' : 'no sandbox'}
                </span>
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
