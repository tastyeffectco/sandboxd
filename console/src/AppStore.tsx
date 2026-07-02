import { useCallback, useEffect, useMemo, useState } from 'react'
import { api, App as TApp } from './api'
import { CATALOG, CATEGORIES, CatalogRecipe, recipeAgentsMd, recipeManifest } from './catalog'

// App Store — one-click installs of curated open-source apps.
//
// Pure /v1 client (docs/APP-CATALOG-CONTRACT.md): create app → create sandbox →
// PUT catalog-run.sh + sandbox.yaml → restart → poll until the web process is
// live. Core is never aware of the catalog; installed entries are ordinary apps
// tagged `catalog:<recipe>` and get the full AppDetail experience.

type Phase = 'idle' | 'creating' | 'writing' | 'restarting' | 'waiting' | 'done' | 'error'

interface InstallState {
  phase: Phase
  appId?: string
  message?: string
}

// Health-wait budget by recipe effort: binaries are up in seconds; source
// builds legitimately take minutes (install runs inside web.command).
const WAIT_MS: Record<CatalogRecipe['effort'], number> = {
  instant: 120_000,
  quick: 420_000,
  build: 900_000,
}
const POLL_MS = 4000

const sleep = (ms: number) => new Promise((r) => setTimeout(r, ms))

async function waitSandbox(sbId: string, want: (s: { status: string; processes?: { kind: string; running: boolean }[] }) => boolean, budgetMs: number): Promise<boolean> {
  const deadline = Date.now() + budgetMs
  while (Date.now() < deadline) {
    try {
      const s = await api.getSandbox(sbId)
      if (want(s)) return true
    } catch {
      /* transient — keep polling */
    }
    await sleep(POLL_MS)
  }
  return false
}

