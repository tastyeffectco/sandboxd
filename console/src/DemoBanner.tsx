// Persistent top banner shown only in the static demo (VITE_DEMO=1). Makes it
// unmistakable that this is a read-only tour, and points to the one-line install.
export default function DemoBanner() {
  return (
    <div
      style={{
        display: 'flex', alignItems: 'center', justifyContent: 'center', gap: 14,
        flexWrap: 'wrap', padding: '7px 16px', fontSize: 13,
        fontFamily: 'var(--mono)',
        background: 'var(--brand-grad-soft), var(--bg-2)', color: 'var(--fg)', borderBottom: '1px solid var(--accent-line)',
      }}
    >
      <span>
        <b className="sbx-grad-text">▶ Live demo</b> · read-only tour with sample data — nothing here runs a real sandbox.
      </span>
      <a
        href="https://github.com/tastyeffectco/sandboxd"
        target="_blank" rel="noreferrer"
        style={{ color: '#fff', background: 'var(--brand-grad)', textDecoration: 'none', fontWeight: 700, padding: '4px 14px', borderRadius: 999, whiteSpace: 'nowrap' }}
      >
        Install it → one command
      </a>
    </div>
  )
}
