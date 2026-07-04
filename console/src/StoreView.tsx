import { useMemo, useState } from 'react'
import { api } from './api'
import { CATALOG, CATEGORIES, CatalogRecipe, recipeManifest, recipeAgentsMd } from './catalog'
import { c, font, Card, Btn, Input } from './design/kit'

const initials = (name: string) => name.replace(/[^A-Za-z0-9 ]/g, '').split(/\s+/).map((w) => w[0]).join('').slice(0, 2).toUpperCase()

// Real project logo from the repo owner's avatar (the project mark for orgs),
// with a monogram fallback for non-GitHub repos or when the avatar 404s/offline.
const repoOwner = (repo?: string) => repo?.match(/github\.com\/([^/]+)\//)?.[1] || null
function AppLogo({ repo, name }: { repo?: string; name: string }) {
  const [failed, setFailed] = useState(false)
  const owner = repoOwner(repo)
  const box = { width: 30, height: 30, borderRadius: 8, border: `1px solid ${c.border}`, flexShrink: 0 } as const
  if (!owner || failed) {
    return <span style={{ ...box, background: c.panel2, display: 'flex', alignItems: 'center', justifyContent: 'center', fontFamily: font.mono, fontSize: 11, fontWeight: 600, color: c.muted }}>{initials(name)}</span>
  }
  return <img src={`https://github.com/${owner}.png?size=80`} alt="" loading="lazy" onError={() => setFailed(true)} style={{ ...box, objectFit: 'cover', background: '#fff' }} />
}

export function StoreView({ onError, toast, onOpen, reloadApps }: { onError: (m: string) => void; toast: (m: string) => void; onOpen: (id: string) => void; reloadApps: () => void }) {
  const [cat, setCat] = useState<string>('all')
  const [q, setQ] = useState('')
  const [installing, setInstalling] = useState<string>('')

  const items = useMemo(() => {
    const s = q.trim().toLowerCase()
    return CATALOG.filter((r) => (cat === 'all' || r.category === cat) && (!s || r.name.toLowerCase().includes(s) || r.blurb.toLowerCase().includes(s)))
  }, [cat, q])

  const install = async (r: CatalogRecipe) => {
    setInstalling(r.id)
    try {
      const app = await api.createApp({ name: r.id, description: r.blurb, tags: [r.category] })
      const sb = await api.createAppSandbox(app.id, {})
      await new Promise((res) => setTimeout(res, 1500))
      await api.putWorkspaceFile(sb.id, 'workspace/app/catalog-run.sh', r.script)
      await api.putWorkspaceFile(sb.id, 'workspace/app/sandbox.yaml', recipeManifest(r))
      await api.putWorkspaceFile(sb.id, 'workspace/app/AGENTS.md', recipeAgentsMd(r))
      await api.stopSandbox(sb.id).catch(() => undefined)
      await api.startSandbox(sb.id)
      toast(`Installing ${r.name}…`)
      reloadApps()
      onOpen(app.id)
    } catch (e) { onError((e as Error).message) } finally { setInstalling('') }
  }

  return (
    <div style={{ maxWidth: 1080, margin: '0 auto', padding: '36px 40px 80px' }}>
      <h1 style={{ fontFamily: font.display, fontSize: 24, fontWeight: 700, margin: '0 0 4px' }}>App Store</h1>
      <p style={{ color: c.muted, margin: '0 0 20px', maxWidth: 720 }}>One-click open-source apps, installed as ordinary sandboxd apps — each runs isolated in its own sandbox with a live preview URL. Recipes are data on top of the public API; the core engine is untouched.</p>
      <div style={{ display: 'flex', gap: 6, alignItems: 'center', marginBottom: 20, flexWrap: 'wrap' }}>
        {CATEGORIES.map((k) => (
          <div key={k.id} onClick={() => setCat(k.id)} className="dc-hoverborder" style={{ padding: '5px 12px', fontSize: 12.5, borderRadius: 7, cursor: 'pointer', border: `1px solid ${cat === k.id ? c.faint : c.border}`, color: cat === k.id ? c.fg : c.muted, background: cat === k.id ? c.panel2 : 'transparent' }}>{k.label}</div>
        ))}
        <Input value={q} onChange={(e) => setQ(e.target.value)} placeholder="Search apps…" style={{ marginLeft: 'auto', width: 220, fontFamily: font.sans }} />
      </div>
      <div style={{ display: 'grid', gridTemplateColumns: 'repeat(auto-fill,minmax(290px,1fr))', gap: 14 }}>
        {items.map((r) => (
          <Card key={r.id} style={{ padding: 16, display: 'flex', flexDirection: 'column', gap: 8 }}>
            <div style={{ display: 'flex', alignItems: 'center', gap: 10 }}>
              <AppLogo repo={r.repo} name={r.name} />
              <span style={{ fontFamily: font.display, fontWeight: 600, fontSize: 14.5 }}>{r.name}</span>
              <span style={{ marginLeft: 'auto', fontSize: 11, color: c.muted2, textTransform: 'capitalize' }}>{r.category}</span>
            </div>
            <div style={{ color: c.fg2, fontSize: 12.5, flex: 1 }}>{r.blurb}</div>
            {r.note && <div style={{ color: c.muted2, fontSize: 11.5 }}>{r.note}</div>}
            <Btn variant="outline" disabled={!!installing} onClick={() => install(r)} style={{ border: `1px solid ${c.border2}`, background: c.panel2 }}>{installing === r.id ? 'Installing…' : 'Install'}</Btn>
          </Card>
        ))}
      </div>
    </div>
  )
}
