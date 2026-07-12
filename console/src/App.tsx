import { useCallback, useEffect, useMemo, useRef, useState } from 'react'
import { api, setOnUnauthorized, App as TApp, Preset, GitCredential } from './api'
import { c, font, mono, Card, Btn, StatusPill, Input, navItem } from './design/kit'
import { PRESET_ICONS } from './design/presetIcons'
import { STARTERS, STARTER_ICONS } from './design/starters'
import { AppView } from './AppView'
import { StoreView } from './StoreView'
import { SettingsView } from './SettingsView'
import { Login, CreatePassword } from './AuthGate'

type Route = { name: 'apps' } | { name: 'store' } | { name: 'settings' } | { name: 'app'; id: string; tab?: string; task?: string }

export default function App() {
  const [route, setRoute] = useState<Route>({ name: 'apps' })
  const [toasts, setToasts] = useState<{ id: number; msg: string }[]>([])
  const [paletteOpen, setPaletteOpen] = useState(false)
  const [apps, setApps] = useState<TApp[]>([])
  const [auth, setAuth] = useState<{ enabled: boolean; authenticated: boolean; password_set: boolean } | null>(null)

  const toast = useCallback((msg: string) => {
    const id = Date.now() + Math.floor(performance.now())
    setToasts((t) => [...t, { id, msg }])
    setTimeout(() => setToasts((t) => t.filter((x) => x.id !== id)), 3200)
  }, [])
  const onError = useCallback((m: string) => toast(m), [toast])

  const loadApps = useCallback(() => api.listApps().then(setApps).catch(() => {}), [])
  const refreshAuth = useCallback(
    () => api.authStatus().then(setAuth).catch(() => setAuth({ enabled: true, authenticated: false, password_set: false })),
    [],
  )

  // On mount: resolve auth state, and register the 401 hook so an expired session
  // bounces back to the gate. Only load apps once we know we're allowed through.
  useEffect(() => {
    refreshAuth()
    setOnUnauthorized(() => setAuth((a) => (a ? { ...a, authenticated: false } : a)))
  }, [refreshAuth])
  useEffect(() => {
    if (auth && (auth.authenticated || auth.enabled === false)) loadApps()
  }, [auth, loadApps])

  const onAuthed = useCallback(() => { refreshAuth().then(loadApps) }, [refreshAuth, loadApps])
  const logout = useCallback(() => { api.logout().finally(() => setAuth((a) => (a ? { ...a, authenticated: false } : a))) }, [])

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

  // Auth gate — render before the app chrome. null = still resolving status.
  if (auth === null) {
    return (
      <div style={{ display: 'flex', alignItems: 'center', justifyContent: 'center', height: '100vh', background: c.bg, color: c.muted2, fontFamily: font.sans }}>
        Loading…
      </div>
    )
  }
  if (auth.enabled && !auth.authenticated) {
    return auth.password_set ? <Login onDone={onAuthed} /> : <CreatePassword onDone={onAuthed} />
  }

  return (
    <div style={{ display: 'flex', flexDirection: 'column', height: '100vh', background: c.bg, color: c.fg, fontFamily: font.sans, overflow: 'hidden' }}>
      {/* TOP BAR */}
      <div style={{ display: 'flex', alignItems: 'center', gap: 18, height: 52, flexShrink: 0, padding: '0 20px', borderBottom: `1px solid ${c.border}`, background: c.panel }}>
        <div onClick={() => setRoute({ name: 'apps' })} style={{ display: 'flex', alignItems: 'center', gap: 9, cursor: 'pointer' }}>
          <div style={{ width: 26, height: 26, borderRadius: 7, background: 'linear-gradient(135deg,#3f3f46,#18181b)', display: 'flex', alignItems: 'center', justifyContent: 'center', fontFamily: font.mono, fontSize: 11, color: c.bg }}>&gt;_</div>
          <span style={{ fontFamily: font.display, fontWeight: 700, fontSize: 15, letterSpacing: '.2px' }}>sandboxd <span style={{ fontWeight: 500, color: c.muted }}>console</span></span>
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
        <div style={{ display: 'flex', gap: 12, alignItems: 'center' }}>
          <a href="https://sandboxd.io" target="_blank" rel="noreferrer" className="dc-hoverink" style={{ color: c.muted, textDecoration: 'none', fontSize: 12 }}>Docs</a>
          <a href="https://github.com/tastyeffectco/sandboxd" target="_blank" rel="noreferrer" className="dc-hoverink" style={{ color: c.muted, textDecoration: 'none', fontSize: 12 }}>GitHub</a>
          {auth?.enabled && (
            <span data-testid="nav-logout" className="dc-hoverink" onClick={logout} style={{ color: c.muted, fontSize: 12, cursor: 'pointer' }}>Log out</span>
          )}
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

// Short label + tagline per preset (cleaner than the API's full sentences).
const PRESET_META: Record<string, { short: string; tag: string }> = {
  'react-vite': { short: 'React', tag: 'Vite SPA · hot reload' },
  nextjs: { short: 'Next.js', tag: 'App Router · SSR' },
  'node-express': { short: 'Express', tag: 'Node REST API' },
  fastapi: { short: 'FastAPI', tag: 'Python REST API' },
  worker: { short: 'Worker', tag: 'Background · no preview' },
}

function AppsScreen({ apps, reload, onOpen, onError, goStore }: { apps: TApp[]; reload: () => void; onOpen: (id: string) => void; onError: (m: string) => void; goStore: () => void }) {
  const [name, setName] = useState('')
  const [mode, setMode] = useState<'template' | 'starter' | 'git'>('template')
  const [starter, setStarter] = useState('')
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
    if (mode === 'git' && !repo.trim()) { onError('Enter a repo URL'); return }
    if (mode === 'starter' && !starter) { onError('Pick a starter'); return }
    const pick = STARTERS.find((s) => s.id === starter)
    // No forced naming: derive a sensible default (from the repo/starter, else a
    // short slug) when the field is blank. Renameable anytime from the app header.
    const slug = (s: string) => s.toLowerCase().replace(/[^a-z0-9]+/g, '-').replace(/(^-|-$)/g, '').slice(0, 40)
    const derived = mode === 'git' && repo.trim() ? slug(repo.trim().replace(/\.git$/, '').split('/').pop() || '')
      : mode === 'starter' && pick ? slug(pick.id) : ''
    const finalName = name.trim() || derived || `app-${Math.random().toString(36).slice(2, 6)}`
    setBusy(true)
    try {
      const a = await api.createApp({
        name: finalName,
        runtime_preset: mode === 'template' && preset ? preset : undefined,
        git: mode === 'git' ? { repo_url: repo.trim(), branch: branch.trim() || 'main', ...(credId ? { credential_id: credId } : {}) } // no credId → public tokenless clone
          : mode === 'starter' && pick ? { repo_url: `https://github.com/${pick.repo}`, branch: pick.branch } // public → tokenless
          : undefined,
      })
      // Zero-friction: boot the sandbox right away so the app is ready to use.
      // If it fails, the app view still offers a Create-sandbox retry.
      try { await api.createAppSandbox(a.id, {}) } catch { /* app exists; retry from the app view */ }
      setName(''); setRepo(''); reload(); onOpen(a.id)
    } catch (e) { onError((e as Error).message) } finally { setBusy(false) }
  }

  const modeChip = (m: 'template' | 'starter' | 'git', label: string) => (
    <div onClick={() => setMode(m)} className="dc-hoverborder" data-testid={`mode-${m}`} style={{ padding: '6px 12px', fontSize: 12.5, borderRadius: 7, cursor: 'pointer', border: `1px solid ${mode === m ? c.faint : c.border}`, color: mode === m ? c.fg : c.muted, background: mode === m ? c.panel2 : 'transparent' }}>{label}</div>
  )

  return (
    <div style={{ maxWidth: 920, margin: '0 auto', padding: '36px 40px 80px' }}>
      <h1 style={{ fontFamily: font.display, fontSize: 24, fontWeight: 700, margin: '0 0 4px' }}>Apps</h1>
      <p style={{ color: c.muted, margin: '0 0 24px' }}>Each app runs isolated in its own sandbox with a live preview URL.</p>

      <Card style={{ padding: 16, marginBottom: 28 }}>
        <div style={{ display: 'flex', gap: 10, marginBottom: 12 }}>
          <Input mono value={name} onChange={(e) => setName(e.target.value)} onKeyDown={(e) => e.key === 'Enter' && create()} placeholder="name — optional, rename anytime" style={{ flex: 1, fontSize: 13 }} data-testid="app-name" />
          <Btn variant="primary" disabled={busy || (mode === 'starter' && !starter)} onClick={create} style={{ padding: '9px 18px', fontSize: 13 }}>{mode === 'starter' ? 'Create from starter' : 'Create app'}</Btn>
        </div>
        <div style={{ display: 'flex', alignItems: 'center', gap: 8, flexWrap: 'wrap' }}>
          {modeChip('template', 'Blank template')}
          {modeChip('starter', 'Starter project')}
          {modeChip('git', 'Import from Git')}
          <span style={{ color: c.muted2, fontSize: 12, marginLeft: 'auto' }}>Want a ready-made app? Browse the <a onClick={goStore} style={{ color: c.link, cursor: 'pointer', textDecoration: 'none' }}>App Store</a>.</span>
        </div>

        {mode === 'starter' ? (
          <div data-testid="starter-grid" style={{ display: 'grid', gridTemplateColumns: 'repeat(auto-fill,minmax(212px,1fr))', gap: 10, marginTop: 14 }}>
            {STARTERS.map((s) => {
              const active = starter === s.id
              return (
                <div key={s.id} data-testid={`starter-${s.id}`} onClick={() => setStarter(active ? '' : s.id)} className="dc-hoverborder"
                  style={{ position: 'relative', display: 'flex', gap: 10, padding: '12px 13px', borderRadius: 10, cursor: 'pointer', border: `1px solid ${active ? c.ink : c.border}`, background: active ? c.panel2 : c.panel, boxShadow: active ? `inset 0 0 0 1px ${c.ink}` : 'none' }}>
                  {active && <span style={{ position: 'absolute', top: 8, right: 8, width: 15, height: 15, borderRadius: '50%', background: c.ink, color: '#fff', fontSize: 9, display: 'flex', alignItems: 'center', justifyContent: 'center' }}>✓</span>}
                  <span className="prov-ico" style={{ width: 24, height: 24, flexShrink: 0, marginTop: 1 }} dangerouslySetInnerHTML={{ __html: STARTER_ICONS[s.tech] || '' }} />
                  <div style={{ minWidth: 0 }}>
                    <div style={{ display: 'flex', alignItems: 'center', gap: 6 }}>
                      <span style={{ fontFamily: font.display, fontWeight: 600, fontSize: 13, overflow: 'hidden', textOverflow: 'ellipsis', whiteSpace: 'nowrap' }}>{s.name}</span>
                      {s.stars && <span style={{ ...mono, fontSize: 10, color: c.muted2, flexShrink: 0 }}>★{s.stars}</span>}
                    </div>
                    <div style={{ color: c.muted2, fontSize: 11, lineHeight: 1.35, marginTop: 2 }}>{s.blurb}</div>
                    <div style={{ ...mono, fontSize: 10, color: c.faint, marginTop: 3, overflow: 'hidden', textOverflow: 'ellipsis', whiteSpace: 'nowrap' }}>{s.repo}</div>
                  </div>
                </div>
              )
            })}
          </div>
        ) : mode === 'template' ? (
          <div data-testid="app-preset" style={{ display: 'grid', gridTemplateColumns: 'repeat(auto-fill,minmax(158px,1fr))', gap: 10, marginTop: 14 }}>
            {presets.map((p) => {
              const meta = PRESET_META[p.id] || { short: p.label, tag: p.description }
              const active = preset === p.id
              return (
                <div key={p.id} data-testid={`preset-${p.id}`} onClick={() => setPreset(active ? '' : p.id)} className="dc-hoverborder"
                  style={{ position: 'relative', display: 'flex', flexDirection: 'column', gap: 8, padding: '13px 13px 12px', borderRadius: 10, cursor: 'pointer', border: `1px solid ${active ? c.ink : c.border}`, background: active ? c.panel2 : c.panel, boxShadow: active ? `inset 0 0 0 1px ${c.ink}` : 'none' }}>
                  {active && <span style={{ position: 'absolute', top: 9, right: 9, width: 16, height: 16, borderRadius: '50%', background: c.ink, color: '#fff', fontSize: 10, display: 'flex', alignItems: 'center', justifyContent: 'center' }}>✓</span>}
                  <span className="prov-ico" style={{ width: 26, height: 26 }} dangerouslySetInnerHTML={{ __html: PRESET_ICONS[p.id] || '' }} />
                  <div>
                    <div style={{ fontFamily: font.display, fontWeight: 600, fontSize: 13.5 }}>{meta.short}</div>
                    <div style={{ color: c.muted2, fontSize: 11.5, lineHeight: 1.35, marginTop: 1 }}>{meta.tag}</div>
                  </div>
                </div>
              )
            })}
          </div>
        ) : (
          <div style={{ display: 'flex', gap: 8, marginTop: 14, flexWrap: 'wrap' }}>
            <Input mono value={repo} onChange={(e) => setRepo(e.target.value)} placeholder="https://github.com/user/repo.git" style={{ flex: 1, minWidth: 220, fontSize: 12.5 }} data-testid="git-repo-url" />
            <Input mono value={branch} onChange={(e) => setBranch(e.target.value)} placeholder="branch" style={{ width: 120, fontSize: 12.5 }} data-testid="git-branch" />
            <select value={credId} onChange={(e) => setCredId(e.target.value)} data-testid="git-cred" style={{ background: c.bg, border: `1px solid ${c.border2}`, borderRadius: 7, padding: '8px 10px', color: c.fg, fontSize: 12.5, fontFamily: font.sans }}>
              <option value="">Public repo — no credential</option>
              {creds.map((g) => <option key={g.id} value={g.id}>{g.name}</option>)}
            </select>
          </div>
        )}
        {mode === 'git' && creds.length === 0 && (
          <div style={{ marginTop: 8, fontSize: 12, color: c.muted }} data-testid="git-no-creds"><b>Public repos import with no credential.</b> For a <b>private</b> repo, add a personal access token in <b>Settings → Git credentials</b>.</div>
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
