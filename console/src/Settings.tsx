import { useEffect, useState, type ReactNode } from 'react'
import { api, Settings as TSettings, Agent, ConnectSession } from './api'

// Instance settings/operability view (Phase 8A read-only + 8B editable lifecycle
// tunables). Only the lifecycle section is editable (and only if the server says
// so via `editable`); everything else is read-only / env-managed.
export function Settings({ onError }: { onError: (m: string) => void }) {
  const [s, setS] = useState<TSettings | null>(null)
  const [agents, setAgents] = useState<Agent[]>([])
  const [connectOpen, setConnectOpen] = useState(false)
  const [idleEnabled, setIdleEnabled] = useState(true)
  const [idleSec, setIdleSec] = useState(0)
  const [keepSec, setKeepSec] = useState(0)
  const [saving, setSaving] = useState(false)
  const [saved, setSaved] = useState(false)

  const apply = (d: TSettings) => {
    setS(d)
    setIdleEnabled(d.lifecycle.idle_reap_enabled)
    setIdleSec(d.lifecycle.idle_threshold_seconds)
    setKeepSec(d.lifecycle.keepalive_max_seconds)
  }

  useEffect(() => {
    api
      .getSettings()
      .then(apply)
      .catch((e) => onError((e as Error).message))
    api
      .getAgents()
      .then(setAgents)
      .catch((e) => onError((e as Error).message))
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

      <Section title="AI Agents" testid="settings-agents">
        <table className="config-table" data-testid="settings-agents-list">
          <thead>
            <tr>
              <th>Provider</th>
              <th>Installed</th>
              <th>Status</th>
              <th>Action</th>
            </tr>
          </thead>
          <tbody>
            {agents.map((a) => (
              <tr key={a.id} data-testid={`agent-${a.id}`}>
                <td>{a.label}</td>
                <td className="muted">
                  {a.installed_state === 'installed'
                    ? 'yes'
                    : a.installed_state === 'not_installed'
                      ? 'not installed'
                      : 'unknown'}
                </td>
                <td>
                  <span className={`badge ${a.status === 'connected' ? 'running' : 'stopped'}`}>
                    {a.status === 'connected' ? 'connected' : 'needs login'}
                  </span>
                </td>
                <td>
                  {a.id === 'claude-code' ? (
                    a.status === 'connected' ? (
                      <span className="agent-actions">
                        <button data-testid="agent-reconnect" onClick={() => setConnectOpen(true)}>
                          Reconnect
                        </button>
                        <button
                          data-testid="agent-disconnect"
                          onClick={async () => {
                            try {
                              await api.disconnectClaude()
                              api.getAgents().then(setAgents)
                            } catch (e) {
                              onError((e as Error).message)
                            }
                          }}
                        >
                          Disconnect
                        </button>
                      </span>
                    ) : (
                      <button data-testid="agent-connect" onClick={() => setConnectOpen(true)}>
                        Use your Claude subscription
                      </button>
                    )
                  ) : (
                    <span className="muted">—</span>
                  )}
                </td>
              </tr>
            ))}
          </tbody>
        </table>
        <p className="muted">
          Only Claude Code supports console login today. No token is ever shown or stored in the
          browser.
        </p>
      </Section>

      {connectOpen && (
        <ClaudeConnectModal
          onClose={() => setConnectOpen(false)}
          onConnected={() => {
            setConnectOpen(false)
            api.getAgents().then(setAgents)
          }}
          onError={onError}
        />
      )}

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

// Console-driven Claude Code login. sandboxd runs the official `claude
// setup-token` flow in an ephemeral auth container; we only show the login URL
// and relay the pasted code. No token is ever shown or stored in the browser.
function ClaudeConnectModal({
  onClose,
  onConnected,
  onError,
}: {
  onClose: () => void
  onConnected: () => void
  onError: (m: string) => void
}) {
  const [session, setSession] = useState<ConnectSession | null>(null)
  const [code, setCode] = useState('')
  const [busy, setBusy] = useState(false)
  const sleep = (ms: number) => new Promise((r) => setTimeout(r, ms))

  useEffect(() => {
    let cancelled = false
    ;(async () => {
      try {
        let s = await api.connectClaude()
        if (!cancelled) setSession(s)
        let tries = 0
        while (!cancelled && s.status === 'starting' && tries < 40) {
          await sleep(800)
          s = await api.getClaudeConnect(s.session_id)
          if (!cancelled) setSession(s)
          tries++
        }
      } catch (e) {
        if (!cancelled) onError((e as Error).message)
      }
    })()
    return () => {
      cancelled = true
    }
  }, [onError])

  async function submit() {
    if (!session || !code.trim()) return
    setBusy(true)
    try {
      let s = await api.submitClaudeCode(session.session_id, code.trim())
      setSession(s)
      let tries = 0
      while (s.status === 'finalizing' && tries < 40) {
        await sleep(800)
        s = await api.getClaudeConnect(session.session_id)
        setSession(s)
        tries++
      }
      if (s.status === 'connected') onConnected()
    } catch (e) {
      onError((e as Error).message)
    } finally {
      setBusy(false)
    }
  }

  const status = session?.status ?? 'starting'
  return (
    <div className="modal-backdrop" data-testid="claude-connect-modal">
      <div className="card">
        <h2 className="card-title">Connect Claude Code</h2>
        <p className="muted">
          Use your Claude subscription. No token is shown or stored in the browser.
        </p>
        {status === 'starting' && <p data-testid="claude-starting">Starting login…</p>}
        {(status === 'awaiting_code' || status === 'finalizing') && (
          <>
            <p>1. Open this URL, sign in with your Claude subscription, and copy the code:</p>
            {session?.url && (
              <a
                href={session.url}
                target="_blank"
                rel="noreferrer"
                className="mono"
                data-testid="claude-connect-url"
              >
                {session.url}
              </a>
            )}
            <p>2. Paste the code here:</p>
            <input
              data-testid="claude-code-input"
              value={code}
              onChange={(e) => setCode(e.target.value)}
              placeholder="authorization code"
            />
            <button
              data-testid="claude-code-submit"
              disabled={busy || !code.trim()}
              onClick={submit}
            >
              {busy ? 'Finishing…' : 'Submit code'}
            </button>
          </>
        )}
        {status === 'connected' && <p data-testid="claude-connected">Connected.</p>}
        {status === 'failed' && (
          <p data-testid="claude-failed" className="error">
            {session?.error || 'Login failed.'}
          </p>
        )}
        <div className="row">
          <button data-testid="claude-connect-close" onClick={onClose}>
            Close
          </button>
        </div>
      </div>
    </div>
  )
}
