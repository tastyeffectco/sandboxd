import { useEffect, useState } from 'react'
import { api, Agent } from './api'
import { Dialog } from './design/Dialog'
import { Icon } from './design/Icon'
import { Btn, Input } from './design/kit'

export type ConnectMode = 'key' | 'oauth' | 'disconnect'
export function AgentConnection({
  agent,
  mode,
  close,
  connected,
}: {
  agent: Agent
  mode: ConnectMode
  close: () => void
  connected: () => void
}) {
  const [localLogin, setLocalLogin] = useState(false)
  useEffect(() => {
    let active = true
    void window.sandboxdDesktop
      ?.agentLogin?.(agent.id)
      .then((result) => {
        if (active) setLocalLogin(result.available)
      })
      .catch(() => {})
    return () => {
      active = false
    }
  }, [agent.id])
  const [value, setValue] = useState('')
  const [busy, setBusy] = useState(false)
  const [error, setError] = useState('')
  const act = async (fn: () => Promise<unknown>, done = true) => {
    if (busy) return
    setError('')
    setBusy(true)
    try {
      await fn()
      if (done) {
        setValue('')
        connected()
        close()
      }
    } catch (e) {
      setError((e as Error).message)
    } finally {
      setBusy(false)
    }
  }
  // When a login was found on this computer, pasting one is a secondary option.
  const form = (
    <form
      className="settings-form"
      onSubmit={(e) => {
        e.preventDefault()
        if (value.trim())
          void act(() =>
            mode === 'key'
              ? api.setAgentApiKey(agent.id, value.trim())
              : api.importAgentCredential(agent.id, value.trim()),
          )
      }}
    >
      <label className="form-field">
        {mode === 'key' ? `${agent.label} API key` : 'Import subscription credential'}
        <Input
          autoFocus
          required
          type="password"
          autoComplete="new-password"
          value={value}
          onChange={(e) => setValue(e.target.value)}
          placeholder={
            mode === 'key' ? 'Paste your API key' : 'Paste the credential from your provider login'
          }
        />
      </label>
      {mode === 'oauth' && (
        <p className="field-help">
          Use the credential from an existing {agent.label} login.{' '}
          <a
            href="https://github.com/tastyeffectco/sandboxd/blob/main/docs/agent-auth.md"
            target="_blank"
            rel="noreferrer"
          >
            Connection guide ↗
          </a>
        </p>
      )}
      <div className="dialog-actions">
        <Btn type="button" disabled={busy} onClick={close}>
          Cancel
        </Btn>
        <Btn type="submit" variant="primary" disabled={busy || !value.trim()}>
          {busy ? 'Connecting…' : 'Connect account'}
        </Btn>
      </div>
    </form>
  )
  return (
    <Dialog
      title={`${mode === 'disconnect' ? 'Disconnect' : 'Connect'} ${agent.label}`}
      close={() => !busy && close()}
    >
      <div className="settings-form">
        <div className="info-banner">
          <Icon name="lock" />
          <p>
            {mode === 'disconnect'
              ? 'Future tasks will no longer use this account. Existing conversations and project files are kept.'
              : 'Your credential is stored server-side and kept out of the sandbox. You can disconnect this account at any time.'}
          </p>
        </div>
        {mode === 'disconnect' ? (
          <div className="dialog-actions">
            <Btn disabled={busy} onClick={close}>
              Keep connection
            </Btn>
            <Btn
              variant="danger"
              disabled={busy}
              onClick={() => void act(() => api.disconnectAgent(agent.id))}
            >
              Disconnect account
            </Btn>
          </div>
        ) : (
          <>
            {mode === 'oauth' && localLogin && (
              <div className="oauth-step">
                <div>
                  <strong>Existing {agent.label} login found</strong>
                  <p>Use the account already signed in on this computer with your connected engine.</p>
                  <Btn
                    disabled={busy}
                    onClick={() =>
                      void act(async () => {
                        const result = await window.sandboxdDesktop?.importAgentLogin?.(agent.id)
                        if (result?.imported) {
                          connected()
                          close()
                        }
                      }, false)
                    }
                  >
                    Use existing login
                  </Btn>
                </div>
              </div>
            )}
            {mode === 'oauth' && localLogin ? (
              <details className="desktop-advanced">
                <summary>Other ways to connect</summary>
                {form}
              </details>
            ) : (
              form
            )}
          </>
        )}
        {error && (
          <p role="alert" className="inline-error">
            {error}
          </p>
        )}
      </div>
    </Dialog>
  )
}
