import { useState } from 'react'
import { api } from './api'
import { c, font, Btn, Input, H } from './design/kit'

// Full-page centered auth screens shown before the app when a session is required.
// Both are minimal single-card forms; Enter submits, errors render inline.

function Shell({ children }: { children: React.ReactNode }) {
  return (
    <div className="auth-page">
      <aside className="auth-story">
        <div className="auth-brand">◇ sandboxd</div>
        <div>
          <span className="eyebrow">YOUR IDEAS. YOUR INFRASTRUCTURE.</span>
          <h1>
            From a spark
            <br />
            to something real.
          </h1>
          <p>A place to build, experiment and make it yours. Your code, your models, your workspace.</p>
          <div className="auth-capabilities">
            <span>
              01 <b>Describe your idea</b>
            </span>
            <span>
              02 <b>Build alongside your agent</b>
            </span>
            <span>
              03 <b>Preview. Refine. Repeat.</b>
            </span>
          </div>
        </div>
        <small>Open source. Self-hosted. Yours.</small>
      </aside>
      <main className="auth-form">
        <div className="auth-form-inner">
          <div className="auth-mobile-brand">◇ sandboxd</div>
          {children}
        </div>
      </main>
    </div>
  )
}

export function Login({ onDone }: { onDone: () => void }) {
  const [pw, setPw] = useState('')
  const [err, setErr] = useState('')
  const [busy, setBusy] = useState(false)
  const [forgot, setForgot] = useState(false)
  const submit = () => {
    if (busy) return
    setErr('')
    setBusy(true)
    api
      .login(pw)
      .then(onDone)
      .catch((e) => {
        setErr((e as Error).message)
        setBusy(false)
      })
  }
  return (
    <Shell>
      <H size={18} style={{ marginBottom: 4 }}>
        Sign in
      </H>
      <div style={{ color: c.muted, fontSize: 12.5, marginBottom: 16 }}>
        Enter the console password to continue.
      </div>
      <Input
        type="password"
        aria-label="Password"
        autoComplete="current-password"
        autoFocus
        value={pw}
        onChange={(e) => setPw(e.target.value)}
        onKeyDown={(e) => e.key === 'Enter' && submit()}
        placeholder="Password"
        style={{ width: '100%', boxSizing: 'border-box', marginBottom: 12 }}
        data-testid="login-password"
      />
      {err && (
        <div style={{ color: c.bad, fontSize: 12, marginBottom: 12 }} data-testid="login-error">
          {err}
        </div>
      )}
      <Btn
        variant="primary"
        disabled={busy}
        onClick={submit}
        style={{ width: '100%', padding: '9px 14px' }}
        data-testid="login-submit"
      >
        Sign in
      </Btn>
      <div style={{ marginTop: 14, textAlign: 'center' }}>
        <a
          href="#"
          onClick={(e) => {
            e.preventDefault()
            setForgot((v) => !v)
          }}
          style={{ color: c.muted, fontSize: 12, textDecoration: 'underline' }}
          data-testid="login-forgot"
        >
          Forgot your password?
        </a>
      </div>
      {forgot && (
        <div
          style={{
            marginTop: 12,
            padding: 12,
            background: c.panel2,
            border: `1px solid ${c.border}`,
            borderRadius: 8,
            fontSize: 12,
            color: c.fg2,
          }}
          data-testid="login-forgot-panel"
        >
          <div style={{ marginBottom: 8 }}>Reset it from the server that runs sandboxd:</div>
          <pre
            style={{
              margin: 0,
              padding: '8px 10px',
              background: c.ink,
              color: c.bg,
              borderRadius: 6,
              fontFamily: font.mono,
              fontSize: 11.5,
              overflowX: 'auto',
            }}
          >
            cd /opt/sandboxd && ./console-login.sh --reset-password
          </pre>
          <div style={{ color: c.muted2, fontSize: 11, marginTop: 4 }}>
            use your checkout directory if it differs
          </div>
          <div style={{ marginTop: 8 }}>Then reload this page and create a new password.</div>
        </div>
      )}
    </Shell>
  )
}

export function CreatePassword({ onDone }: { onDone: () => void }) {
  const [pw, setPw] = useState('')
  const [confirm, setConfirm] = useState('')
  const [err, setErr] = useState('')
  const [busy, setBusy] = useState(false)
  const submit = () => {
    if (busy) return
    setErr('')
    if (pw.length < 8) {
      setErr('Password must be at least 8 characters.')
      return
    }
    if (pw !== confirm) {
      setErr('Passwords do not match.')
      return
    }
    setBusy(true)
    api
      .setupPassword(pw)
      .then(onDone)
      .catch((e) => {
        setErr((e as Error).message)
        setBusy(false)
      })
  }
  return (
    <Shell>
      <H size={18} style={{ marginBottom: 4 }}>
        Welcome to sandboxd
      </H>
      <div style={{ color: c.muted, fontSize: 12.5, marginBottom: 16 }}>
        Create a console password. You'll use it to sign in from now on.
      </div>
      <Input
        type="password"
        autoFocus
        value={pw}
        onChange={(e) => setPw(e.target.value)}
        onKeyDown={(e) => e.key === 'Enter' && submit()}
        placeholder="New password (min 8 characters)"
        style={{ width: '100%', boxSizing: 'border-box', marginBottom: 10 }}
        data-testid="setup-password"
      />
      <Input
        type="password"
        value={confirm}
        onChange={(e) => setConfirm(e.target.value)}
        onKeyDown={(e) => e.key === 'Enter' && submit()}
        placeholder="Confirm password"
        style={{ width: '100%', boxSizing: 'border-box', marginBottom: 12 }}
        data-testid="setup-confirm"
      />
      {err && (
        <div style={{ color: c.bad, fontSize: 12, marginBottom: 12 }} data-testid="setup-error">
          {err}
        </div>
      )}
      <Btn
        variant="primary"
        disabled={busy}
        onClick={submit}
        style={{ width: '100%', padding: '9px 14px' }}
        data-testid="setup-submit"
      >
        Create password
      </Btn>
    </Shell>
  )
}
