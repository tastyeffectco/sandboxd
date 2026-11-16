import { describe, expect, it } from 'vitest'
import { suggestedConnection } from './AgentsView'
import type { ModelProvider } from './api'

const conn = (id: string, model: string) => ({ id, models: [{ id: model }] }) as unknown as ModelProvider

describe('suggestedConnection', () => {
  const models = [conn('a', 'deepseek/deepseek-v4.1-flash'), conn('b', 'qwen2.5:7b'), conn('c', 'deepseek/deepseek-v4-pro')]
  it('suggests the strongest connected model to agents that lean on tools', () => {
    expect(suggestedConnection(models, true)).toBe('c')
  })
  it('keeps the first connection otherwise, or when none is stronger', () => {
    expect(suggestedConnection(models, false)).toBe('a')
    expect(suggestedConnection(models.slice(0, 2), true)).toBe('a')
    expect(suggestedConnection([], true)).toBeUndefined()
  })
})
