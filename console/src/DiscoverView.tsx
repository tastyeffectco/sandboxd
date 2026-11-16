import { useEffect, useState } from 'react'
import { api, App as TApp, AgentCatalogEntry, AgentInstance } from './api'
import { CATALOG, CatalogApp, catalogTag } from './catalog'
import { installCatalogApp } from './catalogInstall'
import { Btn } from './design/kit'
import { Icon } from './design/Icon'

// Free apps: popular open-source projects, installed in one click on this
// computer, each in its own project (and, when built from source, ready to be
// changed by the coding agent). Agents are one click away too.
export function DiscoverView({
  apps,
  agents = [],
  onInstalled,
  onOpen,
  goAgents,
  openAgent,
  onError,
  initialCategory,
}: {
  // Category chip selected on open ("Trending" from the home page's "See all").
  initialCategory?: string
  apps: TApp[]
  agents?: AgentInstance[]
  openAgent?: (id: string) => void
  onInstalled: (id: string) => void
  onOpen: (id: string) => void
  goAgents: () => void
  onError: (message: string) => void
}) {
  const [installing, setInstalling] = useState('')
  const [agentPackages, setAgentPackages] = useState<AgentCatalogEntry[]>([])
  useEffect(() => {
    api
      .listAgentCatalogEntrys()
      .then(setAgentPackages)
      .catch(() => {})
  }, [])
  const [category, setCategory] = useState(initialCategory || 'All')
  const categories = ['All', 'Trending', ...Array.from(new Set(CATALOG.map((a) => a.category)))]
  const shown = CATALOG.filter(
    (a) => category === 'All' || a.category === category || (category === 'Trending' && a.trending),
  )

  const install = async (entry: CatalogApp) => {
    if (installing) return
    setInstalling(entry.id)
    try {
      const id = await installCatalogApp(entry)
      onInstalled(id)
    } catch (e) {
      onError((e as Error).message)
    } finally {
      setInstalling('')
    }
  }

  return (
    <div className="content-page discover">
      <section className="discover-hero">
        <div className="eyebrow">
          <span className="brand-dot" /> FREE · OPEN SOURCE · ON YOUR COMPUTER
        </div>
        <h1>Free apps, installed in one click</h1>
        <p>
          Popular open-source apps that thousands of people use every day. They run privately on this computer: no
          account, no subscription, your data stays with you. Apps built from their source code can be changed by your
          coding agent: just ask.
        </p>
      </section>

      <div className="discover-filters" role="tablist" aria-label="Categories">
        {categories.map((c) => (
          <button
            key={c}
            role="tab"
            aria-selected={category === c}
            className={category === c ? 'chip selected' : 'chip'}
            onClick={() => setCategory(c)}
          >
            {c}
          </button>
        ))}
      </div>

      <div className="project-grid discover-grid" data-testid="catalog">
        {shown.map((entry) => {
          const installed = apps.find((a) => (a.tags || []).includes(catalogTag(entry.id)))
          return (
            <article key={entry.id} className="project-card catalog-card" data-testid={`catalog-${entry.id}`}>
              <div className="project-art catalog-art" style={{ background: `${entry.color}1f`, color: entry.color }}>
                <span style={{ color: entry.color }}>{entry.name.slice(0, 1)}</span>
                <span className="catalog-stars" title="Stars on GitHub">
                  ★ {entry.stars}
                </span>
              </div>
              <div className="project-card-body">
                <div>
                  <h3>{entry.name}</h3>
                  <span className="catalog-category">{entry.category}</span>
                </div>
                <p className="catalog-pitch">{entry.pitch}</p>
                <footer>
                  {installed ? (
                    <Btn sm variant="primary" onClick={() => onOpen(installed.id)} data-testid="catalog-open">
                      Open
                    </Btn>
                  ) : (
                    <Btn
                      sm
                      variant="primary"
                      disabled={!!installing}
                      onClick={() => void install(entry)}
                      data-testid="catalog-install"
                    >
                      {installing === entry.id ? 'Installing…' : 'Install'}
                    </Btn>
                  )}
                  <span>
                    {entry.source ? 'Changeable with AI · ' : ''}
                    {entry.license} ·{' '}
                    <a href={entry.repo} target="_blank" rel="noreferrer">
                      code
                    </a>
                  </span>
                </footer>
              </div>
            </article>
          )
        })}
      </div>

      <section className="discover-agents">
        <div>
          <h2>AI agents that work for you</h2>
          <p>
            Open-source assistants that remember what you tell them, browse the web, work on a schedule and build apps
            for you. They run on this computer.
          </p>
        </div>
        <Btn onClick={goAgents} data-testid="discover-agents">
          <Icon name="spark" size={14} /> Add an agent
        </Btn>
      </section>
      {agentPackages.length > 0 && (
        <div className="project-grid discover-grid" data-testid="catalog-agents">
          {agentPackages.map((p) => {
            const added = agents.find((a) => a.package === p.id)
            return (
              <article key={p.id} className="project-card catalog-card" data-testid={`catalog-agent-${p.id}`}>
                <div className="project-card-body">
                  <div>
                    <h3>{p.label}</h3>
                    <span className="catalog-category">AI agent</span>
                  </div>
                  <p className="catalog-pitch">{p.description}</p>
                  <footer>
                    {added && openAgent ? (
                      <Btn sm variant="primary" onClick={() => openAgent(added.id)}>
                        Open
                      </Btn>
                    ) : (
                      <Btn sm variant="primary" onClick={goAgents}>
                        Add
                      </Btn>
                    )}
                    <span>Open source · runs here</span>
                  </footer>
                </div>
              </article>
            )
          })}
        </div>
      )}
      <p className="discover-note">
        Each app is installed from its official release or source code, at a version we tested. The first start takes
        a minute or two.
      </p>
    </div>
  )
}
