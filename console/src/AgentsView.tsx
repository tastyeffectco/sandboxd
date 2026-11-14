import { useEffect, useState } from 'react'
import { api, AgentInstance, AgentCatalogEntry, ModelProvider } from './api'
import { c, font, Btn, Card, Input } from './design/kit'

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
          </Card>
        ) : (
          <AddAgent key={p.id} pkg={p} models={models} goModels={goModels} onAdded={onOpen} onError={onError} />
        )
      })}
    </div>
  )
}

function AddAgent({
  pkg,
  models,
  goModels,
  onAdded,
  onError,
}: {
  pkg: AgentCatalogEntry
  models: ModelProvider[]
  goModels: () => void
  onAdded: (id: string) => void
  onError: (message: string) => void
}) {
  const [connection, setConnection] = useState('')
  const [token, setToken] = useState('')
  const [users, setUsers] = useState('')
  const [busy, setBusy] = useState(false)
  const [warning, setWarning] = useState('')
  const chosen = connection || models[0]?.id || ''
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
      {models.length === 0 ? (
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
