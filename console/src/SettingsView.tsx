import { useCallback, useEffect, useState, ReactNode } from 'react'
import { api, Settings as TSettings, Agent, GitCredential, ApiKey } from './api'
import { c, font, mono, Card, H, Btn, Pill, Input } from './design/kit'

// --- source badges: every value on this page is one of these ----------------
// editable  → change it here, saved live via PATCH /v1/settings
// env · VAR → set at deploy through that environment variable; restart to change
// managed   → file/secret, never shown or editable
// derived   → computed by the server from other values
type SrcKind = 'editable' | 'managed' | 'derived' | 'build'
function Src({ env, kind }: { env?: string; kind?: SrcKind }) {
  const base = { ...mono, fontSize: 10, borderRadius: 5, padding: '1px 7px', whiteSpace: 'nowrap' as const, border: `1px solid ${c.border2}` }
  if (kind === 'editable') return <span style={{ ...base, color: c.good, background: `${c.good}14`, border: `1px solid ${c.good}40` }}>editable</span>
  if (env) return <span title={`Set the ${env} environment variable at deploy time, then restart`} style={{ ...base, color: c.muted, background: c.panel2 }}>env · {env}</span>
  const label = kind === 'managed' ? 'managed' : kind === 'derived' ? 'derived' : 'build'
  return <span style={{ ...base, color: c.muted2, background: c.panel2 }}>{label}</span>
}

// One read-only instance field: label · value · where it comes from.
function Field({ label, value, env, kind }: { label: string; value: ReactNode; env?: string; kind?: SrcKind }) {
  return (
    <div style={{ display: 'grid', gridTemplateColumns: '150px 1fr auto', gap: 12, alignItems: 'center', padding: '7px 0', borderBottom: `1px solid ${c.panel2}`, fontSize: 12.5 }}>
      <span style={{ color: c.muted }}>{label}</span>
      <span style={{ ...mono, fontSize: 11.5, color: c.fg2, overflow: 'hidden', textOverflow: 'ellipsis' }}>{value}</span>
      <Src env={env} kind={kind} />
    </div>
  )
}

function SectionTitle({ children, note }: { children: ReactNode; note?: string }) {
  return (
    <div style={{ display: 'flex', alignItems: 'baseline', gap: 10, margin: '30px 0 12px' }}>
      <div style={{ fontFamily: font.display, fontSize: 12.5, fontWeight: 600, letterSpacing: '.8px', textTransform: 'uppercase', color: c.muted }}>{children}</div>
      {note && <div style={{ fontSize: 12, color: c.muted2 }}>{note}</div>}
    </div>
  )
}

