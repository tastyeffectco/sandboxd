import type { Agent, ModelProvider } from './api'
import type { AgentChoice } from './AgentPicker'

// One AI a person can pick, in their words: "Free AI", "DeepSeek V4 Pro ·
// OpenRouter", "ChatGPT (Codex)". Each maps to the engine's choice of coding
// agent, connection and model, which stays out of sight.
export type AIOption = {
  key: string
  label: string
  detail: string
  group: 'free' | 'connections' | 'subscriptions'
  choice: AgentChoice
}

export const FREE_AI: AgentChoice = { agent: 'opencode', provider: '', model: '' }

const connected = (agents: Agent[], id: string) => agents.find((a) => a.id === id && a.status === 'connected')

// "deepseek/deepseek-v4-pro" -> "DeepSeek V4 Pro"; "openrouter/free" -> "Free models".
export function modelName(id: string) {
  const last = id.split('/').pop() || id
  if (last === 'free') return 'Free models'
  return last
    .split(/[-_:]/)
    .filter(Boolean)
    .map((w) =>
      /^v?\d/.test(w)
        ? w.toUpperCase()
        : ({ deepseek: 'DeepSeek', gpt: 'GPT', glm: 'GLM', qwen: 'Qwen', llama: 'Llama' } as Record<string, string>)[
            w.toLowerCase()
          ] || w[0].toUpperCase() + w.slice(1),
    )
    .join(' ')
}

export function aiOptions(agents: Agent[], providers: ModelProvider[]): AIOption[] {
  const out: AIOption[] = []
  if (connected(agents, 'opencode'))
    out.push({
      key: 'opencode-account',
      label: 'OpenCode account',
      detail: 'Your OpenCode subscription',
      group: 'subscriptions',
      choice: FREE_AI,
    })
  else
    out.push({
      key: 'free',
      label: 'Free AI',
      detail: 'Built in. No account needed',
      group: 'free',
      choice: FREE_AI,
    })
  for (const p of providers)
    for (const m of p.models)
      out.push({
        key: `${p.id}:${m.id}`,
        label: modelName(m.id),
        detail: p.name,
        group: 'connections',
        choice: { agent: 'opencode', provider: p.id, model: m.id },
      })
  if (connected(agents, 'codex'))
    out.push({
      key: 'codex',
      label: 'ChatGPT (Codex)',
      detail: 'Your ChatGPT subscription',
      group: 'subscriptions',
      choice: { agent: 'codex', provider: '', model: '' },
    })
  const claude = connected(agents, 'claude-code')
  out.push({
    key: 'claude',
    label: 'Claude',
    detail: claude?.method === 'api_key' ? 'Your Claude API key' : 'Your Claude Pro or Max: sign in once in each project',
    group: 'subscriptions',
    choice: { agent: 'claude-code', provider: '', model: '' },
  })
  if (connected(agents, 'pi'))
    out.push({
      key: 'pi',
      label: 'Pi account',
      detail: 'Your Pi subscription',
      group: 'subscriptions',
      choice: { agent: 'pi', provider: '', model: '' },
    })
  return out
}

// The option a choice stands for; anything else (picked in More options)
// gets a plain description built from its parts.
export function describeChoice(choice: AgentChoice, options: AIOption[]): { label: string; detail: string } {
  const match = options.find(
    (o) =>
      o.choice.agent === choice.agent &&
      (o.choice.provider || '') === (choice.provider || '') &&
      (o.choice.model || '') === (choice.model || ''),
  )
  if (match) return match
  const viaConnection = options.find((o) => o.choice.provider && o.choice.provider === choice.provider)
  return {
    label: choice.model ? modelName(choice.model) : viaConnection?.detail || 'Custom',
    detail: viaConnection?.detail || choice.agent,
  }
}
