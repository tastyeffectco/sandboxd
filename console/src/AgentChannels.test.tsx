import { afterEach, describe, expect, it, vi } from 'vitest'
import { act, cleanup, fireEvent, render, screen, waitFor } from '@testing-library/react'
import { AgentChannels } from './AgentChannels'
import { api, AgentInstance } from './api'

vi.mock('./api', () => ({
  api: {
    getAgentChannels: vi.fn(),
    linkWhatsApp: vi.fn(),
    unlinkWhatsApp: vi.fn(),
    linkTelegram: vi.fn(),
    unlinkTelegram: vi.fn(),
  },
}))

afterEach(() => {
  cleanup()
  vi.useRealTimers()
})

const agent: AgentInstance = {
  id: 'a1',
  package: 'openclaw',
  name: 'Robin',
  connection_id: 'c1',
  model: 'm1',
  status: 'running',
  telegram: false,
  apps_today: 0,
  apps_per_day: 10,
  created_at: '',
}
const off = { whatsapp: { status: 'off' as const }, telegram: { status: 'off' as const } }

describe('AgentChannels', () => {
  it('links WhatsApp: shows the QR code to scan, then the linked number', async () => {
    vi.mocked(api.getAgentChannels).mockResolvedValueOnce(off)
    vi.mocked(api.linkWhatsApp).mockResolvedValue({
      ...off,
      whatsapp: { status: 'scan', qr: 'https://wa.me/settings/linked_devices#2@abc' },
    })
    render(<AgentChannels agent={agent} onError={() => {}} />)
    fireEvent.click(await screen.findByTestId('link-whatsapp'))
    const qr = (await screen.findByTestId('whatsapp-qr')) as HTMLImageElement
    expect(qr.src).toMatch(/^data:image\/gif;base64,/) // drawn from the code's text
    expect(screen.getByText(/Linked devices, then Link a device/)).toBeTruthy()

    vi.mocked(api.getAgentChannels).mockResolvedValue({ ...off, whatsapp: { status: 'linked', number: '33612345678' } })
    expect(await screen.findByText(/Linked to \+33612345678/, {}, { timeout: 4000 })).toBeTruthy()
  })

  it('shows a ready QR image as is', async () => {
    vi.mocked(api.getAgentChannels).mockResolvedValue({
      ...off,
      whatsapp: { status: 'scan', qr_image: 'data:image/png;base64,AAAA' },
    })
    render(<AgentChannels agent={agent} onError={() => {}} />)
    expect(((await screen.findByTestId('whatsapp-qr')) as HTMLImageElement).src).toBe('data:image/png;base64,AAAA')
  })

  it('needs the agent running to link WhatsApp', async () => {
    vi.mocked(api.getAgentChannels).mockResolvedValue(off)
    render(<AgentChannels agent={{ ...agent, status: 'stopped' }} onError={() => {}} />)
    expect(((await screen.findByTestId('link-whatsapp')) as HTMLButtonElement).disabled).toBe(true)
    expect(screen.getByText('Start Robin first.')).toBeTruthy()
  })

  it('connects Telegram with a bot token, then asks for a first message', async () => {
    vi.mocked(api.getAgentChannels).mockResolvedValue(off)
    vi.mocked(api.linkTelegram).mockResolvedValue({ ...off, telegram: { status: 'waiting', bot: 'robin_bot' } })
    render(<AgentChannels agent={agent} onError={() => {}} />)
    fireEvent.click(await screen.findByTestId('connect-telegram'))
    expect(screen.getByText('@BotFather')).toBeTruthy()
    fireEvent.change(screen.getByTestId('telegram-token'), { target: { value: ' 123:abc ' } })
    fireEvent.submit(screen.getByTestId('telegram-token').closest('form')!)
    await waitFor(() => expect(api.linkTelegram).toHaveBeenCalledWith('a1', '123:abc'))
    expect(await screen.findByText(/send it any message/)).toBeTruthy()
    expect(screen.getByRole('link', { name: '@robin_bot' }).getAttribute('href')).toBe('https://t.me/robin_bot')
  })

  it('explains a failed link and offers to try again', async () => {
    vi.mocked(api.getAgentChannels).mockResolvedValue({
      ...off,
      whatsapp: { status: 'failed', message: 'The QR code was not scanned in time.' },
    })
    render(<AgentChannels agent={agent} onError={() => {}} />)
    expect(await screen.findByText('The QR code was not scanned in time.')).toBeTruthy()
    expect(screen.getByTestId('link-whatsapp').textContent).toBe('Try again')
  })

  it('shows nothing for an agent without channels', async () => {
    vi.mocked(api.getAgentChannels).mockResolvedValue({ whatsapp: null, telegram: null })
    const { container } = render(<AgentChannels agent={agent} onError={() => {}} />)
    await act(async () => {})
    expect(container.innerHTML).toBe('')
  })
})
