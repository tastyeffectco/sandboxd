import { useEffect, useRef, useState } from 'react'
import { Terminal as XTerm } from '@xterm/xterm'
import { FitAddon } from '@xterm/addon-fit'
import '@xterm/xterm/css/xterm.css'

// SandboxTerminal — an interactive shell inside the sandbox (uid 1000, the app
// workspace), streamed over a WebSocket to the /v1/sandboxes/{id}/terminal
// endpoint. Protocol: BINARY frames carry keystrokes (client→server) and shell
// output (server→client); a TEXT frame `{"resize":{cols,rows}}` reports size.
//
// The shell starts only on an explicit click — never on tab open. A live
// session holds the sandbox awake (it can't idle-sleep), so opening one must
// be a deliberate act, not a side effect of rendering the tab.
export function SandboxTerminal({ sandboxId }: { sandboxId: string }) {
  const host = useRef<HTMLDivElement>(null)
  // 0 = no session requested yet; each increment opens a fresh shell.
  const [session, setSession] = useState(0)
  const [status, setStatus] = useState<'idle' | 'connecting' | 'live' | 'closed'>('idle')

  // Switching sandboxes resets to the not-connected state.
  useEffect(() => {
    setSession(0)
    setStatus('idle')
  }, [sandboxId])

  useEffect(() => {
    if (session === 0) return
    const el = host.current
    if (!el) return

    const term = new XTerm({
      cursorBlink: true,
      fontFamily: 'ui-monospace, SFMono-Regular, Menlo, Consolas, monospace',
      fontSize: 13,
      theme: { background: '#0a0d14', foreground: '#c6d0de', cursor: '#7c93ff', selectionBackground: '#2c374d' },
    })
    const fit = new FitAddon()
    term.loadAddon(fit)
    term.open(el)
    try { fit.fit() } catch { /* not yet laid out */ }

    const proto = location.protocol === 'https:' ? 'wss' : 'ws'
    const ws = new WebSocket(`${proto}://${location.host}/v1/sandboxes/${sandboxId}/terminal`)
    ws.binaryType = 'arraybuffer'
    const enc = new TextEncoder()

    const sendResize = () => {
      try { fit.fit() } catch { /* ignore */ }
      if (ws.readyState === WebSocket.OPEN) ws.send(JSON.stringify({ resize: { cols: term.cols, rows: term.rows } }))
    }

    ws.onopen = () => { setStatus('live'); term.focus(); sendResize() }
    ws.onmessage = (e) => term.write(new Uint8Array(e.data as ArrayBuffer))
    ws.onclose = () => { setStatus('closed'); term.write('\r\n\x1b[90m[ session closed ]\x1b[0m\r\n') }
    ws.onerror = () => { setStatus('closed'); term.write('\r\n\x1b[31m[ connection error ]\x1b[0m\r\n') }

    const inputSub = term.onData((d) => { if (ws.readyState === WebSocket.OPEN) ws.send(enc.encode(d)) })
    const ro = new ResizeObserver(() => sendResize())
    ro.observe(el)

    return () => {
      inputSub.dispose()
      ro.disconnect()
      // Detach handlers first so this deliberate teardown (unmount / sandbox
      // switch) doesn't flash the "closed" state.
      ws.onclose = null
      ws.onerror = null
      ws.close()
      term.dispose()
    }
  }, [sandboxId, session])

  const overlayVisible = status === 'idle' || status === 'closed'

  return (
    <div style={{ position: 'relative' }}>
      <div ref={host} style={{ height: 500, width: '100%', background: '#0a0d14', padding: 8, borderRadius: 8, overflow: 'hidden' }} />
      {overlayVisible && (
        <div
          style={{
            position: 'absolute', inset: 0, display: 'flex', flexDirection: 'column',
            alignItems: 'center', justifyContent: 'center', gap: 10,
            background: status === 'idle' ? '#0a0d14' : 'rgba(10,13,20,0.85)', borderRadius: 8,
          }}
        >
          <button
            onClick={() => { setStatus('connecting'); setSession((s) => s + 1) }}
            style={{
              padding: '10px 22px', borderRadius: 8, border: '1px solid #2c374d',
              background: '#1a2233', color: '#c6d0de', fontSize: 14, cursor: 'pointer',
            }}
          >
            {status === 'idle' ? 'Open terminal' : 'Reconnect'}
          </button>
          <span style={{ color: '#5c6a80', fontSize: 12, maxWidth: 380, textAlign: 'center' }}>
            Starts an interactive shell inside the sandbox. The sandbox stays awake while the session is open.
          </span>
        </div>
      )}
    </div>
  )
}
