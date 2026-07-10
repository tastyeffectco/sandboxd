import { useEffect } from 'react'
import { c, font, mono, Card, Btn } from './design/kit'
import { PROVIDER_ICONS } from './design/providerIcons'

// Deploy-to-production teaser for an app built in a sandbox. Not wired to any
// deploy backend — it showcases the planned one-click deploy and routes
// interest to sponsorship. (The console itself is self-hosted; this deploys the
// APP, not the console.)
const PROVIDERS: { name: string; key: string }[] = [
  { name: 'AWS', key: 'aws' },
  { name: 'Google Cloud', key: 'gcp' },
  { name: 'Azure', key: 'azure' },
  { name: 'Cloudflare', key: 'cloudflare' },
  { name: 'Hetzner', key: 'hetzner' },
  { name: 'DigitalOcean', key: 'digitalocean' },
  { name: 'Vercel', key: 'vercel' },
  { name: 'Fly.io', key: 'flydotio' },
  { name: 'Netlify', key: 'netlify' },
  { name: 'Render', key: 'render' },
  { name: 'Railway', key: 'railway' },
  { name: 'Vultr', key: 'vultr' },
]

export function DeployModal({ appName, close }: { appName?: string; close: () => void }) {
  useEffect(() => {
    const h = (e: KeyboardEvent) => { if (e.key === 'Escape') close() }
    window.addEventListener('keydown', h)
    return () => window.removeEventListener('keydown', h)
  }, [close])
  return (
    <div onClick={close} data-testid="deploy-modal" style={{ position: 'fixed', inset: 0, zIndex: 120, background: 'rgba(9,9,11,.45)', display: 'flex', alignItems: 'center', justifyContent: 'center', padding: 20, backdropFilter: 'blur(2px)' }}>
      <Card onClick={(e: React.MouseEvent) => e.stopPropagation()} style={{ width: 560, maxWidth: '100%', maxHeight: '90vh', overflow: 'auto', padding: 0, boxShadow: '0 24px 70px rgba(0,0,0,.28)' }}>
        <div style={{ padding: '22px 24px 16px', borderBottom: `1px solid ${c.border}` }}>
          <div style={{ display: 'flex', alignItems: 'center', gap: 10 }}>
            <span style={{ display: 'flex', width: 30, height: 30, borderRadius: 8, background: 'linear-gradient(135deg,#3f3f46,#18181b)', color: '#fff', alignItems: 'center', justifyContent: 'center' }}>
              <svg width="16" height="16" viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="2.2" strokeLinecap="round" strokeLinejoin="round"><path d="M12 2 4 6v6c0 5 3.4 8.5 8 10 4.6-1.5 8-5 8-10V6Z" opacity=".35" /><path d="m9 12 2 2 4-4" /></svg>
            </span>
            <h2 style={{ fontFamily: font.display, fontSize: 19, fontWeight: 700, margin: 0 }}>Deploy to production</h2>
            <span style={{ ...mono, fontSize: 10, color: c.warn, background: `${c.warn}14`, border: `1px solid ${c.warn}40`, borderRadius: 5, padding: '2px 8px' }}>coming soon</span>
          </div>
          <p style={{ color: c.muted, fontSize: 13, margin: '10px 0 0', lineHeight: 1.55 }}>
            Happy with {appName ? <b style={{ color: c.fg }}>{appName}</b> : 'this app'}? Soon you'll ship it from this sandbox to <b style={{ color: c.fg }}>your own cloud</b> in one click — your account, your infra, your domain. (The sandboxd console stays self-hosted; this deploys the app.)
          </p>
        </div>

        <div style={{ padding: '18px 24px' }}>
          <div style={{ ...mono, fontSize: 10, letterSpacing: '.6px', color: c.muted2, marginBottom: 12 }}>PLANNED TARGETS</div>
          <div style={{ display: 'grid', gridTemplateColumns: 'repeat(4,1fr)', gap: 10 }}>
            {PROVIDERS.map((p) => (
              <div key={p.key} title={`${p.name} — coming soon`} style={{ display: 'flex', flexDirection: 'column', alignItems: 'center', gap: 8, padding: '14px 6px', border: `1px solid ${c.border}`, borderRadius: 10, background: c.panel }}>
                <span className="prov-ico" style={{ width: 26, height: 26 }} dangerouslySetInnerHTML={{ __html: PROVIDER_ICONS[p.key] || '' }} />
                <span style={{ fontSize: 10.5, color: c.muted, textAlign: 'center', lineHeight: 1.2 }}>{p.name}</span>
              </div>
            ))}
          </div>
          <div style={{ textAlign: 'center', fontSize: 11.5, color: c.muted2, marginTop: 12 }}>…and every other provider — bring-your-own works too.</div>
        </div>

        <div style={{ padding: '18px 24px 22px', borderTop: `1px solid ${c.border}`, background: c.panel3, borderRadius: '0 0 10px 10px' }}>
          <div style={{ fontSize: 12.5, color: c.fg2, lineHeight: 1.55, marginBottom: 14 }}>
            sandboxd is open source and this feature is being built now. If one-click deploy would help you, <b style={{ color: c.fg }}>sponsoring keeps it moving</b> — sponsors help decide which providers land first.
          </div>
          <div style={{ display: 'flex', gap: 10, alignItems: 'center' }}>
            <a href="https://github.com/sponsors/tastyeffectco" target="_blank" rel="noreferrer" data-testid="deploy-sponsor" style={{ display: 'inline-flex', alignItems: 'center', gap: 7, textDecoration: 'none', background: '#db61a2', color: '#fff', fontFamily: font.sans, fontSize: 12.5, fontWeight: 600, borderRadius: 7, padding: '8px 14px' }}>
              <svg width="14" height="14" viewBox="0 0 24 24" fill="currentColor"><path d="M12 21s-7.5-4.6-10-9.3C.4 8.4 1.9 4.9 5.2 4.9c2 0 3.3 1.1 4 2.2.7-1.1 2-2.2 4-2.2 3.3 0 4.8 3.5 3.2 6.8C19.5 16.4 12 21 12 21Z" /></svg>
              Sponsor sandboxd
            </a>
            <Btn variant="ghost" onClick={close} style={{ marginLeft: 'auto' }}>Maybe later</Btn>
          </div>
        </div>
      </Card>
    </div>
  )
}
