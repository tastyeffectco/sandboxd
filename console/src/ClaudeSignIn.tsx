import { useCallback, useEffect, useState } from 'react'
import { api } from './api'
import { Btn, Input } from './design/kit'
import { Icon } from './design/Icon'

// A Claude subscription signs in to Claude Code inside this project, on
// Claude's own website. The login stays in the project; sandboxd never sees it.
export function ClaudeSignIn({
  sandboxId,
  onSignedIn,
}: {
  sandboxId: string
  onSignedIn: (signedIn: boolean) => void
}) {
  const [signedIn, setSignedIn] = useState<boolean | null>(null)
  const [url, setURL] = useState('')
  const [code, setCode] = useState('')
  const [busy, setBusy] = useState(false)
  const [error, setError] = useState('')
  const report = useCallback(
    (value: boolean) => {
      setSignedIn(value)
      onSignedIn(value)
    },
    [onSignedIn],
  )
  useEffect(() => {
    let active = true
    setSignedIn(null)
    api
      .claudeSignInStatus(sandboxId)
      .then((s) => active && report(s.logged_in))
      .catch(() => active && report(false))
    return () => {
      active = false
    }
  }, [sandboxId, report])
  const act = async (fn: () => Promise<void>) => {
    if (busy) return
    setBusy(true)
    setError('')
    try {
      await fn()
    } catch (e) {
      setError((e as Error).message)
    } finally {
      setBusy(false)
    }
  }
  if (signedIn === null) return null
  if (signedIn)
    return (
      <div className="chat-notice" data-testid="claude-signed-in">
        <Icon name="check" size={14} /> Signed in to Claude in this project.{' '}
        <button
          className="text-button"
          disabled={busy}
          onClick={() =>
            void act(async () => {
              await api.signOutClaude(sandboxId)
              setURL('')
              report(false)
            })
          }
        >
          Sign out
        </button>
      </div>
    )
  return (
    <div className="chat-notice claude-sign-in" data-testid="claude-sign-in">
      <strong>Use your Claude subscription in this project</strong>
      <p>Sign in on Claude’s website. Your login stays in this project.</p>
      {!url ? (
        <Btn
          variant="primary"
          disabled={busy}
          data-testid="claude-sign-in-start"
          onClick={() =>
            void act(async () => {
              setURL((await api.startClaudeSignIn(sandboxId)).url)
            })
          }
        >
          {busy ? 'Opening…' : 'Sign in with Claude'}
        </Btn>
      ) : (
        <form
          onSubmit={(e) => {
            e.preventDefault()
            if (code.trim())
              void act(async () => {
                const s = await api.finishClaudeSignIn(sandboxId, code.trim())
                setCode('')
                report(s.logged_in)
              })
          }}
        >
          <a
            className="button-link"
            href={url}
            target="_blank"
            rel="noreferrer"
            data-testid="claude-sign-in-link"
          >
            1. Open Claude sign-in <Icon name="external" size={14} />
          </a>
          <label className="form-field">
            2. Paste the code Claude shows you
            <Input
              value={code}
              onChange={(e) => setCode(e.target.value)}
              placeholder="Code from Claude"
              autoComplete="off"
              disabled={busy}
              data-testid="claude-sign-in-code"
            />
          </label>
          <Btn
            type="submit"
            variant="primary"
            disabled={busy || !code.trim()}
            data-testid="claude-sign-in-finish"
          >
            {busy ? 'Signing in…' : 'Finish sign-in'}
          </Btn>
        </form>
      )}
      {error && (
        <p role="alert" className="inline-error">
          {error}
        </p>
      )}
    </div>
  )
}
