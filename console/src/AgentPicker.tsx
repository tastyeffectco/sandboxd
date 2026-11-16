import { useEffect, useState } from 'react'
import { api, type Agent, type ModelProvider } from './api'

export type AgentChoice = { agent: string; provider: string; model: string }
export const defaultChoice: AgentChoice = { agent: 'opencode', provider: '', model: '' }
export type BuildIntent = AgentChoice & { prompt: string }

export function AgentPicker({
  value,
  onChange,
  providers,
  agents,
  disabled = false,
}: {
  value: AgentChoice
  onChange: (value: AgentChoice) => void
  providers: ModelProvider[]
  agents: Agent[]
  disabled?: boolean
}) {
  const account = agents.find((agent) => agent.id === value.agent)
  // With no OpenCode account, OpenCode runs its built-in free model: there is no
  // catalog to load and paid models would fail.
  const freeOpencode =
    value.agent === 'opencode' &&
    !value.provider &&
    !agents.some((a) => a.id === 'opencode' && a.status === 'connected')
  // Claude Code without an API key uses the Claude sign-in inside the project.
  const claudeSignIn =
    value.agent === 'claude-code' &&
    !agents.some((a) => a.id === 'claude-code' && a.status === 'connected' && a.method === 'api_key')
  const [catalog, setCatalog] = useState<{ id: string; name: string }[]>([])
  const [catalogError, setCatalogError] = useState('')
  const [loading, setLoading] = useState(false)
  const [reload, setReload] = useState(0)
  useEffect(() => {
    let active = true
    setCatalog([])
    setCatalogError('')
    if (value.provider || freeOpencode) {
      setLoading(false)
      return
    }
    setLoading(true)
    Promise.resolve()
      .then(() => api.getAgentModels(value.agent))
      .then((result) => {
        if (active) setCatalog(result.models)
      })
      .catch((error) => {
        if (active) setCatalogError(error.message)
      })
      .finally(() => {
        if (active) setLoading(false)
      })
    return () => {
      active = false
    }
  }, [value.agent, value.provider, reload, account?.status, account?.method, freeOpencode])
  const usable = connectionsFor(value.agent, providers)
  const selected = usable.find((p) => p.id === value.provider)
  return (
    <div className="agent-picker">
      <label>
        Coding agent
        <select
          disabled={disabled}
          data-testid="task-agent"
          value={value.agent}
          onChange={(e) =>
            onChange({
              agent: e.target.value,
              model: '',
              provider: connectionsFor(e.target.value, providers).some((p) => p.id === value.provider)
                ? value.provider
                : '',
            })
          }
        >
          <option value="opencode">OpenCode</option>
          <option value="pi">Pi</option>
          <option value="claude-code">Claude Code</option>
          <option value="codex">Codex</option>
        </select>
      </label>
      {value.agent !== 'claude-code' && (value.agent !== 'codex' || usable.length > 0) && (
        <label>
          Connection
          <select
            disabled={disabled}
            aria-label="Model connection"
            data-testid="task-provider"
            value={value.provider}
            onChange={(e) => onChange({ ...value, provider: e.target.value, model: '' })}
          >
            <option value="">Connected account</option>
            {usable.map((p) => (
              <option value={p.id} key={p.id}>
                {p.name}
              </option>
            ))}
          </select>
        </label>
      )}
      <label className="full-width">
        Model
        <select
          disabled={disabled || (!value.provider && loading)}
          data-testid="task-model"
          value={value.model}
          onChange={(e) => onChange({ ...value, model: e.target.value })}
        >
          <option value="">
            {selected
              ? `${selected.models[0].id} (default)`
              : freeOpencode
                ? 'Free model'
                : claudeSignIn
                  ? 'Claude Code default'
                  : catalog.length
                    ? 'Account default'
                    : 'No account models loaded'}
          </option>
          {selected?.models.slice(1).map((m) => (
            <option key={m.id}>{m.id}</option>
          ))}
          {!value.provider &&
            catalog.map((m) => (
              <option key={m.id} value={m.id}>
                {m.name}
              </option>
            ))}
        </select>
      </label>
      {!value.provider && loading && (
        <p role="status" className="field-help full-width">
          Loading models from your provider…
        </p>
      )}
      {!value.provider && catalogError && (
        <p role="alert" className="field-help full-width">
          {catalogError}{' '}
          <button type="button" onClick={() => setReload((n) => n + 1)}>
            Retry model discovery
          </button>
        </p>
      )}
      <p className="field-help full-width">
        {value.agent === 'pi'
          ? value.provider
            ? 'Pi uses your model connection.'
            : 'Pi uses the account connected in Settings.'
          : value.provider
            ? 'Uses your model connection. Its API key stays encrypted in sandboxd.'
            : freeOpencode
              ? 'Free to use. No account needed.'
              : claudeSignIn
                ? 'Uses your Claude sign-in in this project.'
                : 'Uses the account connected in Settings.'}
      </p>
    </div>
  )
}

// Codex speaks only OpenAI's API, so it can use OpenAI-compatible connections
// (OpenRouter, OpenAI, Ollama...) but not Anthropic ones; Claude Code uses its
// own sign-in or key.
export function connectionsFor(agent: string, providers: ModelProvider[]) {
  if (agent === 'claude-code') return []
  if (agent === 'codex') return providers.filter((p) => p.protocol !== 'anthropic-messages')
  return providers
}

export function choiceBlocked(choice: AgentChoice, providers: ModelProvider[], agents: Agent[]) {
  if (choice.provider) return !providers.some((p) => p.id === choice.provider)
  if (choice.agent === 'pi') return !agents.some((a) => a.id === 'pi' && a.status === 'connected')
  // Claude Code without an API key signs in inside the project (see ClaudeSignIn).
  return choice.agent === 'codex' && !agents.some((a) => a.id === 'codex' && a.status === 'connected')
}
