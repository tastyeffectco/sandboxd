import { useCallback, useEffect, useMemo, useRef, useState } from 'react'
import { api, App as TApp, Preset, GitCredential } from './api'
import { c, font, mono, Card, Btn, StatusPill, Input, navItem } from './design/kit'
import { AppView } from './AppView'
import { StoreView } from './StoreView'
import { SettingsView } from './SettingsView'

type Route = { name: 'apps' } | { name: 'store' } | { name: 'settings' } | { name: 'app'; id: string; tab?: string; task?: string }

export default function App() {
  const [route, setRoute] = useState<Route>({ name: 'apps' })
  const [toasts, setToasts] = useState<{ id: number; msg: string }[]>([])
  const [paletteOpen, setPaletteOpen] = useState(false)
  const [deployOpen, setDeployOpen] = useState(false)
  const [apps, setApps] = useState<TApp[]>([])

  const toast = useCallback((msg: string) => {
    const id = Date.now() + Math.floor(performance.now())
    setToasts((t) => [...t, { id, msg }])
    setTimeout(() => setToasts((t) => t.filter((x) => x.id !== id)), 3200)
  }, [])
  const onError = useCallback((m: string) => toast(m), [toast])

  const loadApps = useCallback(() => api.listApps().then(setApps).catch(() => {}), [])
  useEffect(() => { loadApps() }, [loadApps])

  useEffect(() => {
    const h = (e: KeyboardEvent) => {
      if ((e.metaKey || e.ctrlKey) && e.key.toLowerCase() === 'k') { e.preventDefault(); setPaletteOpen((o) => !o) }
      else if (e.key === 'Escape') setPaletteOpen(false)
    }
    window.addEventListener('keydown', h)
    return () => window.removeEventListener('keydown', h)
  }, [])

  const goApp = (id: string, tab?: string) => setRoute({ name: 'app', id, tab })
  const running = apps.find((a) => a.current_sandbox_id)

  const nav = [
    { key: 'apps', label: 'Apps' },
    { key: 'store', label: 'App Store' },
    { key: 'settings', label: 'Settings' },
  ]

  return (
    <div style={{ display: 'flex', flexDirection: 'column', height: '100vh', background: c.bg, color: c.fg, fontFamily: font.sans, overflow: 'hidden' }}>
      {/* TOP BAR */}
      <div style={{ display: 'flex', alignItems: 'center', gap: 18, height: 52, flexShrink: 0, padding: '0 20px', borderBottom: `1px solid ${c.border}`, background: c.panel }}>
        <div onClick={() => setRoute({ name: 'apps' })} style={{ display: 'flex', alignItems: 'center', gap: 9, cursor: 'pointer' }}>
          <div style={{ width: 26, height: 26, borderRadius: 7, background: 'linear-gradient(135deg,#3f3f46,#18181b)', display: 'flex', alignItems: 'center', justifyContent: 'center', fontFamily: font.mono, fontSize: 11, color: c.bg }}>&gt;_</div>
          <span style={{ fontFamily: font.display, fontWeight: 700, fontSize: 15, letterSpacing: '.2px' }}>sandboxd</span>
        </div>
        <div style={{ display: 'flex', gap: 2 }}>
          {nav.map((n) => (
            <div key={n.key} data-testid={`nav-${n.key}`} className="dc-hoverink" onClick={() => setRoute({ name: n.key } as Route)} style={navItem(route.name === n.key)}>
              {n.label}
            </div>
          ))}
        </div>
        <div style={{ flex: 1 }} />
        {running && (
          <div onClick={() => goApp(running.id)} className="dc-hoverborder" style={{ display: 'flex', alignItems: 'center', gap: 7, border: `1px solid ${c.border}`, background: c.bg, borderRadius: 7, padding: '4px 10px', cursor: 'pointer' }}>
            <span style={{ width: 6, height: 6, borderRadius: '50%', background: c.good }} />
            <span style={{ ...mono, fontSize: 11.5 }}>{running.name}</span>
          </div>
        )}
        <div onClick={() => setPaletteOpen(true)} className="dc-hoverborder" style={{ display: 'flex', alignItems: 'center', gap: 8, border: `1px solid ${c.border}`, background: c.bg, borderRadius: 7, padding: '5px 10px', cursor: 'pointer', width: 180 }}>
          <span style={{ color: c.muted2, fontSize: 12, flex: 1 }}>Search…</span>
          <span style={{ ...mono, fontSize: 10, color: c.muted2, background: c.panel2, border: `1px solid ${c.border}`, borderRadius: 4, padding: '1px 5px' }}>⌘K</span>
        </div>
        <button onClick={() => setDeployOpen(true)} data-testid="deploy-btn" className="dc-hoverborder" style={{ display: 'flex', alignItems: 'center', gap: 7, border: 'none', borderRadius: 7, padding: '6px 13px', cursor: 'pointer', background: 'linear-gradient(135deg,#3f3f46,#18181b)', color: '#fff', fontFamily: font.sans, fontSize: 12.5, fontWeight: 600 }}>
          <svg width="13" height="13" viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="2.2" strokeLinecap="round" strokeLinejoin="round"><path d="M12 2 4 6v6c0 5 3.4 8.5 8 10 4.6-1.5 8-5 8-10V6Z" opacity=".35"/><path d="m9 12 2 2 4-4"/></svg>
          Deploy
        </button>
        <div style={{ display: 'flex', gap: 12 }}>
          <a href="https://github.com/tastyeffectco/sandboxd" target="_blank" rel="noreferrer" className="dc-hoverink" style={{ color: c.muted, textDecoration: 'none', fontSize: 12 }}>Docs</a>
          <a href="https://github.com/tastyeffectco/sandboxd" target="_blank" rel="noreferrer" className="dc-hoverink" style={{ color: c.muted, textDecoration: 'none', fontSize: 12 }}>GitHub</a>
        </div>
      </div>

      {/* MAIN */}
      <div style={{ flex: 1, overflowY: 'auto', overflowX: 'hidden' }}>
        {route.name === 'apps' && <AppsScreen apps={apps} reload={loadApps} onOpen={(id) => goApp(id)} onError={onError} goStore={() => setRoute({ name: 'store' })} />}
        {route.name === 'store' && <StoreView onError={onError} toast={toast} onOpen={(id) => goApp(id)} reloadApps={loadApps} />}
        {route.name === 'settings' && <SettingsView onError={onError} toast={toast} />}
        {route.name === 'app' && (
          <AppView
            appId={route.id}
            initialTab={route.tab}
            onError={onError}
            toast={toast}
            goApps={() => { setRoute({ name: 'apps' }); loadApps() }}
            goSettings={() => setRoute({ name: 'settings' })}
          />
        )}
      </div>

      {paletteOpen && <Palette apps={apps} close={() => setPaletteOpen(false)} onGo={(r) => { setRoute(r); setPaletteOpen(false) }} />}
      {deployOpen && <DeployModal close={() => setDeployOpen(false)} />}
      <Helper />

      {/* TOASTS */}
      {toasts.length > 0 && (
        <div style={{ position: 'fixed', bottom: 80, right: 20, zIndex: 99, display: 'flex', flexDirection: 'column', gap: 8 }}>
          {toasts.map((t) => (
            <div key={t.id} style={{ background: c.ink, color: '#fff', borderRadius: 8, padding: '10px 16px', fontSize: 12.5, boxShadow: '0 8px 24px rgba(0,0,0,.18)', display: 'flex', alignItems: 'center', gap: 8 }}>
              <span style={{ color: '#4ade80' }}>✓</span>{t.msg}
            </div>
          ))}
        </div>
      )}
    </div>
  )
}