export function AppStore({ onOpen, onError, onInfo }: { onOpen: (appId: string) => void; onError: (m: string) => void; onInfo: (m: string) => void }) {
  const [installs, setInstalls] = useState<Record<string, InstallState>>({})
  const [installed, setInstalled] = useState<Record<string, string>>({}) // recipeId -> appId
  const [cat, setCat] = useState<string>('all')
  const [q, setQ] = useState('')

  const set = (id: string, st: InstallState) => setInstalls((m) => ({ ...m, [id]: st }))

  // Map already-installed catalog apps (tag `catalog:<id>`) so cards show Open.
  const loadInstalled = useCallback(() => {
    api
      .listApps()
      .then((apps: TApp[]) => {
        const m: Record<string, string> = {}
        for (const a of apps) {
          const t = (a.tags || []).find((x) => x.startsWith('catalog:'))
          if (t) m[t.slice('catalog:'.length)] = a.id
        }
        setInstalled(m)
      })
      .catch(() => setInstalled({}))
  }, [])
  useEffect(loadInstalled, [loadInstalled])

  const install = async (r: CatalogRecipe) => {
    set(r.id, { phase: 'creating' })
    try {
      const app = await api.createApp({
        name: r.id,
        description: r.blurb,
        tags: ['catalog', `catalog:${r.id}`],
      })
      const sb = await api.createAppSandbox(app.id, {})
      set(r.id, { phase: 'writing', appId: app.id })

      // Recipe = three workspace files (script, manifest, agent context);
      // retry briefly while the sandbox settles.
      let lastErr: Error | null = null
      for (let i = 0; i < 5; i++) {
        try {
          await api.putWorkspaceFile(sb.id, 'workspace/app/catalog-run.sh', r.script)
          await api.putWorkspaceFile(sb.id, 'workspace/app/sandbox.yaml', recipeManifest(r))
          await api.putWorkspaceFile(sb.id, 'workspace/app/AGENTS.md', recipeAgentsMd(r))
          // Optional per-app skills — how-tos for OPERATING the app (create an
          // n8n workflow, send a gotify message, …) that agent tasks can read.
          for (const sk of r.skills || []) {
            await api.putWorkspaceFile(sb.id, `workspace/app/skills/${sk.name}.md`, sk.content)
          }
          lastErr = null
          break
        } catch (e) {
          lastErr = e as Error
          await sleep(3000)
        }
      }
      if (lastErr) throw lastErr

      // Restart so runtimed adopts the manifest (and evicts the template
      // dev server that squats :3000 — contract §4 step 5).
      set(r.id, { phase: 'restarting', appId: app.id })
      await api.stopSandbox(sb.id).catch(() => undefined)
      await waitSandbox(sb.id, (s) => s.status === 'stopped', 60_000)
      await api.startSandbox(sb.id)

      set(r.id, { phase: 'waiting', appId: app.id })
      const ok = await waitSandbox(
        sb.id,
        (s) => s.status === 'running' && !!s.processes?.some((p) => p.kind === 'web' && p.running),
        WAIT_MS[r.effort],
      )
      if (!ok) {
        set(r.id, {
          phase: 'error',
          appId: app.id,
          message: 'Timed out waiting for the app to become healthy — open it to inspect logs.',
        })
        return
      }
      set(r.id, { phase: 'done', appId: app.id })
      setInstalled((m) => ({ ...m, [r.id]: app.id }))
      onInfo(`${r.name} installed${r.entryPath ? ` — UI at ${r.entryPath}` : ''}`)
    } catch (e) {
      const msg = (e as Error).message
      set(r.id, { phase: 'error', message: msg })
      onError(`${r.name}: ${msg}`)
    }
  }

  const visible = useMemo(() => {
    const needle = q.trim().toLowerCase()
    return CATALOG.filter((r) => (cat === 'all' || r.category === cat))
      .filter((r) => !needle || r.name.toLowerCase().includes(needle) || r.blurb.toLowerCase().includes(needle))
  }, [cat, q])

  return (
    <div className="stack">
      <h1>App Store</h1>
      <p className="muted">
        One-click open-source apps, installed as ordinary sandboxd apps — each runs isolated in its own
        sandbox with a live preview URL. v1 ships Node, Python & single-binary apps that run on the stock
        base image. Recipes are data on top of the public API; the core engine is untouched.
      </p>
      <div className="row" style={{ gap: 8, flexWrap: 'wrap' }}>
        {CATEGORIES.map((c) => (
          <button
            key={c.id}
            className={`btn btn-sm ${cat === c.id ? 'btn-primary' : 'btn-ghost'}`}
            onClick={() => setCat(c.id)}
            data-testid={`store-cat-${c.id}`}
          >
            {c.label}
          </button>
        ))}
        <div className="spacer" />
        <input
          className="input"
          style={{ maxWidth: 220 }}
          placeholder="Search apps…"
          value={q}
          onChange={(e) => setQ(e.target.value)}
          data-testid="store-search"
        />
      </div>
      <div className="grid" data-testid="store-grid">
        {visible.map((r) => {
          const st = installs[r.id] || { phase: 'idle' as Phase }
          const existingId = installed[r.id] || (st.phase === 'done' ? st.appId : undefined)
          const busy = st.phase !== 'idle' && st.phase !== 'done' && st.phase !== 'error'
          return (
            <div key={r.id} className="card" data-testid={`store-card-${r.id}`}>
              <div className="row" style={{ alignItems: 'baseline', gap: 8 }}>
                <div className="card-title">{r.name}</div>
                <span className="badge">{r.effort === 'instant' ? '⚡ instant' : r.effort === 'quick' ? '~1 min' : 'build (mins)'}</span>
                <span className="badge" title={r.modifiable === 'source' ? 'Full source in the workspace — agent tasks can modify the app itself' : 'Prebuilt release — agent tasks can modify config, flags, plugins, and data'}>
                  {r.modifiable === 'source' ? '✎ source' : '⚙ config'}
                </span>
              </div>
              <p className="muted" style={{ minHeight: '2.4em' }}>{r.blurb}</p>
              {r.note && <p className="muted" style={{ fontSize: '0.85em' }}>{r.note}</p>}
              <div className="row" style={{ gap: 8 }}>
                {existingId ? (
                  <button className="btn btn-outline btn-sm" onClick={() => onOpen(existingId)} data-testid={`store-open-${r.id}`}>
                    Open
                  </button>
                ) : (
                  <button
                    className="btn btn-primary btn-sm"
                    disabled={busy}
                    onClick={() => install(r)}
                    data-testid={`store-install-${r.id}`}
                  >
                    {st.phase === 'idle' && 'Install'}
                    {st.phase === 'creating' && 'Creating…'}
                    {st.phase === 'writing' && 'Writing recipe…'}
                    {st.phase === 'restarting' && 'Starting…'}
                    {st.phase === 'waiting' && 'Installing…'}
                    {st.phase === 'error' && 'Retry'}
                  </button>
                )}
                {st.phase === 'error' && st.appId && (
                  <button className="btn btn-ghost btn-sm" onClick={() => onOpen(st.appId as string)}>
                    Inspect
                  </button>
                )}
              </div>
              {st.phase === 'error' && st.message && <p className="error" style={{ fontSize: '0.85em' }}>{st.message}</p>}
              {st.phase === 'waiting' && (
                <p className="muted" style={{ fontSize: '0.85em' }}>
                  Installing inside the sandbox — {r.effort === 'build' ? 'source builds take a few minutes' : 'usually well under a minute'}.
                </p>
              )}
            </div>
          )
        })}
        {visible.length === 0 && <p className="muted">No apps match.</p>}
      </div>
    </div>
  )
}
