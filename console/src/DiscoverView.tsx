import { useState } from 'react'
import { api, App as TApp } from './api'
import { BuildIntent, defaultChoice } from './AgentPicker'
import { CATALOG, CatalogApp, catalogTag, installPrompt } from './catalog'
import { Btn } from './design/kit'
import { Icon } from './design/Icon'

// Free apps: popular open-source projects, installed in one click on this
// computer, each in its own project (and, when built from source, ready to be
// changed by the coding agent). Agents are one click away too.
export function DiscoverView({
  apps,
  onInstalled,
  onOpen,
  goAgents,
  onError,
}: {
  apps: TApp[]
  onInstalled: (id: string, intent: BuildIntent) => void
  onOpen: (id: string) => void
  goAgents: () => void
  onError: (message: string) => void
}) {
  const [installing, setInstalling] = useState('')
  const [category, setCategory] = useState('All')
  const categories = ['All', ...Array.from(new Set(CATALOG.map((a) => a.category)))]
  const shown = CATALOG.filter((a) => category === 'All' || a.category === category)

  const install = async (entry: CatalogApp) => {
    if (installing) return
    setInstalling(entry.id)
    try {
      const app = await api.createApp({
        name: entry.name,
        description: entry.pitch,
        tags: [catalogTag(entry.id)],
        ...(entry.git ? { git: entry.git } : {}),
      })
      // Downloaded apps start from an empty project; source apps from their code.
      await api.createAppSandbox(app.id, entry.git ? {} : { template: 'blank' })
      onInstalled(app.id, { ...defaultChoice, prompt: installPrompt(entry) })
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
          <h2>Want an assistant that works for you?</h2>
          <p>
            Agents remember what you tell them, browse the web, work on a schedule and build apps for you. Open
            source, running on this computer.
          </p>
        </div>
        <Btn onClick={goAgents} data-testid="discover-agents">
          <Icon name="spark" size={14} /> Add an agent
        </Btn>
      </section>
      <p className="discover-note">
        Each app is installed from its official release or source code, at a version we tested. The first start takes
        a minute or two.
      </p>
    </div>
  )
}
