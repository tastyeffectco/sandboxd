import { afterEach, describe, expect, it } from 'vitest'
import { agentAppUrl } from './AgentRoom'

describe('agent app view', () => {
  afterEach(() => {
    delete (window as { sandboxdDesktop?: unknown }).sandboxdDesktop
  })
  it('opens the agent’s own app on its private address in the desktop app only', () => {
    const agent = { id: '01M7C3X5A07QFRMWAR40QJGS0W', ui_label: 'Control panel' }
    expect(agentAppUrl(agent)).toBe('')
    ;(window as { sandboxdDesktop?: unknown }).sandboxdDesktop = {}
    expect(agentAppUrl(agent)).toBe(`http://ui-01m7c3x5a07qfrmwar40qjgs0w.localhost:${location.port}/`)
  })
  it('is hidden for agents without an app and for odd ids', () => {
    ;(window as { sandboxdDesktop?: unknown }).sandboxdDesktop = {}
    expect(agentAppUrl({ id: 'abcdef123', ui_label: '' })).toBe('')
    expect(agentAppUrl({ id: 'evil.example/x', ui_label: 'Control panel' })).toBe('')
  })
})
