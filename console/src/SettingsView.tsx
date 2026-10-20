import { useCallback, useEffect, useState, ReactNode } from 'react'
import { api, Settings as TSettings, Agent, GitCredential, ApiKey } from './api'
import { c, font, mono, Card, H, Btn, Pill, Input } from './design/kit'
import { Icon } from './design/Icon'
import { Dialog } from './design/Dialog'
import { AgentConnection, ConnectMode } from './AgentConnection'
import { ModelProviders } from './ModelProviders'
import { ApiKeyRow, ApiKeyAccessSelect, KeyAccess, accessScopes } from './ApiKeys'

// --- source badges: every value on this page is one of these ----------------
// editable  → change it here, saved live via PATCH /v1/settings
// env · VAR → set at deploy through that environment variable; restart to change
// managed   → file/secret, never shown or editable
// derived   → computed by the server from other values
type SrcKind = 'editable' | 'managed' | 'derived' | 'build'
function Src({ env, kind }: { env?: string; kind?: SrcKind }) {
  const base = {
    ...mono,
    fontSize: 10,
    borderRadius: 5,
    padding: '1px 7px',
    whiteSpace: 'nowrap' as const,
    border: `1px solid ${c.border2}`,
  }
  if (kind === 'editable')
    return (
      <span style={{ ...base, color: c.good, background: `${c.good}14`, border: `1px solid ${c.good}40` }}>
        editable
      </span>
    )
  if (env)
    return (
      <span
        title={`Set the ${env} environment variable at deploy time, then restart`}
        style={{ ...base, color: c.muted, background: c.panel2 }}
      >
        env · {env}
      </span>
    )
  const label = kind === 'managed' ? 'managed' : kind === 'derived' ? 'derived' : 'build'
  return <span style={{ ...base, color: c.muted2, background: c.panel2 }}>{label}</span>
}

// One read-only instance field: label · value · where it comes from.
function Field({
  label,
  value,
  env,
  kind,
}: {
  label: string
  value: ReactNode
  env?: string
  kind?: SrcKind
}) {
  return (
    <div
      style={{
        display: 'grid',
        gridTemplateColumns: '150px 1fr auto',
        gap: 12,
        alignItems: 'center',
        padding: '7px 0',
        borderBottom: `1px solid ${c.panel2}`,
        fontSize: 12.5,
      }}
    >
      <span style={{ color: c.muted }}>{label}</span>
      <span style={{ ...mono, fontSize: 11.5, color: c.fg2, overflow: 'hidden', textOverflow: 'ellipsis' }}>
        {value}
      </span>
      <Src env={env} kind={kind} />
    </div>
  )
}

