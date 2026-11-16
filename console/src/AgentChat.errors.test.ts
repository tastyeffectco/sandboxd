import { describe, expect, it } from 'vitest'
import { plainTaskError } from './AgentChat'

describe('plainTaskError', () => {
  it('turns the task timeout into a next step', () => {
    const text = plainTaskError('agent did not finish within the task timeout')!
    expect(text).toMatch(/took longer than the time limit/)
    expect(text).toMatch(/Please continue/)
    expect(text).not.toMatch(/task timeout/)
  })
  it('leaves other messages and empty ones alone', () => {
    expect(plainTaskError('The build failed: missing module')).toBe('The build failed: missing module')
    expect(plainTaskError(undefined)).toBeUndefined()
  })
})
