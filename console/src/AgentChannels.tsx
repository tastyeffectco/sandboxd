import { useCallback, useEffect, useMemo, useState } from 'react'
import qrcode from 'qrcode-generator'
import { api, AgentChannel, AgentChannels as Channels, AgentInstance } from './api'
import { c, Btn, Input } from './design/kit'

// Where an agent talks to its owner: their own WhatsApp (linked by scanning a
// QR code) and their own Telegram bot. The agent answers only its owner.

const busyStatuses: AgentChannel['status'][] = ['preparing', 'scan', 'waiting', 'connecting']

export function AgentChannels({ agent, onError }: { agent: AgentInstance; onError: (message: string) => void }) {
  const [channels, setChannels] = useState<Channels | null>(null)
  const load = useCallback(
    () =>
      api
        .getAgentChannels(agent.id)
        .then(setChannels)
        .catch((e) => onError((e as Error).message)),
    [agent.id, onError],
  )
  useEffect(() => {
    void load()
  }, [load])
  // Follow a link in progress: the QR code refreshes, then the phone connects.
  const linking = !!channels && [channels.whatsapp, channels.telegram].some((ch) => ch && busyStatuses.includes(ch.status))
  useEffect(() => {
    if (!linking) return
    const t = window.setInterval(() => void load(), 2000)
    return () => window.clearInterval(t)
  }, [linking, load])

  const act = async (fn: () => Promise<Channels>) => {
    try {
      setChannels(await fn())
    } catch (e) {
      onError((e as Error).message)
    }
  }
  if (!channels || (!channels.whatsapp && !channels.telegram)) return null
  const running = agent.status === 'running'
  return (
    <section style={{ margin: '16px 0' }} data-testid="agent-channels">
      <h3 style={{ fontSize: 14, margin: '0 0 4px' }}>Where you talk to {agent.name}</h3>
      <p style={{ color: c.muted, fontSize: 13, margin: '0 0 12px' }}>
        Message {agent.name} from the apps you already use. It answers only you.
      </p>
      <div style={{ display: 'grid', gap: 12, gridTemplateColumns: 'repeat(auto-fit, minmax(260px, 1fr))' }}>
        {channels.whatsapp && (
          <WhatsApp
            name={agent.name}
            ch={channels.whatsapp}
            running={running}
            link={() => void act(() => api.linkWhatsApp(agent.id))}
            unlink={() => void act(() => api.unlinkWhatsApp(agent.id))}
          />
        )}
        {channels.telegram && (
          <Telegram
            name={agent.name}
            ch={channels.telegram}
            link={(token) => act(() => api.linkTelegram(agent.id, token))}
            unlink={() => void act(() => api.unlinkTelegram(agent.id))}
          />
        )}
      </div>
    </section>
  )
}

function Box({ title, linked, children, testid }: { title: string; linked: boolean; children: React.ReactNode; testid: string }) {
  return (
    <div
      data-testid={testid}
      style={{ border: `1px solid ${c.border}`, borderRadius: 10, padding: 14, display: 'grid', gap: 8, alignContent: 'start' }}
    >
      <div style={{ display: 'flex', alignItems: 'center', gap: 8 }}>
        <strong>{title}</strong>
        {linked && <span style={{ color: c.good, fontSize: 12 }}>● Connected</span>}
      </div>
      {children}
    </div>
  )
}

const note = { color: c.muted, fontSize: 13, margin: 0 } as const

