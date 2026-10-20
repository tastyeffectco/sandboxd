import { useCallback, useEffect, useRef, useState } from 'react'
import { api, setOnUnauthorized, App as TApp, Settings, UpgradeState } from './api'
import { previewReach, type PreviewReach } from './publicurl'
import { c, font, mono, Card, Input } from './design/kit'
import { AppView } from './AppView'
import { AppBrain, brainExcerpt, buildBrainGraph } from './brain'
import { BrainGraph } from './BrainGraph'
import { StoreView } from './StoreView'
import { SettingsView } from './SettingsView'
import { BuilderHome, BuilderDraft } from './BuilderHome'
import { BuildIntent } from './AgentPicker'
import { Dialog } from './design/Dialog'
import { IS_DEMO } from './demo'
import { Icon, IconName } from './design/Icon'
import { confirmLeaveWorkspace } from './unsavedChanges'
import { VersionPill } from './VersionPill'
import { Login, CreatePassword } from './AuthGate'

function readRoute(): Route {
  const parts = location.hash.replace(/^#\/?/, '').split('/')
  if (parts[0] === 'app' && parts[1]) {
    try {
      return { name: 'app', id: decodeURIComponent(parts[1]), tab: parts[2] }
    } catch {
      return { name: 'apps' }
    }
  }
  if (parts[0] === 'settings') return { name: 'settings', section: parts[1] }
  return { name: ['brain', 'store', 'settings'].includes(parts[0]) ? parts[0] : 'apps' } as Route
}

type Route =
  | { name: 'apps' }
  | { name: 'brain' }
  | { name: 'store' }
  | { name: 'settings'; section?: string }
  | { name: 'app'; id: string; tab?: string; task?: string }

export default function App() {
  const [route, setRouteState] = useState<Route>(readRoute)
  const routeRef = useRef(route)
  routeRef.current = route
  const setRoute = useCallback((next: Route) => {
    const current = routeRef.current
    if (
      current.name === 'app' &&
      (next.name !== 'app' || next.id !== current.id) &&
      !confirmLeaveWorkspace()
    ) {
      history.replaceState(
        null,
        '',
        `#/app/${encodeURIComponent(current.id)}${current.tab ? '/' + current.tab : ''}`,
      )
      return
    }
    routeRef.current = next
    setRouteState(next)
  }, [])
  const [builderDraft, setBuilderDraft] = useState<BuilderDraft>()
  const [chatDrafts, setChatDrafts] = useState<Record<string, BuildIntent>>({})
  const [pendingBuilds, setPendingBuilds] = useState<Record<string, BuildIntent>>({})
  const [mobileNav, setMobileNav] = useState(false)
  useEffect(() => {
    const listener = () => setRoute(readRoute())
    window.addEventListener('hashchange', listener)
    return () => window.removeEventListener('hashchange', listener)
  }, [])
  useEffect(() => {
    const hash =
      route.name === 'app'
        ? `#/app/${encodeURIComponent(route.id)}${route.tab ? '/' + route.tab : ''}`
        : `#/${route.name}${route.name === 'settings' && route.section ? '/' + route.section : ''}`
    if (location.hash !== hash) location.hash = hash
    setMobileNav(false)
  }, [route])
  const [toasts, setToasts] = useState<{ id: number; msg: string }[]>([])
  const [paletteOpen, setPaletteOpen] = useState(false)
  const [apps, setApps] = useState<TApp[]>([])
  const [auth, setAuth] = useState<{
    enabled: boolean
    authenticated: boolean
    password_set: boolean
  } | null>(null)
  // Version pill (bottom left): the control plane checks GitHub releases
  // (cached ~6h) and reports update_available + notes in /v1/settings.
  // Dismissal is remembered per version, so each new release nudges once.
  const [settings, setSettings] = useState<Settings | null>(null)
  const [updDismissed, setUpdDismissed] = useState(false)
  // In-place upgrade driven from the pill: confirm -> POST /v1/upgrade ->
  // poll GET /v1/upgrade. The control plane restarts mid-way, so polling
  // tolerates fetch errors and keeps going until a terminal phase.
  const [upg, setUpg] = useState<UpgradeState | null>(null)
  // Reachability hint: previews on a LAN-only or bare-IP domain can't be shared
  // with anyone outside this network. One strip, dismissed once, never shown
  // again once a real domain (or a tunnel) is configured.
  const [reach, setReach] = useState<PreviewReach | null>(null)

  const toast = useCallback((msg: string) => {
    const id = Date.now() + Math.floor(performance.now())
    setToasts((t) => [...t, { id, msg }])
    setTimeout(() => setToasts((t) => t.filter((x) => x.id !== id)), 3200)
  }, [])
  const onError = useCallback((m: string) => toast(m), [toast])

  const loadApps = useCallback(
    () =>
      api
        .listApps()
        .then(setApps)
        .catch(() => {}),
    [],
  )
  const refreshAuth = useCallback(
    () =>
      api
        .authStatus()
        .then(setAuth)
        .catch(() => setAuth({ enabled: true, authenticated: false, password_set: false })),
    [],
  )

  // On mount: resolve auth state, and register the 401 hook so an expired session
  // bounces back to the gate. Only load apps once we know we're allowed through.
  useEffect(() => {
    refreshAuth()
    setOnUnauthorized(() => {
      setAuth((a) => (a ? { ...a, authenticated: false } : a))
      setBuilderDraft(undefined)
      setChatDrafts({})
      setPendingBuilds({})
    })
  }, [refreshAuth])
  useEffect(() => {
    if (auth && (auth.authenticated || auth.enabled === false)) loadApps()
  }, [auth, loadApps])
  const loadSettings = useCallback(
    () =>
      api.getSettings().then((s) => {
        setSettings(s)
        setUpdDismissed(
          !!s.latest_version && localStorage.getItem('sandboxd-update-dismissed') === s.latest_version,
        )
        return s
      }),
    [],
  )
  useEffect(() => {
    if (!(auth && (auth.authenticated || auth.enabled === false))) return
    loadSettings()
      .then((s) => {
        const r = previewReach(s.networking?.preview_domain || '', !!s.networking?.preview_tls)
        if (!IS_DEMO && r !== 'ok' && localStorage.getItem('sandboxd-public-url-hint') !== 'dismissed')
          setReach(r)
      })
      .catch(() => {})
  }, [auth, loadSettings])
  const dismissUpdate = useCallback(() => {
    if (settings?.latest_version) localStorage.setItem('sandboxd-update-dismissed', settings.latest_version)
    setUpdDismissed(true)
  }, [settings])

  const startUpgrade = useCallback(
    (target: string) => {
      api
        .startUpgrade(target)
        .then(setUpg)
        .catch((e: Error) => {
          toast(e.message || 'could not start upgrade')
          setUpg(null)
        })
    },
    [toast],
  )
  useEffect(() => {
    if (!upg || upg.phase !== 'running') return
    const id = setInterval(() => {
      api
        .getUpgrade()
        .then((s) => {
          setUpg(s)
          if (s.phase === 'succeeded') {
            refreshAuth().then(loadApps)
            loadSettings().catch(() => {})
          }
        })
        .catch(() => {
          /* control plane restarting — keep polling */
        })
    }, 3000)
    return () => clearInterval(id)
  }, [upg, refreshAuth, loadApps, loadSettings])

  const onAuthed = useCallback(() => {
    refreshAuth().then(loadApps)
  }, [refreshAuth, loadApps])
  const logout = useCallback(() => {
    if (!confirmLeaveWorkspace()) return
    setBuilderDraft(undefined)
    setChatDrafts({})
    setPendingBuilds({})
    api.logout().finally(() => setAuth((a) => (a ? { ...a, authenticated: false } : a)))
  }, [])

  useEffect(() => {
    const h = (e: KeyboardEvent) => {
      if ((e.metaKey || e.ctrlKey) && e.key.toLowerCase() === 'k') {
        e.preventDefault()
        setPaletteOpen((o) => !o)
      } else if (e.key === 'Escape') setPaletteOpen(false)
    }
    window.addEventListener('keydown', h)
    return () => window.removeEventListener('keydown', h)
  }, [])

  const goApp = (id: string, tab?: string) => setRoute({ name: 'app', id, tab })

  const nav = [
    { key: 'apps', label: 'Build & projects', icon: 'grid' },
    { key: 'brain', label: 'Project memory', icon: 'book' },
    { key: 'store', label: 'App library', icon: 'box' },
    { key: 'settings', label: 'Settings', icon: 'settings' },
  ]

  // Auth gate — render before the app chrome. null = still resolving status.
  if (auth === null) {
    return (
      <div
        style={{
          display: 'flex',
          alignItems: 'center',
          justifyContent: 'center',
          height: '100vh',
          background: c.bg,
          color: c.muted2,
          fontFamily: font.sans,
        }}
      >
        Loading…
      </div>
    )
  }
  if (auth.enabled && !auth.authenticated) {
    return auth.password_set ? <Login onDone={onAuthed} /> : <CreatePassword onDone={onAuthed} />
  }

  return (
    <div className="studio-shell">
      {reach && (
        <div className="reachability-hint" data-testid="public-url-hint">
          <Icon name="external" size={14} />
          <span>
            {reach === 'lan' ? 'Preview links are available on your network.' : 'Preview links use HTTP.'}{' '}
            <a href="https://sandboxd.io/guides/production-tls" target="_blank" rel="noreferrer">
              Configure public sharing ↗
            </a>
          </span>
          <button
            className="icon-button"
            aria-label="Dismiss sharing notice"
            data-testid="public-url-hint-dismiss"
            onClick={() => {
              localStorage.setItem('sandboxd-public-url-hint', 'dismissed')
              setReach(null)
            }}
          >
            <Icon name="close" size={13} />
          </button>
        </div>
      )}
      <header className="mobile-bar">
        <button className="icon-button" aria-label="Open navigation" onClick={() => setMobileNav(true)}>
          <Icon name="menu" />
        </button>
        <strong>sandboxd</strong>
        <button
          className="icon-button"
          aria-label="Search projects and commands"
          onClick={() => setPaletteOpen(true)}
        >
          <Icon name="search" />
        </button>
      </header>
      <div className="studio-body">
        {mobileNav && (
          <button
            className="nav-backdrop"
            aria-label="Close navigation"
            onClick={() => setMobileNav(false)}
          />
        )}
        <aside className={`studio-sidebar ${mobileNav ? 'is-open' : ''}`}>
          <button className="studio-brand" onClick={() => setRoute({ name: 'apps' })}>
            <span>
              <Icon name="box" size={23} />
            </span>
            <strong>
              sandboxd<small>BUILD SOMETHING YOURS</small>
            </strong>
          </button>
          <button
            className="sidebar-new"
            onClick={() => {
              setRoute({ name: 'apps' })
              setTimeout(() => document.getElementById('build-prompt')?.focus(), 0)
            }}
          >
            <Icon name="plus" size={18} />
            <span>New project</span>
          </button>
          <button className="sidebar-search" onClick={() => setPaletteOpen(true)}>
            <Icon name="search" size={16} />
            <span>Quick search</span>
            <kbd>⌘ K</kbd>
          </button>
          <nav aria-label="Main navigation">
            {nav.map((n) => (
              <button
                key={n.key}
                data-testid={`nav-${n.key}`}
                title={n.label}
                aria-current={route.name === n.key ? 'page' : undefined}
                className={route.name === n.key ? 'sidebar-link active' : 'sidebar-link'}
                onClick={() => setRoute({ name: n.key } as Route)}
              >
                <Icon name={n.icon as IconName} />
                <span>{n.label}</span>
              </button>
            ))}
          </nav>
          <div className="sidebar-projects">
            <div className="sidebar-caption">RECENT PROJECTS</div>
            {apps.slice(0, 7).map((a) => (
              <button
                key={a.id}
                title={a.name}
                className={
                  route.name === 'app' && route.id === a.id ? 'sidebar-project active' : 'sidebar-project'
                }
                onClick={() => goApp(a.id)}
              >
                <span className="project-initial" aria-hidden="true">
                  {a.name.slice(0, 1).toUpperCase()}
                </span>
                <span>{a.name}</span>
              </button>
            ))}
            {!apps.length && <p>Your next idea starts here.</p>}
          </div>
          <footer className="sidebar-footer">
            <a href="https://sandboxd.io" target="_blank" rel="noreferrer">
              <Icon name="book" size={16} />
              <span>Documentation</span>
              <Icon name="external" size={12} />
            </a>
            <a
              href="https://github.com/tastyeffectco/sandboxd/discussions"
              target="_blank"
              rel="noreferrer"
              data-testid="nav-feedback"
            >
              <Icon name="code" size={16} />
              <span>Community & feedback</span>
            </a>
            {auth.enabled && (
              <button onClick={logout} data-testid="nav-logout">
                <Icon name="logout" size={16} />
                <span>Sign out</span>
              </button>
            )}
            <div className="instance-label">
              <span className="brand-dot" />
              Self-hosted workspace
            </div>
            {settings && (
              <VersionPill
                settings={settings}
                dismissed={updDismissed}
                onDismiss={dismissUpdate}
                upg={upg}
                startUpgrade={startUpgrade}
                clearUpgrade={() => setUpg(null)}
              />
            )}
          </footer>
        </aside>
        {/* MAIN */}
        <main
          id="main-content"
          className={route.name === 'app' ? 'studio-main workspace-main' : 'studio-main'}
        >
          {route.name === 'apps' && (
            <BuilderHome
              draft={builderDraft}
              saveDraft={setBuilderDraft}
              apps={apps}
              reload={loadApps}
              onOpen={(id) => goApp(id)}
              goStore={() => setRoute({ name: 'store' })}
              goSettings={() => setRoute({ name: 'settings' })}
              onCreated={(id, intent) => {
                if (intent) setPendingBuilds((p) => ({ ...p, [id]: intent }))
                goApp(id)
              }}
            />
          )}
          {route.name === 'brain' && <BrainOverview apps={apps} onOpen={(id) => goApp(id, 'brain')} />}
          {route.name === 'store' && (
            <StoreView onError={onError} toast={toast} onOpen={(id) => goApp(id)} reloadApps={loadApps} />
          )}
          {route.name === 'settings' && (
            <SettingsView initialSection={route.section} onError={onError} toast={toast} />
          )}
          {route.name === 'app' && (
            <AppView
              key={route.id}
              appId={route.id}
              initialBuild={pendingBuilds[route.id]}
              chatDraft={chatDrafts[route.id]}
              saveChatDraft={(draft) => setChatDrafts((p) => ({ ...p, [route.id]: draft }))}
              consumeBuild={() =>
                setPendingBuilds((p) => {
                  const next = { ...p }
                  delete next[route.id]
                  return next
                })
              }
              initialTab={route.tab}
              onError={onError}
              toast={toast}
              goApps={() => {
                setRoute({ name: 'apps' })
                loadApps()
              }}
              goSettings={() => setRoute({ name: 'settings' })}
              apps={apps}
              onOpenApp={(id, tab) => goApp(id, tab)}
            />
          )}
        </main>
      </div>

      {paletteOpen && (
        <Palette
          apps={apps}
          close={() => setPaletteOpen(false)}
          onGo={(r) => {
            setRoute(r)
            setPaletteOpen(false)
          }}
        />
      )}

      {/* TOASTS */}
      {toasts.length > 0 && (
        <div
          style={{
            position: 'fixed',
            bottom: 80,
            right: 20,
            zIndex: 99,
            display: 'flex',
            flexDirection: 'column',
            gap: 8,
          }}
        >
          {toasts.map((t) => (
            <div
              role="status"
              key={t.id}
              style={{
                background: c.ink,
                color: '#fff',
                borderRadius: 8,
                padding: '10px 16px',
                fontSize: 12.5,
                boxShadow: '0 8px 24px rgba(0,0,0,.18)',
                display: 'flex',
                alignItems: 'center',
                gap: 8,
              }}
            >
              {t.msg}
            </div>
          ))}
        </div>
      )}
    </div>
  )
}

// ---------- BRAIN OVERVIEW (every project's memory at a glance) ----------
// Excerpt/graph logic lives in brain.ts (pure + unit-tested); this renders it.

function BrainOverview({ apps, onOpen }: { apps: TApp[]; onOpen: (id: string) => void }) {
  // null = no sandbox; undefined = still loading; AppBrain = hub + spoke notes.
  const [brains, setBrains] = useState<Record<string, AppBrain | null | undefined>>({})

  useEffect(() => {
    apps.forEach((a) => {
      if (!a.current_sandbox_id) {
        setBrains((b) => ({ ...b, [a.id]: null }))
        return
      }
      api
        .getAppBrain(a.current_sandbox_id)
        .then((br) => setBrains((b) => ({ ...b, [a.id]: br })))
        .catch(() => setBrains((b) => ({ ...b, [a.id]: null })))
    })
  }, [apps])

  const hasBrain = (id: string) => {
    const b = brains[id]
    return !!b && (typeof b.hub === 'string' || Object.keys(b.spokes).length > 0)
  }
  const withBrain = apps.filter((a) => hasBrain(a.id))
  const without = apps.filter((a) => brains[a.id] !== undefined && !hasBrain(a.id))

  return (
    <div className="content-page">
      <h1 style={{ fontFamily: font.display, fontSize: 24, fontWeight: 700, margin: '0 0 6px' }}>
        Project memory
      </h1>
      <p style={{ color: c.muted, margin: '0 0 24px', maxWidth: 600 }}>
        Every project's memory in one place — state, decisions, gotchas. Each app's{' '}
        <span style={{ ...mono, fontSize: 12 }}>BRAIN.md</span> is read by the agent before every task and
        updated as it learns.
      </p>

      {apps.length === 0 && (
        <p style={{ color: c.muted2 }}>Your project notes and decisions will appear here as you build.</p>
      )}

      {/* Knowledge graph: [[wikilinks]] between brains become edges; dashed
          nodes are ghosts (no brain yet, or a linked concept with no app). */}
      {(() => {
        const settled = apps.every((a) => brains[a.id] !== undefined)
        if (!settled || apps.length === 0) return null
        const g = buildBrainGraph(apps, brains)
        if (g.edges.length === 0 && g.nodes.every((n) => n.empty)) return null
        return (
          <>
            <Card style={{ padding: '8px 10px', marginBottom: 22 }}>
              <BrainGraph nodes={g.nodes} edges={g.edges} onOpen={onOpen} />
              <div
                style={{ ...mono, fontSize: 10.5, color: c.muted2, textAlign: 'center', paddingBottom: 6 }}
              >
                Link projects &amp; concepts from any brain with [[name]] — dashed nodes have no note yet ·
                err-* concepts flag repeat mistakes.
              </div>
            </Card>
            {g.concepts.length > 0 && (
              <Card style={{ padding: 16, marginBottom: 22 }} data-testid="brain-concepts">
                <div style={{ fontFamily: font.display, fontWeight: 700, fontSize: 13, marginBottom: 4 }}>
                  Concepts without a note yet
                </div>
                <div style={{ color: c.muted2, fontSize: 12, marginBottom: 10 }}>
                  Linked from your brains but written nowhere. A concept shared by several apps is knowledge
                  worth a note — an <span style={{ ...mono, fontSize: 11 }}>err-*</span> concept shared by
                  several apps is a <b style={{ color: c.fg2 }}>repeat mistake</b>.
                </div>
                <div style={{ display: 'flex', gap: 8, flexWrap: 'wrap' }}>
                  {g.concepts.slice(0, 12).map((cs) => (
                    <span
                      key={cs.slug}
                      title={`${cs.mentions} mention${cs.mentions > 1 ? 's' : ''} across ${cs.apps} app${cs.apps > 1 ? 's' : ''}`}
                      style={{
                        ...mono,
                        fontSize: 12,
                        display: 'inline-flex',
                        alignItems: 'center',
                        gap: 6,
                        border: `1px dashed ${cs.isError ? c.warn : c.border2}`,
                        borderRadius: 7,
                        padding: '5px 11px',
                        color: cs.isError ? c.warn : c.muted,
                      }}
                    >
                      <span
                        style={{
                          width: 6,
                          height: 6,
                          borderRadius: '50%',
                          background: cs.isError && cs.apps > 1 ? c.warn : c.muted2,
                        }}
                      />
                      [[{cs.label}]]
                      <span style={{ color: c.muted2, fontSize: 10.5 }}>{cs.apps}×</span>
                    </span>
                  ))}
                </div>
              </Card>
            )}
          </>
        )
      })()}

      <div
        style={{ display: 'grid', gridTemplateColumns: 'repeat(auto-fill,minmax(280px,1fr))', gap: 14 }}
        data-testid="brain-list"
      >
        {withBrain.map((a) => {
          const b = brains[a.id] as AppBrain
          const md = b.hub || ''
          const spokeCount = Object.keys(b.spokes).length
          const excerpt = brainExcerpt(md)
          return (
            <Card key={a.id} style={{ padding: 16, cursor: 'pointer' }}>
              <div className="dc-hoverborder" onClick={() => onOpen(a.id)} data-testid="brain-card">
                <div style={{ display: 'flex', alignItems: 'center', gap: 8, marginBottom: 6 }}>
                  <span
                    style={{
                      ...mono,
                      fontWeight: 500,
                      fontSize: 14,
                      flex: 1,
                      overflow: 'hidden',
                      textOverflow: 'ellipsis',
                      whiteSpace: 'nowrap',
                    }}
                  >
                    {a.name}
                  </span>
                  <span style={{ ...mono, fontSize: 10, color: c.muted2 }}>
                    {md.split('\n').length} lines
                    {spokeCount > 0 ? ` · ${spokeCount} note${spokeCount > 1 ? 's' : ''}` : ''}
                  </span>
                </div>
                <div style={{ color: c.muted, fontSize: 12.5, lineHeight: 1.5, minHeight: 38 }}>
                  {excerpt || <i style={{ color: c.muted2 }}>No current state written yet.</i>}
                </div>
                <div style={{ marginTop: 10, color: c.link, fontSize: 12 }}>Open brain →</div>
              </div>
            </Card>
          )
        })}
      </div>

      {without.length > 0 && (
        <div style={{ marginTop: 26 }}>
          <div
            style={{
              fontFamily: font.display,
              fontWeight: 700,
              fontSize: 13,
              color: c.muted,
              marginBottom: 10,
            }}
          >
            No brain yet
          </div>
          <div style={{ display: 'flex', gap: 8, flexWrap: 'wrap' }}>
            {without.map((a) => (
              <span
                key={a.id}
                onClick={() => onOpen(a.id)}
                className="dc-hoverborder"
                style={{
                  ...mono,
                  fontSize: 12,
                  color: c.muted,
                  border: `1px solid ${c.border}`,
                  borderRadius: 7,
                  padding: '5px 11px',
                  cursor: 'pointer',
                }}
              >
                {a.name} +
              </span>
            ))}
          </div>
        </div>
      )}
    </div>
  )
}

function Palette({ apps, close, onGo }: { apps: TApp[]; close: () => void; onGo: (r: Route) => void }) {
  const [q, setQ] = useState('')
  const [selected, setSelected] = useState(0)
  const commands: { label: string; route: Route }[] = [
    { label: 'Build a new project', route: { name: 'apps' } },
    { label: 'Explore the app library', route: { name: 'store' } },
    { label: 'Project memory', route: { name: 'brain' } },
    { label: 'Model connections', route: { name: 'settings', section: 'models' } },
    { label: 'Connect a coding agent', route: { name: 'settings', section: 'agents' } },
    { label: 'Git credentials', route: { name: 'settings', section: 'git' } },
    { label: 'Security & API keys', route: { name: 'settings', section: 'access' } },
    ...apps.map((a) => ({ label: a.name, route: { name: 'app', id: a.id } as Route })),
  ]
  const items = commands.filter((c) => c.label.toLowerCase().includes(q.trim().toLowerCase()))
  return (
    <Dialog title="Jump to…" close={close}>
      <Input
        autoFocus
        aria-label="Search projects and commands"
        placeholder="Search projects, settings, tools…"
        value={q}
        onChange={(e) => {
          setQ(e.target.value)
          setSelected(0)
        }}
        style={{ width: '100%' }}
        onKeyDown={(e) => {
          if (e.key === 'ArrowDown') {
            e.preventDefault()
            setSelected((i) => Math.min(i + 1, items.length - 1))
          }
          if (e.key === 'ArrowUp') {
            e.preventDefault()
            setSelected((i) => Math.max(0, i - 1))
          }
          if (e.key === 'Enter' && items[selected]) {
            e.preventDefault()
            onGo(items[selected].route)
          }
        }}
      />
      <div className="command-list">
        {items.map((c, i) => (
          <button key={c.label} className={i === selected ? 'selected' : ''} onClick={() => onGo(c.route)}>
            <Icon name={c.route.name === 'app' ? 'code' : 'arrow'} size={16} />
            {c.label}
          </button>
        ))}
        {items.length === 0 && <p role="status">No matching projects or commands.</p>}
      </div>
      <p className="field-help">↑ ↓ to choose · Enter to open · Esc to close</p>
    </Dialog>
  )
}
