import { useEffect, useRef, useState } from 'react'
import type { AssistantApproval, DesktopAssistant, DesktopServer } from './desktop'
import { Btn, Input } from './design/kit'
import { Dialog } from './design/Dialog'
import { Icon } from './design/Icon'

type Line =
  | { kind: 'you'; text: string }
  | { kind: 'assistant'; text: string }
  | { kind: 'tool'; text: string }
  | { kind: 'error'; text: string }

const TOOL_WORDS: Record<string, string> = {
  look: 'Looked at your server',
  read: 'Read a file',
  run: 'Ran a command',
  write: 'Changed a file',
  undo: 'Put a file back',
}

const SUGGESTIONS = [
  'Get this server ready to put my apps online',
  'Check that everything on this server is working',
  'I want to use my own domain name',
]

const clean = (message: string) => message.replace(/^Error invoking remote method '[^']+': (Error: )?/, '')

// The server assistant: a chat about one server. It can look freely, but every
// change waits for the user's "Allow" below.
export function ServerAssistant({
  assistant,
  server,
  close,
}: {
  assistant: DesktopAssistant
  server: DesktopServer
  close: () => void
}) {
  const [lines, setLines] = useState<Line[]>([])
  const [draft, setDraft] = useState('')
  const [busy, setBusy] = useState(false)
  const [status, setStatus] = useState('')
  const [setup, setSetup] = useState<number | null>(null)
  const [approvals, setApprovals] = useState<AssistantApproval[]>([])
  const end = useRef<HTMLDivElement>(null)

  useEffect(() => {
    // Approvals already waiting when this opens; merged, so one that arrives
    // meanwhile is kept.
    void assistant.pending().then((all) =>
      setApprovals((current) => [
        ...all.filter((a) => a.serverId === server.id && !current.some((c) => c.id === a.id)),
        ...current,
      ]),
    )
    const stopEvents = assistant.onEvent((event) => {
      if (event.serverId !== server.id) return
      if (event.type === 'status') return setStatus(event.text)
      if (event.type === 'text') return setLines((l) => [...l, { kind: 'assistant', text: event.text }])
      if (event.type === 'error') return setLines((l) => [...l, { kind: 'error', text: event.text }])
      if (event.type === 'tool' && event.status === 'completed')
        setLines((l) => [...l, { kind: 'tool', text: TOOL_WORDS[event.tool] || 'Used a tool' }])
    })
    const stopTools = assistant.onTools((event) => {
      if (event.type === 'approval' && event.approval.serverId === server.id)
        setApprovals((a) => [...a, event.approval])
      if (event.type === 'decided') setApprovals((a) => a.filter((x) => x.id !== event.id))
    })
    const stopSetup = assistant.onSetup(({ received, total }) =>
      setSetup(total ? Math.round((received / total) * 100) : null),
    )
    return () => {
      stopEvents()
      stopTools()
      stopSetup()
    }
  }, [assistant, server.id])

  useEffect(() => end.current?.scrollIntoView?.({ block: 'end' }), [lines, approvals])

  const send = async (text: string) => {
    const message = text.trim()
    if (!message || busy) return
    setDraft('')
    setBusy(true)
    setStatus('')
    setLines((l) => [...l, { kind: 'you', text: message }])
    try {
      const result = await assistant.send(server.id, message)
      // Plain errors already arrived as events; others (busy, invalid) are shown here.
      if (!result.ok && result.code) setLines((l) => [...l, { kind: 'error', text: result.message || 'Try again.' }])
    } catch (e) {
      setLines((l) => [...l, { kind: 'error', text: clean((e as Error).message) }])
    } finally {
      setBusy(false)
      setStatus('')
      setSetup(null)
    }
  }

  return (
    <Dialog title={`Assistant · ${server.name}`} close={close}>
      <div data-testid="assistant" style={{ display: 'grid', gap: 12, minWidth: 'min(560px, 80vw)' }}>
        <p className="field-help">
          The assistant can look at your server. Before it changes anything, it asks you here, and nothing
          happens until you click Allow.
        </p>
        <div
          role="log"
          aria-live="polite"
          data-testid="assistant-log"
          style={{ display: 'grid', gap: 8, maxHeight: '45vh', overflow: 'auto' }}
        >
          {lines.length === 0 && !busy && (
            <div style={{ display: 'flex', flexWrap: 'wrap', gap: 8 }}>
              {SUGGESTIONS.map((s) => (
                <Btn sm key={s} onClick={() => void send(s)} data-testid="assistant-suggestion">
                  {s}
                </Btn>
              ))}
            </div>
          )}
          {lines.map((line, i) =>
            line.kind === 'tool' ? (
              <small key={i} className="field-help" style={{ display: 'flex', gap: 6, alignItems: 'center' }}>
                <Icon name="check" size={12} /> {line.text}
              </small>
            ) : (
              <p
                key={i}
                role={line.kind === 'error' ? 'alert' : undefined}
                className={line.kind === 'error' ? 'inline-error' : undefined}
                data-testid={`assistant-${line.kind}`}
                style={{
                  margin: 0,
                  whiteSpace: 'pre-wrap',
                  justifySelf: line.kind === 'you' ? 'end' : 'start',
                  maxWidth: '85%',
                  padding: line.kind === 'error' ? undefined : '8px 12px',
                  borderRadius: 12,
                  background: line.kind === 'you' ? 'var(--accent-soft, rgba(99,102,241,.12))' : 'var(--surface-2, rgba(127,127,127,.08))',
                }}
              >
                {line.text}
              </p>
            ),
          )}
          {approvals.map((approval) => (
            <ApprovalCard key={approval.id} approval={approval} assistant={assistant} />
          ))}
          {busy && approvals.length === 0 && (
            <p role="status" className="field-help" data-testid="assistant-status">
              {setup !== null ? `Getting the assistant ready, first time only (${setup}%)…` : status || 'Thinking…'}
            </p>
          )}
          <div ref={end} />
        </div>
        <form
          onSubmit={(e) => {
            e.preventDefault()
            void send(draft)
          }}
          style={{ display: 'flex', gap: 8 }}
        >
          <Input
            value={draft}
            onChange={(e) => setDraft(e.target.value)}
            placeholder="Ask about your server…"
            maxLength={8000}
            disabled={busy}
            data-testid="assistant-input"
            style={{ flex: 1 }}
          />
          {busy ? (
            <Btn onClick={() => void assistant.stop(server.id)} data-testid="assistant-stop">
              Stop
            </Btn>
          ) : (
            <Btn type="submit" variant="primary" disabled={!draft.trim()} data-testid="assistant-send">
              Send
            </Btn>
          )}
        </form>
        {lines.length > 0 && !busy && (
          <div>
            <Btn
              sm
              onClick={async () => {
                await assistant.reset(server.id)
                setLines([])
              }}
              data-testid="assistant-reset"
            >
              New conversation
            </Btn>
          </div>
        )}
      </div>
    </Dialog>
  )
}

