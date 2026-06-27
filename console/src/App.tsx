import { useCallback, useEffect, useState } from 'react'
import { api, App as TApp, Preset, GitCredential } from './api'
import { AppDetail } from './AppDetail'
import { Settings } from './Settings'
import { StatusBadge } from './ui'

export default function App() {
  const [appId, setAppId] = useState<string | null>(null)
  const [view, setView] = useState<'apps' | 'settings'>('apps')
  const [error, setError] = useState<string | null>(null)
  const [info, setInfo] = useState<string | null>(null)

  const goApps = () => {
    setView('apps')
    setAppId(null)
  }

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
        <div className="brand" style={{ cursor: 'pointer' }} onClick={goApps}>
          sandboxd <span className="dot">/</span> console
        </div>
        <div className="spacer" />
        {(appId || view === 'settings') && (
          <button className="btn btn-ghost btn-sm" onClick={goApps}>
            ← Apps
          </button>
        )}
        {view !== 'settings' && (
          <button className="btn btn-ghost btn-sm" data-testid="nav-settings" onClick={() => setView('settings')}>
            Settings
          </button>
        )}
      </div>
      <div className="container">
        {view === 'settings' ? (
          <Settings onError={setError} />
        ) : appId ? (
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
  const [presets, setPresets] = useState<Preset[]>([])
  const [presetID, setPresetID] = useState('')
  const [busy, setBusy] = useState(false)
  // Git import (A1): blank-from-preset by default; "git" imports a private repo.
  const [mode, setMode] = useState<'blank' | 'git'>('blank')
  const [gitCreds, setGitCreds] = useState<GitCredential[]>([])
  const [repoURL, setRepoURL] = useState('')
  const [branch, setBranch] = useState('main')
  const [credId, setCredId] = useState('')

  useEffect(() => {
    api.listPresets().then(setPresets).catch(() => setPresets([]))
    api.listGitCredentials().then(setGitCreds).catch(() => setGitCreds([]))
  }, [])

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
    if (mode === 'git' && (!repoURL.trim() || !credId)) return
    setBusy(true)
    try {
      const a = await api.createApp({
        name: name.trim(),
        runtime_preset: presetID || undefined,
        git:
          mode === 'git'
            ? { repo_url: repoURL.trim(), branch: branch.trim() || 'main', credential_id: credId }
            : undefined,
      })
      setName('')
      setRepoURL('')
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
          <select
            className="input"
            value={presetID}
            onChange={(e) => setPresetID(e.target.value)}
            data-testid="app-preset"
            title="App type — generates a sandbox.yaml so it boots"
          >
            <option value="">App type (default)…</option>
            {presets.map((p) => (
              <option key={p.id} value={p.id}>
                {p.label}
              </option>
            ))}
          </select>
          <button
            className="btn btn-primary"
            onClick={create}
            disabled={busy || !name.trim() || (mode === 'git' && (!repoURL.trim() || !credId))}
            data-testid="create-app"
          >
            Create app
          </button>
        </div>
        <div className="row" style={{ marginTop: 8 }}>
          <label>
            <input
              type="radio"
              data-testid="mode-blank"
              checked={mode === 'blank'}
              onChange={() => setMode('blank')}
            />{' '}
            Blank from preset
          </label>
          <label>
            <input
              type="radio"
              data-testid="mode-git"
              checked={mode === 'git'}
              onChange={() => setMode('git')}
            />{' '}
            Import from Git URL
          </label>
        </div>
        {mode === 'git' && (
          <div className="row" data-testid="git-import-fields" style={{ marginTop: 8, gap: 8, flexWrap: 'wrap' }}>
            <input
              className="input"
              placeholder="https://github.com/org/repo.git"
              value={repoURL}
              onChange={(e) => setRepoURL(e.target.value)}
              data-testid="git-repo-url"
            />
            <input
              className="input"
              placeholder="branch"
              value={branch}
              onChange={(e) => setBranch(e.target.value)}
              data-testid="git-branch"
            />
            <select
              className="input"
              value={credId}
              onChange={(e) => setCredId(e.target.value)}
              data-testid="git-credential"
            >
              <option value="">Credential…</option>
              {gitCreds.map((c) => (
                <option key={c.id} value={c.id}>
                  {c.name}
                  {c.host ? ` (${c.host})` : ''}
                </option>
              ))}
            </select>
            {gitCreds.length === 0 && (
              <span className="muted" data-testid="git-no-creds">
                Add a Git credential in Settings first.
              </span>
            )}
          </div>
        )}
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