function AppsScreen({ apps, reload, onOpen, onError, goStore }: { apps: TApp[]; reload: () => void; onOpen: (id: string) => void; onError: (m: string) => void; goStore: () => void }) {
  const [name, setName] = useState('')
  const [mode, setMode] = useState<'template' | 'git'>('template')
  const [preset, setPreset] = useState('')
  const [presets, setPresets] = useState<Preset[]>([])
  const [repo, setRepo] = useState('')
  const [branch, setBranch] = useState('main')
  const [credId, setCredId] = useState('')
  const [creds, setCreds] = useState<GitCredential[]>([])
  const [busy, setBusy] = useState(false)
  const [sbStatus, setSbStatus] = useState<Record<string, string>>({})

  useEffect(() => {
    api.listPresets().then(setPresets).catch(() => {})
    api.listGitCredentials().then(setCreds).catch(() => {})
  }, [])
  useEffect(() => {
    Promise.all(apps.filter((a) => a.current_sandbox_id).map(async (a) => {
      try { const s = await api.getSandbox(a.current_sandbox_id as string); return [a.id, s.status] as const } catch { return [a.id, 'unknown'] as const }
    })).then((p) => setSbStatus(Object.fromEntries(p)))
  }, [apps])

  const create = async () => {
    if (!name.trim()) return
    if (mode === 'git' && !repo.trim()) { onError('Enter a repo URL'); return }
    if (mode === 'git' && !credId) { onError('Pick a Git credential (add one in Settings → Git credentials)'); return }
    setBusy(true)
    try {
      const a = await api.createApp({
        name: name.trim(),
        runtime_preset: mode === 'template' && preset ? preset : undefined,
        git: mode === 'git' ? { repo_url: repo.trim(), branch: branch.trim() || 'main', credential_id: credId } : undefined,
      })
      setName(''); setRepo(''); reload(); onOpen(a.id)
    } catch (e) { onError((e as Error).message) } finally { setBusy(false) }
  }

  const modeChip = (m: 'template' | 'git', label: string) => (
    <div onClick={() => setMode(m)} className="dc-hoverborder" style={{ padding: '6px 12px', fontSize: 12.5, borderRadius: 7, cursor: 'pointer', border: `1px solid ${mode === m ? c.faint : c.border}`, color: mode === m ? c.fg : c.muted, background: mode === m ? c.panel2 : 'transparent' }}>{label}</div>
  )

  return (
    <div style={{ maxWidth: 920, margin: '0 auto', padding: '36px 40px 80px' }}>
      <h1 style={{ fontFamily: font.display, fontSize: 24, fontWeight: 700, margin: '0 0 4px' }}>Apps</h1>
      <p style={{ color: c.muted, margin: '0 0 24px' }}>Each app runs isolated in its own sandbox with a live preview URL.</p>

      <Card style={{ padding: 16, marginBottom: 28 }}>
        <div style={{ display: 'flex', gap: 10, marginBottom: 12 }}>
          <Input mono value={name} onChange={(e) => setName(e.target.value)} onKeyDown={(e) => e.key === 'Enter' && create()} placeholder="new-app-name" style={{ flex: 1, fontSize: 13 }} data-testid="app-name" />
          <Btn variant="primary" disabled={busy || !name.trim()} onClick={create} style={{ padding: '9px 18px', fontSize: 13 }}>Create app</Btn>
        </div>
        <div style={{ display: 'flex', alignItems: 'center', gap: 8 }}>
          {modeChip('template', 'Start from a template')}
          {modeChip('git', 'Import from Git')}
          {mode === 'template' ? (
            <select value={preset} onChange={(e) => setPreset(e.target.value)} data-testid="app-preset" style={{ background: c.bg, border: `1px solid ${c.border2}`, borderRadius: 7, padding: '8px 10px', color: c.fg, fontSize: 12.5, fontFamily: font.sans }}>
              <option value="">Template…</option>
              {presets.map((p) => <option key={p.id} value={p.id}>{p.label}</option>)}
            </select>
          ) : (
            <>
              <Input mono value={repo} onChange={(e) => setRepo(e.target.value)} placeholder="https://github.com/user/repo.git" style={{ flex: 1, fontSize: 12.5 }} data-testid="git-repo-url" />
              <Input mono value={branch} onChange={(e) => setBranch(e.target.value)} placeholder="branch" style={{ width: 120, fontSize: 12.5 }} data-testid="git-branch" />
              <select value={credId} onChange={(e) => setCredId(e.target.value)} data-testid="git-cred" style={{ background: c.bg, border: `1px solid ${c.border2}`, borderRadius: 7, padding: '8px 10px', color: c.fg, fontSize: 12.5, fontFamily: font.sans }}>
                <option value="">Credential…</option>
                {creds.map((g) => <option key={g.id} value={g.id}>{g.name}</option>)}
              </select>
            </>
          )}
          <span style={{ color: c.muted2, fontSize: 12, marginLeft: 'auto' }}>Want a ready-made app? Browse the <a onClick={goStore} style={{ color: c.link, cursor: 'pointer', textDecoration: 'none' }}>App Store</a>.</span>
        </div>
        {mode === 'git' && creds.length === 0 && (
          <div style={{ marginTop: 8, fontSize: 12, color: c.warn }} data-testid="git-no-creds">No Git credentials yet — add a personal access token in <b>Settings → Git credentials</b> first. Cloning runs control-plane-side, so a credential is required even for public repos.</div>
        )}
      </Card>

      {apps.length === 0 ? (
        <p style={{ color: c.muted2 }}>No apps yet — create one above to get started.</p>
      ) : (
        <div style={{ display: 'grid', gridTemplateColumns: 'repeat(auto-fill,minmax(270px,1fr))', gap: 14 }} data-testid="app-list">
          {apps.map((a) => (
            <Card key={a.id} style={{ padding: 16, cursor: 'pointer' }} >
              <div className="dc-hoverborder" onClick={() => onOpen(a.id)} data-testid="app-card">
                <div style={{ display: 'flex', alignItems: 'center', gap: 8, marginBottom: 4 }}>
                  <span style={{ ...mono, fontWeight: 500, fontSize: 14 }}>{a.name}</span>
                  {a.current_sandbox_id ? <StatusPill status={sbStatus[a.id]} /> : <StatusPill status={undefined} />}
                </div>
                <div style={{ color: c.muted, fontSize: 12.5, marginBottom: 12 }}>{a.description || a.id}</div>
                <div style={{ display: 'flex', gap: 6, alignItems: 'center' }}>
                  {(a.tags || []).slice(0, 3).map((t) => (
                    <span key={t} style={{ ...mono, fontSize: 10.5, color: c.muted, background: c.panel2, border: `1px solid ${c.border}`, borderRadius: 5, padding: '2px 7px' }}>{t}</span>
                  ))}
                  <span style={{ marginLeft: 'auto', color: c.link, fontSize: 12 }}>Open →</span>
                </div>
              </div>
            </Card>
          ))}
        </div>
      )}
    </div>
  )
}

