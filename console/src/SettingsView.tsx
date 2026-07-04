import { useCallback, useEffect, useState } from 'react'
import { api, Settings as TSettings, Agent, GitCredential } from './api'
import { c, font, mono, Card, H, Btn, Pill, Input } from './design/kit'

export function SettingsView({ onError, toast }: { onError: (m: string) => void; toast: (m: string) => void }) {
  const [s, setS] = useState<TSettings | null>(null)
  const [agents, setAgents] = useState<Agent[]>([])
  const [creds, setCreds] = useState<GitCredential[]>([])
  const [idle, setIdle] = useState(true)
  const [idleSec, setIdleSec] = useState(0)
  const [keepSec, setKeepSec] = useState(0)
  const [gc, setGc] = useState({ name: '', host: '', username: '', token: '' })

  const loadAgents = useCallback(() => api.getAgents().then(setAgents).catch(() => {}), [])
  const loadCreds = useCallback(() => api.listGitCredentials().then(setCreds).catch(() => {}), [])
  useEffect(() => {
    api.getSettings().then((d) => { setS(d); setIdle(d.lifecycle.idle_reap_enabled); setIdleSec(d.lifecycle.idle_threshold_seconds); setKeepSec(d.lifecycle.keepalive_max_seconds) }).catch((e) => onError((e as Error).message))
    loadAgents(); loadCreds()
  }, [onError, loadAgents, loadCreds])

  if (!s) return <div style={{ padding: 40, color: c.muted2 }}>Loading…</div>

  const connectClaude = async () => {
    try { const r = await api.oauthStart('claude-code'); window.open(r.authorize_url, '_blank'); const code = window.prompt('Approve in the opened tab, then paste the code here:'); if (code) { await api.oauthFinish('claude-code', code.trim()); toast('Claude connected'); loadAgents() } } catch (e) { onError((e as Error).message) }
  }
  const apiKey = async (id: string) => { const key = window.prompt(`Paste the ${id} API key:`); if (key) try { await api.setAgentApiKey(id, key.trim()); toast('Connected'); loadAgents() } catch (e) { onError((e as Error).message) } }
  const importCred = async (id: string) => { const v = window.prompt(`Paste the credential (from your ${id} login):`); if (v) try { await api.importAgentCredential(id, v); toast('Connected'); loadAgents() } catch (e) { onError((e as Error).message) } }

  const saveLifecycle = async () => { try { await api.patchSettings({ lifecycle: { idle_reap_enabled: idle, idle_threshold_seconds: idleSec, keepalive_max_seconds: keepSec } }); toast('Lifecycle saved') } catch (e) { onError((e as Error).message) } }
  const addCred = async () => { if (!gc.name || !gc.host || !gc.token) return; try { await api.createGitCredential(gc); setGc({ name: '', host: '', username: '', token: '' }); toast('Credential added'); loadCreds() } catch (e) { onError((e as Error).message) } }

  const Row = ({ k, v }: { k: string; v: React.ReactNode }) => (
    <div style={{ display: 'flex', justifyContent: 'space-between', alignItems: 'center', padding: '5px 0', borderBottom: `1px solid ${c.panel2}`, fontSize: 12.5 }}>
      <span style={{ color: c.muted }}>{k}</span>
      <span style={{ ...mono, fontSize: 11.5 }}>{v}</span>
    </div>
  )

  return (
    <div style={{ maxWidth: 820, margin: '0 auto', padding: '36px 40px 80px' }}>
      <h1 style={{ fontFamily: font.display, fontSize: 24, fontWeight: 700, margin: '0 0 4px' }}>Settings</h1>
      <p style={{ color: c.muted, margin: '0 0 24px' }}>Most settings are read-only (set via environment / install). Only the lifecycle tunables can be edited.</p>

      <Card style={{ padding: 16, marginBottom: 16 }}>
        <H style={{ marginBottom: 8 }}>System</H>
        <Row k="Version" v={s.version} />
        <Row k="Base image" v={s.runtime.base_image} />
        <Row k="Storage mode" v={s.runtime.storage_mode} />
      </Card>

      <Card style={{ padding: 16, marginBottom: 16 }} >
        <H style={{ marginBottom: 10 }}>Agents</H>
        <div data-testid="settings-agents-list">
          {agents.map((a) => (
            <div key={a.id} style={{ display: 'flex', alignItems: 'center', gap: 10, padding: '11px 14px', border: `1px solid ${c.border}`, borderRadius: 8, background: c.panel2, marginBottom: 8 }} data-testid={`agent-${a.id}`}>
              <div>
                <div style={{ fontWeight: 500 }}>{a.label}</div>
                <div style={{ ...mono, fontSize: 11, color: c.muted2 }}>{a.installed_state === 'installed' ? 'installed' : 'not installed'}{a.status === 'connected' && a.method ? ` · via ${a.method === 'oauth' ? 'subscription' : 'API key'}` : ''}</div>
              </div>
              <span style={{ marginLeft: 'auto' }}><Pill tone={a.status === 'connected' ? 'good' : 'warn'}>{a.status === 'connected' ? 'connected' : 'needs login'}</Pill></span>
              {a.status === 'connected' ? (
                <Btn sm variant="ghost" onClick={() => api.disconnectAgent(a.id).then(() => { toast('Disconnected'); loadAgents() })} data-testid="agent-disconnect">Disconnect</Btn>
              ) : (
                <>
                  {a.supports_oauth && <Btn sm onClick={() => (a.id === 'claude-code' ? connectClaude() : importCred(a.id))} data-testid="agent-connect-oauth">Connect subscription</Btn>}
                  {a.supports_api_key && <Btn sm variant="ghost" onClick={() => apiKey(a.id)} data-testid="agent-connect-apikey">Use API key</Btn>}
                </>
              )}
            </div>
          ))}
        </div>
        <div style={{ color: c.muted2, fontSize: 12 }}>Each agent runs on your own account. Credentials are stored opaquely server-side, never shown in the browser, and kept out of every sandbox snapshot.</div>
      </Card>

      <Card style={{ padding: 16, marginBottom: 16 }} data-testid="settings-lifecycle">
        <div style={{ display: 'flex', alignItems: 'center', marginBottom: 10 }}>
          <H>Lifecycle</H><span style={{ marginLeft: 'auto', fontSize: 11.5, color: c.muted2 }}>editable</span>
        </div>
        <div style={{ display: 'grid', gridTemplateColumns: '200px 1fr', gap: '10px 16px', alignItems: 'center', fontSize: 12.5 }}>
          <span style={{ color: c.muted }}>Idle reaping</span>
          <label onClick={() => setIdle((v) => !v)} style={{ display: 'flex', alignItems: 'center', gap: 8, cursor: 'pointer' }}>
            <span style={{ width: 15, height: 15, borderRadius: 4, border: `1px solid ${c.border2}`, background: idle ? c.ink : '#fff', color: '#fff', display: 'flex', alignItems: 'center', justifyContent: 'center', fontSize: 10 }}>{idle ? '✓' : ''}</span>
            <span style={{ color: c.muted }}>{idle ? 'on' : 'off'}</span>
          </label>
          <span style={{ color: c.muted }}>Idle threshold (seconds)</span>
          <Input mono value={idleSec} onChange={(e) => setIdleSec(Number(e.target.value))} style={{ width: 140 }} />
          <span style={{ color: c.muted }}>Keepalive max (seconds)</span>
          <Input mono value={keepSec} onChange={(e) => setKeepSec(Number(e.target.value))} style={{ width: 140 }} />
        </div>
        <Btn onClick={saveLifecycle} style={{ marginTop: 12 }}>Save lifecycle settings</Btn>
      </Card>

      <Card style={{ padding: 16, marginBottom: 16 }}>
        <H style={{ marginBottom: 8 }}>Networking / previews</H>
        <Row k="Preview domain" v={s.networking.preview_domain} />
        <Row k="Preview base" v={s.networking.preview_base} />
        <Row k="TLS" v={s.networking.preview_tls ? 'enabled' : 'plain HTTP'} />
      </Card>

      <Card style={{ padding: 16, marginBottom: 16 }}>
        <H style={{ marginBottom: 8 }}>Security</H>
        <Row k="API auth" v={<Pill tone={s.auth.enabled ? 'good' : 'warn'}>{s.auth.enabled ? 'enabled' : 'disabled'}</Pill>} />
        <Row k="Egress" v={<Pill tone="neutral">{s.egress.mode}</Pill>} />
        <div style={{ color: c.muted2, fontSize: 12, marginTop: 8 }}>Auth tokens, the secrets key, and egress are env/file-only and never shown or editable here.</div>
      </Card>

      <Card style={{ padding: 16 }}>
        <H style={{ marginBottom: 6 }}>Git credentials</H>
        <div style={{ color: c.muted, fontSize: 12.5, marginBottom: 12 }}>Personal access tokens for <b style={{ color: c.fg }}>importing private repos</b>. Stored encrypted; the token is never shown again.</div>
        {creds.map((g) => (
          <div key={g.id} style={{ display: 'flex', alignItems: 'center', gap: 12, padding: '9px 12px', border: `1px solid ${c.border}`, borderRadius: 7, background: c.panel2, marginBottom: 6, fontSize: 12.5 }}>
            <span style={{ fontWeight: 500 }}>{g.name}</span>
            <span style={{ ...mono, fontSize: 11.5, color: c.muted }}>{g.host}</span>
            <a onClick={() => api.deleteGitCredential(g.id).then(loadCreds)} className="dc-hoverink" style={{ marginLeft: 'auto', color: c.muted2, fontSize: 12, cursor: 'pointer' }}>Remove</a>
          </div>
        ))}
        <div style={{ display: 'flex', gap: 8, marginTop: 6, flexWrap: 'wrap' }}>
          <Input value={gc.name} onChange={(e) => setGc({ ...gc, name: e.target.value })} placeholder="name (github)" style={{ width: 150, fontFamily: font.sans }} />
          <Input value={gc.host} onChange={(e) => setGc({ ...gc, host: e.target.value })} placeholder="host (github.com)" style={{ width: 170, fontFamily: font.sans }} />
          <Input value={gc.token} onChange={(e) => setGc({ ...gc, token: e.target.value })} placeholder="access token (write-only)" type="password" style={{ flex: 1, fontFamily: font.sans }} />
          <Btn variant="primary" onClick={addCred}>Add</Btn>
        </div>
      </Card>
    </div>
  )
}
