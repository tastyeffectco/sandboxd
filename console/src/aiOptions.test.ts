import { describe, expect, it } from 'vitest'
import { aiOptions, describeChoice, FREE_AI, modelName } from './aiOptions'
import type { Agent, ModelProvider } from './api'

const agent = (id: string, status: Agent['status'], method: Agent['method'] = 'oauth') =>
  ({ id, label: id, installed_state: 'installed', status, method, supports_oauth: true, supports_api_key: true, runnable: true }) as Agent
const openrouter = {
  id: 'c1',
  name: 'OpenRouter',
  protocol: 'openai-completions',
  base_url: 'https://openrouter.ai/api/v1',
  has_api_key: true,
  models: [
    { id: 'deepseek/deepseek-v4-pro', context_window: 1, max_tokens: 1 },
    { id: 'openrouter/free', context_window: 1, max_tokens: 1 },
  ],
} as ModelProvider

describe('AI choices in plain words', () => {
  it('names models the way people read them', () => {
    expect(modelName('deepseek/deepseek-v4-pro')).toBe('DeepSeek V4 Pro')
    expect(modelName('openrouter/free')).toBe('Free models')
    expect(modelName('anthropic/claude-sonnet-4.5')).toBe('Claude Sonnet 4.5')
  })

  it('offers the free AI, each connected model, and the subscriptions', () => {
    const list = aiOptions([agent('opencode', 'needs_login'), agent('codex', 'connected')], [openrouter])
    expect(list.map((o) => `${o.label} · ${o.detail}`)).toEqual([
      'Free AI · Built in. No account needed',
      'DeepSeek V4 Pro · OpenRouter',
      'Free models · OpenRouter',
      'ChatGPT (Codex) · Your ChatGPT subscription',
      'Claude · Your Claude Pro or Max: sign in once in each project',
    ])
    expect(list[1].choice).toEqual({ agent: 'opencode', provider: 'c1', model: 'deepseek/deepseek-v4-pro' })
  })

  it('says OpenCode account instead of Free AI once one is connected', () => {
    const list = aiOptions([agent('opencode', 'connected')], [])
    expect(list[0]).toMatchObject({ label: 'OpenCode account', choice: FREE_AI })
  })

  it('describes the current choice, even one made in More options', () => {
    const list = aiOptions([], [openrouter])
    expect(describeChoice({ agent: 'opencode', provider: 'c1', model: 'deepseek/deepseek-v4-pro' }, list).label).toBe(
      'DeepSeek V4 Pro',
    )
    expect(describeChoice({ agent: 'pi', provider: 'c1', model: 'x/glm-5' }, list)).toEqual({
      label: 'GLM 5',
      detail: 'OpenRouter',
    })
  })
})
