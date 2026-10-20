import { useState } from 'react'
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
  const [value, setValue] = useState('')
  const [url, setURL] = useState('')
  const [busy, setBusy] = useState(false)
  const [error, setError] = useState('')
  const guided = mode === 'oauth' && agent.id === 'claude-code'
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
            {guided && (
              <div className="oauth-step">
                <span className="step-number">1</span>
                <div>
                  <strong>Authorize your Claude subscription</strong>
                  <p>Sign in with your provider, approve access, then return with the authorization code.</p>
                  {url ? (
                    <a className="button-link" href={url} target="_blank" rel="noreferrer">
                      Open authorization page <Icon name="external" size={14} />
                    </a>
                  ) : (
                    <Btn
                      disabled={busy}
                      onClick={() =>
                        void act(async () => {
                          const result = await api.oauthStart(agent.id)
                          setURL(result.authorize_url)
                        }, false)
                      }
                    >
                      {busy ? 'Preparing…' : 'Start authorization'}
                    </Btn>
                  )}
                </div>
              </div>
            )}
            {(!guided || url) && (
              <form
                className="settings-form"
                onSubmit={(e) => {
                  e.preventDefault()
                  if (value.trim())
                    void act(() =>
                      guided
                        ? api.oauthFinish(agent.id, value.trim())
                        : mode === 'key'
                          ? api.setAgentApiKey(agent.id, value.trim())
                          : api.importAgentCredential(agent.id, value.trim()),
                    )
                }}
              >
                <label className="form-field">
                  {guided
                    ? '2. Paste the authorization code'
                    : mode === 'key'
                      ? `${agent.label} API key`
                      : 'Import subscription credential'}
                  <Input
                    autoFocus
                    required
                    type="password"
                    autoComplete="new-password"
                    value={value}
                    onChange={(e) => setValue(e.target.value)}
                    placeholder={
                      guided
                        ? 'Code from the authorization page'
                        : mode === 'key'
                          ? 'Paste your API key'
                          : 'Paste the credential from your provider login'
                    }
                  />
                </label>
                {mode === 'oauth' && !guided && (
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
