import { ApiKey } from './api'
import { c, font, mono, Pill } from './design/kit'

// Access levels offered when minting a key. '' = full access (no scopes).
export type KeyAccess = '' | 'upgrade'
export const accessScopes = (a: KeyAccess): string[] => (a ? [a] : [])

export function ApiKeyScopeBadge({ scopes }: { scopes?: string[] }) {
  if (!scopes || scopes.length === 0) return null
  return <span data-testid="api-key-scope"><Pill tone="warn">{scopes.join(', ')}-only</Pill></span>
}

export function ApiKeyRow({ k, onRevoke }: { k: ApiKey; onRevoke: (id: string) => void }) {
  return (
    <div style={{ display: 'flex', alignItems: 'center', gap: 12, padding: '9px 12px', border: `1px solid ${c.border}`, borderRadius: 7, background: c.panel2, marginBottom: 6, fontSize: 12.5 }} data-testid={`api-key-${k.id}`}>
      <span style={{ fontWeight: 500 }}>{k.name}</span>
      <span style={{ ...mono, fontSize: 11.5, color: c.muted }}>{k.prefix}…</span>
      <ApiKeyScopeBadge scopes={k.scopes} />
      <span style={{ fontSize: 11.5, color: c.muted2 }}>{k.last_used_at || 'never used'}</span>
      <a onClick={() => onRevoke(k.id)} className="dc-hoverink" style={{ marginLeft: 'auto', color: c.muted2, fontSize: 12, cursor: 'pointer' }} data-testid="api-key-revoke">Revoke</a>
    </div>
  )
}

export function ApiKeyAccessSelect({ value, onChange }: { value: KeyAccess; onChange: (v: KeyAccess) => void }) {
  return (
    <select value={value} onChange={(e) => onChange(e.target.value as KeyAccess)} title="What the key may call"
      style={{ fontFamily: font.sans, fontSize: 12.5, color: c.fg2, background: c.panel2, border: `1px solid ${c.border}`, borderRadius: 7, padding: '0 10px' }}
      data-testid="api-key-access">
      <option value="">Access: full</option>
      <option value="upgrade">Access: upgrade only</option>
    </select>
  )
}
