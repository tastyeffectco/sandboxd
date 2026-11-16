import { useEffect, useState } from 'react'
import { api, AgentInstance, AgentCatalogEntry, ModelProvider } from './api'
import { c, font, Btn, Card, Input } from './design/kit'
import { ConnectOpenRouter, canConnectOpenRouter } from './ConnectOpenRouter'
import { browserTimeZone, canSetTimeZone } from './timeZone'

// The agent catalog: add an agent with one click. Each agent you add gets its
// own window, listed under AGENTS in the sidebar.
export function AgentsView({
  onError,
  agents,
  goModels,
  onOpen,
}: {
  onError: (message: string) => void
  agents: AgentInstance[]
  goModels: () => void
  onOpen: (id: string) => void
}) {
  const [packages, setPackages] = useState<AgentCatalogEntry[] | null>(null)
  const [models, setModels] = useState<ModelProvider[]>([])
  const [notice, setNotice] = useState('')
  const connected = (p: ModelProvider, free: boolean) => {
    setModels((m) => [p, ...m])
    setNotice(
      free
        ? 'OpenRouter is connected with its free models. They have a daily limit and can be slow; adding a few dollars of credit on openrouter.ai unlocks the stronger model.'
        : 'OpenRouter is connected.',
    )
  }
  useEffect(() => {
    Promise.all([api.listAgentCatalogEntrys(), api.listModelProviders()])
      .then(([p, m]) => {
        setPackages(p)
        // Agents speak the OpenAI chat protocol.
        setModels(m.filter((x) => x.protocol === 'openai-completions' && x.models.length > 0))
      })
      .catch((e) => onError((e as Error).message))
  }, [onError])

  return (
    <div className="content-page">
      <h1 style={{ fontFamily: font.display, fontSize: 24, fontWeight: 700, margin: '0 0 6px' }}>Add an agent</h1>
      <p style={{ color: c.muted, margin: '0 0 24px', maxWidth: 620 }}>
        Agents are assistants that work for you around the clock. Each one gets its own chat in the sidebar; they
        remember what you tell them and can create and change apps on this computer.
      </p>
      {notice && (
        <p role="status" className="chat-notice" style={{ maxWidth: 680 }} data-testid="openrouter-connected">
          {notice}
        </p>
      )}
      {packages === null && <p style={{ color: c.muted2 }}>Loading…</p>}
      {packages?.map((p) => {
        const added = agents.find((a) => a.package === p.id)
        return added ? (
          <Card key={p.id} pad={20} style={{ marginBottom: 16, maxWidth: 680 }} data-testid={`added-agent-${p.id}`}>
            <div style={{ display: 'flex', alignItems: 'center', gap: 12 }}>
              <strong style={{ fontSize: 16 }}>{p.label}</strong>
              <span style={{ color: c.muted2, fontSize: 12 }}>Added</span>
              <span style={{ flex: 1 }} />
              <Btn sm variant="primary" onClick={() => onOpen(added.id)} data-testid="open-agent">
                Open {added.name}
              </Btn>
            </div>
            <p style={{ color: c.muted, margin: '6px 0 0' }}>{p.description}</p>
          </Card>
        ) : (
          <AddAgent
            key={p.id}
            pkg={p}
            models={models}
            goModels={goModels}
            onConnected={connected}
            onAdded={onOpen}
            onError={onError}
          />
        )
      })}
      {packages !== null && <AgentTimeZone onError={onError} />}
    </div>
  )
}

const zones = () => {
  try {
    return (Intl as unknown as { supportedValuesOf(k: string): string[] }).supportedValuesOf('timeZone')
  } catch {
    return [browserTimeZone()].filter(Boolean)
  }
}

// Agents schedule in this zone: "every morning at 8" is 8 here.
function AgentTimeZone({ onError }: { onError: (message: string) => void }) {
  const [zone, setZone] = useState<string | null>(null)
  const [busy, setBusy] = useState(false)
  useEffect(() => {
    api
      .getSettings()
      .then((s) => setZone(canSetTimeZone(s) ? s.agents.time_zone || 'UTC' : null))
      .catch(() => {})
  }, [])
  if (zone === null) return null
  const options = zones()
  if (!options.includes(zone)) options.unshift(zone)
  const change = async (next: string) => {
    setBusy(true)
    try {
      const s = await api.patchSettings({ agents: { time_zone: next === 'UTC' ? '' : next } })
      setZone(s.agents.time_zone || 'UTC')
    } catch (e) {
      onError((e as Error).message)
    } finally {
      setBusy(false)
    }
  }
  return (
    <label className="form-field" style={{ maxWidth: 680, marginTop: 8 }}>
      Your time zone (agents use it for reminders and schedules)
      <select value={zone} disabled={busy} onChange={(e) => void change(e.target.value)} data-testid="agent-time-zone">
        {options.map((z) => (
          <option key={z} value={z}>
            {z.replace(/_/g, ' ')}
          </option>
        ))}
      </select>
      {busy && <span style={{ color: c.muted2, fontSize: 12 }}>Your agents restart to use it…</span>}
    </label>
  )
}

