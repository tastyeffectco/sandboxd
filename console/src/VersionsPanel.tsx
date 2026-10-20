import { useCallback, useEffect, useState } from 'react'
import { api, Sandbox, Snapshot } from './api'
import { Btn } from './design/kit'
import { Dialog } from './design/Dialog'
import { Icon } from './design/Icon'

type Action = { kind: 'capture' } | { kind: 'restore' | 'fork'; snapshot: Snapshot }

export function VersionsPanel({
  appId,
  appName,
  sb,
  refresh,
  toast,
  onOpenApp,
}: {
  appId: string
  appName: string
  sb: Sandbox | null
  refresh: () => void
  toast: (message: string) => void
  onOpenApp?: (id: string) => void
}) {
  const [snapshots, setSnapshots] = useState<Snapshot[]>([])
  const [loading, setLoading] = useState(true)
  const [error, setError] = useState('')
  const [action, setAction] = useState<Action | null>(null)
  const [name, setName] = useState('')
  const [busy, setBusy] = useState(false)
  const load = useCallback(async () => {
    setLoading(true)
    try {
      setSnapshots(await api.listAppSnapshots(appId))
    } catch (e) {
      setError((e as Error).message)
    } finally {
      setLoading(false)
    }
  }, [appId])
  useEffect(() => {
    void load()
  }, [load])
  const open = (next: Action) => {
    setError('')
    setName(next.kind === 'capture' ? `${appName} — ${new Date().toLocaleDateString()}` : `${appName} copy`)
    setAction(next)
  }
  const submit = async () => {
    if (!action || busy) return
    setBusy(true)
    setError('')
    try {
      if (action.kind === 'capture') {
        if (!sb || !name.trim()) return
        await api.createSnapshot(sb.id, name.trim())
        toast('Version saved')
        await load()
      } else if (action.kind === 'restore') {
        await api.restoreApp(appId, action.snapshot.id)
        toast('Version restored')
        refresh()
      } else {
        if (!name.trim()) return
        const result = await api.forkApp(appId, action.snapshot.id, name.trim())
        toast('Project duplicated')
        onOpenApp?.(result.app.id)
      }
      setAction(null)
    } catch (e) {
      setError((e as Error).message)
    } finally {
      setBusy(false)
    }
  }
  return (
    <section className="settings-card versions-panel" data-testid="versions-panel">
      <header className="settings-card-heading">
        <div>
          <h2>Versions</h2>
          <p>Save your workspace, return to an earlier version, or use it to start another project.</p>
        </div>
        <Btn variant="primary" disabled={!sb || busy} onClick={() => open({ kind: 'capture' })}>
          <Icon name="plus" size={15} /> Save version
        </Btn>
      </header>
      <p className="field-help">
        Snapshots include code, installed packages, build output and workspace data. Stop the workspace before
        capturing a consistent version.
      </p>
      {error && !action && (
        <p className="form-error" role="alert">
          {error}{' '}
          <button
            className="text-button"
            onClick={() => {
              setError('')
              void load()
            }}
          >
            Retry
          </button>
        </p>
      )}
      {loading ? (
        <p role="status">Loading versions…</p>
      ) : snapshots.length === 0 ? (
        <div className="panel-empty">
          <Icon name="history" size={28} />
          <h3>A little peace of mind.</h3>
          <p>Save a version before your next big change.</p>
        </div>
      ) : (
        <div className="version-list">
          {snapshots.map((snapshot) => (
            <article className="version-row" key={snapshot.id}>
              <span className="version-icon">
                <Icon name="history" size={18} />
              </span>
              <div className="version-info">
                <strong>{snapshot.name}</strong>
                <span>
                  {new Date(snapshot.created_at).toLocaleString()}
                  {snapshot.size_bytes ? ` · ${(snapshot.size_bytes / 1024 / 1024).toFixed(1)} MB` : ''}
                </span>
              </div>
              <div className="version-actions">
                <Btn sm onClick={() => open({ kind: 'fork', snapshot })}>
                  Duplicate
                </Btn>
                <Btn sm onClick={() => open({ kind: 'restore', snapshot })}>
                  Restore
                </Btn>
              </div>
            </article>
          ))}
        </div>
      )}
      {action && (
        <Dialog
          title={
            action.kind === 'capture'
              ? 'Save a version'
              : action.kind === 'fork'
                ? 'Duplicate this project'
                : 'Restore this version?'
          }
          close={() => !busy && setAction(null)}
        >
          <form
            onSubmit={(e) => {
              e.preventDefault()
              void submit()
            }}
          >
            {action.kind === 'restore' ? (
              <p>
                Restore <strong>{action.snapshot.name}</strong>? This replaces the current sandbox and
                workspace with this snapshot. Save a version first if you need to keep your current work.
              </p>
            ) : (
              <label className="form-field">
                {action.kind === 'capture' ? 'Version name' : 'Project name'}
                <input
                  autoFocus
                  required
                  maxLength={160}
                  value={name}
                  onChange={(e) => setName(e.target.value)}
                />
              </label>
            )}
            {action.kind === 'capture' && sb?.status === 'running' && (
              <p className="field-help">
                The workspace is running. Files may change during capture. For a consistent snapshot, cancel
                and stop it from the project menu first.
              </p>
            )}
            {action.kind === 'fork' && (
              <p className="field-help">
                Create an independent project from <strong>{action.snapshot.name}</strong>.
              </p>
            )}
            {error && (
              <p className="form-error" role="alert">
                {error}
              </p>
            )}
            <div className="dialog-actions">
              <Btn disabled={busy} onClick={() => setAction(null)}>
                Cancel
              </Btn>
              <Btn
                type="submit"
                variant={action.kind === 'restore' ? 'danger' : 'primary'}
                disabled={busy || (action.kind !== 'restore' && !name.trim())}
              >
                {busy
                  ? 'Working…'
                  : action.kind === 'capture'
                    ? 'Save version'
                    : action.kind === 'fork'
                      ? 'Create copy'
                      : 'Restore version'}
              </Btn>
            </div>
          </form>
        </Dialog>
      )}
    </section>
  )
}