function WhatsApp({
  name,
  ch,
  running,
  link,
  unlink,
}: {
  name: string
  ch: AgentChannel
  running: boolean
  link: () => void
  unlink: () => void
}) {
  const image = useMemo(() => {
    if (ch.qr_image) return ch.qr_image
    if (!ch.qr) return ''
    const q = qrcode(0, 'M')
    q.addData(ch.qr)
    q.make()
    return q.createDataURL(6, 2)
  }, [ch.qr, ch.qr_image])
  switch (ch.status) {
    case 'linked':
      return (
        <Box title="WhatsApp" linked testid="channel-whatsapp">
          <p style={note}>
            Linked to +{ch.number}. In WhatsApp, open the chat with yourself (“Message yourself”) and write to {name}{' '}
            there.
          </p>
          <div>
            <Btn
              sm
              onClick={() => {
                if (window.confirm(`Unlink WhatsApp? ${name} stops answering you there.`)) unlink()
              }}
            >
              Unlink
            </Btn>
          </div>
        </Box>
      )
    case 'preparing':
    case 'connecting':
      return (
        <Box title="WhatsApp" linked={false} testid="channel-whatsapp">
          <p role="status" style={note}>
            {ch.message}
          </p>
          <div>
            <Btn sm onClick={unlink}>
              Cancel
            </Btn>
          </div>
        </Box>
      )
    case 'scan':
      return (
        <Box title="WhatsApp" linked={false} testid="channel-whatsapp">
          {image && (
            <img
              src={image}
              alt="QR code to link WhatsApp"
              width={220}
              height={220}
              style={{ background: '#fff', padding: 8, borderRadius: 8, imageRendering: 'pixelated' }}
              data-testid="whatsapp-qr"
            />
          )}
          <ol style={{ ...note, paddingLeft: 18 }}>
            <li>Open WhatsApp on your phone.</li>
            <li>Tap Settings (or ⋮), then Linked devices, then Link a device.</li>
            <li>Point your phone at this code.</li>
          </ol>
          <p style={{ ...note, fontSize: 12 }}>
            This links {name} to your own WhatsApp account, like WhatsApp Web. The code refreshes by itself.
          </p>
          <div>
            <Btn sm onClick={unlink}>
              Cancel
            </Btn>
          </div>
        </Box>
      )
    default:
      return (
        <Box title="WhatsApp" linked={false} testid="channel-whatsapp">
          {ch.status === 'failed' && (
            <p role="alert" className="inline-error" style={{ margin: 0 }}>
              {ch.message}
            </p>
          )}
          <p style={note}>Chat with {name} from your own WhatsApp. You only need to scan a code with your phone.</p>
          <div>
            <Btn sm variant="primary" disabled={!running} onClick={link} data-testid="link-whatsapp">
              {ch.status === 'failed' ? 'Try again' : 'Link WhatsApp'}
            </Btn>
          </div>
          {!running && <p style={{ ...note, fontSize: 12 }}>Start {name} first.</p>}
        </Box>
      )
  }
}

function Telegram({
  name,
  ch,
  link,
  unlink,
}: {
  name: string
  ch: AgentChannel
  link: (token: string) => Promise<void>
  unlink: () => void
}) {
  const [open, setOpen] = useState(false)
  const [token, setToken] = useState('')
  const [busy, setBusy] = useState(false)
  const botLink = ch.bot ? (
    <a href={`https://t.me/${ch.bot}`} target="_blank" rel="noreferrer">
      @{ch.bot}
    </a>
  ) : null
  switch (ch.status) {
    case 'linked':
      return (
        <Box title="Telegram" linked testid="channel-telegram">
          <p style={note}>
            {name} answers you as {botLink}.
          </p>
          <div>
            <Btn
              sm
              onClick={() => {
                if (window.confirm(`Disconnect Telegram? ${name} stops answering you there.`)) unlink()
              }}
            >
              Disconnect
            </Btn>
          </div>
        </Box>
      )
    case 'waiting':
      return (
        <Box title="Telegram" linked={false} testid="channel-telegram">
          <p role="status" style={note}>
            Almost done: open {botLink} in Telegram and send it any message, so it knows it’s you.
          </p>
          <div>
            <Btn sm onClick={unlink}>
              Cancel
            </Btn>
          </div>
        </Box>
      )
    default:
      return (
        <Box title="Telegram" linked={false} testid="channel-telegram">
          {ch.status === 'failed' && (
            <p role="alert" className="inline-error" style={{ margin: 0 }}>
              {ch.message}
            </p>
          )}
          {!open ? (
            <>
              <p style={note}>Chat with {name} through your own Telegram bot. It takes about a minute to create one.</p>
              <div>
                <Btn sm variant="primary" onClick={() => setOpen(true)} data-testid="connect-telegram">
                  Connect Telegram
                </Btn>
              </div>
            </>
          ) : (
            <form
              style={{ display: 'grid', gap: 8 }}
              onSubmit={(e) => {
                e.preventDefault()
                setBusy(true)
                void link(token.trim()).finally(() => setBusy(false))
              }}
            >
              <ol style={{ ...note, paddingLeft: 18 }}>
                <li>
                  In Telegram, open{' '}
                  <a href="https://t.me/BotFather" target="_blank" rel="noreferrer">
                    @BotFather
                  </a>{' '}
                  and send <code>/newbot</code>.
                </li>
                <li>Choose a name for your bot. BotFather replies with a token.</li>
                <li>Paste the token here.</li>
              </ol>
              <Input
                value={token}
                onChange={(e) => setToken(e.target.value)}
                placeholder="123456789:ABC…"
                autoComplete="off"
                aria-label="Bot token"
                data-testid="telegram-token"
              />
              <div style={{ display: 'flex', gap: 8 }}>
                <Btn sm type="submit" variant="primary" disabled={busy || !token.trim()}>
                  {busy ? 'Checking…' : 'Connect'}
                </Btn>
                <Btn sm type="button" onClick={() => setOpen(false)}>
                  Cancel
                </Btn>
              </div>
            </form>
          )}
        </Box>
      )
  }
}
