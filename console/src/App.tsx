import { useCallback, useEffect, useRef, useState } from 'react'
import { api, setOnUnauthorized, App as TApp, AgentInstance, Settings, UpgradeState } from './api'
import { previewReach, type PreviewReach } from './publicurl'
import { c, font, mono, Card, Input } from './design/kit'
import { AppView } from './AppView'
import { AppBrain, brainExcerpt, buildBrainGraph } from './brain'
import { AgentsView } from './AgentsView'
import { DiscoverView } from './DiscoverView'
import { AssistantView, ASSISTANT_TAG } from './AssistantView'
import { TrendingStrip } from './TrendingStrip'
import { AgentRoom } from './AgentRoom'
import { syncAgentTimeZone } from './timeZone'
import { BrainGraph } from './BrainGraph'
import { SettingsView } from './SettingsView'
import { MyApps } from './MyApps'
import { BuildIntent } from './AgentPicker'
import { Dialog } from './design/Dialog'
import { IS_DEMO } from './demo'
import { Icon, IconName } from './design/Icon'
import { Bot, agentHue, moodFor } from './design/Bot'
import { Avatar } from './design/Avatar'
import { ProjectIcon } from './design/ProjectIcon'
import { EditLook } from './EditLook'
import { catalogIdOf } from './catalog'
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
  if (parts[0] === 'agent' && parts[1]) {
    try {
      return { name: 'agent', id: decodeURIComponent(parts[1]) }
    } catch {
      return { name: 'agents' }
    }
  }
  if (parts[0] === 'settings') return { name: 'settings', section: parts[1] }
  if (parts[0] === 'discover' && parts[1]) return { name: 'discover', category: decodeURIComponent(parts[1]) }
  // Home is the sandboxd agent: the one place to ask for apps, agents and tasks.
  return {
    name: ['brain', 'discover', 'agents', 'settings', 'apps'].includes(parts[0]) ? parts[0] : 'assistant',
  } as Route
}

type Route =
  | { name: 'apps' }
  | { name: 'brain' }
  | { name: 'discover'; category?: string }
  | { name: 'assistant' }
  | { name: 'agents' }
  | { name: 'agent'; id: string }
  | { name: 'settings'; section?: string }
  | { name: 'app'; id: string; tab?: string; task?: string }

