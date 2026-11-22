import { useCallback, useEffect, useState } from 'react'
import { api, type Agent, type ModelProvider } from './api'
import { aiOptions, FREE_AI, type AIOption } from './aiOptions'
import type { AgentChoice } from './AgentPicker'
import { ConnectOpenRouter, canConnectOpenRouter } from './ConnectOpenRouter'
import { Btn, Input } from './design/kit'
import { Icon } from './design/Icon'

// sandboxd's CometAPI referral. Always shown with a plain disclosure.
export const COMETAPI_SIGNUP = 'https://www.cometapi.com/?ref=abdelouahed'
// "auto" lets CometAPI pick a suitable model per request; the others are
// popular coding models from its public list.
export const COMETAPI_MODELS = ['auto', 'deepseek-v4-pro', 'claude-sonnet-4-6', 'qwen3-coder-plus']

export function cometConnection(key: string) {
  return {
    name: 'CometAPI',
    protocol: 'openai-completions' as const,
    base_url: 'https://api.cometapi.com/v1',
    api_key: key,
    models: COMETAPI_MODELS.map((id) => ({ id, context_window: 131072, max_tokens: 8192 })),
  }
}

const openLink = (url: string) =>
  window.sandboxdDesktop?.openExternal ? void window.sandboxdDesktop.openExternal(url) : window.open(url, '_blank', 'noopener')

const sameChoice = (a: AgentChoice, b: AgentChoice) =>
  a.agent === b.agent && (a.provider || '') === (b.provider || '') && (a.model || '') === (b.model || '')

