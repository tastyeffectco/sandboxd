import { useEffect, useState } from 'react'
import { api } from './api'

export function AccountModelSelect({
  agent,
  value,
  onChange,
  disabled,
}: {
  agent: string
  value: string
  onChange: (model: string) => void
  disabled: boolean
}) {
  const [models, setModels] = useState<{ id: string; name: string }[]>([])
  const [error, setError] = useState('')
  const [loading, setLoading] = useState(true)
  const [reload, setReload] = useState(0)
  useEffect(() => {
    let active = true
    setLoading(true)
    setError('')
    void api
      .getAgentModels(agent)
      .then((result) => {
        if (active) setModels(result.models)
      })
      .catch((error) => {
        if (active) {
          setModels([])
          setError(error.message)
        }
      })
      .finally(() => {
        if (active) setLoading(false)
      })
    return () => {
      active = false
    }
  }, [agent, reload])
  return (
    <>
      <select
        value={value}
        onChange={(e) => onChange(e.target.value)}
        disabled={disabled || loading}
        data-testid={`agent-model-${agent}`}
      >
        <option value="">Use first listed model</option>
        {value && !models.some((model) => model.id === value) && (
          <option value={value} disabled>
            {value} (saved; unavailable)
          </option>
        )}
        {models.map((model) => (
          <option key={model.id} value={model.id}>
            {model.name}
          </option>
        ))}
      </select>
      {loading && <span className="field-help">Loading account models…</span>}
      {error && (
        <span role="alert" className="field-help">
          {error}{' '}
          <button type="button" onClick={() => setReload((n) => n + 1)}>
            Retry
          </button>
        </span>
      )}
    </>
  )
}
