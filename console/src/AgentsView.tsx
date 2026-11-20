import { useEffect, useState } from 'react'
import { api, AgentInstance, AgentCatalogEntry, ModelProvider } from './api'
import { c, Btn, Input } from './design/kit'
import { Bot, BOTS, BotMood, agentHue, moodFor } from './design/Bot'
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
    <div className="content-page crew-page">
      <header className="page-hero">
        <div className="page-hero-text">
          <span className="eyebrow">Your crew</span>
          <h1>Add an agent</h1>
          <p>
            Agents are assistants that work for you around the clock. Each one gets its own chat in the sidebar; they
            remember what you tell them and can create and change apps on this computer.
          </p>
        </div>
        <div className="page-hero-crew" aria-hidden="true">
          {(packages || []).slice(0, 3).map((p, i) => (
            <Bot key={p.id} who={p.id} size={i === 1 ? 84 : 62} />
          ))}
        </div>
      </header>
      {notice && (
        <p role="status" className="chat-notice" data-testid="openrouter-connected">
          {notice}
        </p>
      )}
      {packages === null && (
        <div className="crew-list" aria-label="Loading">
          <div className="sbx-skeleton" style={{ height: 150 }} />
          <div className="sbx-skeleton" style={{ height: 150 }} />
        </div>
      )}
      <div className="crew-list">
        {packages?.map((p) => {
          const mine = agents.filter((a) => a.package === p.id)
          return mine.length > 0 ? (
            <AddedAgents
              key={p.id}
              pkg={p}
              mine={mine}
              all={agents}
              models={models}
              goModels={goModels}
              onConnected={connected}
              onOpen={onOpen}
              onError={onError}
            />
          ) : (
            <CrewCard key={p.id} id={p.id} mood="idle" data-testid={`add-agent-${p.id}`}>
              <div className="crew-card-title">
                <h2>{p.label}</h2>
              </div>
              <p>{p.description}</p>
              <AddAgent
                pkg={p}
                models={models}
                goModels={goModels}
                onConnected={connected}
                onAdded={onOpen}
                onError={onError}
              />
            </CrewCard>
          )
        })}
      </div>
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
      const s = await api.patchSettings({
        agents: { time_zone: next === 'UTC' ? '' : next },
      })
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

// The agents of one kind you already have, each with its own window, and a
// way to add another (a second Hermes for work, say).
function AddedAgents({
  pkg,
  mine,
  all,
  models,
  goModels,
  onConnected,
  onOpen,
  onError,
}: {
  pkg: AgentCatalogEntry
  mine: AgentInstance[]
  all: AgentInstance[]
  models: ModelProvider[]
  goModels: () => void
  onConnected: (p: ModelProvider, free: boolean) => void
  onOpen: (id: string) => void
  onError: (message: string) => void
}) {
  const [adding, setAdding] = useState(false)
  const names = mine.map((a) => a.name)
  return (
    <CrewCard id={pkg.id} mood={adding ? 'thinking' : moodFor(mine[0].status)} data-testid={`added-agent-${pkg.id}`}>
      <div className="crew-card-title">
        <h2>{pkg.label}</h2>
        <span className="crew-badge">{mine.length > 1 ? `${mine.length} on your crew` : 'On your crew'}</span>
      </div>
      <p>{pkg.description}</p>
      <ul className="crew-members">
        {mine.map((a) => (
          <li key={a.id}>
            <Bot who={a.package} hue={agentHue(all, a)} mood={moodFor(a.status)} size={34} still />
            <span>
              <strong>{a.name}</strong>
              <small>{agentState(a.status)}</small>
            </span>
            <Btn sm variant="primary" onClick={() => onOpen(a.id)} data-testid="open-agent">
              Open
            </Btn>
          </li>
        ))}
      </ul>
      {adding ? (
        <div className="crew-add-another">
          <AddAgent
            pkg={pkg}
            models={models}
            goModels={goModels}
            onConnected={onConnected}
            onAdded={onOpen}
            onError={onError}
            taken={names}
            onCancel={() => setAdding(false)}
          />
        </div>
      ) : (
        <div className="crew-card-actions">
          <Btn onClick={() => setAdding(true)} data-testid={`add-another-${pkg.id}`}>
            + Add another {pkg.label}
          </Btn>
        </div>
      )}
    </CrewCard>
  )
}

