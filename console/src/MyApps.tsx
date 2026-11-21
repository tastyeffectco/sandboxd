import type React from 'react'
import { useEffect, useState } from 'react'
import { api, App } from './api'
import { catalogIdOf } from './catalog'
import { Icon } from './design/Icon'
import { Bot } from './design/Bot'
import { ProjectIcon } from './design/ProjectIcon'
import { Btn, StatusPill } from './design/kit'

// My apps: everything the person's crew built, to open again. There is no
// second build box here: new apps and agents are asked of the sandboxd agent
// on Home, the one place to start anything.
export function MyApps({
  apps,
  onOpen,
  onAsk,
  featured,
}: {
  apps: App[]
  onOpen: (id: string) => void
  // Home, with the cursor in sandboxd's box.
  onAsk: () => void
  // Trending free apps, shown under the list.
  featured?: React.ReactNode
}) {
  const [query, setQuery] = useState('')
  const [status, setStatus] = useState<Record<string, string>>({})
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
  const filtered = apps.filter((a) =>
    `${a.name} ${a.description}`.toLowerCase().includes(query.toLowerCase()),
  )
  return (
    <div className="builder-home">
      <section className="projects-section">
        <div className="section-heading">
          <div>
            <span className="eyebrow">Your work</span>
            <h1>
              My apps <span>{apps.length}</span>
            </h1>
            <p>Every app your crew built. Open one to see it live or to change it.</p>
          </div>
          {apps.length > 0 && (
            <label className="project-search">
              <Icon name="search" size={16} />
              <input
                aria-label="Search apps"
                placeholder="Find an app…"
                value={query}
                onChange={(e) => setQuery(e.target.value)}
              />
            </label>
          )}
        </div>
        {apps.length === 0 ? (
          <div className="empty-state" data-testid="apps-empty">
            <Bot who="sandboxd" size={72} mood="happy" />
            <h3>No apps yet</h3>
            <p>Tell sandboxd what you want on Home. It builds the app and it shows up here.</p>
            <Btn variant="primary" onClick={onAsk} data-testid="apps-ask">
              Ask sandboxd <Icon name="arrow" size={16} />
            </Btn>
          </div>
        ) : filtered.length === 0 ? (
          <p className="empty-state">No apps match “{query}”.</p>
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
                  <p>{a.description || 'Ready for your next idea'}</p>
                  <footer>
                    <StatusPill status={a.current_sandbox_id ? status[a.id] || 'loading' : 'not started'} />
                  </footer>
                </div>
              </button>
            ))}
          </div>
        )}
      </section>
      {featured}
    </div>
  )
}
