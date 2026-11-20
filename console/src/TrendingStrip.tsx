import { useState } from 'react'
import { App as TApp } from './api'
import { CatalogApp, TRENDING, catalogTag } from './catalog'
import { installCatalogApp } from './catalogInstall'
import { CatalogCard } from './CatalogCard'

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
  onInstalled: (id: string) => void
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
      onInstalled(id)
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
            <CatalogCard
              key={entry.id}
              entry={entry}
              installed={installed}
              installing={installing}
              onInstall={(e) => void install(e)}
              onOpen={onOpen}
              testId={`trending-${entry.id}`}
              openTestId="trending-open"
              installTestId="trending-try"
              installLabel="Try it"
              installingLabel="Getting it ready…"
            />
          )
        })}
      </div>
    </section>
  )
}
