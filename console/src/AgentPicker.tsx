import type { Agent, ModelProvider } from './api'

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
              provider: e.target.value === 'claude-code' ? '' : value.provider,
            })
          }
        >
          <option value="opencode">OpenCode</option>
          <option value="pi">Pi</option>
          <option value="claude-code">Claude Code</option>
        </select>
      </label>
      {value.agent !== 'claude-code' && (
        <label>
          Connection
          <select
            disabled={disabled}
            aria-label="Model connection"
            data-testid="task-provider"
            value={value.provider}
            onChange={(e) => onChange({ ...value, provider: e.target.value, model: '' })}
          >
            <option value="">{value.agent === 'pi' ? 'Select a connection…' : 'Built-in provider'}</option>
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
          disabled={disabled}
          data-testid="task-model"
          value={value.model}
          onChange={(e) => onChange({ ...value, model: e.target.value })}
        >
          <option value="">{selected ? `${selected.models[0].id} (default)` : 'Default model'}</option>
          {selected?.models.slice(1).map((m) => (
            <option key={m.id}>{m.id}</option>
          ))}
          {!value.provider &&
            value.agent === 'opencode' &&
            ['glm-5', 'kimi-k2.6', 'deepseek-v4-pro', 'MiniMax-M3.1-Flash', 'MiniMax-M3'].map((m) => (
              <option value={`opencode/${m}`} key={m}>
                {m}
              </option>
            ))}
          {value.agent === 'claude-code' &&
            ['sonnet', 'opus', 'haiku'].map((m) => <option key={m}>{m}</option>)}
        </select>
      </label>
      <p className="field-help full-width">
        {value.agent === 'pi'
          ? 'Pi uses your model connection.'
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
  if (choice.agent === 'pi') return true
  return (
    choice.agent === 'claude-code' && !agents.some((a) => a.id === 'claude-code' && a.status === 'connected')
  )
}
