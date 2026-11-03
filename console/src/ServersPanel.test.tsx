import { describe, it, expect, vi } from 'vitest'
import { render, fireEvent, screen, waitFor, act } from '@testing-library/react'
import { ServersPanel } from './ServersPanel'
import type {
  AddServerProgress,
  AddServerResult,
  DesktopServer,
  DesktopServers,
  ServerStatus,
} from './desktop'

// jsdom has no <dialog> modal support.
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
  username: 'root',
  hostKeySha256: 'SHA256:' + 'a'.repeat(43),
  front: 'caddy',
  domain: '203-0-113-10.sslip.io',
  proxy: 'none',
  addedAt: '2026-11-02T00:00:00Z',
  status: { state: 'connected', reason: '' },
}

function fake(initial: DesktopServer[] = [], results: AddServerResult[] = []) {
  let progress: (p: AddServerProgress) => void = () => {}
  let change: (c: { id: string; status: ServerStatus }) => void = () => {}
  let list = [...initial]
  let release: () => void = () => {}
  const bridge = {
    list: vi.fn(async () => list),
    status: vi.fn(),
    add: vi.fn(async () => {
      progress({ step: 'connecting', message: 'Connecting to your server…' })
      progress({ step: 'installing', message: 'Installing sandboxd (this takes a few minutes)…' })
      await new Promise<void>((r) => (release = r))
      const result = results.shift() || { ok: true as const, server }
      if (result.ok) list = [...list, result.server]
      return result
    }),
    remove: vi.fn(async (id: string) => {
      list = list.filter((s) => s.id !== id)
      return { removed: true as const, note: 'My VPS was removed from this computer.' }
    }),
    trustIdentity: vi.fn(async () => ({ trusted: true })),
    onProgress: (cb: typeof progress) => {
      progress = cb
      return () => {}
    },
    onChange: (cb: typeof change) => {
      change = cb
      return () => {}
    },
  }
  return {
    bridge: bridge as unknown as DesktopServers & typeof bridge,
    finish: () => act(() => release()),
    change: (c: { id: string; status: ServerStatus }) => act(() => change(c)),
  }
}

describe('ServersPanel', () => {
  it('adds a server with progress steps, sending the password only in the add call', async () => {
    const f = fake()
    const toast = vi.fn()
    render(<ServersPanel servers={f.bridge} toast={toast} />)
    expect(await screen.findByTestId('servers-empty')).toBeTruthy()
    fireEvent.click(screen.getByTestId('server-add-open'))
    fireEvent.change(screen.getByTestId('server-host'), { target: { value: ' 203.0.113.10 ' } })
    fireEvent.change(screen.getByTestId('server-password'), { target: { value: 'hunter2' } })
    fireEvent.submit(screen.getByTestId('server-add-submit').closest('form')!)
    const steps = await screen.findByTestId('server-add-progress')
    expect(steps.querySelector('[aria-current="step"]')?.textContent).toMatch(/Installing sandboxd/)
    expect(f.bridge.add).toHaveBeenCalledWith({
      host: '203.0.113.10',
      password: 'hunter2',
      installDocker: false,
    })
    await f.finish()
    await waitFor(() => expect(screen.getByTestId(`server-${server.id}`)).toBeTruthy())
    expect(toast).toHaveBeenCalledWith('My VPS is ready')
    expect(screen.getByText('Connected')).toBeTruthy()
  })

  it('passes more options and offers to install Docker after asking', async () => {
    const f = fake(
      [],
      [
        { ok: false, code: 'needs_docker', message: 'This server doesn’t have Docker yet.' },
        { ok: true, server },
      ],
    )
    render(<ServersPanel servers={f.bridge} toast={() => {}} />)
    fireEvent.click(await screen.findByTestId('server-add-open'))
    fireEvent.change(screen.getByTestId('server-host'), { target: { value: 'vps.example.com' } })
    fireEvent.change(screen.getByTestId('server-password'), { target: { value: 'pw' } })
    fireEvent.change(screen.getByTestId('server-username'), { target: { value: 'ubuntu' } })
    fireEvent.change(screen.getByTestId('server-port'), { target: { value: '2222' } })
    fireEvent.submit(screen.getByTestId('server-add-submit').closest('form')!)
    await f.finish()
    expect((await screen.findByTestId('server-add-error')).textContent).toMatch(/Docker yet/)
    fireEvent.click(screen.getByTestId('server-install-docker'))
    expect(f.bridge.add).toHaveBeenLastCalledWith({
      host: 'vps.example.com',
      password: 'pw',
      username: 'ubuntu',
      port: 2222,
      installDocker: true,
    })
    await f.finish()
    await waitFor(() => expect(screen.getByTestId(`server-${server.id}`)).toBeTruthy())
  })

  it('shows install details on failure', async () => {
    const f = fake(
      [],
      [
        {
          ok: false,
          code: 'install_failed',
          message: 'The installation stopped.',
          detail: 'ERROR: disk full',
        },
      ],
    )
    render(<ServersPanel servers={f.bridge} toast={() => {}} />)
    fireEvent.click(await screen.findByTestId('server-add-open'))
    fireEvent.change(screen.getByTestId('server-host'), { target: { value: '203.0.113.10' } })
    fireEvent.change(screen.getByTestId('server-password'), { target: { value: 'pw' } })
    fireEvent.submit(screen.getByTestId('server-add-submit').closest('form')!)
    await f.finish()
    const error = await screen.findByTestId('server-add-error')
    expect(error.textContent).toMatch(/installation stopped/)
    expect(error.textContent).toMatch(/disk full/)
  })

  it('updates status live, explains a changed identity and asks before trusting it', async () => {
    const f = fake([server])
    render(<ServersPanel servers={f.bridge} toast={() => {}} />)
    await screen.findByTestId(`server-${server.id}`)
    await f.change({
      id: server.id,
      status: {
        state: 'offline',
        reason: "This server's identity changed since you added it.",
        identityChanged: true,
        presentedSha256: 'SHA256:' + 'b'.repeat(43),
      },
    })
    expect(screen.getByText('Offline')).toBeTruthy()
    expect(screen.getByRole('alert').textContent).toMatch(/identity changed/)
    fireEvent.click(screen.getByTestId('server-trust'))
    await waitFor(() => expect(f.bridge.trustIdentity).toHaveBeenCalledWith(server.id))
  })

  it('removes a server only after confirmation', async () => {
    const f = fake([server])
    const toast = vi.fn()
    render(<ServersPanel servers={f.bridge} toast={toast} />)
    fireEvent.click(await screen.findByTestId('server-remove'))
    expect(f.bridge.remove).not.toHaveBeenCalled()
    expect(screen.getByText(/apps keep running on the/)).toBeTruthy()
    fireEvent.click(screen.getByTestId('server-remove-confirm'))
    await waitFor(() => expect(f.bridge.remove).toHaveBeenCalledWith(server.id))
    await waitFor(() => expect(screen.getByTestId('servers-empty')).toBeTruthy())
    expect(toast).toHaveBeenCalledWith('My VPS was removed from this computer.')
  })
})
