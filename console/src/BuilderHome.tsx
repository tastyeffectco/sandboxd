import type React from 'react'
import { useEffect, useMemo, useRef, useState } from 'react'
import { api, App, Agent, Preset, ModelProvider } from './api'
import { AgentPicker, BuildIntent, choiceBlocked, defaultChoice } from './AgentPicker'
import { CATALOG, catalogIdOf } from './catalog'
import { Icon } from './design/Icon'
import { Avatar } from './design/Avatar'
import { ProjectIcon } from './design/ProjectIcon'
import { Btn, Input, StatusPill } from './design/kit'
import { PRESET_ICONS } from './design/presetIcons'
import { appTitle } from './appTitle'
import { STACK_LABELS, stackFromIdea } from './stackFromIdea'

const ideas = [
  {
    title: 'A personal portfolio',
    prompt:
      'Build a polished personal portfolio with an introduction, project gallery, about section and contact form.',
  },
  {
    title: 'A team dashboard',
    prompt:
      'Build a team project dashboard with a kanban board, task filtering and a clean responsive layout.',
  },
  {
    title: 'A beautiful landing page',
    prompt:
      'Build a modern landing page for a small creative studio, with services, selected work, testimonials and a contact section.',
  },
]

export type BuilderDraft = {
  prompt: string
  name: string
  choice: typeof defaultChoice
  preset: string
}
export function BuilderHome({
  apps,
  reload,
  onOpen,
  onCreated,
  goSettings,
  goDiscover,
  featured,
  draft,
  saveDraft,
  onAsk,
}: {
  // Ask the sandboxd agent (Home) for a new app.
  onAsk?: () => void
  apps: App[]
  reload: () => void
  onOpen: (id: string) => void
  onCreated: (id: string, intent?: BuildIntent) => void
  goSettings: () => void
  goDiscover?: () => void
  // Shown between the build box and the project list (Trending on GitHub).
  featured?: React.ReactNode
  draft?: BuilderDraft
  saveDraft: (draft?: BuilderDraft) => void
}) {
  const [prompt, setPrompt] = useState(draft?.prompt || '')
  const [name, setName] = useState(draft?.name || '')
  const [choice, setChoice] = useState(draft?.choice || defaultChoice)
  const [presets, setPresets] = useState<Preset[]>([])
  const [agents, setAgents] = useState<Agent[]>([])
  const [providers, setProviders] = useState<ModelProvider[]>([])
  const [preset, setPreset] = useState(draft?.preset || '')
  const [panel, setPanel] = useState<'stack' | 'agent' | 'project' | null>(null)
  const [busy, setBusy] = useState('')
  const [error, setError] = useState('')
  const [query, setQuery] = useState('')
  const [status, setStatus] = useState<Record<string, string>>({})
  // Automatic picks the stack from the idea; a stack chosen by hand wins.
  const guess = useMemo(() => stackFromIdea(prompt), [prompt])
  const creating = useRef(false)
  const composer = useRef<HTMLTextAreaElement>(null)
  useEffect(() => {
    saveDraft({ prompt, name, choice, preset })
  }, [prompt, name, choice, preset, saveDraft])

  useEffect(() => {
    let alive = true
    Promise.all([api.listPresets(), api.getAgents(), api.listModelProviders()])
      .then(([p, a, m]) => {
        if (alive) {
          setPresets(p)
          setAgents(a)
          setProviders(m)
        }
      })
      .catch(() => {
        if (alive) setError('Some build options could not load. Refresh the page to try again.')
      })
    return () => {
      alive = false
    }
  }, [])
  useEffect(() => {
    let alive = true
    Promise.all(
      apps
        .filter((a) => a.current_sandbox_id)
        .map(async (a) => {
          try {
            return [a.id, (await api.getSandbox(a.current_sandbox_id!)).status]
          } catch {
            return [a.id, 'unknown']
          }
        }),
    ).then((s) => {
      if (alive) setStatus(Object.fromEntries(s))
    })
    return () => {
      alive = false
    }
  }, [apps])

  // Every new app starts from an idea: create it, then the agent builds the prompt.
  const create = async () => {
    if (creating.current || !prompt.trim() || choiceBlocked(choice, providers, agents)) return
    creating.current = true
    setError('')
    setBusy('Creating your app…')
    const title = name.trim() || appTitle(prompt)
    try {
      const app = await api.createApp({
        name: title.slice(0, 80),
        description: prompt.trim().slice(0, 2000),
        // React / Vite is the default starter, so Automatic only sends a stack
        // the idea asks for.
        runtime_preset: preset || (guess.preset !== 'react-vite' ? guess.preset : undefined),
      })
      setBusy('Preparing the workspace…')
      // The app exists even when boot fails. Open it with the draft intact so
      // users can retry starting it, without accidentally creating another app.
      try {
        await api.createAppSandbox(app.id)
      } catch {
        /* workspace offers an explicit retry */
      }
      reload()
      saveDraft(undefined)
      onCreated(app.id, { ...choice, prompt: prompt.trim() })
    } catch (e) {
      setError((e as Error).message)
    } finally {
      creating.current = false
      setBusy('')
    }
  }
  const toggle = (p: typeof panel) => setPanel(panel === p ? null : p)
  const filtered = apps.filter((a) =>
    `${a.name} ${a.description}`.toLowerCase().includes(query.toLowerCase()),
  )
  const blocked = !!prompt.trim() && choiceBlocked(choice, providers, agents)
  return (
    <div className="builder-home">
      <section className="projects-section">
        <div className="section-heading">
          <div>
            <span className="eyebrow">Your work</span>
            <h1>
              Projects <span>{apps.length}</span>
            </h1>
            <p>Every app you or your crew built. Open one to see it live, change it or chat with its agent.</p>
          </div>
          {onAsk && (
            <Btn variant="primary" onClick={onAsk} data-testid="projects-ask">
              <Icon name="plus" size={14} /> New app
            </Btn>
          )}
          <label className="project-search">
            <Icon name="search" size={16} />
            <input
              aria-label="Search projects"
              placeholder="Find a project…"
              value={query}
              onChange={(e) => setQuery(e.target.value)}
            />
          </label>
        </div>
        {/* No projects yet: the box below is the one way to start, as everywhere else. */}
        {apps.length === 0 ? null : filtered.length === 0 ? (
          <p className="empty-state">No projects match “{query}”.</p>
        ) : (
          <div className="project-grid" data-testid="app-list">
            {filtered.map((a, i) => (
              <button key={a.id} className="project-card" onClick={() => onOpen(a.id)} data-testid="app-card">
                <div className="project-art" style={{ ['--art' as string]: `${(i * 47) % 360}` }}>
                  <ProjectIcon
                    name={a.name}
                    tags={a.tags}
                    logo={catalogIdOf(a.tags)}
                    size={64}
                    still
                    mood={status[a.id] === 'running' ? 'idle' : 'sleeping'}
                  />
                  <Icon name={a.git ? 'git' : 'code'} size={16} />
                </div>
                <div className="project-card-body">
                  <div>
                    <h3>{a.name}</h3>
                    <Icon name="arrow" size={17} />
                  </div>
                  <p>{a.description || (a.git ? a.git.repo_url : 'Ready for your next idea')}</p>
                  <footer>
                    <StatusPill status={a.current_sandbox_id ? status[a.id] || 'loading' : 'not started'} />
                    <span>{a.runtime_preset || (a.git ? 'Imported project' : 'App workspace')}</span>
                  </footer>
                </div>
              </button>
            ))}
          </div>
        )}
      </section>
      <section className="build-hero build-yourself">
        <h2>What should your crew build?</h2>
        <p>Describe it in your own words. Your crew builds it and shows it to you here.</p>
        <div className="build-composer">
          <label className="sr-only" htmlFor="build-prompt">
            Describe your app
          </label>
          <textarea
            ref={composer}
            id="build-prompt"
            data-testid="build-prompt"
            value={prompt}
            maxLength={16000}
            onChange={(e) => setPrompt(e.target.value)}
            disabled={!!busy}
            onKeyDown={(e) => {
              if ((e.metaKey || e.ctrlKey) && e.key === 'Enter' && prompt.trim()) {
                e.preventDefault()
                void create()
              }
            }}
            placeholder="Describe an app, a website, a tool… What should it do?"
          />
          <div className="composer-controls">
            <div className="composer-options">
              <button
                className={panel === 'stack' ? 'chip selected' : 'chip'}
                aria-expanded={panel === 'stack'}
                disabled={!!busy}
                onClick={() => toggle('stack')}
                data-testid="stack-toggle"
              >
                <Icon name="box" size={15} />
                {presets.find((p) => p.id === preset)?.label ||
                  (prompt.trim() ? `${STACK_LABELS[guess.preset]} · Auto` : 'Choose stack')}
                <Icon name="chevron" size={13} />
              </button>
              <button
                className={panel === 'agent' ? 'chip selected' : 'chip'}
                aria-expanded={panel === 'agent'}
                onClick={() => toggle('agent')}
                disabled={!!busy}
              >
                <Icon name="spark" size={15} />
                {choice.agent === 'opencode'
                  ? 'OpenCode'
                  : choice.agent === 'pi'
                    ? 'Pi'
                    : choice.agent === 'codex'
                      ? 'Codex'
                      : 'Claude Code'}
                <Icon name="chevron" size={13} />
              </button>
              <button
                className="icon-button"
                aria-label="Project options"
                aria-expanded={panel === 'project'}
                onClick={() => toggle('project')}
                disabled={!!busy}
              >
                <Icon name="settings" size={17} />
              </button>
            </div>
            <button
              className="build-button"
              onClick={() => void create()}
              disabled={!!busy || blocked || !prompt.trim()}
              data-testid="build-app"
            >
              {busy || 'Build app'}
              <Icon name="arrow" size={18} />
            </button>
          </div>
          {panel && (
            <div className="composer-panel">
              <div className="panel-heading">
                <strong>
                  {
                    {
                      stack: 'Start with the right foundation',
                      agent: 'Your coding agent & model',
                      project: 'Project details',
                    }[panel]
                  }
                </strong>
                <button
                  className="icon-button"
                  aria-label="Close build options"
                  onClick={() => setPanel(null)}
                >
                  <Icon name="close" size={16} />
                </button>
              </div>
              {panel === 'stack' && (
                <div className="stack-grid" data-testid="app-preset">
                  <button
                    className={!preset ? 'stack-option selected' : 'stack-option'}
                    onClick={() => setPreset('')}
                  >
                    <Icon name="spark" />
                    <strong>Automatic</strong>
                    <span>
                      Picked from your idea: Next.js, Express, FastAPI or a background worker when you mention them,
                      otherwise React / Vite.
                    </span>
                  </button>
                  {presets.map((p) => (
                    <button
                      key={p.id}
                      data-testid={`preset-${p.id}`}
                      className={preset === p.id ? 'stack-option selected' : 'stack-option'}
                      onClick={() => setPreset(p.id)}
                      aria-pressed={preset === p.id}
                    >
                      <span
                        className="prov-ico"
                        dangerouslySetInnerHTML={{ __html: PRESET_ICONS[p.id] || '' }}
                      />
                      <strong>{p.label}</strong>
                      <span>{p.description}</span>
                    </button>
                  ))}
                </div>
              )}
              {panel === 'agent' && (
                <>
                  <AgentPicker value={choice} onChange={setChoice} providers={providers} agents={agents} />
                  <button className="text-button" onClick={goSettings}>
                    Manage agents & connections <Icon name="arrow" size={14} />
                  </button>
                </>
              )}
              {panel === 'project' && (
                <label className="form-field">
                  App name{' '}
                  <Input
                    value={name}
                    onChange={(e) => setName(e.target.value)}
                    placeholder="Optional — we’ll use your idea"
                    maxLength={80}
                    data-testid="app-name"
                  />
                </label>
              )}
            </div>
          )}
        </div>
        {!preset && guess.note && !busy && (
          <p role="status" className={guess.tone === 'warn' ? 'stack-note warn' : 'stack-note'} data-testid="stack-note">
            {guess.note}
          </p>
        )}
        {busy && (
          <p role="status" className="build-status">
            {busy} Your prompt will run when the workspace is ready.
          </p>
        )}
        {error && (
          <p role="alert" className="inline-error">
            {error}
          </p>
        )}
        {blocked && (
          <p role="status" className="inline-error">
            Connect {choice.agent === 'pi' ? 'a model for Pi' : 'your agent'} to build.{' '}
            <button className="text-button" onClick={goSettings}>
              Open Settings
            </button>
          </p>
        )}
        <div className="idea-chips">
          {ideas.map((i) => (
            <button
              key={i.title}
              disabled={!!busy}
              onClick={() => {
                setPrompt(i.prompt)
                composer.current?.focus()
              }}
            >
              {i.title}
              <Icon name="arrow" size={13} />
            </button>
          ))}
        </div>
        {goDiscover && (
          <p className="free-apps-line">
            Or install a popular free app in one click:{' '}
            {CATALOG.filter((a) => !a.trending).slice(0, 3).map((a, i) => (
              <span key={a.id}>
                {i > 0 && ', '}
                <button className="text-button" onClick={goDiscover} data-testid={`home-free-${a.id}`}>
                  {a.name}
                </button>
              </span>
            ))}
            {' … '}
            <button className="text-button" onClick={goDiscover} data-testid="home-free-apps">
              see all free apps
            </button>
          </p>
        )}
      </section>
      {featured}
      <div className="capability-strip">
        <span>
          <Icon name="monitor" />
          Live previews
        </span>
        <span>
          <Icon name="code" />
          Code & terminal
        </span>
        <span>
          <Icon name="history" />
          Snapshots & restore
        </span>
        <span>
          <Icon name="lock" />
          Your infrastructure
        </span>
      </div>
    </div>
  )
}