// Settings → AI: which AI new chats start with, and every way to add one, in
// plain words. The detailed pages (Model connections, Coding agents) stay
// under Advanced for people who want them.
export function SettingsAI({
  onError,
  toast,
  goSection,
}: {
  onError: (m: string) => void
  toast: (m: string) => void
  goSection: (id: string) => void
}) {
  const [agents, setAgents] = useState<Agent[]>([])
  const [providers, setProviders] = useState<ModelProvider[]>([])
  const [current, setCurrent] = useState<AgentChoice>(FREE_AI)
  const [saving, setSaving] = useState('')
  const [cometKey, setCometKey] = useState<string | null>(null)
  const [checking, setChecking] = useState(false)
  const load = useCallback(async () => {
    try {
      const [a, p, s] = await Promise.all([api.getAgents(), api.listModelProviders(), api.getSettings()])
      setAgents(a)
      setProviders(p)
      const d = s.agents?.default_ai
      setCurrent(d?.agent ? { agent: d.agent, provider: d.provider || '', model: d.model || '' } : FREE_AI)
    } catch (e) {
      onError((e as Error).message)
    }
  }, [onError])
  useEffect(() => {
    void load()
  }, [load])

  const choose = async (o: AIOption) => {
    setSaving(o.key)
    try {
      await api.patchSettings({ agents: { default_ai: { ...o.choice } } })
      setCurrent(o.choice)
      toast(`New chats now use ${o.label}`)
    } catch (e) {
      onError((e as Error).message)
    } finally {
      setSaving('')
    }
  }

  const addComet = async () => {
    const key = (cometKey || '').trim()
    if (!key) return
    setChecking(true)
    try {
      // CometAPI answers 401 for a wrong key; check before saving it.
      const res = await fetch('https://api.cometapi.com/v1/models', { headers: { Authorization: `Bearer ${key}` } })
      if (res.status === 401 || res.status === 403) throw new Error('That CometAPI key did not work. Copy it again from your CometAPI dashboard.')
      const saved = await api.saveModelProvider(undefined, cometConnection(key))
      setCometKey(null)
      await load()
      await api.patchSettings({ agents: { default_ai: { agent: 'opencode', provider: saved.id, model: 'auto' } } })
      setCurrent({ agent: 'opencode', provider: saved.id, model: 'auto' })
      toast('CometAPI connected. New chats use it now.')
    } catch (e) {
      onError((e as Error).message)
    } finally {
      setChecking(false)
    }
  }

  const options = aiOptions(agents, providers)
  const hasOpenRouter = providers.some((p) => p.base_url.startsWith('https://openrouter.ai/'))
  const hasComet = providers.some((p) => p.base_url.startsWith('https://api.cometapi.com/'))
  const codex = agents.find((a) => a.id === 'codex' && a.status === 'connected')

  return (
    <div className="settings-ai">
      <section className="ai-default" aria-labelledby="ai-default-title">
        <h3 id="ai-default-title">New chats use</h3>
        <p className="field-help">You can still switch for one chat with the AI button above any message box.</p>
        <div className="ai-cards" role="radiogroup" aria-label="New chats use">
          {options.map((o) => (
            <button
              key={o.key}
              role="radio"
              aria-checked={sameChoice(o.choice, current)}
              className={sameChoice(o.choice, current) ? 'ai-card active' : 'ai-card'}
              disabled={!!saving}
              onClick={() => void choose(o)}
              data-testid={`default-ai-${o.key}`}
            >
              <strong>{o.label}</strong>
              <small>{o.detail}</small>
              {sameChoice(o.choice, current) && <Icon name="check" size={14} />}
            </button>
          ))}
        </div>
      </section>

      <section className="ai-connect" aria-labelledby="ai-connect-title">
        <h3 id="ai-connect-title">Add an AI</h3>
        <div className="ai-connect-grid">
          {!hasOpenRouter && (
            <div className="ai-provider">
              <strong>OpenRouter</strong>
              <p>Hundreds of models, some of them free. Sign in with Google, GitHub or email.</p>
              {canConnectOpenRouter() ? (
                <ConnectOpenRouter
                  onError={onError}
                  onConnected={(_, free) => {
                    toast(free ? 'OpenRouter connected with its free models' : 'OpenRouter connected')
                    void load()
                  }}
                />
              ) : (
                <Btn onClick={() => goSection('models')}>Add an OpenRouter key</Btn>
              )}
            </div>
          )}
          {!hasComet && (
            <div className="ai-provider" data-testid="cometapi-card">
              <strong>CometAPI</strong>
              <p>One key for 500+ models (GPT, Claude, DeepSeek…), pay as you go.</p>
              {cometKey === null ? (
                <div className="ai-provider-actions">
                  <Btn variant="primary" onClick={() => openLink(COMETAPI_SIGNUP)} data-testid="cometapi-signup">
                    Create a CometAPI account
                  </Btn>
                  <Btn onClick={() => setCometKey('')}>I have a key</Btn>
                </div>
              ) : (
                <form
                  className="ai-provider-actions"
                  onSubmit={(e) => {
                    e.preventDefault()
                    void addComet()
                  }}
                >
                  <Input
                    type="password"
                    placeholder="Paste your CometAPI key"
                    value={cometKey}
                    onChange={(e) => setCometKey(e.target.value)}
                    autoFocus
                    aria-label="CometAPI key"
                    data-testid="cometapi-key"
                  />
                  <Btn variant="primary" type="submit" disabled={checking || !cometKey.trim()}>
                    {checking ? 'Checking…' : 'Connect'}
                  </Btn>
                </form>
              )}
              <small className="ai-disclosure" data-testid="cometapi-disclosure">
                Affiliate link: sandboxd earns a commission if you sign up. It costs you nothing extra.
              </small>
            </div>
          )}
          {!codex && (
            <div className="ai-provider">
              <strong>ChatGPT subscription</strong>
              <p>Already pay for ChatGPT Plus or Pro? Use it to build, through Codex.</p>
              <Btn onClick={() => goSection('agents')}>Use my ChatGPT</Btn>
            </div>
          )}
          <div className="ai-provider">
            <strong>Claude Pro or Max</strong>
            <p>Choose Claude above, then sign in once in each project when it asks. Anthropic does not let apps keep your login.</p>
          </div>
          <div className="ai-provider">
            <strong>Something else</strong>
            <p>A model on this computer (Ollama, LM Studio) or any OpenAI-compatible service.</p>
            <Btn onClick={() => goSection('models')}>Add a connection</Btn>
          </div>
        </div>
      </section>
    </div>
  )
}
