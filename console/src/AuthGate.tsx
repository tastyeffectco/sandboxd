import { useState } from 'react'
import { api } from './api'
import { c, font, Card, Btn, Input, H } from './design/kit'

// Full-page centered auth screens shown before the app when a session is required.
// Both are minimal single-card forms; Enter submits, errors render inline.

function Shell({ children }: { children: React.ReactNode }) {
  return (
    <div style={{ display: 'flex', alignItems: 'center', justifyContent: 'center', height: '100vh', background: c.bg, color: c.fg, fontFamily: font.sans, padding: 20 }}>
      <Card style={{ width: 360, maxWidth: '100%', padding: 28 }}>
        <div style={{ display: 'flex', alignItems: 'center', gap: 9, marginBottom: 18 }}>
          <div style={{ width: 26, height: 26, borderRadius: 7, background: 'linear-gradient(135deg,#3f3f46,#18181b)', display: 'flex', alignItems: 'center', justifyContent: 'center', fontFamily: font.mono, fontSize: 11, color: c.bg }}>&gt;_</div>
          <span style={{ fontFamily: font.display, fontWeight: 700, fontSize: 15, letterSpacing: '.2px' }}>sandboxd <span style={{ fontWeight: 500, color: c.muted }}>console</span></span>
        </div>
        {children}
      </Card>
    </div>
  )
}

export function Login({ onDone }: { onDone: () => void }) {
  const [pw, setPw] = useState('')
  const [err, setErr] = useState('')
  const [busy, setBusy] = useState(false)
  const submit = () => {
    if (busy) return
    setErr('')
    setBusy(true)
    api.login(pw).then(onDone).catch((e) => { setErr((e as Error).message); setBusy(false) })
  }
  return (
    <Shell>
      <H size={18} style={{ marginBottom: 4 }}>Sign in</H>
      <div style={{ color: c.muted, fontSize: 12.5, marginBottom: 16 }}>Enter the console password to continue.</div>
      <Input
        type="password"
        autoFocus
        value={pw}
        onChange={(e) => setPw(e.target.value)}
        onKeyDown={(e) => e.key === 'Enter' && submit()}
        placeholder="Password"
        style={{ width: '100%', boxSizing: 'border-box', marginBottom: 12 }}
        data-testid="login-password"
      />
      {err && <div style={{ color: c.bad, fontSize: 12, marginBottom: 12 }} data-testid="login-error">{err}</div>}
      <Btn variant="primary" disabled={busy} onClick={submit} style={{ width: '100%', padding: '9px 14px' }} data-testid="login-submit">Sign in</Btn>
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
    if (pw.length < 8) { setErr('Password must be at least 8 characters.'); return }
    if (pw !== confirm) { setErr('Passwords do not match.'); return }
    setBusy(true)
    api.setupPassword(pw).then(onDone).catch((e) => { setErr((e as Error).message); setBusy(false) })
  }
  return (
    <Shell>
      <H size={18} style={{ marginBottom: 4 }}>Welcome to sandboxd</H>
      <div style={{ color: c.muted, fontSize: 12.5, marginBottom: 16 }}>Create a console password. You'll use it to sign in from now on.</div>
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
      {err && <div style={{ color: c.bad, fontSize: 12, marginBottom: 12 }} data-testid="setup-error">{err}</div>}
      <Btn variant="primary" disabled={busy} onClick={submit} style={{ width: '100%', padding: '9px 14px' }} data-testid="setup-submit">Create password</Btn>
    </Shell>
  )
}
