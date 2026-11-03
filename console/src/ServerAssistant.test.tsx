import { describe, it, expect, vi } from 'vitest'
import { render, fireEvent, screen, waitFor, act } from '@testing-library/react'
import { ServerAssistant } from './ServerAssistant'
import type {
  AssistantApproval,
  AssistantEvent,
  AssistantToolsEvent,
  DesktopAssistant,
  DesktopServer,
} from './desktop'

HTMLDialogElement.prototype.showModal ||= function (this: HTMLDialogElement) {
  this.setAttribute('open', '')
}
HTMLDialogElement.prototype.close ||= function (this: HTMLDialogElement) {
  this.removeAttribute('open')
}

const server: DesktopServer = {
  id: 'srv_0123456789ab',
  name: 'My VPS',
  host: '203.0.113.10',
  port: 22,
  username: 'ubuntu',
  hostKeySha256: 'SHA256:' + 'a'.repeat(43),
  front: 'existing',
  domain: '203-0-113-10.sslip.io',
  proxy: 'nginx',
  addedAt: '2026-11-02T00:00:00Z',
  status: { state: 'connected', reason: '' },
}

const approval: AssistantApproval = {
  id: 'ap_1',
  serverId: server.id,
  serverName: 'My VPS',
  login: 'ubuntu',
  kind: 'run',
  title: 'Reload nginx so your shop is online',
  command: 'systemctl reload nginx',
  admin: true,
  needsPassword: true,
}

function fake() {
  let onEvent: (e: AssistantEvent) => void = () => {}
  let onTools: (e: AssistantToolsEvent) => void = () => {}
  let finish: (r: { ok: boolean }) => void = () => {}
  const bridge = {
    send: vi.fn(() => new Promise<{ ok: boolean }>((r) => (finish = r))),
    stop: vi.fn(async () => {}),
    reset: vi.fn(async () => {}),
    pending: vi.fn(async () => [] as AssistantApproval[]),
    activity: vi.fn(async () => []),
    decide: vi.fn(async (id: string) => {
      onTools({ type: 'decided', id, allow: true })
      return true
    }),
    onEvent: (cb: typeof onEvent) => ((onEvent = cb), () => {}),
    onTools: (cb: typeof onTools) => ((onTools = cb), () => {}),
    onSetup: () => () => {},
  }
  return {
    bridge: bridge as unknown as DesktopAssistant & typeof bridge,
    event: (e: AssistantEvent) => act(() => onEvent(e)),
    tools: (e: AssistantToolsEvent) => act(() => onTools(e)),
    finish: (r = { ok: true }) => act(() => finish(r)),
  }
}

describe('ServerAssistant', () => {
  it('chats, shows what it did, and asks before a change (with the sudo password)', async () => {
    const f = fake()
    render(<ServerAssistant assistant={f.bridge} server={server} close={() => {}} />)
    fireEvent.click(screen.getAllByTestId('assistant-suggestion')[0])
    expect(f.bridge.send).toHaveBeenCalledWith(server.id, 'Get this server ready to put my apps online')
    expect(screen.getByTestId('assistant-you').textContent).toBe('Get this server ready to put my apps online')
    expect(screen.getByTestId('assistant-stop')).toBeTruthy()

    await f.event({ serverId: server.id, type: 'tool', tool: 'look', status: 'completed' })
    await f.event({ serverId: server.id, type: 'text', text: 'Your server runs nginx. I will connect it.' })
    // Another server's events stay out.
    await f.event({ serverId: 'srv_ffffffffffff', type: 'text', text: 'not here' })
    expect(screen.getByText('Looked at your server')).toBeTruthy()
    expect(screen.getByTestId('assistant-assistant').textContent).toMatch(/runs nginx/)
    expect(screen.queryByText('not here')).toBeNull()

    await f.tools({ type: 'approval', approval })
    const card = screen.getByTestId('assistant-approval')
    expect(card.textContent).toMatch(/Reload nginx so your shop is online/)
    expect(card.textContent).toMatch(/with admin rights/)
    expect(screen.getByTestId('assistant-approval-command').textContent).toBe('systemctl reload nginx')
    const allow = screen.getByTestId('assistant-allow') as HTMLButtonElement
    expect(allow.disabled).toBe(true)
    fireEvent.change(screen.getByTestId('assistant-approval-password'), { target: { value: 'hunter2' } })
    fireEvent.click(allow)
    await waitFor(() => expect(screen.queryByTestId('assistant-approval')).toBeNull())
    expect(f.bridge.decide).toHaveBeenCalledWith('ap_1', { allow: true, password: 'hunter2' })

    await f.finish()
    expect(screen.getByTestId('assistant-send')).toBeTruthy()
    fireEvent.click(screen.getByTestId('assistant-reset'))
    await waitFor(() => expect(screen.queryByTestId('assistant-you')).toBeNull())
    expect(f.bridge.reset).toHaveBeenCalledWith(server.id)
  })

  it('“Don’t allow” declines, and errors are said plainly', async () => {
    const f = fake()
    render(<ServerAssistant assistant={f.bridge} server={server} close={() => {}} />)
    fireEvent.change(screen.getByTestId('assistant-input'), { target: { value: 'Use my domain' } })
    fireEvent.click(screen.getByTestId('assistant-send'))
    await f.tools({
      type: 'approval',
      approval: {
        ...approval,
        id: 'ap_2',
        kind: 'write',
        path: '/etc/nginx/sites-available/sandboxd-shop.conf',
        before: null,
        after: 'server {}\n',
        command: undefined,
        needsPassword: false,
      },
    })
    expect(screen.getByTestId('assistant-approval').textContent).toMatch(
      /Create \/etc\/nginx\/sites-available\/sandboxd-shop\.conf/,
    )
    fireEvent.click(screen.getByTestId('assistant-deny'))
    await waitFor(() => expect(f.bridge.decide).toHaveBeenCalledWith('ap_2', { allow: false }))
    await f.event({ serverId: server.id, type: 'error', text: 'The free assistant is busy.' })
    expect(screen.getByRole('alert').textContent).toBe('The free assistant is busy.')
    fireEvent.click(screen.getByTestId('assistant-stop'))
    expect(f.bridge.stop).toHaveBeenCalledWith(server.id)
  })

  it('shows approvals that were already waiting when it opens', async () => {
    const f = fake()
    f.bridge.pending.mockResolvedValue([approval, { ...approval, id: 'ap_other', serverId: 'srv_ffffffffffff' }])
    render(<ServerAssistant assistant={f.bridge} server={server} close={() => {}} />)
    await waitFor(() => expect(screen.getAllByTestId('assistant-approval')).toHaveLength(1))
  })
})