export function SettingsView({ onError, toast }: { onError: (m: string) => void; toast: (m: string) => void }) {
  const [s, setS] = useState<TSettings | null>(null)
  const [agents, setAgents] = useState<Agent[]>([])
  const [creds, setCreds] = useState<GitCredential[]>([])
  const [idle, setIdle] = useState(true)
  const [idleSec, setIdleSec] = useState(0)
  const [keepSec, setKeepSec] = useState(0)
  const [gc, setGc] = useState({ name: '', host: '', username: '', token: '' })
  const [showPrompt, setShowPrompt] = useState(false)
  const [curPw, setCurPw] = useState('')
  const [newPw, setNewPw] = useState('')
  const [keys, setKeys] = useState<ApiKey[]>([])
  const [keyName, setKeyName] = useState('')
  const [newKey, setNewKey] = useState('')

  const loadAgents = useCallback(() => api.getAgents().then(setAgents).catch(() => {}), [])
  const loadCreds = useCallback(() => api.listGitCredentials().then(setCreds).catch(() => {}), [])
  const loadKeys = useCallback(() => api.listApiKeys().then(setKeys).catch(() => {}), [])
  useEffect(() => {
    api.getSettings().then((d) => { setS(d); setIdle(d.lifecycle.idle_reap_enabled); setIdleSec(d.lifecycle.idle_threshold_seconds); setKeepSec(d.lifecycle.keepalive_max_seconds) }).catch((e) => onError((e as Error).message))
    loadAgents(); loadCreds(); loadKeys()
  }, [onError, loadAgents, loadCreds, loadKeys])

  if (!s) return <div style={{ padding: 40, color: c.muted2 }}>Loading…</div>
  const lifecycleEditable = (s.editable || []).some((p) => p.startsWith('lifecycle.'))

  const connectClaude = async () => {
    try { const r = await api.oauthStart('claude-code'); window.open(r.authorize_url, '_blank'); const code = window.prompt('Approve in the opened tab, then paste the code here:'); if (code) { await api.oauthFinish('claude-code', code.trim()); toast('Claude connected'); loadAgents() } } catch (e) { onError((e as Error).message) }
  }
  const apiKey = async (id: string) => { const key = window.prompt(`Paste the ${id} API key:`); if (key) try { await api.setAgentApiKey(id, key.trim()); toast('Connected'); loadAgents() } catch (e) { onError((e as Error).message) } }
  const importCred = async (id: string) => { const v = window.prompt(`Paste the credential (from your ${id} login):`); if (v) try { await api.importAgentCredential(id, v); toast('Connected'); loadAgents() } catch (e) { onError((e as Error).message) } }
  const saveLifecycle = async () => { try { await api.patchSettings({ lifecycle: { idle_reap_enabled: idle, idle_threshold_seconds: idleSec, keepalive_max_seconds: keepSec } }); toast('Lifecycle saved') } catch (e) { onError((e as Error).message) } }
  const addCred = async () => { if (!gc.name || !gc.host || !gc.token) return; try { await api.createGitCredential(gc); setGc({ name: '', host: '', username: '', token: '' }); toast('Credential added'); loadCreds() } catch (e) { onError((e as Error).message) } }
  const changePw = async () => { if (!curPw || !newPw) return; try { await api.changePassword({ current_password: curPw, new_password: newPw }); setCurPw(''); setNewPw(''); toast('Password changed') } catch (e) { onError((e as Error).message) } }
  const signOutEverywhere = async () => { try { await api.logoutEverywhere(); location.reload() } catch (e) { onError((e as Error).message) } }
  const createKey = async () => { if (!keyName.trim()) return; try { const k = await api.createApiKey(keyName.trim()); setNewKey(k.key); setKeyName(''); loadKeys() } catch (e) { onError((e as Error).message) } }
  const copyKey = () => { navigator.clipboard?.writeText(newKey).then(() => toast('Copied')).catch(() => {}) }

  const legendItem = (badge: ReactNode, text: string) => (
    <div style={{ display: 'flex', alignItems: 'center', gap: 7 }}>{badge}<span style={{ fontSize: 11.5, color: c.muted }}>{text}</span></div>
  )

  return (
    <div style={{ maxWidth: 780, margin: '0 auto', padding: '36px 40px 90px' }}>
      <h1 style={{ fontFamily: font.display, fontSize: 24, fontWeight: 700, margin: '0 0 4px' }}>Settings</h1>
      <p style={{ color: c.muted, margin: '0 0 16px' }}>Connect agents and tune the sandbox lifecycle here. Everything else is fixed at deploy time — each value below shows where to change it.</p>

      {/* legend — how to read the page */}
      <Card style={{ padding: '12px 16px', marginBottom: 8, display: 'flex', gap: 20, flexWrap: 'wrap', background: c.panel3 }}>
        {legendItem(<Src kind="editable" />, 'change here, saved instantly')}
        {legendItem(<Src env="VAR" />, 'set at deploy — change the env var + restart')}
        {legendItem(<Src kind="managed" />, 'file or secret — never shown')}
      </Card>

      {/* ─────────────── YOU MANAGE HERE ─────────────── */}
      <SectionTitle note="agents, credentials & lifecycle — changes take effect immediately">You manage here</SectionTitle>

      <Card style={{ padding: 16, marginBottom: 12 }}>
        <H style={{ marginBottom: 10 }}>Agents</H>
        <div data-testid="settings-agents-list">
          {agents.map((a) => {
            // Codex is disabled for now — its ChatGPT-subscription auth can't be
            // put behind the credential proxy yet (see the note below).
            const disabled = a.id === 'codex'
            return (
            <div key={a.id} style={{ display: 'flex', alignItems: 'center', gap: 10, padding: '11px 14px', border: `1px solid ${c.border}`, borderRadius: 8, background: c.panel2, marginBottom: 8, opacity: disabled ? 0.6 : 1 }} data-testid={`agent-${a.id}`}>
              <div>
                <div style={{ fontWeight: 500 }}>{a.label}</div>
                <div style={{ ...mono, fontSize: 11, color: c.muted2 }}>{disabled ? 'temporarily unavailable' : `${a.installed_state === 'installed' ? 'installed' : 'not installed'}${a.status === 'connected' && a.method ? ` · via ${a.method === 'oauth' ? 'subscription' : 'API key'}` : ''}`}</div>
              </div>
              {disabled ? (
                <span style={{ marginLeft: 'auto' }}><Pill tone="neutral">disabled</Pill></span>
              ) : (<>
                <span style={{ marginLeft: 'auto' }}><Pill tone={a.status === 'connected' ? 'good' : 'warn'}>{a.status === 'connected' ? 'connected' : 'needs login'}</Pill></span>
                {a.status === 'connected' ? (
                  <Btn sm variant="ghost" onClick={() => api.disconnectAgent(a.id).then(() => { toast('Disconnected'); loadAgents() })} data-testid="agent-disconnect">Disconnect</Btn>
                ) : (
                  <>
                    {a.supports_oauth && <Btn sm onClick={() => (a.id === 'claude-code' ? connectClaude() : importCred(a.id))} data-testid="agent-connect-oauth">Connect subscription</Btn>}
                    {a.supports_api_key && <Btn sm variant="ghost" onClick={() => apiKey(a.id)} data-testid="agent-connect-apikey">Use API key</Btn>}
                  </>
                )}
              </>)}
            </div>
          )})}
        </div>
        <div style={{ color: c.muted2, fontSize: 12, lineHeight: 1.5 }}>Each agent runs on your own account; credentials are stored opaquely server-side, never shown in the browser, and kept out of snapshots. <b style={{ color: c.fg2 }}>Claude Code</b> subscriptions run through a credential-injecting proxy — the token never enters the sandbox. <b style={{ color: c.fg2 }}>Codex is disabled for now</b>: its ChatGPT-subscription auth uses a WebSocket backend that can't yet be put behind that proxy, and we won't mount a raw token into the sandbox.</div>
      </Card>

      <Card style={{ padding: 16, marginBottom: 12 }} data-testid="settings-lifecycle">
        <div style={{ display: 'flex', alignItems: 'center', gap: 8, marginBottom: 10 }}>
          <H>Lifecycle</H><Src kind={lifecycleEditable ? 'editable' : undefined} env={lifecycleEditable ? undefined : 'SANDBOXD_IDLE_THRESHOLD_SECONDS'} />
          <span style={{ marginLeft: 'auto', fontSize: 11.5, color: c.muted2 }}>when idle sandboxes are paused/stopped</span>
        </div>
        <div style={{ display: 'grid', gridTemplateColumns: '210px 1fr', gap: '10px 16px', alignItems: 'center', fontSize: 12.5 }}>
          <span style={{ color: c.muted }}>Idle reaping</span>
          <label onClick={() => lifecycleEditable && setIdle((v) => !v)} style={{ display: 'flex', alignItems: 'center', gap: 8, cursor: lifecycleEditable ? 'pointer' : 'default', opacity: lifecycleEditable ? 1 : 0.6 }}>
            <span style={{ width: 15, height: 15, borderRadius: 4, border: `1px solid ${c.border2}`, background: idle ? c.ink : '#fff', color: '#fff', display: 'flex', alignItems: 'center', justifyContent: 'center', fontSize: 10 }}>{idle ? '✓' : ''}</span>
            <span style={{ color: c.muted }}>{idle ? 'on' : 'off'}</span>
          </label>
          <span style={{ color: c.muted }}>Idle threshold (seconds)</span>
          <Input mono value={idleSec} onChange={(e) => setIdleSec(Number(e.target.value))} disabled={!lifecycleEditable} style={{ width: 140 }} />
          <span style={{ color: c.muted }}>Keepalive max (seconds)</span>
          <Input mono value={keepSec} onChange={(e) => setKeepSec(Number(e.target.value))} disabled={!lifecycleEditable} style={{ width: 140 }} />
        </div>
        {lifecycleEditable
          ? <Btn onClick={saveLifecycle} style={{ marginTop: 12 }}>Save lifecycle settings</Btn>
          : <div style={{ marginTop: 10, fontSize: 12, color: c.muted2 }}>Read-only on this instance — set the <span style={{ ...mono, fontSize: 11 }}>SANDBOXD_IDLE_*</span> / <span style={{ ...mono, fontSize: 11 }}>SANDBOXD_KEEPALIVE_MAX_SECONDS</span> env vars.</div>}
      </Card>

      <Card style={{ padding: 16 }}>
        <H style={{ marginBottom: 6 }}>Git credentials</H>
        <div style={{ color: c.muted, fontSize: 12.5, marginBottom: 12 }}>Personal access tokens for <b style={{ color: c.fg }}>importing private repos</b>. Stored encrypted; the token is never shown again.</div>
        {creds.length === 0 && <div style={{ color: c.muted2, fontSize: 12, marginBottom: 8 }}>No credentials yet.</div>}
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

      <Card style={{ padding: 16, marginTop: 12 }} data-testid="settings-security">
        <H style={{ marginBottom: 6 }}>Security</H>
        <div style={{ color: c.muted, fontSize: 12.5, marginBottom: 12 }}>Change the console password, or sign out every session (clears the cookie everywhere).</div>
        <div style={{ display: 'flex', gap: 8, flexWrap: 'wrap', alignItems: 'center' }}>
          <Input value={curPw} onChange={(e) => setCurPw(e.target.value)} placeholder="current password" type="password" style={{ width: 200, fontFamily: font.sans }} data-testid="security-current" />
          <Input value={newPw} onChange={(e) => setNewPw(e.target.value)} placeholder="new password (min 8)" type="password" style={{ width: 200, fontFamily: font.sans }} data-testid="security-new" />
          <Btn variant="primary" onClick={changePw} data-testid="security-change">Change password</Btn>
          <Btn variant="danger" onClick={signOutEverywhere} style={{ marginLeft: 'auto' }} data-testid="security-signout">Sign out everywhere</Btn>
        </div>
      </Card>

      <Card style={{ padding: 16, marginTop: 12 }} data-testid="settings-api-keys">
        <H style={{ marginBottom: 6 }}>API keys</H>
        <div style={{ color: c.muted, fontSize: 12.5, marginBottom: 12 }}>Programmatic access to the <span style={{ ...mono, fontSize: 11.5 }}>/v1</span> API. The full key is shown once at creation — store it somewhere safe.</div>
        {keys.length === 0 && <div style={{ color: c.muted2, fontSize: 12, marginBottom: 8 }}>No API keys yet.</div>}
        {keys.map((k) => (
          <div key={k.id} style={{ display: 'flex', alignItems: 'center', gap: 12, padding: '9px 12px', border: `1px solid ${c.border}`, borderRadius: 7, background: c.panel2, marginBottom: 6, fontSize: 12.5 }} data-testid={`api-key-${k.id}`}>
            <span style={{ fontWeight: 500 }}>{k.name}</span>
            <span style={{ ...mono, fontSize: 11.5, color: c.muted }}>{k.prefix}…</span>
            <span style={{ fontSize: 11.5, color: c.muted2 }}>{k.last_used_at || 'never used'}</span>
            <a onClick={() => api.revokeApiKey(k.id).then(loadKeys)} className="dc-hoverink" style={{ marginLeft: 'auto', color: c.muted2, fontSize: 12, cursor: 'pointer' }} data-testid="api-key-revoke">Revoke</a>
          </div>
        ))}
        {newKey && (
          <div style={{ marginTop: 10, padding: '12px 14px', border: `1px solid ${c.good}40`, background: `${c.good}14`, borderRadius: 8 }} data-testid="api-key-new">
            <div style={{ fontSize: 12, color: c.good, fontWeight: 600, marginBottom: 6 }}>Copy it now — it won't be shown again.</div>
            <div style={{ display: 'flex', gap: 8, alignItems: 'center' }}>
              <Input mono readOnly value={newKey} onFocus={(e) => e.target.select()} style={{ flex: 1, fontSize: 11.5 }} data-testid="api-key-new-value" />
              <Btn onClick={copyKey}>Copy</Btn>
              <Btn variant="ghost" onClick={() => setNewKey('')}>Done</Btn>
            </div>
          </div>
        )}
        <div style={{ display: 'flex', gap: 8, marginTop: 10, flexWrap: 'wrap' }}>
          <Input value={keyName} onChange={(e) => setKeyName(e.target.value)} onKeyDown={(e) => e.key === 'Enter' && createKey()} placeholder="key name (ci, laptop…)" style={{ flex: 1, fontFamily: font.sans }} data-testid="api-key-name" />
          <Btn variant="primary" onClick={createKey} data-testid="api-key-create">Create key</Btn>
        </div>
      </Card>

      {/* ─────────────── INSTANCE CONFIGURATION (read-only) ─────────────── */}
      <SectionTitle note="fixed at deploy — the badge shows the env var to change each one">Instance configuration</SectionTitle>

      <Card style={{ padding: '6px 16px 12px', marginBottom: 12 }}>
        <div style={{ ...mono, fontSize: 10, color: c.muted2, letterSpacing: '.5px', padding: '12px 0 2px' }}>SYSTEM</div>
        <Field label="Version" value={s.version + (s.git_commit ? ` · ${s.git_commit.slice(0, 7)}` : '')} kind="build" />
        <Field label="Base image" value={s.runtime.base_image} env="SANDBOXD_IMAGE" />
        <Field label="Storage mode" value={s.runtime.storage_mode} kind="build" />

        <div style={{ ...mono, fontSize: 10, color: c.muted2, letterSpacing: '.5px', padding: '16px 0 2px' }}>NETWORKING &amp; PREVIEWS</div>
        <Field label="Preview domain" value={s.networking.preview_domain} env="PREVIEW_DOMAIN" />
        <Field label="Public HTTP port" value={s.networking.public_http_port || '—'} env="HTTP_PORT" />
        <Field label="Preview base" value={s.networking.preview_base} kind="derived" />
        <Field label="TLS" value={<Pill tone={s.networking.preview_tls ? 'good' : 'warn'}>{s.networking.preview_tls ? 'enabled' : 'plain HTTP'}</Pill>} env="PREVIEW_TLS" />
        <Field label="Entrypoint" value={s.networking.preview_entrypoint || 'web'} env="PREVIEW_ENTRYPOINT" />

        <div style={{ ...mono, fontSize: 10, color: c.muted2, letterSpacing: '.5px', padding: '16px 0 2px' }}>SECURITY</div>
        <Field label="API auth" value={<Pill tone={s.auth.enabled ? 'good' : 'bad'}>{s.auth.enabled ? 'enabled' : 'disabled'}</Pill>} env="SANDBOXD_API_AUTH_DISABLED" />
        <Field label="Sandbox egress" value={<Pill tone="neutral">{s.egress.mode}</Pill>} kind="build" />
        <Field label="Secrets key" value="set (never shown)" kind="managed" />
      </Card>

      {/* ─────────────── REFERENCE ─────────────── */}
      <SectionTitle>Reference</SectionTitle>
      {s.agents.system_prompt && (
        <Card style={{ padding: 16 }} data-testid="settings-system-prompt">
          <div style={{ display: 'flex', alignItems: 'center', gap: 8 }}>
            <H>Agent system prompt</H>
            <Src kind="managed" />
            <span style={{ marginLeft: 'auto', fontSize: 11.5, color: c.muted2 }}>from <span style={{ ...mono, fontSize: 11 }}>prompt.md</span></span>
          </div>
          <div style={{ color: c.muted, fontSize: 12.5, margin: '6px 0 0' }}>Appended to every agent run so it understands the sandbox and its guardrails. Ports/paths shown use defaults; each sandbox renders its real values at run time.</div>
          <a onClick={() => setShowPrompt((v) => !v)} className="dc-hoverink" style={{ display: 'inline-block', marginTop: 10, fontSize: 12, color: c.link, cursor: 'pointer' }}>{showPrompt ? 'Hide' : 'Show'} the full prompt {showPrompt ? '▲' : '▼'}</a>
          {showPrompt && (
            <pre style={{ background: c.bg, border: `1px solid ${c.border}`, borderRadius: 7, padding: '12px 14px', ...mono, fontSize: 11.5, color: c.fg2, margin: '10px 0 0', maxHeight: 320, overflow: 'auto', lineHeight: 1.55, whiteSpace: 'pre-wrap' }}>{s.agents.system_prompt}</pre>
          )}
        </Card>
      )}
    </div>
  )
}
