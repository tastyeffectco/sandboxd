import { useEffect, useRef, useState } from 'react'
import { api, App, Agent, Preset, GitCredential, ModelProvider } from './api'
import { AgentPicker, BuildIntent, choiceBlocked, defaultChoice } from './AgentPicker'
import { Icon } from './design/Icon'
import { Btn, Input, StatusPill } from './design/kit'
import { PRESET_ICONS } from './design/presetIcons'
import { STARTERS } from './design/starters'

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
  repo: string
  branch: string
  credential: string
  starter: string
}
export function BuilderHome({
  apps,
  reload,
  onOpen,
  onCreated,
  goStore,
  goSettings,
  draft,
  saveDraft,
}: {
  apps: App[]
  reload: () => void
  onOpen: (id: string) => void
  onCreated: (id: string, intent?: BuildIntent) => void
  goStore: () => void
  goSettings: () => void
  draft?: BuilderDraft
  saveDraft: (draft?: BuilderDraft) => void
}) {
  const [prompt, setPrompt] = useState(draft?.prompt || '')
  const [name, setName] = useState(draft?.name || '')
  const [choice, setChoice] = useState(draft?.choice || defaultChoice)
  const [presets, setPresets] = useState<Preset[]>([])
  const [agents, setAgents] = useState<Agent[]>([])
  const [providers, setProviders] = useState<ModelProvider[]>([])
  const [creds, setCreds] = useState<GitCredential[]>([])
  const [preset, setPreset] = useState(draft?.preset || '')
  const [repo, setRepo] = useState(draft?.repo || '')
  const [branch, setBranch] = useState(draft?.branch || 'main')
  const [credential, setCredential] = useState(draft?.credential || '')
  const [starter, setStarter] = useState(draft?.starter || '')
  const [panel, setPanel] = useState<'stack' | 'agent' | 'git' | 'project' | 'starter' | null>(null)
  const [busy, setBusy] = useState('')
  const [error, setError] = useState('')
  const [query, setQuery] = useState('')
  const [status, setStatus] = useState<Record<string, string>>({})
  const creating = useRef(false)
  const composer = useRef<HTMLTextAreaElement>(null)
  useEffect(() => {
    saveDraft({ prompt, name, choice, preset, repo, branch, credential, starter })
  }, [prompt, name, choice, preset, repo, branch, credential, starter, saveDraft])

  useEffect(() => {
    let alive = true
    Promise.all([api.listPresets(), api.getAgents(), api.listModelProviders(), api.listGitCredentials()])
      .then(([p, a, m, g]) => {
        if (alive) {
          setPresets(p)
          setAgents(a)
          setProviders(m)
          setCreds(g)
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

  const create = async (build = true) => {
    if (creating.current || (build && choiceBlocked(choice, providers, agents))) return
    if (panel === 'git' && !repo.trim()) {
      setError('Enter a repository URL to import.')
      return
    }
    creating.current = true
    setError('')
    setBusy('Creating your app…')
    const selectedStarter = STARTERS.find((s) => s.id === starter)
    const fromRepo = repo.trim() || (selectedStarter ? `https://github.com/${selectedStarter.repo}` : '')
    const title =
      name.trim() ||
      (fromRepo
        ? fromRepo
            .split('/')
            .pop()
            ?.replace(/\.git$/, '')
        : '') ||
      prompt.trim().split(/\s+/).slice(0, 6).join(' ') ||
      'Untitled app'
    try {
      const app = await api.createApp({
        name: title.slice(0, 80),
        description: prompt.trim().slice(0, 2000),
        runtime_preset: fromRepo ? undefined : preset || undefined,
        git: fromRepo
          ? {
              repo_url: fromRepo,
              branch: selectedStarter?.branch || branch.trim() || 'main',
              ...(credential && !selectedStarter ? { credential_id: credential } : {}),
            }
          : undefined,
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
      onCreated(app.id, build && prompt.trim() ? { ...choice, prompt: prompt.trim() } : undefined)
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
      <section className="build-hero">
        <div className="eyebrow">
          <span className="brand-dot" /> YOUR IDEAS. YOUR INFRASTRUCTURE.
        </div>
        <h1>
          What do you want
          <br className="hero-break" /> to build?
        </h1>
        <p>Start with an idea. Bring it to life with your coding agent.</p>
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
                disabled={!!repo || !!starter || !!busy}
                onClick={() => toggle('stack')}
                data-testid="stack-toggle"
              >
                <Icon name="box" size={15} />
                {presets.find((p) => p.id === preset)?.label || 'Choose stack'}
                <Icon name="chevron" size={13} />
              </button>
              <button
                className={panel === 'agent' ? 'chip selected' : 'chip'}
                aria-expanded={panel === 'agent'}
                onClick={() => toggle('agent')}
                disabled={!!busy}
              >
                <Icon name="spark" size={15} />
                {choice.agent === 'opencode' ? 'OpenCode' : choice.agent === 'pi' ? 'Pi' : 'Claude Code'}
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
              disabled={!!busy || blocked || (!prompt.trim() && !repo && !starter)}
              data-testid="build-app"
            >
              {busy || (prompt.trim() || (!repo && !starter) ? 'Build app' : 'Create app')}
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
                      git: 'Import a repository',
                      project: 'Project details',
                      starter: 'Start from a template',
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
                    <span>Use the default starter. Detect the runtime as you build.</span>
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
              {panel === 'git' && (
                <div className="form-grid">
                  <label className="form-field full-width">
                    Repository URL
                    <Input
                      type="url"
                      value={repo}
                      onChange={(e) => {
                        setRepo(e.target.value)
                        setStarter('')
                      }}
                      placeholder="https://github.com/you/project"
                      data-testid="git-repo-url"
                    />
                  </label>
                  <label className="form-field">
                    Branch
                    <Input
                      value={branch}
                      onChange={(e) => setBranch(e.target.value)}
                      data-testid="git-branch"
                    />
                  </label>
                  <label className="form-field">
                    Git credential
                    <select
                      value={credential}
                      onChange={(e) => setCredential(e.target.value)}
                      data-testid="git-cred"
                    >
                      <option value="">Public repository</option>
                      {creds.map((c) => (
                        <option key={c.id} value={c.id}>
                          {c.name}
                        </option>
                      ))}
                    </select>
                  </label>
                  <p className="field-help full-width">
                    Private repository?{' '}
                    <button className="text-button" onClick={goSettings}>
                      Add a credential in Settings
                    </button>
                    . The runtime is detected from your repository.
                  </p>
                </div>
              )}
              {panel === 'starter' && (
                <div className="starter-options">
                  {STARTERS.map((s) => (
                    <button
                      key={s.id}
                      className={s.id === starter ? 'stack-option selected' : 'stack-option'}
                      onClick={() => {
                        setStarter(s.id)
                        setRepo('')
                      }}
                    >
                      <strong>{s.name}</strong>
                      <span>{s.blurb}</span>
                    </button>
                  ))}
                </div>
              )}
            </div>
          )}
          {(repo || starter) && (
            <div className="import-summary">
              <Icon name="git" size={15} />
              <span>{repo || STARTERS.find((s) => s.id === starter)?.name}</span>
              <button
                className="text-button"
                onClick={() => {
                  setRepo('')
                  setStarter('')
                  setCredential('')
                }}
              >
                Clear import
              </button>
            </div>
          )}
        </div>
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
        <div className="start-paths">
          <button onClick={() => toggle('git')} disabled={!!busy}>
            <Icon name="git" size={16} /> Import from Git
          </button>
          <button onClick={() => toggle('starter')} disabled={!!busy}>
            <Icon name="box" size={16} /> Use a starter
          </button>
          <button onClick={() => void create(false)} disabled={!!busy}>
            <Icon name="plus" size={16} /> Blank project
          </button>
        </div>
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
      </section>
      <section className="projects-section">
        <div className="section-heading">
          <div>
            <h2>
              Your projects <span>{apps.length}</span>
            </h2>
            <p>Pick up where you left off.</p>
          </div>
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
        {apps.length === 0 ? (
          <div className="empty-projects">
            <Icon name="grid" size={26} />
            <h3>A home for everything you build</h3>
            <p>Your projects, live previews and conversations will appear here.</p>
            <Btn onClick={goStore}>Explore the app library</Btn>
          </div>
        ) : filtered.length === 0 ? (
          <p className="empty-state">No projects match “{query}”.</p>
        ) : (
          <div className="project-grid" data-testid="app-list">
            {filtered.map((a, i) => (
              <button key={a.id} className="project-card" onClick={() => onOpen(a.id)} data-testid="app-card">
                <div className={`project-art art-${i % 4}`}>
                  <span>{a.name.slice(0, 1).toUpperCase()}</span>
                  <Icon name={a.git ? 'git' : 'code'} size={22} />
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
