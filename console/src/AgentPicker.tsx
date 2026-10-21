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
  const [catalog, setCatalog] = useState<{ id: string; name: string }[]>([])
  const [catalogError, setCatalogError] = useState('')
  const [loading, setLoading] = useState(false)
  const [reload, setReload] = useState(0)
  useEffect(() => {
    let active = true
    setCatalog([])
    setCatalogError('')
    if (value.provider) {
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
  }, [value.agent, value.provider, reload])
  const selected = providers.find((p) => p.id === value.provider)
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
              provider: ['claude-code', 'codex'].includes(e.target.value) ? '' : value.provider,
            })
          }
        >
          <option value="opencode">OpenCode</option>
          <option value="pi">Pi</option>
          <option value="claude-code">Claude Code</option>
          <option value="codex">Codex</option>
        </select>
      </label>
      {!['claude-code', 'codex'].includes(value.agent) && (
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
            {providers.map((p) => (
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
            ? 'Your model endpoint. Your API key stays on the control plane.'
            : value.agent === 'opencode' &&
                !agents.some((a) => a.id === 'opencode' && a.status === 'connected')
              ? 'OpenCode can start with its built-in free tier.'
              : 'Uses the account connected in Settings.'}
      </p>
    </div>
  )
}

export function choiceBlocked(choice: AgentChoice, providers: ModelProvider[], agents: Agent[]) {
  if (choice.provider) return !providers.some((p) => p.id === choice.provider)
  if (choice.agent === 'pi') return !agents.some((a) => a.id === 'pi' && a.status === 'connected')
  return (
    ['claude-code', 'codex'].includes(choice.agent) &&
    !agents.some((a) => a.id === choice.agent && a.status === 'connected')
  )
}