// Deploy-to-production teaser. Not wired to anything yet — it showcases the
// planned one-click deploy across providers and routes interest to sponsorship.
const DEPLOY_PROVIDERS: { name: string; color: string; mark: string }[] = [
  { name: 'AWS', color: '#FF9900', mark: 'aws' },
  { name: 'Google Cloud', color: '#4285F4', mark: 'GCP' },
  { name: 'Azure', color: '#0078D4', mark: 'Az' },
  { name: 'Cloudflare', color: '#F38020', mark: '☁' },
  { name: 'Hetzner', color: '#D50C2D', mark: 'H' },
  { name: 'DigitalOcean', color: '#0080FF', mark: 'DO' },
  { name: 'Vercel', color: '#000000', mark: '▲' },
  { name: 'Fly.io', color: '#7B3FE4', mark: 'Fly' },
  { name: 'Netlify', color: '#00AD9F', mark: 'N' },
  { name: 'Render', color: '#6E56CF', mark: 'R' },
  { name: 'Railway', color: '#8B5CF6', mark: 'Rw' },
  { name: 'Vultr', color: '#007BFC', mark: 'V' },
]

function DeployModal({ close }: { close: () => void }) {
  useEffect(() => {
    const h = (e: KeyboardEvent) => { if (e.key === 'Escape') close() }
    window.addEventListener('keydown', h)
    return () => window.removeEventListener('keydown', h)
  }, [close])
  return (
    <div onClick={close} style={{ position: 'fixed', inset: 0, zIndex: 120, background: 'rgba(9,9,11,.45)', display: 'flex', alignItems: 'center', justifyContent: 'center', padding: 20, backdropFilter: 'blur(2px)' }}>
      <Card onClick={(e: React.MouseEvent) => e.stopPropagation()} style={{ width: 560, maxWidth: '100%', maxHeight: '90vh', overflow: 'auto', padding: 0, boxShadow: '0 24px 70px rgba(0,0,0,.28)' }}>
        <div style={{ padding: '22px 24px 16px', borderBottom: `1px solid ${c.border}` }}>
          <div style={{ display: 'flex', alignItems: 'center', gap: 10 }}>
            <span style={{ display: 'flex', width: 30, height: 30, borderRadius: 8, background: 'linear-gradient(135deg,#3f3f46,#18181b)', color: '#fff', alignItems: 'center', justifyContent: 'center' }}>
              <svg width="16" height="16" viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="2.2" strokeLinecap="round" strokeLinejoin="round"><path d="M12 2 4 6v6c0 5 3.4 8.5 8 10 4.6-1.5 8-5 8-10V6Z" opacity=".35"/><path d="m9 12 2 2 4-4"/></svg>
            </span>
            <h2 style={{ fontFamily: font.display, fontSize: 19, fontWeight: 700, margin: 0 }}>Deploy to production</h2>
            <span style={{ ...mono, fontSize: 10, color: c.warn, background: `${c.warn}14`, border: `1px solid ${c.warn}40`, borderRadius: 5, padding: '2px 8px' }}>coming soon</span>
          </div>
          <p style={{ color: c.muted, fontSize: 13, margin: '10px 0 0', lineHeight: 1.55 }}>Happy with what you built here? Soon you'll ship this exact environment to <b style={{ color: c.fg }}>your own cloud</b> in one click — your account, your infra, your domain.</p>
        </div>

        <div style={{ padding: '18px 24px' }}>
          <div style={{ ...mono, fontSize: 10, letterSpacing: '.6px', color: c.muted2, marginBottom: 12 }}>PLANNED TARGETS</div>
          <div style={{ display: 'grid', gridTemplateColumns: 'repeat(4,1fr)', gap: 10 }}>
            {DEPLOY_PROVIDERS.map((p) => (
              <div key={p.name} title={`${p.name} — coming soon`} style={{ display: 'flex', flexDirection: 'column', alignItems: 'center', gap: 7, padding: '12px 6px', border: `1px solid ${c.border}`, borderRadius: 10, background: c.panel2 }}>
                <span style={{ display: 'flex', width: 34, height: 34, borderRadius: 9, alignItems: 'center', justifyContent: 'center', background: `${p.color}18`, border: `1px solid ${p.color}33`, color: p.color, fontFamily: font.display, fontWeight: 700, fontSize: p.mark.length > 2 ? 11 : 14 }}>{p.mark}</span>
                <span style={{ fontSize: 10.5, color: c.muted, textAlign: 'center', lineHeight: 1.2 }}>{p.name}</span>
              </div>
            ))}
          </div>
          <div style={{ textAlign: 'center', fontSize: 11.5, color: c.muted2, marginTop: 12 }}>…and every other provider — bring-your-own works too.</div>
        </div>

        <div style={{ padding: '18px 24px 22px', borderTop: `1px solid ${c.border}`, background: c.panel3, borderRadius: '0 0 10px 10px' }}>
          <div style={{ fontSize: 12.5, color: c.fg2, lineHeight: 1.55, marginBottom: 14 }}>
            sandboxd is open source and this feature is being built now. If one-click deploy would help you, <b style={{ color: c.fg }}>sponsoring keeps it moving</b> — sponsors help decide which providers land first.
          </div>
          <div style={{ display: 'flex', gap: 10, alignItems: 'center' }}>
            <a href="https://github.com/sponsors/tastyeffectco" target="_blank" rel="noreferrer" data-testid="deploy-sponsor" style={{ display: 'inline-flex', alignItems: 'center', gap: 7, textDecoration: 'none', background: '#db61a2', color: '#fff', fontFamily: font.sans, fontSize: 12.5, fontWeight: 600, borderRadius: 7, padding: '8px 14px' }}>
              <svg width="14" height="14" viewBox="0 0 24 24" fill="currentColor"><path d="M12 21s-7.5-4.6-10-9.3C.4 8.4 1.9 4.9 5.2 4.9c2 0 3.3 1.1 4 2.2.7-1.1 2-2.2 4-2.2 3.3 0 4.8 3.5 3.2 6.8C19.5 16.4 12 21 12 21Z"/></svg>
              Sponsor sandboxd
            </a>
            <Btn variant="ghost" onClick={close} style={{ marginLeft: 'auto' }}>Maybe later</Btn>
          </div>
        </div>
      </Card>
    </div>
  )
}