// signIn: the host owns this engine's login (the desktop app's own local engine)
// and can restore a session without asking the user for a password.
// localEngine: the engine runs on this computer, so network-sharing hints don't apply.
export default function App({
  signIn,
  localEngine = false,
}: { signIn?: () => Promise<unknown>; localEngine?: boolean } = {}) {
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
  const [chatDrafts, setChatDrafts] = useState<Record<string, BuildIntent>>({})
  const [mobileNav, setMobileNav] = useState(false)
  // Rename & icon, from the sidebar.
  const [editing, setEditing] = useState<{ app?: TApp; agent?: AgentInstance } | null>(null)
  useEffect(() => {
    const listener = () => setRoute(readRoute())
    window.addEventListener('hashchange', listener)
    return () => window.removeEventListener('hashchange', listener)
  }, [])
  useEffect(() => {
    const hash =
      route.name === 'app'
        ? `#/app/${encodeURIComponent(route.id)}${route.tab ? '/' + route.tab : ''}`
        : route.name === 'agent'
          ? `#/agent/${encodeURIComponent(route.id)}`
          : `#/${route.name}${route.name === 'settings' && route.section ? '/' + route.section : ''}${route.name === 'discover' && route.category ? '/' + encodeURIComponent(route.category) : ''}`
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
        .then((list) => setApps(list.filter((a) => !(a.tags || []).includes(ASSISTANT_TAG))))
        .catch(() => {}),
    [],
  )
  // Agents the person added: each has its own window, listed in the sidebar.
  const [agents, setAgents] = useState<AgentInstance[] | null>(null)
  const loadAgents = useCallback(
    () =>
      api
        .listAgentInstances()
        .then(setAgents)
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
      setChatDrafts({})
    })
  }, [refreshAuth])
  useEffect(() => {
    if (auth && (auth.authenticated || auth.enabled === false)) {
      loadApps()
      loadAgents()
      void syncAgentTimeZone().catch(() => {})
    }
  }, [auth, loadApps, loadAgents])
  // A new or restarted agent takes about a minute; refresh until it's ready.
  const agentStarting = agents?.some((a) => a.status === 'starting')
  useEffect(() => {
    if (!agentStarting) return
    const t = setInterval(loadAgents, 3000)
    return () => clearInterval(t)
  }, [agentStarting, loadAgents])
  // Apps change elsewhere too (another device, an agent building one): keep
  // the list current, and catch up when the window is back in front.
  useEffect(() => {
    if (!auth || !(auth.authenticated || auth.enabled === false)) return
    const t = setInterval(loadApps, 30000)
    window.addEventListener('focus', loadApps)
    return () => {
      clearInterval(t)
      window.removeEventListener('focus', loadApps)
    }
  }, [auth, loadApps])
  // Agents also stop or start on their own (engine restart, low memory, a crash):
  // keep the list honest, and catch up as soon as the window is back in front.
  useEffect(() => {
    if (!agents?.length) return
    const t = setInterval(loadAgents, 15000)
    window.addEventListener('focus', loadAgents)
    return () => {
      clearInterval(t)
      window.removeEventListener('focus', loadAgents)
    }
  }, [agents?.length, loadAgents])
  // 'failed' falls back to the password form (e.g. the user changed it).
  const [hostSignIn, setHostSignIn] = useState<'idle' | 'trying' | 'failed'>('idle')
  useEffect(() => {
    if (!signIn || !auth?.enabled || auth.authenticated || hostSignIn !== 'idle') return
    setHostSignIn('trying')
    signIn()
      .catch(() => {})
      .then(() => api.authStatus())
      .then((next) => {
        setAuth(next)
        setHostSignIn(next.authenticated ? 'idle' : 'failed')
      })
      .catch(() => setHostSignIn('failed'))
  }, [signIn, auth, hostSignIn])
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
        if (
          !IS_DEMO &&
          !localEngine &&
          r !== 'ok' &&
          localStorage.getItem('sandboxd-public-url-hint') !== 'dismissed'
        )
          setReach(r)
      })
      .catch(() => {})
  }, [auth, loadSettings, localEngine])
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
    setChatDrafts({})
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

  // Home, with the cursor in sandboxd's box (it may still be getting ready).
  const askSandboxd = () => {
    setRoute({ name: 'assistant' })
    setMobileNav(false)
    let tries = 0
    const focus = () => {
      const box = document.getElementById('chat-prompt') as HTMLTextAreaElement | null
      if (box) return box.focus()
      if (++tries < 40) setTimeout(focus, 100)
    }
    setTimeout(focus, 0)
  }
  const nav = [
    { key: 'assistant', label: 'Home', icon: 'spark' },
    { key: 'apps', label: 'My apps', icon: 'grid' },
    { key: 'discover', label: 'Free apps', icon: 'spark' },
    { key: 'brain', label: 'Project memory', icon: 'book' },
    { key: 'settings', label: 'Settings', icon: 'settings' },
  ]

  // Auth gate — render before the app chrome. null = still resolving status.
  if (auth === null || (auth.enabled && !auth.authenticated && signIn && hostSignIn !== 'failed')) {
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
        <strong>
          <Bot who="sandboxd" size={24} still /> sandboxd
        </strong>
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
          <button className="studio-brand" onClick={() => setRoute({ name: 'assistant' })}>
            <Bot who="sandboxd" size={40} />
            <strong>
              sandboxd<small>Your AI crew, on your machine</small>
            </strong>
          </button>
          <button className="sidebar-search" onClick={() => setPaletteOpen(true)}>
            <Icon name="search" size={16} />
            <span>Quick search</span>
            <kbd>{/Mac|iPhone|iPad/.test(navigator.platform) ? '⌘' : 'Ctrl'} K</kbd>
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
                {n.key === 'assistant' ? <Bot who="sandboxd" size={20} still /> : <Icon name={n.icon as IconName} />}
                <span>{n.label}</span>
              </button>
            ))}
          </nav>
          <div className="sidebar-projects" data-testid="sidebar-agents">
            <div className="sidebar-caption">AGENTS</div>
            {(agents || []).map((a) => (
              <div className="sidebar-row" key={a.id}>
                <button
                  title={a.name}
                  data-testid={`sidebar-agent-${a.id}`}
                  className={
                    route.name === 'agent' && route.id === a.id
                      ? 'sidebar-project sidebar-agent active'
                      : 'sidebar-project sidebar-agent'
                  }
                  onClick={() => setRoute({ name: 'agent', id: a.id })}
                >
                  <Avatar
                    who={a.package}
                    hue={agentHue(agents || [], a)}
                    mood={moodFor(a.status)}
                    size={26}
                    still={a.status !== 'running'}
                  />
                  <span>{a.name}</span>
                  <i className={`agent-dot ${a.status}`} aria-label={a.status} />
                </button>
                <button
                  className="sidebar-edit"
                  aria-label={`Rename or change the icon of ${a.name}`}
                  title="Rename & icon"
                  onClick={() => setEditing({ agent: a })}
                  data-testid={`edit-agent-${a.id}`}
                >
                  <Icon name="edit" size={13} />
                </button>
              </div>
            ))}
            <button
              className={route.name === 'agents' ? 'sidebar-project active' : 'sidebar-project'}
              onClick={() => setRoute({ name: 'agents' })}
              data-testid="sidebar-add-agent"
            >
              <span className="project-initial" aria-hidden="true">
                <Icon name="plus" size={12} />
              </span>
              <span>Add an agent</span>
            </button>
          </div>
          <div className="sidebar-projects">
            <div className="sidebar-caption">RECENT APPS</div>
            {apps.slice(0, 7).map((a) => (
              <div className="sidebar-row" key={a.id}>
                <button
                  title={a.name}
                  className={
                    route.name === 'app' && route.id === a.id ? 'sidebar-project active' : 'sidebar-project'
                  }
                  onClick={() => goApp(a.id)}
                >
                  <ProjectIcon name={a.name} tags={a.tags} logo={catalogIdOf(a.tags)} size={22} still />
                  <span>{a.name}</span>
                </button>
                <button
                  className="sidebar-edit"
                  aria-label={`Rename or change the icon of ${a.name}`}
                  title="Rename & icon"
                  onClick={() => setEditing({ app: a })}
                  data-testid={`edit-app-${a.id}`}
                >
                  <Icon name="edit" size={13} />
                </button>
              </div>
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
            {auth.enabled && !signIn && (
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
          className={
            // Workspaces and chats fill the window and scroll inside (the conversation, not the page).
            route.name === 'app' || route.name === 'agent' || route.name === 'assistant'
              ? 'studio-main workspace-main'
              : 'studio-main'
          }
        >
          {route.name === 'apps' && (
            <MyApps
              apps={apps}
              onAsk={askSandboxd}
              onOpen={(id) => goApp(id)}
              featured={
                <TrendingStrip
                  apps={apps}
                  onSeeAll={() => setRoute({ name: 'discover', category: 'Trending' })}
                  onError={onError}
                  onOpen={(id) => goApp(id)}
                  onInstalled={(id) => {
                    loadApps()
                    goApp(id)
                  }}
                />
              }
            />
          )}
          {route.name === 'discover' && (
            <DiscoverView
              key={route.category || 'All'}
              initialCategory={route.category}
              apps={apps}
              agents={agents || []}
              openAgent={(id) => setRoute({ name: 'agent', id })}
              onError={onError}
              onOpen={(id) => goApp(id)}
              goAgents={() => setRoute({ name: 'agents' })}
              onInstalled={(id) => {
                loadApps()
                goApp(id)
              }}
            />
          )}
          {route.name === 'assistant' && (
            <AssistantView
              onError={onError}
              toast={toast}
              goApps={() => setRoute({ name: 'apps' })}
              goSettings={() => setRoute({ name: 'settings' })}
              apps={apps}
              onOpenApp={(id, tab) => goApp(id, tab)}
              reloadApps={loadApps}
            />
          )}
          {route.name === 'brain' && <BrainOverview apps={apps} onOpen={(id) => goApp(id, 'brain')} />}
          {route.name === 'agents' && (
            <AgentsView
              onError={onError}
              agents={agents || []}
              goModels={() => setRoute({ name: 'settings', section: 'models' })}
              onOpen={(id) => {
                loadAgents()
                setRoute({ name: 'agent', id })
              }}
            />
          )}
          {route.name === 'agent' &&
            (() => {
              const agent = agents?.find((a) => a.id === route.id)
              if (!agent)
                return (
                  agents && (
                    <div className="content-page">
                      <p>This agent was removed.</p>
                      <button className="text-button" onClick={() => setRoute({ name: 'agents' })}>
                        Add an agent
                      </button>
                    </div>
                  )
                )
              return (
                <AgentRoom
                  key={agent.id}
                  agent={agent}
                  hue={agentHue(agents || [], agent)}
                  onChanged={loadAgents}
                  onRemoved={() => {
                    loadAgents()
                    setRoute({ name: 'agents' })
                  }}
                  onError={onError}
                  reloadApps={loadApps}
                />
              )
            })()}
          {route.name === 'settings' && (
            <SettingsView initialSection={route.section} onError={onError} toast={toast} />
          )}
          {route.name === 'app' && (
            <AppView
              key={route.id}
              appId={route.id}
              reloadApps={loadApps}
              chatDraft={chatDrafts[route.id]}
              saveChatDraft={(draft) => setChatDrafts((p) => ({ ...p, [route.id]: draft }))}
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

      {editing?.app && (
        <EditLook
          app={editing.app}
          onClose={() => setEditing(null)}
          onError={onError}
          onSaved={() => {
            loadApps()
            toast('Saved')
          }}
        />
      )}
      {editing?.agent && (
        <EditLook
          agent={editing.agent}
          onClose={() => setEditing(null)}
          onError={onError}
          onSaved={() => {
            loadAgents()
            toast('Saved')
          }}
        />
      )}
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
        <div className="toast-stack">
          {toasts.map((t) => (
            <div role="status" key={t.id} className="toast">
              <Bot who="sandboxd" mood="happy" size={26} still />
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
      <header className="page-hero">
        <div className="page-hero-text">
          <span className="eyebrow">What your crew remembers</span>
          <h1>Project memory</h1>
          <p>
            Every project's memory in one place: its state, decisions and lessons. Each app's{' '}
            <code>BRAIN.md</code> is read by its agent before every task and updated as it learns.
          </p>
        </div>
        <div className="page-hero-crew" aria-hidden="true">
          <Bot who="sandboxd" mood="thinking" size={84} />
        </div>
      </header>

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
            <Card className="brain-graph-card" style={{ padding: '8px 10px', marginBottom: 22 }}>
              <BrainGraph nodes={g.nodes} edges={g.edges} onOpen={onOpen} />
              <div
                style={{ fontSize: 12, color: c.muted2, textAlign: 'center', paddingBottom: 8 }}
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

      <div className="brain-list" data-testid="brain-list">
        {withBrain.map((a) => {
          const b = brains[a.id] as AppBrain
          const md = b.hub || ''
          const spokeCount = Object.keys(b.spokes).length
          const excerpt = brainExcerpt(md)
          return (
            <button key={a.id} className="brain-card" onClick={() => onOpen(a.id)} data-testid="brain-card">
              <header>
                <ProjectIcon name={a.name} tags={a.tags} logo={catalogIdOf(a.tags)} size={30} still />
                <strong>{a.name}</strong>
                <small>
                  {md.split('\n').length} lines
                  {spokeCount > 0 ? ` · ${spokeCount} note${spokeCount > 1 ? 's' : ''}` : ''}
                </small>
              </header>
              <p>{excerpt || <i>No current state written yet.</i>}</p>
              <span className="text-button">Open brain →</span>
            </button>
          )
        })}
      </div>

      {without.length > 0 && (
        <div className="brain-without">
          <h2>No brain yet</h2>
          <div>
            {without.map((a) => (
              <button key={a.id} className="chip" onClick={() => onOpen(a.id)}>
                <ProjectIcon name={a.name} tags={a.tags} logo={catalogIdOf(a.tags)} size={18} still mood="sleeping" />
                {a.name}
              </button>
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
    { label: 'Ask sandboxd', route: { name: 'assistant' } },
    { label: 'My apps', route: { name: 'apps' } },
    { label: 'Project memory', route: { name: 'brain' } },
    { label: 'Free apps', route: { name: 'discover' } },
    { label: 'Add an agent', route: { name: 'agents' } },
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
