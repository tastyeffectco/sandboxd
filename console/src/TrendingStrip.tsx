import { useState } from 'react'
import { App as TApp } from './api'
import { BuildIntent, defaultChoice } from './AgentPicker'
import { CatalogApp, TRENDING, catalogTag, installPrompt } from './catalog'
import { installCatalogApp } from './catalogInstall'
import { Btn } from './design/kit'

// "Trending on GitHub" on the home page: projects everyone talks about right
// now, ready in one click, in words a non-technical person understands.
export function TrendingStrip({
  apps,
  onInstalled,
  onOpen,
  onError,
  items = TRENDING,
  limit = 6,
  onSeeAll,
}: {
  apps: TApp[]
  onInstalled: (id: string, intent: BuildIntent) => void
  onOpen: (id: string) => void
  onError: (message: string) => void
  items?: CatalogApp[]
  // How many to show; "See all" opens the rest.
  limit?: number
  onSeeAll?: () => void
}) {
  const [installing, setInstalling] = useState('')
  if (items.length === 0) return null

  const install = async (entry: CatalogApp) => {
    if (installing) return
    setInstalling(entry.id)
    try {
      const id = await installCatalogApp(entry)
      onInstalled(id, { ...defaultChoice, prompt: installPrompt(entry) })
    } catch (e) {
      onError((e as Error).message)
    } finally {
      setInstalling('')
    }
  }

  return (
    <section className="trending-section" data-testid="trending">
      <div className="section-heading">
        <div>
          <h2>Trending on GitHub</h2>
          <p>Open-source projects everyone is talking about, ready to use in one click. No setup.</p>
        </div>
        {onSeeAll && items.length > limit && (
          <button className="text-button" onClick={onSeeAll} data-testid="trending-all">
            See all {items.length}
          </button>
        )}
      </div>
      <div className="project-grid discover-grid">
        {items.slice(0, limit).map((entry) => {
          const installed = apps.find((a) => (a.tags || []).includes(catalogTag(entry.id)))
          return (
            <article key={entry.id} className="project-card catalog-card" data-testid={`trending-${entry.id}`}>
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
                    <Btn sm variant="primary" onClick={() => onOpen(installed.id)} data-testid="trending-open">
                      Open
                    </Btn>
                  ) : (
                    <Btn
                      sm
                      variant="primary"
                      disabled={!!installing}
                      onClick={() => void install(entry)}
                      data-testid="trending-try"
                    >
                      {installing === entry.id ? 'Getting it ready…' : 'Try it'}
                    </Btn>
                  )}
                  <span>
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
    </section>
  )
}