function Palette({ apps, close, onGo }: { apps: TApp[]; close: () => void; onGo: (r: Route) => void }) {
  const [q, setQ] = useState('')
  const ref = useRef<HTMLInputElement>(null)
  useEffect(() => { ref.current?.focus() }, [])
  const items = useMemo(() => {
    const cmds: { kind: string; label: string; go: Route }[] = [
      { kind: 'go', label: 'Apps', go: { name: 'apps' } },
      { kind: 'go', label: 'App Store', go: { name: 'store' } },
      { kind: 'go', label: 'Settings', go: { name: 'settings' } },
      ...apps.map((a) => ({ kind: 'app', label: a.name, go: { name: 'app', id: a.id } as Route })),
    ]
    const s = q.trim().toLowerCase()
    return s ? cmds.filter((x) => x.label.toLowerCase().includes(s)) : cmds
  }, [q, apps])
  return (
    <div onClick={close} style={{ position: 'fixed', inset: 0, background: 'rgba(9,9,11,.32)', zIndex: 90, display: 'flex', alignItems: 'flex-start', justifyContent: 'center', paddingTop: '14vh' }}>
      <div onClick={(e) => e.stopPropagation()} style={{ width: 560, maxWidth: '90vw', background: c.panel, border: `1px solid ${c.border}`, borderRadius: 12, boxShadow: '0 24px 64px rgba(0,0,0,.18)', overflow: 'hidden' }}>
        <input ref={ref} value={q} onChange={(e) => setQ(e.target.value)} placeholder="Search apps, commands…" style={{ width: '100%', border: 'none', borderBottom: `1px solid ${c.border}`, padding: '14px 16px', fontSize: 14, color: c.fg, outline: 'none', fontFamily: font.sans }} />
        <div style={{ maxHeight: 320, overflowY: 'auto', padding: 6 }}>
          {items.length === 0 ? (
            <div style={{ padding: 16, textAlign: 'center', color: c.muted2, fontSize: 12.5 }}>No matching commands</div>
          ) : items.map((it, i) => (
            <div key={i} onClick={() => onGo(it.go)} className="dc-hoverborder" style={{ display: 'flex', alignItems: 'center', gap: 10, padding: '8px 10px', borderRadius: 7, cursor: 'pointer', border: '1px solid transparent' }}>
              <span style={{ ...mono, fontSize: 10.5, color: c.muted2, background: c.panel2, border: `1px solid ${c.border}`, borderRadius: 4, padding: '1px 6px', minWidth: 44, textAlign: 'center' }}>{it.kind}</span>
              <span style={{ fontSize: 13 }}>{it.label}</span>
            </div>
          ))}
        </div>
        <div style={{ display: 'flex', gap: 14, padding: '8px 14px', borderTop: `1px solid ${c.panel2}`, background: c.panel3, fontSize: 11, color: c.faint }}>
          <span>↑↓ navigate</span><span>↵ run</span><span>esc close</span>
        </div>
      </div>
    </div>
  )
}