const SECTIONS = [
  { id: 'models', title: 'Model connections', note: 'Local models and remote endpoints', icon: 'box' },
  { id: 'agents', title: 'Coding agents', note: 'Accounts, subscriptions and defaults', icon: 'spark' },
  { id: 'git', title: 'Git credentials', note: 'Connect private repositories', icon: 'git' },
  { id: 'access', title: 'Security & API keys', note: 'Manage access to your instance', icon: 'lock' },
  {
    id: 'lifecycle',
    title: 'Workspace lifecycle',
    note: 'Balance availability and resources',
    icon: 'history',
  },
  { id: 'instance', title: 'Instance', note: 'Networking, runtime and system context', icon: 'settings' },
] as const
export function SettingsView({
  onError,
  toast,
  initialSection,
}: {
  initialSection?: string
  onError: (m: string) => void
  toast: (m: string) => void
}) {
  const [section, setSection] = useState<string>(
    SECTIONS.some((s) => s.id === initialSection) ? initialSection! : 'models',
  )
  useEffect(() => {
    if (SECTIONS.some((s) => s.id === initialSection)) setSection(initialSection!)
  }, [initialSection])
  const [s, setS] = useState<TSettings | null>(null)
  const [agents, setAgents] = useState<Agent[]>([])
  const [creds, setCreds] = useState<GitCredential[]>([])
  const [keys, setKeys] = useState<ApiKey[]>([])
  const [idle, setIdle] = useState(true)
  const [idleSec, setIdleSec] = useState(2100)
  const [keepSec, setKeepSec] = useState(86400)
  const [models, setModels] = useState<Record<string, string>>({})
  const [showPrompt, setShowPrompt] = useState(false)
  const [connecting, setConnecting] = useState<{ agent: Agent; mode: ConnectMode } | null>(null)
  const [gitOpen, setGitOpen] = useState(false)
  const [gc, setGc] = useState({ name: '', host: 'github.com', username: '', token: '' })
  const [curPw, setCurPw] = useState('')
  const [newPw, setNewPw] = useState('')
  const [keyName, setKeyName] = useState('')
  const [keyAccess, setKeyAccess] = useState<KeyAccess>('')
  const [newKey, setNewKey] = useState('')
  const [busy, setBusy] = useState(false)
  const [error, setError] = useState('')
  const [confirm, setConfirm] = useState<{ title: string; body: string; run: () => Promise<unknown> } | null>(
    null,
  )
  const loadAgents = useCallback(() => api.getAgents().then(setAgents), [])
  const loadCreds = useCallback(() => api.listGitCredentials().then(setCreds), [])
  const loadKeys = useCallback(() => api.listApiKeys().then(setKeys), [])
  const load = useCallback(() => {
    setError('')
    Promise.all([api.getSettings(), loadAgents(), loadCreds(), loadKeys()])
      .then(([d]) => {
        setS(d)
        setIdle(d.lifecycle.idle_reap_enabled)
        setIdleSec(d.lifecycle.idle_threshold_seconds)
        setKeepSec(d.lifecycle.keepalive_max_seconds)
        setModels(d.agents.default_models || {})
      })
      .catch((e) => setError((e as Error).message))
  }, [loadAgents, loadCreds, loadKeys])
  useEffect(load, [load])
  const run = async (fn: () => Promise<unknown>, message?: string) => {
    if (busy) return
    setBusy(true)
    setError('')
    try {
      await fn()
      if (message) toast(message)
    } catch (e) {
      setError((e as Error).message)
    } finally {
      setBusy(false)
    }
  }
  if (!s)
    return (
      <div className="content-page">
        {error ? (
          <div role="alert" className="inline-error">
            {error}
            <Btn onClick={load}>Retry settings</Btn>
          </div>
        ) : (
          <p role="status">Loading settings…</p>
        )}
      </div>
    )
  const lifecycleEditable = (s.editable || []).some((p) => p.startsWith('lifecycle.'))
  const modelsEditable = (s.editable || []).includes('agents.default_models')
  const selected = SECTIONS.find((p) => p.id === section)!
  return (
    <div className="settings-page">
      <header className="page-heading">
        <div className="eyebrow">YOUR WORKSPACE, YOUR WAY</div>
        <h1>Settings</h1>
        <p>Connect your tools. Choose your models. Stay in control.</p>
      </header>
      <div className="settings-layout">
        <nav className="settings-nav" aria-label="Settings sections">
          {SECTIONS.map((p) => (
            <button
              key={p.id}
              className={section === p.id ? 'active' : ''}
              aria-current={section === p.id ? 'page' : undefined}
              onClick={() => {
                setSection(p.id)
                history.replaceState(null, '', `#/settings/${p.id}`)
                setError('')
                setCurPw('')
                setNewPw('')
                setNewKey('')
              }}
            >
              <Icon name={p.icon} size={18} />
              <span>
                <strong>{p.title}</strong>
                <small>{p.note}</small>
              </span>
            </button>
          ))}
        </nav>
        <div className="settings-content">
          <div className="settings-section-heading">
            <h2>{selected.title}</h2>
            <p>{selected.note}</p>
          </div>
          {error && (
            <p role="alert" className="inline-error">
              {error}
            </p>
          )}
          {section === 'models' && <ModelProviders onError={onError} toast={toast} />}
          {section === 'agents' && (
            <>
              <div className="agent-accounts" data-testid="settings-agents-list">
                {agents.map((a) => (
                  <article className="agent-account" data-testid={`agent-${a.id}`} key={a.id}>
                    <div className="agent-account-header">
                      <span className="account-mark">{a.label.slice(0, 1)}</span>
                      <div>
                        <h3>{a.label}</h3>
                        <p>
                          {a.id === 'pi'
                            ? 'Flexible agent for your own models'
                            : a.id === 'opencode'
                              ? 'Build with the free tier or your own account'
                              : a.id === 'claude-code'
                                ? 'Build with your Claude account'
                                : a.runnable
                                  ? 'Coding agent'
                                  : 'Model gateway for OpenCode'}
                        </p>
                      </div>
                      <Pill tone={a.status === 'connected' ? 'good' : 'neutral'}>
                        {a.id === 'codex'
                          ? 'Unavailable'
                          : a.status === 'connected'
                            ? 'Connected'
                            : a.id === 'pi'
                              ? 'Model connection'
                              : a.id === 'opencode'
                                ? 'Ready to start'
                                : 'Not connected'}
                      </Pill>
                    </div>
                    <div className="agent-account-actions">
                      <span>
                        {a.installed_state === 'installed' ? 'Installed' : 'Not installed'}
                        {a.method ? ` · ${a.method === 'oauth' ? 'Subscription' : 'API key'}` : ''}
                      </span>
                      {a.id === 'pi' ? (
                        <Btn sm onClick={() => setSection('models')}>
                          Manage models
                        </Btn>
                      ) : a.id === 'codex' ? (
                        <small>Subscription authentication is not supported by the proxy yet.</small>
                      ) : a.status === 'connected' ? (
                        <Btn
                          sm
                          onClick={() => setConnecting({ agent: a, mode: 'disconnect' })}
                          data-testid="agent-disconnect"
                        >
                          Disconnect
                        </Btn>
                      ) : (
                        <>
                          {a.supports_oauth && (
                            <Btn
                              sm
                              onClick={() => setConnecting({ agent: a, mode: 'oauth' })}
                              data-testid="agent-connect-oauth"
                            >
                              Connect subscription
                            </Btn>
                          )}
                          {a.supports_api_key && (
                            <Btn
                              sm
                              onClick={() => setConnecting({ agent: a, mode: 'key' })}
                              data-testid="agent-connect-apikey"
                            >
                              Use API key
                            </Btn>
                          )}
                        </>
                      )}
                    </div>
                  </article>
                ))}
              </div>
              <Card pad style={{ marginTop: 20 }} data-testid="settings-agent-models">
                <h3>Default models</h3>
                <p className="field-help">
                  For built-in provider accounts. Model connections use their first listed model by default.
                </p>
                <form
                  className="settings-form"
                  onSubmit={(e) => {
                    e.preventDefault()
                    void run(async () => {
                      const d = await api.patchSettings({ agents: { default_models: models } })
                      setModels(d.agents.default_models || {})
                    }, 'Default models saved')
                  }}
                >
                  {agents
                    .filter((a) => a.runnable && a.id !== 'codex' && a.id !== 'pi')
                    .map((a) => (
                      <label key={a.id} className="form-field">
                        {a.label}
                        <Input
                          value={models[a.id] || ''}
                          onChange={(e) => setModels({ ...models, [a.id]: e.target.value })}
                          placeholder="Use the agent default"
                          disabled={!modelsEditable}
                          data-testid={`agent-model-${a.id}`}
                        />
                      </label>
                    ))}
                  {modelsEditable ? (
                    <div>
                      <Btn type="submit" disabled={busy} data-testid="save-agent-models">
                        Save defaults
                      </Btn>
                    </div>
                  ) : (
                    <p className="field-help">Default models are managed by this instance’s operator.</p>
                  )}
                </form>
              </Card>
            </>
          )}
          {section === 'git' && (
            <>
              <div className="info-banner">
                <Icon name="git" />
                <div>
                  <strong>Bring your existing projects</strong>
                  <p>
                    Public repositories need no credential. For private repositories, save a personal access
                    token with access to the repositories you want to import.
                  </p>
                </div>
              </div>
              <div className="resource-list">
                {creds.map((g) => (
                  <div className="resource-row" key={g.id}>
                    <span className="resource-icon">
                      <Icon name="git" />
                    </span>
                    <div className="resource-main">
                      <strong>{g.name}</strong>
                      <span>
                        {g.host} · {g.token_set ? 'Token saved' : 'No token'}
                      </span>
                    </div>
                    <Btn
                      sm
                      onClick={() =>
                        setConfirm({
                          title: `Remove ${g.name}?`,
                          body: 'Imports and Git operations using this credential may fail until you connect another one.',
                          run: async () => {
                            await api.deleteGitCredential(g.id)
                            await loadCreds()
                          },
                        })
                      }
                    >
                      Remove
                    </Btn>
                  </div>
                ))}
              </div>
              {!creds.length && (
                <div className="empty-state">
                  <Icon name="git" size={28} />
                  <h3>No private repository connections yet</h3>
                  <p>Add a credential when you’re ready to import a private project.</p>
                </div>
              )}
              <Btn
                variant="primary"
                onClick={() => {
                  setGc({ name: '', host: 'github.com', username: '', token: '' })
                  setGitOpen(true)
                }}
              >
                <Icon name="plus" size={15} /> Add Git credential
              </Btn>
            </>
          )}
          {section === 'access' && (
            <>
              <Card pad data-testid="settings-security">
                <h3>Console password</h3>
                <p className="field-help">Protect access to your workspace.</p>
                <form
                  className="settings-form"
                  onSubmit={(e) => {
                    e.preventDefault()
                    void run(async () => {
                      await api.changePassword({ current_password: curPw, new_password: newPw })
                      setCurPw('')
                      setNewPw('')
                    }, 'Password changed')
                  }}
                >
                  <div className="form-grid">
                    <label className="form-field">
                      Current password
                      <Input
                        required
                        value={curPw}
                        onChange={(e) => setCurPw(e.target.value)}
                        type="password"
                        autoComplete="current-password"
                        data-testid="security-current"
                      />
                    </label>
                    <label className="form-field">
                      New password
                      <Input
                        required
                        minLength={8}
                        value={newPw}
                        onChange={(e) => setNewPw(e.target.value)}
                        type="password"
                        autoComplete="new-password"
                        data-testid="security-new"
                      />
                    </label>
                  </div>
                  <div>
                    <Btn type="submit" disabled={busy} data-testid="security-change">
                      Update password
                    </Btn>
                  </div>
                </form>
                <div className="settings-divider">
                  <div>
                    <strong>Active sessions</strong>
                    <p className="field-help">Sign out every browser, including this one.</p>
                  </div>
                  <Btn
                    variant="danger"
                    data-testid="security-signout"
                    onClick={() =>
                      setConfirm({
                        title: 'Sign out everywhere?',
                        body: 'All console sessions will end. You will need your password to sign back in.',
                        run: async () => {
                          await api.logoutEverywhere()
                          location.reload()
                        },
                      })
                    }
                  >
                    Sign out everywhere
                  </Btn>
                </div>
              </Card>
              <Card pad style={{ marginTop: 18 }} data-testid="settings-api-keys">
                <h3>API keys</h3>
                <p className="field-help">Connect scripts and integrations. New keys are shown once.</p>
                {keys.map((k) => (
                  <ApiKeyRow
                    key={k.id}
                    k={k}
                    onRevoke={(id) =>
                      setConfirm({
                        title: `Revoke ${k.name}?`,
                        body: 'Integrations using this key will lose access immediately.',
                        run: async () => {
                          await api.revokeApiKey(id)
                          await loadKeys()
                        },
                      })
                    }
                  />
                ))}
                {!keys.length && <p className="field-help">No API keys created yet.</p>}
                {newKey && (
                  <div className="new-key-box" data-testid="api-key-new">
                    <strong>Copy this key now. It won’t be shown again.</strong>
                    <Input
                      readOnly
                      value={newKey}
                      aria-label="New API key"
                      onFocus={(e) => e.target.select()}
                      data-testid="api-key-new-value"
                    />
                    <div>
                      <Btn
                        onClick={async () => {
                          try {
                            await navigator.clipboard.writeText(newKey)
                            toast('Key copied')
                          } catch {
                            setError('Select the key and copy it manually.')
                          }
                        }}
                      >
                        Copy key
                      </Btn>{' '}
                      <Btn onClick={() => setNewKey('')}>Done</Btn>
                    </div>
                  </div>
                )}
                <form
                  className="settings-form"
                  onSubmit={(e) => {
                    e.preventDefault()
                    void run(async () => {
                      const k = await api.createApiKey(keyName.trim(), accessScopes(keyAccess))
                      setNewKey(k.key)
                      setKeyName('')
                      await loadKeys()
                    })
                  }}
                >
                  <div className="form-grid">
                    <label className="form-field">
                      Key name
                      <Input
                        required
                        value={keyName}
                        onChange={(e) => setKeyName(e.target.value)}
                        placeholder="CI pipeline"
                        data-testid="api-key-name"
                      />
                    </label>
                    <label className="form-field">
                      Access
                      <ApiKeyAccessSelect value={keyAccess} onChange={setKeyAccess} />
                    </label>
                  </div>
                  <div>
                    <Btn type="submit" disabled={busy} data-testid="api-key-create">
                      Create API key
                    </Btn>
                  </div>
                </form>
              </Card>
            </>
          )}
          {section === 'lifecycle' && (
            <Card pad data-testid="settings-lifecycle">
              <h3>Let idle workspaces rest</h3>
              <p className="field-help">Stop idle sandboxes to free resources. They wake when requested.</p>
              <form
                className="settings-form"
                onSubmit={(e) => {
                  e.preventDefault()
                  void run(
                    () =>
                      api.patchSettings({
                        lifecycle: {
                          idle_reap_enabled: idle,
                          idle_threshold_seconds: idleSec,
                          keepalive_max_seconds: keepSec,
                        },
                      }),
                    'Lifecycle settings saved',
                  )
                }}
              >
                <label className="check-label">
                  <input
                    type="checkbox"
                    checked={idle}
                    disabled={!lifecycleEditable}
                    onChange={(e) => setIdle(e.target.checked)}
                  />{' '}
                  Automatically stop idle workspaces
                </label>
                <div className="form-grid">
                  <label className="form-field">
                    Idle timeout (seconds)
                    <Input
                      required
                      type="number"
                      min={1}
                      value={idleSec}
                      disabled={!lifecycleEditable}
                      onChange={(e) => setIdleSec(Number(e.target.value))}
                    />
                  </label>
                  <label className="form-field">
                    Maximum keepalive (seconds)
                    <Input
                      required
                      type="number"
                      min={1}
                      value={keepSec}
                      disabled={!lifecycleEditable}
                      onChange={(e) => setKeepSec(Number(e.target.value))}
                    />
                  </label>
                </div>
                {lifecycleEditable ? (
                  <div>
                    <Btn type="submit" disabled={busy}>
                      Save lifecycle settings
                    </Btn>
                  </div>
                ) : (
                  <p className="field-help">
                    Managed by the operator through SANDBOXD_IDLE_* and SANDBOXD_KEEPALIVE_MAX_SECONDS.
                  </p>
                )}
              </form>
            </Card>
          )}
          {section === 'instance' && (
            <div className="instance-settings">
              <div className="info-banner">
                <Icon name="settings" />
                <p>
                  These values are managed on the server. Environment badges show which deployment setting
                  controls each value.
                </p>
              </div>
              <Card style={{ padding: '6px 16px 12px', marginBottom: 12 }}>
                <div
                  style={{
                    ...mono,
                    fontSize: 10,
                    color: c.muted2,
                    letterSpacing: '.5px',
                    padding: '12px 0 2px',
                  }}
                >
                  SYSTEM
                </div>
                <Field
                  label="Version"
                  value={s.version + (s.git_commit ? ` · ${s.git_commit.slice(0, 7)}` : '')}
                  kind="build"
                />
                <Field label="Base image" value={s.runtime.base_image} env="SANDBOXD_IMAGE" />
                <Field label="Storage mode" value={s.runtime.storage_mode} kind="build" />

                <div
                  style={{
                    ...mono,
                    fontSize: 10,
                    color: c.muted2,
                    letterSpacing: '.5px',
                    padding: '16px 0 2px',
                  }}
                >
                  NETWORKING &amp; PREVIEWS
                </div>
                <Field label="Preview domain" value={s.networking.preview_domain} env="PREVIEW_DOMAIN" />
                <Field
                  label="Public HTTP port"
                  value={s.networking.public_http_port || '—'}
                  env="HTTP_PORT"
                />
                <Field label="Preview base" value={s.networking.preview_base} kind="derived" />
                <Field
                  label="Host style"
                  value={s.networking.preview_host_style || 'nested'}
                  env="PREVIEW_HOST_STYLE"
                />
                {s.networking.preview_host_style === 'flat' && (
                  <Field
                    label="Host tag"
                    value={s.networking.preview_host_tag || '—'}
                    env="PREVIEW_HOST_TAG"
                  />
                )}
                <Field
                  label="TLS"
                  value={
                    <Pill tone={s.networking.preview_tls ? 'good' : 'warn'}>
                      {s.networking.preview_tls ? 'enabled' : 'plain HTTP'}
                    </Pill>
                  }
                  env="PREVIEW_TLS"
                />
                <Field
                  label="Entrypoint"
                  value={s.networking.preview_entrypoint || 'web'}
                  env="PREVIEW_ENTRYPOINT"
                />

                <div
                  style={{
                    ...mono,
                    fontSize: 10,
                    color: c.muted2,
                    letterSpacing: '.5px',
                    padding: '16px 0 2px',
                  }}
                >
                  SECURITY
                </div>
                <Field
                  label="API auth"
                  value={
                    <Pill tone={s.auth.enabled ? 'good' : 'bad'}>
                      {s.auth.enabled ? 'enabled' : 'disabled'}
                    </Pill>
                  }
                  env="SANDBOXD_API_AUTH_DISABLED"
                />
                <Field
                  label="Sandbox egress"
                  value={<Pill tone="neutral">{s.egress.mode}</Pill>}
                  kind="build"
                />
                <Field label="Secrets key" value="set (never shown)" kind="managed" />
              </Card>
              {s.agents.system_prompt && (
                <Card style={{ padding: 16 }} data-testid="settings-system-prompt">
                  <div style={{ display: 'flex', alignItems: 'center', gap: 8 }}>
                    <H>Agent system prompt</H>
                    <Src kind="managed" />
                    <span style={{ marginLeft: 'auto', fontSize: 11.5, color: c.muted2 }}>
                      from <span style={{ ...mono, fontSize: 11 }}>prompt.md</span>
                    </span>
                  </div>
                  <div style={{ color: c.muted, fontSize: 12.5, margin: '6px 0 0' }}>
                    Appended to every agent run so it understands the sandbox and its guardrails. Ports/paths
                    shown use defaults; each sandbox renders its real values at run time.
                  </div>
                  <a
                    onClick={() => setShowPrompt((v) => !v)}
                    className="dc-hoverink"
                    style={{
                      display: 'inline-block',
                      marginTop: 10,
                      fontSize: 12,
                      color: c.link,
                      cursor: 'pointer',
                    }}
                  >
                    {showPrompt ? 'Hide' : 'Show'} the full prompt {showPrompt ? '▲' : '▼'}
                  </a>
                  {showPrompt && (
                    <pre
                      style={{
                        background: c.bg,
                        border: `1px solid ${c.border}`,
                        borderRadius: 7,
                        padding: '12px 14px',
                        ...mono,
                        fontSize: 11.5,
                        color: c.fg2,
                        margin: '10px 0 0',
                        maxHeight: 320,
                        overflow: 'auto',
                        lineHeight: 1.55,
                        whiteSpace: 'pre-wrap',
                      }}
                    >
                      {s.agents.system_prompt}
                    </pre>
                  )}
                </Card>
              )}{' '}
            </div>
          )}
        </div>
      </div>
      {connecting && (
        <AgentConnection
          agent={connecting.agent}
          mode={connecting.mode}
          close={() => setConnecting(null)}
          connected={() => {
            void loadAgents().catch((e) => setError((e as Error).message))
            toast('Agent connection updated')
          }}
        />
      )}
      {gitOpen && (
        <Dialog
          title="Add a Git credential"
          close={() => {
            if (!busy) {
              setGitOpen(false)
              setGc({ ...gc, token: '' })
            }
          }}
        >
          <form
            className="settings-form"
            onSubmit={(e) => {
              e.preventDefault()
              void run(async () => {
                await api.createGitCredential(gc)
                setGc({ name: '', host: 'github.com', username: '', token: '' })
                setGitOpen(false)
                await loadCreds()
              }, 'Git credential saved')
            }}
          >
            <label className="form-field">
              Connection name
              <Input
                autoFocus
                required
                value={gc.name}
                onChange={(e) => setGc({ ...gc, name: e.target.value })}
                placeholder="My GitHub account"
              />
            </label>
            <div className="form-grid">
              <label className="form-field">
                Host
                <Input required value={gc.host} onChange={(e) => setGc({ ...gc, host: e.target.value })} />
              </label>
              <label className="form-field">
                Username (optional)
                <Input value={gc.username} onChange={(e) => setGc({ ...gc, username: e.target.value })} />
              </label>
            </div>
            <label className="form-field">
              Access token
              <Input
                required
                type="password"
                autoComplete="new-password"
                value={gc.token}
                onChange={(e) => setGc({ ...gc, token: e.target.value })}
              />
            </label>
            <p className="field-help">Encrypted at rest. The saved token is never returned to the browser.</p>
            {error && (
              <p role="alert" className="inline-error">
                {error}
              </p>
            )}
            <div className="dialog-actions">
              <Btn
                type="button"
                disabled={busy}
                onClick={() => {
                  setGitOpen(false)
                  setGc({ ...gc, token: '' })
                }}
              >
                Cancel
              </Btn>
              <Btn type="submit" variant="primary" disabled={busy}>
                Save credential
              </Btn>
            </div>
          </form>
        </Dialog>
      )}
      {confirm && (
        <Dialog title={confirm.title} close={() => !busy && setConfirm(null)}>
          <p>{confirm.body}</p>
          {error && (
            <p role="alert" className="inline-error">
              {error}
            </p>
          )}
          <div className="dialog-actions">
            <Btn disabled={busy} onClick={() => setConfirm(null)}>
              Cancel
            </Btn>
            <Btn
              variant="danger"
              disabled={busy}
              onClick={() =>
                void run(async () => {
                  await confirm.run()
                  setConfirm(null)
                }, 'Updated')
              }
            >
              Confirm
            </Btn>
          </div>
        </Dialog>
      )}
    </div>
  )
}
