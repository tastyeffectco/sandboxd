import { describe, expect, it } from 'vitest'
import { choiceBlocked, defaultChoice } from './AgentPicker'
import type { Agent, ModelProvider } from './api'

const agent = (id: string, status: Agent['status']): Agent => ({
  id,
  label: id,
  installed_state: 'installed',
  status,
  method: status === 'connected' ? 'oauth' : '',
  supports_oauth: true,
  supports_api_key: true,
  runnable: true,
})
const provider = { id: 'mp_ollama' } as ModelProvider
const choice = (agentId: string, providerId = '') => ({ agent: agentId, provider: providerId, model: '' })

describe('choiceBlocked', () => {
  it('never blocks the default: OpenCode works with no account (free model)', () => {
    expect(defaultChoice.agent).toBe('opencode')
    expect(choiceBlocked(defaultChoice, [], [])).toBe(false)
    expect(choiceBlocked(defaultChoice, [], [agent('opencode', 'needs_login')])).toBe(false)
  })

  it('blocks a model connection that no longer exists', () => {
    expect(choiceBlocked(choice('opencode', 'mp_ollama'), [provider], [])).toBe(false)
    expect(choiceBlocked(choice('opencode', 'mp_gone'), [provider], [])).toBe(true)
    expect(choiceBlocked(choice('pi', 'mp_ollama'), [provider], [])).toBe(false)
  })

  it('needs a connected account for Pi and Codex', () => {
    for (const id of ['pi', 'codex']) {
      expect(choiceBlocked(choice(id), [], [])).toBe(true)
      expect(choiceBlocked(choice(id), [], [agent(id, 'needs_login')])).toBe(true)
      expect(choiceBlocked(choice(id), [], [agent(id, 'connected')])).toBe(false)
    }
  })

  it('lets Claude Code through without an account (it signs in inside the project)', () => {
    expect(choiceBlocked(choice('claude-code'), [], [])).toBe(false)
  })
})
