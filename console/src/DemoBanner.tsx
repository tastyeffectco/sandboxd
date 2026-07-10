// Persistent top banner shown only in the static demo (VITE_DEMO=1). Makes it
// unmistakable that this is a read-only tour, and points to the one-line install.
export default function DemoBanner() {
  return (
    <div
      style={{
        display: 'flex', alignItems: 'center', justifyContent: 'center', gap: 14,
        flexWrap: 'wrap', padding: '7px 16px', fontSize: 13,
        fontFamily: 'IBM Plex Mono, ui-monospace, monospace',
        background: '#0a0a0a', color: '#fcfaf6', borderBottom: '2px solid #ff6b00',
      }}
    >
      <span>
        <b style={{ color: '#ff6b00' }}>▶ Live demo</b> · read-only tour with sample data — nothing here runs a real sandbox.
      </span>
      <a
        href="https://github.com/tastyeffectco/sandboxd"
        target="_blank" rel="noreferrer"
        style={{ color: '#0a0a0a', background: '#ff6b00', textDecoration: 'none', fontWeight: 700, padding: '3px 12px', whiteSpace: 'nowrap' }}
      >
        Install it → one command
      </a>
    </div>
  )
}