function agentState(status: string) {
  if (status === 'running') return 'Ready'
  if (status === 'starting') return 'Starting…'
  if (status === 'missing') return 'Needs attention'
  return 'Paused'
}

// A next name nobody has yet: "Hermes 2", "Hermes 3"…
export function nextName(label: string, taken: string[]): string {
  const used = new Set(taken.map((n) => n.trim().toLowerCase()))
  for (let i = 2; ; i++) if (!used.has(`${label} ${i}`.toLowerCase())) return `${label} ${i}`
}

function AddAgent({
  pkg,
  models,
  goModels,
  onConnected,
  onAdded,
  onError,
  taken,
  onCancel,
}: {
  pkg: AgentCatalogEntry
  // Names of the agents of this kind already added; set when adding another.
  taken?: string[]
  onCancel?: () => void
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
  const another = !!taken
  const [name, setName] = useState(() => (taken ? nextName(pkg.label, taken) : ''))
  const clash = another && taken!.some((n) => n.trim().toLowerCase() === name.trim().toLowerCase())
  const chosen = connection || suggestedConnection(models, pkg.strong_model) || ''
  const add = async () => {
    setBusy(true)
    setWarning('')
    try {
      const res = await api.createAgentInstance({
        package: pkg.id,
        ...(another ? { name: name.trim() } : {}),
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
    <>
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
          {another && (
            <label className="form-field">
              Name
              <Input
                value={name}
                onChange={(e) => setName(e.target.value)}
                maxLength={40}
                autoFocus
                data-testid="agent-name"
              />
              <small className={clash ? 'crew-name-hint bad' : 'crew-name-hint'} role={clash ? 'alert' : undefined}>
                {clash
                  ? `You already have an agent called ${name.trim()}.`
                  : `A name to tell it apart, like “${pkg.label} for work”. It has its own chats and memory.`}
              </small>
            </label>
          )}
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
                Create a bot with @BotFather and paste its token. Add your Telegram user ID (from @userinfobot) so only
                you can talk to it.
                {another && ' Use a different bot for each agent.'}
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
          <div className="crew-card-actions">
            <Btn
              variant="primary"
              disabled={busy || !chosen || (another && (!name.trim() || clash))}
              onClick={() => void add()}
              data-testid="add-agent"
            >
              {busy
                ? `Setting up ${another ? name.trim() : pkg.label}…`
                : `Add ${another ? name.trim() || pkg.label : pkg.label}`}
            </Btn>
            {onCancel && !busy && <Btn onClick={onCancel}>Cancel</Btn>}
          </div>
          <p style={{ color: c.muted2, fontSize: 12, margin: '10px 0 0' }}>
            {another
              ? `Nothing new to download. Each ${pkg.label} runs in its own protected space and uses ${pkg.memory} of memory.`
              : `The first setup downloads ${pkg.download}. ${pkg.label} runs in its own protected space, uses ${pkg.memory} of memory and keeps running while this computer is on.`}
          </p>
        </>
      )}
      {warning && (
        <p role="alert" className="inline-error">
          {warning}
        </p>
      )}
    </>
  )
}

// One agent on the roster: its bot on a stage lit in its colour, then its card.
function CrewCard({
  id,
  mood,
  children,
  ...rest
}: {
  id: string
  mood: BotMood
  children: React.ReactNode
} & React.HTMLAttributes<HTMLElement>) {
  const look = BOTS[id]
  return (
    <article className="crew-card" style={{ ['--crew' as string]: look?.hi || 'var(--accent)' }} {...rest}>
      <div className="crew-stage" aria-hidden="true">
        <Bot who={id} mood={mood} size={104} />
      </div>
      <div className="crew-card-body">{children}</div>
    </article>
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
