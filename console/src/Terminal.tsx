import { useEffect, useRef } from 'react'
import { Terminal as XTerm } from '@xterm/xterm'
import { FitAddon } from '@xterm/addon-fit'
import '@xterm/xterm/css/xterm.css'

// SandboxTerminal — an interactive shell inside the sandbox (uid 1000, the app
// workspace), streamed over a WebSocket to the /v1/sandboxes/{id}/terminal
// endpoint. Protocol: BINARY frames carry keystrokes (client→server) and shell
// output (server→client); a TEXT frame `{"resize":{cols,rows}}` reports size.
export function SandboxTerminal({ sandboxId }: { sandboxId: string }) {
  const host = useRef<HTMLDivElement>(null)

  useEffect(() => {
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

    ws.onopen = () => { term.focus(); sendResize() }
    ws.onmessage = (e) => term.write(new Uint8Array(e.data as ArrayBuffer))
    ws.onclose = () => term.write('\r\n\x1b[90m[ session closed ]\x1b[0m\r\n')
    ws.onerror = () => term.write('\r\n\x1b[31m[ connection error ]\x1b[0m\r\n')

    const inputSub = term.onData((d) => { if (ws.readyState === WebSocket.OPEN) ws.send(enc.encode(d)) })
    const ro = new ResizeObserver(() => sendResize())
    ro.observe(el)

    return () => {
      inputSub.dispose()
      ro.disconnect()
      ws.close()
      term.dispose()
    }
  }, [sandboxId])

  return <div ref={host} style={{ height: 500, width: '100%', background: '#0a0d14', padding: 8, borderRadius: 8, overflow: 'hidden' }} />
}