function Helper() {
  const [open, setOpen] = useState(false)
  if (!open) {
    return (
      <div onClick={() => setOpen(true)} title="Ask sandboxd" style={{ position: 'fixed', bottom: 20, right: 20, zIndex: 80, width: 44, height: 44, borderRadius: '50%', background: c.ink, color: c.bg, display: 'flex', alignItems: 'center', justifyContent: 'center', fontFamily: font.mono, fontSize: 15, cursor: 'pointer', boxShadow: '0 8px 24px rgba(0,0,0,.22)' }}>?</div>
    )
  }
  return (
    <div style={{ position: 'fixed', bottom: 20, right: 20, zIndex: 81, width: 320, height: 380, background: c.panel, border: `1px solid ${c.border}`, borderRadius: 12, boxShadow: '0 16px 48px rgba(0,0,0,.2)', display: 'flex', flexDirection: 'column' }}>
      <div style={{ display: 'flex', alignItems: 'center', gap: 8, padding: '10px 14px', borderBottom: `1px solid ${c.border}`, background: c.panel3, borderRadius: '12px 12px 0 0' }}>
        <div style={{ width: 22, height: 22, borderRadius: 6, background: 'linear-gradient(135deg,#3f3f46,#18181b)', display: 'flex', alignItems: 'center', justifyContent: 'center', fontFamily: font.mono, fontSize: 10, color: c.bg }}>?</div>
        <div>
          <div style={{ fontFamily: font.display, fontSize: 13, fontWeight: 600, lineHeight: 1.2 }}>Ask sandboxd</div>
          <div style={{ fontSize: 10.5, color: c.muted2, lineHeight: 1.2 }}>Platform-wide help</div>
        </div>
        <div onClick={() => setOpen(false)} className="dc-hoverink" style={{ marginLeft: 'auto', color: c.muted2, cursor: 'pointer', fontSize: 15, padding: '2px 6px', borderRadius: 5 }}>×</div>
      </div>
      <div style={{ flex: 1, overflowY: 'auto', padding: 12, color: c.muted2, fontSize: 12 }}>
        Ask about sandboxes, agents, git, snapshots, networking. (Chat wiring coming in a later phase.)
      </div>
    </div>
  )
}
