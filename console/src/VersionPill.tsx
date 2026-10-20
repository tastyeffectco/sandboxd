import { useEffect, useRef, useState } from 'react'
import type { Settings, UpgradeState } from './api'
import { c, mono } from './design/kit'
import { Notes } from './notes'

// The one place the console talks about versions: a small pill in the bottom
// left showing the running build, with a panel above it for what's new, the
// breaking-changes notice, the in-place upgrade, and a bug-report link that
// carries the version automatically. Nothing here shifts the layout.
export function VersionPill({ settings, dismissed, onDismiss, upg, startUpgrade, clearUpgrade }: {
  settings: Settings
  // Dismissal is per latest version ("Remind me later"); it only drops the
  // accent dot — the panel keeps offering the update.
  dismissed: boolean
  onDismiss: () => void
  upg: UpgradeState | null
  startUpgrade: (target: string) => void
  clearUpgrade: () => void
}) {
  const [open, setOpen] = useState(false)
  const [confirm, setConfirm] = useState(false)
  const [ack, setAck] = useState(false)
  const root = useRef<HTMLDivElement>(null)

  useEffect(() => {
    if (!open) return
    const key = (e: KeyboardEvent) => { if (e.key === 'Escape') setOpen(false) }
    const click = (e: MouseEvent) => { if (root.current && !root.current.contains(e.target as Node)) setOpen(false) }
    window.addEventListener('keydown', key)
    document.addEventListener('mousedown', click)
    return () => { window.removeEventListener('keydown', key); document.removeEventListener('mousedown', click) }
  }, [open])

  const version = settings.version || 'unknown'
  const commit = (settings.git_commit || '').slice(0, 7)
  const latest = settings.update_available ? settings.latest_version : undefined
  const untagged = settings.update_kind === 'untagged'
  const breaking = (latest && settings.latest_breaking) || ''
  const running = upg?.phase === 'running'
  const failed = upg?.phase === 'failed' || upg?.phase === 'rolled_back'
  const showDot = !!latest && !dismissed && !running && !failed

  const pillText = running ? `Upgrading to ${upg?.target}…`
    : failed ? `Upgrade ${upg?.phase === 'rolled_back' ? 'rolled back' : 'failed'}`
    : latest ? `sandboxd ${version} · ${untagged ? `untagged build · latest ${latest}` : `${latest} available`}`
    : `sandboxd ${version}`

  const issueBody = [
    `**Version:** ${version}${commit ? ` (${commit})` : ''}`,
    `**Preview host style:** ${settings.networking?.preview_host_style || ''}`,
    `**Browser:** ${typeof navigator !== 'undefined' ? navigator.userAgent : ''}`,
    '', '**What happened**', '', '**Steps**', '',
  ].join('\n')
  const issueURL = `https://github.com/tastyeffectco/sandboxd/issues/new?title=&body=${encodeURIComponent(issueBody)}`

  const btn = (primary: boolean, disabled = false) => ({
    font: 'inherit', fontSize: 12, padding: '3px 10px', borderRadius: 5, border: `1px solid ${c.border}`,
    background: primary ? c.good : c.bg, color: primary ? c.bg : c.fg, cursor: disabled ? 'not-allowed' : 'pointer', opacity: disabled ? 0.5 : 1,
  } as const)
  const published = settings.latest_published_at ? new Date(settings.latest_published_at) : null
  const publishedLabel = published && !isNaN(+published) ? published.toISOString().slice(0, 10) : ''

  const breakingBox = breaking && (
    <div data-testid="breaking-box" style={{ border: `1px solid ${c.bad}`, borderRadius: 6, padding: '8px 10px', marginBottom: 10, background: 'rgba(220,38,38,.04)' }}>
      <div style={{ color: c.bad, fontWeight: 600, fontSize: 12.5, marginBottom: 2 }}>Breaking changes in {latest}</div>
      <Notes md={breaking} />
    </div>
  )

  return (
    <div className="version-widget" ref={root} style={{ position: 'fixed', left: 12, bottom: 12, zIndex: 100, fontSize: 12 }}>
      {open && (
        <div data-testid="version-panel" role="dialog" style={{ position: 'absolute', left: 0, bottom: 'calc(100% + 8px)', width: 440, maxWidth: 'calc(100vw - 24px)', background: c.panel, border: `1px solid ${c.border}`, borderRadius: 10, boxShadow: '0 12px 32px rgba(0,0,0,.14)', padding: 14 }}>
          <div style={{ display: 'flex', alignItems: 'baseline', gap: 8, marginBottom: 8 }}>
            <span style={{ ...mono, fontSize: 12.5, fontWeight: 600 }}>{version}{commit && <span style={{ color: c.muted, fontWeight: 400 }}> · {commit}</span>}</span>
            <span style={{ flex: 1 }} />
            <a data-testid="report-issue" href={issueURL} target="_blank" rel="noreferrer" style={{ color: c.muted, textDecoration: 'none' }}>Report an issue ↗</a>
          </div>

          {running ? (
            <div data-testid="upgrade-progress"><b>Upgrading to {upg?.target}…</b> backing up, rebuilding and restarting — this page reconnects by itself (2–5 min).</div>
          ) : failed ? (
            <div data-testid="upgrade-result" style={{ color: c.bad }}>
              <b>Upgrade {upg?.phase === 'rolled_back' ? 'rolled back' : 'failed'}:</b> {upg?.message}
              <span className="dc-hoverink" style={{ marginLeft: 8, color: c.link, cursor: 'pointer' }} onClick={clearUpgrade}>ok</span>
            </div>
          ) : !latest ? (
            <div style={{ color: c.muted }}>You're on the latest release.</div>
          ) : confirm ? (
            <div data-testid="upgrade-confirm">
              {breakingBox}
              <div style={{ marginBottom: 8 }}>Upgrade to <b>{latest}</b>? It backs up first, rebuilds, restarts and rolls back on failure (2–5 min). Sandboxes keep running.</div>
              {breaking && (
                <label style={{ display: 'flex', alignItems: 'center', gap: 6, marginBottom: 8, cursor: 'pointer' }}>
                  <input data-testid="upgrade-ack" type="checkbox" checked={ack} onChange={(e) => setAck(e.target.checked)} />
                  I have read the breaking changes
                </label>
              )}
              <button data-testid="upgrade-go" disabled={!!breaking && !ack} onClick={() => { setConfirm(false); startUpgrade(latest) }} style={btn(true, !!breaking && !ack)}>Upgrade now</button>
              <span className="dc-hoverink" style={{ marginLeft: 10, color: c.muted, cursor: 'pointer' }} onClick={() => setConfirm(false)}>cancel</span>
            </div>
          ) : (
            <div>
              {breakingBox}
              {untagged && (
                <div style={{ color: c.muted, marginBottom: 6 }}>You're on an untagged build ({version}); the latest release is <b>{latest}</b>.</div>
              )}
              <div style={{ fontWeight: 600, fontSize: 12.5 }}>What's new in {latest}{publishedLabel && <span style={{ color: c.muted, fontWeight: 400 }}> · {publishedLabel}</span>}</div>
              {settings.latest_notes ? (
                <div style={{ maxHeight: '50vh', overflowY: 'auto', margin: '2px 0 10px' }}><Notes md={settings.latest_notes} testid="update-notes" /></div>
              ) : (
                <div style={{ color: c.muted, margin: '2px 0 10px' }}>No notes for this release.</div>
              )}
              <div style={{ display: 'flex', alignItems: 'center', gap: 12 }}>
                <button data-testid="upgrade-open" onClick={() => { setAck(false); setConfirm(true) }} style={btn(true)}>Upgrade now</button>
                {settings.changelog_url && <a href={settings.changelog_url} target="_blank" rel="noreferrer" style={{ color: c.link, textDecoration: 'none' }}>Release notes ↗</a>}
                <span style={{ flex: 1 }} />
                {!dismissed && <span data-testid="update-dismiss" className="dc-hoverink" style={{ color: c.muted, cursor: 'pointer' }} onClick={onDismiss}>Remind me later</span>}
              </div>
              <div style={{ color: c.muted, marginTop: 8 }}>Or run <span style={{ ...mono, fontSize: 11.5, background: c.bg, border: `1px solid ${c.border}`, borderRadius: 4, padding: '1px 6px' }}>./upgrade.sh</span> on your server.</div>
            </div>
          )}
        </div>
      )}
      <div
        data-testid="version-pill"
        data-version={version}
        className="dc-hoverborder"
        onClick={() => setOpen((o) => !o)}
        title="Version, release notes and upgrades"
        style={{ display: 'inline-flex', alignItems: 'center', gap: 7, padding: '3px 10px', borderRadius: 999, border: `1px solid ${failed ? c.bad : c.border}`, background: c.panel, color: failed ? c.bad : c.muted, cursor: 'pointer', boxShadow: '0 1px 3px rgba(0,0,0,.06)', userSelect: 'none' }}>
        {showDot && <span data-testid="version-dot" style={{ width: 7, height: 7, borderRadius: '50%', background: c.good, flexShrink: 0 }} />}
        <span style={{ ...mono, fontSize: 11.5 }}>{pillText}</span>
      </div>
    </div>
  )
}