function ApprovalCard({ approval, assistant }: { approval: AssistantApproval; assistant: DesktopAssistant }) {
  const [password, setPassword] = useState('')
  const [sending, setSending] = useState(false)
  const decide = async (allow: boolean) => {
    setSending(true)
    await assistant.decide(approval.id, allow && password ? { allow, password } : { allow })
    setPassword('')
  }
  const what =
    approval.kind === 'read'
      ? `Read ${approval.path}`
      : approval.kind === 'write'
        ? `${approval.before === null ? 'Create' : 'Change'} ${approval.path}`
        : approval.kind === 'undo'
          ? 'Put back an earlier version of a file'
          : 'Run a command on your server'
  return (
    <div
      role="group"
      aria-label="The assistant asks for permission"
      data-testid="assistant-approval"
      className="agent-account"
      style={{ display: 'grid', gap: 8, borderWidth: 2 }}
    >
      <strong>{approval.title}</strong>
      <small className="field-help">
        {what}
        {approval.admin ? ', with admin rights' : ''}.
      </small>
      {(approval.command || approval.after !== undefined) && (
        <details>
          <summary>Show exactly what will happen</summary>
          {approval.command && (
            <pre data-testid="assistant-approval-command" style={{ whiteSpace: 'pre-wrap', fontSize: 12 }}>
              {approval.command}
            </pre>
          )}
          {approval.after !== undefined && (
            <div style={{ display: 'grid', gap: 4 }}>
              {approval.before !== null && approval.before !== undefined && (
                <>
                  <small>Now</small>
                  <pre style={{ whiteSpace: 'pre-wrap', fontSize: 12, maxHeight: 160, overflow: 'auto' }}>
                    {approval.before}
                  </pre>
                </>
              )}
              <small>{approval.before === null ? 'New file' : 'After'}</small>
              <pre style={{ whiteSpace: 'pre-wrap', fontSize: 12, maxHeight: 160, overflow: 'auto' }}>
                {approval.after}
              </pre>
            </div>
          )}
        </details>
      )}
      {approval.needsPassword && (
        <label className="form-field">
          Your server password (used for this change only, never saved)
          <Input
            type="password"
            autoComplete="off"
            value={password}
            onChange={(e) => setPassword(e.target.value)}
            data-testid="assistant-approval-password"
          />
        </label>
      )}
      <div style={{ display: 'flex', gap: 8, justifyContent: 'flex-end' }}>
        <Btn disabled={sending} onClick={() => void decide(false)} data-testid="assistant-deny">
          Don’t allow
        </Btn>
        <Btn
          variant="primary"
          disabled={sending || (approval.needsPassword && !password)}
          onClick={() => void decide(true)}
          data-testid="assistant-allow"
        >
          Allow
        </Btn>
      </div>
    </div>
  )
}
