import { useEffect, useState, type ReactNode } from 'react'
import { api, Settings as TSettings } from './api'

// Read-only instance settings/operability view (Phase 8A). Renders the safe
// metadata from GET /v1/settings — no editing, no secrets.
export function Settings({ onError }: { onError: (m: string) => void }) {
  const [s, setS] = useState<TSettings | null>(null)

  useEffect(() => {
    api
      .getSettings()
      .then(setS)
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

  return (
    <div className="stack" data-testid="settings-page">
      <h1>Settings</h1>
      <p className="muted">Read-only instance overview. Configuration is set via environment / install.</p>

      <Section title="System overview" testid="settings-system">
        <Row k="Version" v={s.version} />
        {s.git_commit && <Row k="Build" v={s.git_commit} />}
        <Row k="Storage mode" v={s.runtime.storage_mode} />
        <Row k="Base image" v={s.runtime.base_image} />
        <Row k="Idle reaper" v={s.lifecycle.idle_reap_enabled ? `on (${s.lifecycle.idle_threshold_seconds}s)` : 'off'} />
        <Row k="Keepalive max" v={`${s.lifecycle.keepalive_max_seconds}s`} />
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
          This summary never includes tokens or secret values.
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
