import { useMemo, useState } from 'react'
import { c, font, mono } from './design/kit'
import { BrainNode, BrainEdge } from './brain'

// Obsidian-style knowledge graph, hand-rolled: a small deterministic force
// simulation (repulsion + edge springs + center gravity) computed synchronously
// — node counts here are tens, not thousands, so no worker/RAF machinery.
// Solid nodes = apps with a brain; faded = ghosts (no brain yet, or a concept
// mentioned via [[wikilink]] that has no app). No external dependencies.

const W = 860
const H = 380

function layout(nodes: BrainNode[], edges: BrainEdge[]): Map<string, { x: number; y: number }> {
  const pos = new Map<string, { x: number; y: number }>()
  const n = nodes.length
  // Deterministic start: a circle, connected nodes end up pulling together.
  nodes.forEach((node, i) => {
    const a = (2 * Math.PI * i) / Math.max(1, n)
    pos.set(node.id, { x: W / 2 + Math.cos(a) * 130, y: H / 2 + Math.sin(a) * 130 })
  })
  const k = Math.sqrt((W * H) / Math.max(1, n)) * 0.7
  for (let iter = 0; iter < 260; iter++) {
    const cool = 1 - iter / 260
    const disp = new Map<string, { x: number; y: number }>()
    nodes.forEach((a) => disp.set(a.id, { x: 0, y: 0 }))
    // Repulsion between every pair.
    for (let i = 0; i < n; i++) for (let j = i + 1; j < n; j++) {
      const A = pos.get(nodes[i].id)!, B = pos.get(nodes[j].id)!
      let dx = A.x - B.x, dy = A.y - B.y
      let d = Math.sqrt(dx * dx + dy * dy) || 0.01
      const f = (k * k) / d
      dx /= d; dy /= d
      const da = disp.get(nodes[i].id)!, db = disp.get(nodes[j].id)!
      da.x += dx * f; da.y += dy * f; db.x -= dx * f; db.y -= dy * f
    }
    // Springs along edges.
    for (const e of edges) {
      const A = pos.get(e.from), B = pos.get(e.to)
      if (!A || !B) continue
      let dx = A.x - B.x, dy = A.y - B.y
      const d = Math.sqrt(dx * dx + dy * dy) || 0.01
      const f = (d * d) / k / 8
      dx /= d; dy /= d
      const da = disp.get(e.from)!, db = disp.get(e.to)!
      da.x -= dx * f; da.y -= dy * f; db.x += dx * f; db.y += dy * f
    }
    // Apply with cooling + gentle center gravity, clamp to the viewport.
    for (const node of nodes) {
      const p = pos.get(node.id)!, d = disp.get(node.id)!
      const len = Math.sqrt(d.x * d.x + d.y * d.y) || 0.01
      const step = Math.min(len, 14 * cool)
      p.x += (d.x / len) * step + (W / 2 - p.x) * 0.012
      p.y += (d.y / len) * step + (H / 2 - p.y) * 0.012
      p.x = Math.max(50, Math.min(W - 50, p.x))
      p.y = Math.max(26, Math.min(H - 26, p.y))
    }
  }
  return pos
}

export function BrainGraph({ nodes, edges, onOpen }: { nodes: BrainNode[]; edges: BrainEdge[]; onOpen: (appId: string) => void }) {
  const pos = useMemo(() => layout(nodes, edges), [nodes, edges])
  const [hover, setHover] = useState<string | null>(null)

  if (nodes.length === 0) return null
  const touching = (id: string) => edges.some((e) => (e.from === id && e.to === hover) || (e.to === id && e.from === hover))
  const active = (id: string) => hover === null || hover === id || touching(id)

  return (
    <svg viewBox={`0 0 ${W} ${H}`} style={{ width: '100%', height: 'auto', display: 'block' }} data-testid="brain-graph" role="img" aria-label="Knowledge graph of project brains">
      {edges.map((e, i) => {
        const A = pos.get(e.from), B = pos.get(e.to)
        if (!A || !B) return null
        const lit = hover !== null && (e.from === hover || e.to === hover)
        return <line key={i} x1={A.x} y1={A.y} x2={B.x} y2={B.y} stroke={lit ? c.link : c.border} strokeWidth={lit ? 1.6 : 1} opacity={hover === null || lit ? 0.9 : 0.25} style={{ transition: 'all .15s ease' }} />
      })}
      {nodes.map((node) => {
        const p = pos.get(node.id)!
        const isErr = node.kind === 'concept' && node.id.startsWith('ghost:err-')
        const r = node.kind === 'concept' ? 6 : node.kind === 'spoke' ? 6.5 : node.empty ? 7 : Math.min(13, 8 + node.lines / 40)
        const clickable = node.kind !== 'concept' && !!node.appId
        const stroke = isErr ? c.warn : node.empty ? c.muted2 : node.kind === 'spoke' ? c.muted : c.ink
        return (
          <g key={node.id}
            onMouseEnter={() => setHover(node.id)} onMouseLeave={() => setHover(null)}
            onClick={() => clickable && onOpen(node.appId!)}
            style={{ cursor: clickable ? 'pointer' : 'default' }}
            opacity={active(node.id) ? 1 : 0.3}>
            <circle cx={p.x} cy={p.y} r={r}
              fill={node.empty ? 'transparent' : node.kind === 'spoke' ? c.muted : c.ink}
              stroke={stroke}
              strokeWidth={node.empty ? 1.2 : node.kind === 'spoke' ? 0 : 0}
              strokeDasharray={node.empty ? '3 3' : undefined}
              style={{ transition: 'opacity .15s ease' }} />
            <text x={p.x} y={p.y + r + 13} textAnchor="middle"
              style={{ fontFamily: font.mono, fontSize: node.kind === 'app' ? 10.5 : 9.5, fill: node.empty ? (isErr ? c.warn : c.muted2) : node.kind === 'spoke' ? c.muted : c.fg, userSelect: 'none' }}>
              {node.label.length > 22 ? node.label.slice(0, 21) + '…' : node.label}
            </text>
          </g>
        )
      })}
    </svg>
  )
}
