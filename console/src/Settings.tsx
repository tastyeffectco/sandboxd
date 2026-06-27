import { useEffect, useState, type ReactNode } from 'react'
import { api, Settings as TSettings, GitCredential } from './api'

// Instance settings/operability view (Phase 8A read-only + 8B editable lifecycle
// tunables). Only the lifecycle section is editable (and only if the server says
// so via `editable`); everything else is read-only / env-managed.
export function Settings({ onError }: { onError: (m: string) => void }) {
  const [s, setS] = useState<TSettings | null>(null)
  const [idleEnabled, setIdleEnabled] = useState(true)
  const [idleSec, setIdleSec] = useState(0)
  const [keepSec, setKeepSec] = useState(0)
  const [saving, setSaving] = useState(false)
  const [saved, setSaved] = useState(false)
  const [gitCreds, setGitCreds] = useState<GitCredential[]>([])

  const apply = (d: TSettings) => {
    setS(d)
    setIdleEnabled(d.lifecycle.idle_reap_enabled)
    setIdleSec(d.lifecycle.idle_threshold_seconds)
    setKeepSec(d.lifecycle.keepalive_max_seconds)
  }

  const reloadGitCreds = () =>
    api
      .listGitCredentials()
      .then(setGitCreds)
      .catch((e) => onError((e as Error).message))

  useEffect(() => {
    api
      .getSettings()
      .then(apply)
      .catch((e) => onError((e as Error).message))
    reloadGitCreds()
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [onError])

  if (!s) {
    return (
      <div className="stack" data-testid="settings-page">
        <h1>Settings</h1>
        <p className="muted" data-testid="settings-loading">
          Loading…
        </p>
      </div>
    )
  }

  const canEdit = (s.editable || []).some((e) => e.startsWith('lifecycle.'))

  const save = async () => {
    setSaving(true)
    setSaved(false)
    try {
      const updated = await api.patchSettings({
        lifecycle: {
          idle_reap_enabled: idleEnabled,
          idle_threshold_seconds: idleSec,
          keepalive_max_seconds: keepSec,
        },
      })
      apply(updated)
      setSaved(true)
      setTimeout(() => setSaved(false), 2500)
    } catch (e) {
      onError((e as Error).message)
    } finally {
      setSaving(false)
    }
  }

  return (
    <div className="stack" data-testid="settings-page">
      <h1>Settings</h1>
      <p className="muted">
        Most settings are read-only (set via environment / install). Only the lifecycle tunables below can be edited.
      </p>

      <Section title="System overview" testid="settings-system">
        <Row k="Version" v={s.version} />
        {s.git_commit && <Row k="Build" v={s.git_commit} />}
        <Row k="Storage mode" v={s.runtime.storage_mode} />
        <Row k="Base image" v={s.runtime.base_image} />
      </Section>

      <Section title="Lifecycle" testid="settings-lifecycle">
        {canEdit ? (
          <>
            <label className="row" style={{ justifyContent: 'space-between' }}>
              <span className="muted">Idle reaping</span>
              <input
                type="checkbox"
                data-testid="settings-idle-enabled"
                checked={idleEnabled}
                onChange={(e) => setIdleEnabled(e.target.checked)}
              />
            </label>
            <label className="row" style={{ justifyContent: 'space-between' }}>
              <span className="muted">Idle threshold (seconds)</span>
              <input
                className="input"
                type="number"
                data-testid="settings-idle-threshold"
                value={idleSec}
                onChange={(e) => setIdleSec(Number(e.target.value))}
              />
            </label>
            <label className="row" style={{ justifyContent: 'space-between' }}>
              <span className="muted">Keepalive max (seconds)</span>
              <input
                className="input"
                type="number"
                data-testid="settings-keepalive"
                value={keepSec}
                onChange={(e) => setKeepSec(Number(e.target.value))}
              />
            </label>
            <div className="row">
              <button className="btn btn-primary" data-testid="settings-save" disabled={saving} onClick={save}>
                {saving ? 'Saving…' : 'Save lifecycle settings'}
              </button>
              {saved && (
                <span className="muted" data-testid="settings-saved">
                  Saved — applied live.
                </span>
              )}
            </div>
          </>
        ) : (
          <>
            <Row k="Idle reaping" v={s.lifecycle.idle_reap_enabled ? `on (${s.lifecycle.idle_threshold_seconds}s)` : 'off'} />
            <Row k="Keepalive max" v={`${s.lifecycle.keepalive_max_seconds}s`} />
          </>
        )}
      </Section>

      <Section title="Networking / previews" testid="settings-networking">
        <Row k="Preview domain" v={s.networking.preview_domain} />
        <Row k="Preview base" v={s.networking.preview_base} />
        {s.networking.public_http_port && <Row k="Public HTTP port" v={s.networking.public_http_port} />}
        <Row k="TLS" v={s.networking.preview_tls ? 'enabled' : 'disabled (plain HTTP)'} />
        {s.networking.preview_entrypoint && <Row k="Entrypoint" v={s.networking.preview_entrypoint} />}
      </Section>

      <Section title="Runtime & presets" testid="settings-runtime">
        <Row k="Storage mode" v={s.runtime.storage_mode} />
        <table className="config-table" data-testid="settings-presets">
          <thead>
            <tr>
              <th>Preset</th>
              <th>ID</th>
              <th>Template</th>
            </tr>
          </thead>
          <tbody>
            {s.presets.map((p) => (
              <tr key={p.id}>
                <td>{p.label}</td>
                <td className="mono">{p.id}</td>
                <td className="muted mono">{p.template || '—'}</td>
              </tr>
            ))}
          </tbody>
        </table>
      </Section>

      <Section title="Agents" testid="settings-agents">
        <Row k="Providers" v={s.agents.providers.join(', ') || 'none'} />
      </Section>

      <Section title="Security / auth" testid="settings-security">
        <Row k="API auth" v={s.auth.enabled ? 'enabled' : 'disabled'} />
        <p className="muted" data-testid="settings-security-note">
          Auth tokens, the secrets key, and egress are env/file-only and never shown or editable here.
        </p>
      </Section>

      <Section title="Egress" testid="settings-egress">
        <Row k="Mode" v={s.egress.mode} />
      </Section>

      <Section title="Capabilities" testid="settings-capabilities">
        {Object.entries(s.capabilities).map(([k, v]) => (
          <Row key={k} k={k} v={v ? 'yes' : 'no'} />
        ))}
      </Section>

      <Section title="Git credentials" testid="settings-git">
        <p className="muted">
          Personal access tokens for <b>importing private repos</b> (Git import lands in a later
          v0.4.x release). Stored encrypted on the server; the token is never shown again.
        </p>
        <table className="config-table" data-testid="git-cred-list">
          <thead>
            <tr>
              <th>Name</th>
              <th>Host</th>
              <th>Username</th>
              <th></th>
            </tr>
          </thead>
          <tbody>
            {gitCreds.map((g) => (
              <tr key={g.id} data-testid={`git-cred-${g.id}`}>
                <td>{g.name}</td>
                <td className="muted">{g.host || '—'}</td>
                <td className="muted">{g.username || '—'}</td>
                <td>
                  <button
                    data-testid={`git-cred-delete-${g.id}`}
                    onClick={async () => {
                      try {
                        await api.deleteGitCredential(g.id)
                        reloadGitCreds()
                      } catch (e) {
                        onError((e as Error).message)
                      }
                    }}
                  >
                    Delete
                  </button>
                </td>
              </tr>
            ))}
            {gitCreds.length === 0 && (
              <tr>
                <td colSpan={4} className="muted">
                  No Git credentials yet.
                </td>
              </tr>
            )}
          </tbody>
        </table>
        <GitCredentialForm onAdded={reloadGitCreds} onError={onError} />
      </Section>
    </div>
  )
}

// Add-credential form. The token field is write-only: it is cleared on success
// and never populated from server data, so a token is never rendered after creation.
function GitCredentialForm({ onAdded, onError }: { onAdded: () => void; onError: (m: string) => void }) {
  const [name, setName] = useState('')
  const [host, setHost] = useState('')
  const [username, setUsername] = useState('')
  const [token, setToken] = useState('')
  const [busy, setBusy] = useState(false)

  async function add() {
    if (!name.trim() || !token.trim()) return
    setBusy(true)
    try {
      await api.createGitCredential({
        name: name.trim(),
        host: host.trim() || undefined,
        username: username.trim() || undefined,
        token,
      })
      setName('')
      setHost('')
      setUsername('')
      setToken('') // write-only: clear the token after submit
      onAdded()
    } catch (e) {
      onError((e as Error).message)
    } finally {
      setBusy(false)
    }
  }

  return (
    <div className="row" data-testid="git-cred-form" style={{ gap: 8, flexWrap: 'wrap' }}>
      <input data-testid="git-cred-name" placeholder="name (e.g. github)" value={name} onChange={(e) => setName(e.target.value)} />
      <input data-testid="git-cred-host" placeholder="host (e.g. github.com)" value={host} onChange={(e) => setHost(e.target.value)} />
      <input data-testid="git-cred-username" placeholder="username (optional)" value={username} onChange={(e) => setUsername(e.target.value)} />
      <input
        data-testid="git-cred-token"
        type="password"
        placeholder="access token (write-only)"
        value={token}
        onChange={(e) => setToken(e.target.value)}
      />
      <button data-testid="git-cred-add" disabled={busy || !name.trim() || !token.trim()} onClick={add}>
        {busy ? 'Adding…' : 'Add credential'}
      </button>
    </div>
  )
}

function Section({ title, testid, children }: { title: string; testid: string; children: ReactNode }) {
  return (
    <div className="card" data-testid={testid}>
      <h2 className="card-title">{title}</h2>
      {children}
    </div>
  )
}

function Row({ k, v }: { k: string; v: string }) {
  return (
    <div className="row" style={{ justifyContent: 'space-between' }}>
      <span className="muted">{k}</span>
      <span className="mono">{v}</span>
    </div>
  )
}
