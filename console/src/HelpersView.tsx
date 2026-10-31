import { useCallback, useEffect, useState } from 'react'
import { api, Helper, HelperPackage, ModelProvider } from './api'
import { AgentMessage } from './AgentMessage'
import { c, font, Btn, Card, Input, StatusPill } from './design/kit'

// Helpers are assistants (Hermes first) that sandboxd runs for you. They talk
// to you here or on Telegram and can create and change apps on this sandboxd.
export function HelpersView({
  onError,
  reloadApps,
  goModels,
}: {
  onError: (message: string) => void
  reloadApps: () => void
  goModels: () => void
}) {
  const [packages, setPackages] = useState<HelperPackage[]>([])
  const [helpers, setHelpers] = useState<Helper[] | null>(null)
  const [models, setModels] = useState<ModelProvider[]>([])
  const load = useCallback(() => {
    Promise.all([api.listHelperPackages(), api.listHelpers(), api.listModelProviders()])
      .then(([p, h, m]) => {
        setPackages(p)
        setHelpers(h)
        // Helpers speak the OpenAI chat protocol.
        setModels(m.filter((x) => x.protocol === 'openai-completions' && x.models.length > 0))
      })
      .catch((e) => onError((e as Error).message))
  }, [onError])
  useEffect(load, [load])

  return (
    <div className="content-page">
      <h1 style={{ fontFamily: font.display, fontSize: 24, fontWeight: 700, margin: '0 0 6px' }}>Helpers</h1>
      <p style={{ color: c.muted, margin: '0 0 24px', maxWidth: 620 }}>
        Assistants that work for you around the clock. Chat with them here or on Telegram; they remember what
        you tell them and can create and change apps on this sandboxd.
      </p>
      {helpers === null && <p style={{ color: c.muted2 }}>Loading…</p>}
      {helpers?.map((h) => (
        <HelperCard key={h.id} helper={h} onChange={load} onError={onError} reloadApps={reloadApps} />
      ))}
      {helpers !== null &&
        packages
          .filter((p) => !helpers.some((h) => h.package === p.id))
          .map((p) => (
            <AddHelper key={p.id} pkg={p} models={models} goModels={goModels} onAdded={load} onError={onError} />
          ))}
    </div>
  )
}

function AddHelper({
  pkg,
  models,
  goModels,
  onAdded,
  onError,
}: {
  pkg: HelperPackage
  models: ModelProvider[]
  goModels: () => void
  onAdded: () => void
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
      const res = await api.createHelper({
        package: pkg.id,
        connection_id: chosen,
        telegram_bot_token: token.trim() || undefined,
        telegram_allowed_users: users.trim() || undefined,
      })
      if (res.warning) setWarning(res.warning)
      onAdded()
    } catch (e) {
      onError((e as Error).message)
    } finally {
      setBusy(false)
    }
  }
  return (
    <Card pad={20} style={{ marginBottom: 16, maxWidth: 680 }} data-testid={`add-helper-${pkg.id}`}>
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
              <select value={chosen} onChange={(e) => setConnection(e.target.value)} data-testid="helper-model">
                {models.map((m) => (
                  <option key={m.id} value={m.id}>
                    {m.name} · {m.models[0].id}
                  </option>
                ))}
              </select>
            </label>
          )}
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
          <Btn variant="primary" disabled={busy || !chosen} onClick={() => void add()} data-testid="add-helper">
            {busy ? `Setting up ${pkg.label}…` : `Add ${pkg.label}`}
          </Btn>
          <p style={{ color: c.muted2, fontSize: 12, margin: '10px 0 0' }}>
            The first setup downloads {pkg.download}. {pkg.label} runs in its own protected space and keeps
            running while this computer is on.
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

type Turn = { role: 'user' | 'assistant'; content: string }

function HelperCard({
  helper,
  onChange,
  onError,
  reloadApps,
}: {
  helper: Helper
  onChange: () => void
  onError: (message: string) => void
  reloadApps: () => void
}) {
  const [turns, setTurns] = useState<Turn[]>([])
  const [text, setText] = useState('')
  const [thinking, setThinking] = useState(false)
  const [busy, setBusy] = useState(false)
  const act = async (fn: () => Promise<unknown>) => {
    setBusy(true)
    try {
      await fn()
      onChange()
    } catch (e) {
      onError((e as Error).message)
    } finally {
      setBusy(false)
    }
  }
  const send = async () => {
    const content = text.trim()
    if (!content || thinking) return
    const next: Turn[] = [...turns, { role: 'user', content }]
    setTurns(next)
    setText('')
    setThinking(true)
    try {
      const { reply } = await api.chatWithHelper(helper.id, next)
      setTurns([...next, { role: 'assistant', content: reply }])
      reloadApps() // it may have created an app
    } catch (e) {
      setTurns(turns)
      setText(content)
      onError((e as Error).message)
    } finally {
      setThinking(false)
    }
  }
  const running = helper.status === 'running'
  return (
    <Card pad={20} style={{ marginBottom: 16, maxWidth: 820 }} data-testid={`helper-${helper.id}`}>
      <div style={{ display: 'flex', alignItems: 'center', gap: 10, flexWrap: 'wrap' }}>
        <strong style={{ fontSize: 16 }}>{helper.name}</strong>
        <StatusPill status={helper.status === 'missing' ? 'not started' : helper.status} />
        <span style={{ color: c.muted2, fontSize: 12 }}>
          {helper.apps_today}/{helper.apps_per_day} new apps today{helper.telegram ? ' · on Telegram' : ''}
        </span>
        <span style={{ flex: 1 }} />
        {running ? (
          <Btn sm disabled={busy} onClick={() => void act(() => api.stopHelper(helper.id))}>
            Pause
          </Btn>
        ) : (
          <Btn sm disabled={busy} onClick={() => void act(() => api.startHelper(helper.id))}>
            Start
          </Btn>
        )}
        <Btn
          sm
          disabled={busy}
          onClick={() => {
            if (window.confirm(`Remove ${helper.name}? Its memory and skills are deleted. Your apps stay.`))
              void act(() => api.deleteHelper(helper.id))
          }}
        >
          Remove
        </Btn>
      </div>
      <div style={{ margin: '16px 0', display: 'grid', gap: 10 }} data-testid="helper-chat">
        {turns.length === 0 && (
          <p style={{ color: c.muted, margin: 0 }}>
            Try: “Build me a recipe app with a shopping list” or “What can you do?”
          </p>
        )}
        {turns.map((t, i) =>
          t.role === 'user' ? (
            <div key={i} className="user-message">
              {t.content}
            </div>
          ) : (
            <div key={i} className="agent-message">
              <AgentMessage text={t.content} />
            </div>
          ),
        )}
        {thinking && (
          <p role="status" style={{ color: c.muted, margin: 0 }}>
            {helper.name} is working… building an app can take a few minutes.
          </p>
        )}
      </div>
      <form
        onSubmit={(e) => {
          e.preventDefault()
          void send()
        }}
        style={{ display: 'flex', gap: 8 }}
      >
        <Input
          value={text}
          onChange={(e) => setText(e.target.value)}
          placeholder={running ? `Message ${helper.name}` : `Start ${helper.name} to chat`}
          disabled={!running || thinking}
          data-testid="helper-message"
          style={{ flex: 1 }}
        />
        <Btn type="submit" variant="primary" disabled={!running || thinking || !text.trim()}>
          Send
        </Btn>
      </form>
    </Card>
  )
}
