// Project Brain — pure logic (no React, no fetch) so it is unit-testable:
// wikilink parsing, cross-brain graph building, the "Current state" excerpt,
// and the inline splitter the Brain tab's view mode renders from.

export interface BrainNode {
  id: string      // app id, or "ghost:<slug>" for an unresolved link target
  label: string
  ghost: boolean  // true = mentioned in a brain but has no brain of its own
  lines: number   // brain size (0 for ghosts)
}

export interface BrainEdge { from: string; to: string }

// slugKey normalizes a name for matching: "[[API Gateway]]" links the app
// named "api-gateway" (case/space/punctuation-insensitive).
export const slugKey = (s: string) =>
  s.toLowerCase().replace(/[^a-z0-9]+/g, '-').replace(/(^-|-$)/g, '')

// parseWikiLinks returns every [[target]] in the markdown, trimmed, in order,
// duplicates included (the graph builder dedupes). Multiline or empty targets
// are ignored — a wikilink is a short name, not a paragraph.
export function parseWikiLinks(md: string): string[] {
  const out: string[] = []
  const re = /\[\[([^\]\n]+)\]\]/g
  let m: RegExpExecArray | null
  while ((m = re.exec(md))) {
    const t = m[1].trim()
    if (t) out.push(t)
  }
  return out
}

// buildBrainGraph turns apps + their brain contents into nodes and edges.
// Every app is a node (ghost when it has no brain yet); wikilinks that match
// an app name (by slugKey) become edges; unmatched links become ghost concept
// nodes. Self-links and duplicate edges are dropped.
export function buildBrainGraph(
  apps: { id: string; name: string }[],
  brains: Record<string, string | null | undefined>,
): { nodes: BrainNode[]; edges: BrainEdge[] } {
  const bySlug = new Map<string, string>() // slug -> app id
  for (const a of apps) bySlug.set(slugKey(a.name), a.id)

  const nodes: BrainNode[] = apps.map((a) => {
    const md = brains[a.id]
    return {
      id: a.id,
      label: a.name,
      ghost: typeof md !== 'string',
      lines: typeof md === 'string' ? md.split('\n').length : 0,
    }
  })

  const edges: BrainEdge[] = []
  const seen = new Set<string>()
  const ghosts = new Map<string, string>() // slug -> label as first written

  for (const a of apps) {
    const md = brains[a.id]
    if (typeof md !== 'string') continue
    for (const target of parseWikiLinks(md)) {
      const slug = slugKey(target)
      if (!slug) continue
      let to = bySlug.get(slug)
      if (to === a.id) continue // self-link
      if (!to) {
        to = `ghost:${slug}`
        if (!ghosts.has(slug)) ghosts.set(slug, target)
      }
      const key = `${a.id}->${to}`
      if (seen.has(key)) continue
      seen.add(key)
      edges.push({ from: a.id, to })
    }
  }

  for (const [slug, label] of ghosts) {
    nodes.push({ id: `ghost:${slug}`, label, ghost: true, lines: 0 })
  }

  return { nodes, edges }
}

// brainExcerpt: the section under `## Current state` when present (lenient —
// renamed sections fall back to the first non-heading lines). Comments and
// blank lines are skipped; at most 5 lines, joined for a one-card summary.
export function brainExcerpt(md: string): string {
  const lines = md.split('\n')
  const start = lines.findIndex((l) => /^##\s+current state/i.test(l))
  const body: string[] = []
  const from = start >= 0 ? start + 1 : 1
  for (let i = from; i < lines.length && body.length < 5; i++) {
    const l = lines[i]
    if (start >= 0 && /^##\s/.test(l)) break
    const t = l.trim()
    if (t === '' || t.startsWith('<!--') || t.startsWith('#')) continue
    body.push(t)
  }
  return body.join(' · ')
}

// splitInline breaks one line into renderable segments for the Brain tab's
// view mode: plain text, `code`, **bold**, and [[wikilinks]]. Kept as data so
// the component stays dumb and this logic stays testable.
export type InlineSeg =
  | { kind: 'text'; v: string }
  | { kind: 'code'; v: string }
  | { kind: 'bold'; v: string }
  | { kind: 'wiki'; v: string }

export function splitInline(line: string): InlineSeg[] {
  const out: InlineSeg[] = []
  const re = /\[\[([^\]\n]+)\]\]|`([^`\n]+)`|\*\*([^*\n]+)\*\*/g
  let last = 0
  let m: RegExpExecArray | null
  while ((m = re.exec(line))) {
    if (m.index > last) out.push({ kind: 'text', v: line.slice(last, m.index) })
    if (m[1] !== undefined) out.push({ kind: 'wiki', v: m[1].trim() })
    else if (m[2] !== undefined) out.push({ kind: 'code', v: m[2] })
    else out.push({ kind: 'bold', v: m[3] })
    last = m.index + m[0].length
  }
  if (last < line.length) out.push({ kind: 'text', v: line.slice(last) })
  return out
}
