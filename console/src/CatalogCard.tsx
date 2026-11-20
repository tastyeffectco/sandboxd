import { ReactNode } from 'react'
import { App as TApp } from './api'
import { CatalogApp } from './catalog'
import { Btn } from './design/kit'
import { Avatar, logoFor } from './design/Avatar'

// One free app on a card: its icon tile in the app's own colour, what it does,
// and Install (or Open once it is installed). Used by Free apps and the
// home page's Trending strip.
export function CatalogCard({
  entry,
  installed,
  installing,
  onInstall,
  onOpen,
  testId,
  installLabel = 'Install',
  installingLabel = 'Installing…',
  openTestId,
  installTestId,
  extra,
}: {
  entry: CatalogApp
  installed?: TApp
  // The app being installed right now, if any (one at a time).
  installing: string
  onInstall: (entry: CatalogApp) => void
  onOpen: (id: string) => void
  testId: string
  installLabel?: string
  installingLabel?: string
  openTestId: string
  installTestId: string
  // Shown before the licence, e.g. "Changeable with AI".
  extra?: ReactNode
}) {
  return (
    <article className="project-card catalog-card" data-testid={testId} style={{ ['--app' as string]: entry.color }}>
      <div className="catalog-art">
        {logoFor(entry.id) ? (
          <Avatar who={entry.name} logo={entry.id} size={64} toggle />
        ) : (
          <span className="catalog-icon" aria-hidden="true">
            {entry.name.slice(0, 1)}
          </span>
        )}
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
            <Btn sm variant="primary" onClick={() => onOpen(installed.id)} data-testid={openTestId}>
              Open
            </Btn>
          ) : (
            <Btn
              sm
              variant="primary"
              disabled={!!installing}
              onClick={() => onInstall(entry)}
              data-testid={installTestId}
            >
              {installing === entry.id ? installingLabel : installLabel}
            </Btn>
          )}
          <span>
            {extra}
            {entry.license} ·{' '}
            <a href={entry.repo} target="_blank" rel="noreferrer">
              code
            </a>
          </span>
        </footer>
      </div>
    </article>
  )
}