function AddAgent({
  pkg,
  models,
  goModels,
  onConnected,
  onAdded,
  onError,
}: {
  pkg: AgentCatalogEntry
  models: ModelProvider[]
  goModels: () => void
  onConnected: (p: ModelProvider, free: boolean) => void
  onAdded: (id: string) => void
  onError: (message: string) => void
}) {
  const [connection, setConnection] = useState('')
  const [token, setToken] = useState('')
  const [users, setUsers] = useState('')
  const [busy, setBusy] = useState(false)
  const [warning, setWarning] = useState('')
  const chosen = connection || suggestedConnection(models, pkg.strong_model) || ''
  const add = async () => {
    setBusy(true)
    setWarning('')
    try {
      const res = await api.createAgentInstance({
        package: pkg.id,
        connection_id: chosen,
        telegram_bot_token: token.trim() || undefined,
        telegram_allowed_users: users.trim() || undefined,
      })
      if (res.warning) setWarning(res.warning)
      onAdded(res.agent.id)
    } catch (e) {
      onError((e as Error).message)
    } finally {
      setBusy(false)
    }
  }
  return (
    <Card pad={20} style={{ marginBottom: 16, maxWidth: 680 }} data-testid={`add-agent-${pkg.id}`}>
      <strong style={{ fontSize: 16 }}>{pkg.label}</strong>
      <p style={{ color: c.muted, margin: '6px 0 14px' }}>{pkg.description}</p>
      {models.length === 0 && canConnectOpenRouter() ? (
        <>
          <p style={{ margin: '0 0 12px' }}>
            {pkg.label} needs an AI model to think with. The easiest way is a free OpenRouter account: sign in with
            Google, GitHub or email and you're back here in a minute.
          </p>
          <ConnectOpenRouter onConnected={onConnected} onError={onError} />
          <p style={{ color: c.muted2, fontSize: 12, margin: '10px 0 0' }}>
            Prefer another model, or one running on this computer?{' '}
            <button className="text-button" onClick={goModels}>
              Settings → Model connections
            </button>
          </p>
        </>
      ) : models.length === 0 ? (
        <p style={{ margin: 0 }}>
          {pkg.label} needs an AI model to think with. Add one (for example an OpenRouter key) in{' '}
          <button className="text-button" onClick={goModels}>
            Settings → Model connections
          </button>
          , then come back here.
        </p>
      ) : (
        <>
          {models.length > 1 && (
            <label className="form-field">
              AI model
              <select value={chosen} onChange={(e) => setConnection(e.target.value)} data-testid="agent-model">
                {models.map((m) => (
                  <option key={m.id} value={m.id}>
                    {m.name} · {m.models[0].id}
                  </option>
                ))}
              </select>
            </label>
          )}
          {pkg.telegram && (
            <details style={{ margin: '4px 0 14px' }}>
              <summary>Talk to {pkg.label} on Telegram (optional)</summary>
              <p style={{ color: c.muted, fontSize: 13 }}>
                Create a bot with @BotFather and paste its token. Add your Telegram user ID (from @userinfobot) so
                only you can talk to it.
              </p>
              <label className="form-field">
                Bot token
                <Input value={token} onChange={(e) => setToken(e.target.value)} autoComplete="off" />
              </label>
              <label className="form-field">
                Your Telegram user ID
                <Input value={users} onChange={(e) => setUsers(e.target.value)} />
              </label>
            </details>
          )}
          <Btn variant="primary" disabled={busy || !chosen} onClick={() => void add()} data-testid="add-agent">
            {busy ? `Setting up ${pkg.label}…` : `Add ${pkg.label}`}
          </Btn>
          <p style={{ color: c.muted2, fontSize: 12, margin: '10px 0 0' }}>
            The first setup downloads {pkg.download}. {pkg.label} runs in its own protected space, uses{' '}
            {pkg.memory} of memory and keeps running while this computer is on.
          </p>
        </>
      )}
      {warning && (
        <p role="alert" className="inline-error">
          {warning}
        </p>
      )}
    </Card>
  )
}

// Small or fast models garble the nested tool calls some agents make; for
// those, the strongest model connected is suggested first.
const SMALL_MODEL = /flash|mini|lite|nano|haiku|small|:\d+(\.\d+)?b\b|\b\d+(\.\d+)?b\b/i
export function suggestedConnection(models: ModelProvider[], strong?: boolean): string | undefined {
  if (strong) {
    const big = models.find((m) => !SMALL_MODEL.test(m.models[0]?.id || ''))
    if (big) return big.id
  }
  return models[0]?.id
}
