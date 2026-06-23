export function StatusBadge({ status }: { status?: string }) {
  const s = status || 'unknown'
  const cls = s === 'running' ? 'running' : s === 'stopped' ? 'stopped' : s === 'error' ? 'error' : ''
  return (
    <span className={`badge ${cls}`} data-testid="status">
      <span className="dot-i" />
      {s}
    </span>
  )
}
