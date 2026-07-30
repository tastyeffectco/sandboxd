// Project Brain — pure logic (no React, no fetch) so it is unit-testable:
// wikilink parsing, the cross-brain knowledge graph (hub + spoke notes +
// concept ghosts), shared-concept stats, the "Current state" excerpt, and the
// inline splitter the Brain tab's view mode renders from.

// An app's brain: the hub (BRAIN.md) plus optional spoke notes under brain/
// ("decisions" -> content of brain/decisions.md). Spokes hold topics that
// outgrew the hub; the hub links to them.
export interface AppBrain {
  hub: string | null
  spokes: Record<string, string>
}

export interface BrainNode {
  id: string        // app id | "<appId>:<spoke-slug>" | "ghost:<slug>"
  label: string
  kind: 'app' | 'spoke' | 'concept'
  lines: number
  appId?: string    // spokes: the owning app (click target)
  empty: boolean    // dashed in the graph: app without a brain, or a concept
}

export interface BrainEdge { from: string; to: string }

// A concept: a [[wikilink]] target that resolves to no app and no spoke.
// Mentioned across >1 app it is shared knowledge — an err-* concept with
// multiple apps is the repeat-mistake signal.
export interface ConceptStat {
  slug: string
  label: string
  mentions: number  // total inbound links
  apps: number      // distinct apps linking it
  isError: boolean  // err-* naming convention
}

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

// buildBrainGraph turns apps + their brains into the knowledge graph.
// Nodes: every app (dashed when brainless), every spoke note, every unresolved
// concept. Edges: hub→spoke (details live there), plus each [[wikilink]] —
// resolved own-spokes first, then app names, else a concept ghost. Ghost slugs
// are GLOBAL, so two apps linking [[err-x]] converge on one shared node —
// cross-project backlinks with no central vault. Self-links and duplicate
// edges are dropped.
export function buildBrainGraph(
  apps: { id: string; name: string }[],
  brains: Record<string, AppBrain | null | undefined>,
): { nodes: BrainNode[]; edges: BrainEdge[]; concepts: ConceptStat[] } {
  const appBySlug = new Map<string, string>()
  for (const a of apps) appBySlug.set(slugKey(a.name), a.id)

  const nodes: BrainNode[] = []
  const edges: BrainEdge[] = []
  const seen = new Set<string>()
  const concepts = new Map<string, { label: string; mentions: number; apps: Set<string> }>()

  const addEdge = (from: string, to: string) => {
    const key = `${from}->${to}`
    if (from === to || seen.has(key)) return
    seen.add(key)
    edges.push({ from, to })
  }

  for (const a of apps) {
    const b = brains[a.id]
    const hub = b && typeof b.hub === 'string' ? b.hub : null
    const spokes = (b && b.spokes) || {}
    const spokeNames = Object.keys(spokes)

    nodes.push({
      id: a.id, label: a.name, kind: 'app', appId: a.id,
      lines: hub ? hub.split('\n').length : 0,
      empty: hub === null && spokeNames.length === 0,
    })

    const spokeIdBySlug = new Map<string, string>()
    for (const name of spokeNames) {
      const sid = `${a.id}:${slugKey(name)}`
      spokeIdBySlug.set(slugKey(name), sid)
      nodes.push({ id: sid, label: name, kind: 'spoke', appId: a.id, lines: spokes[name].split('\n').length, empty: false })
      addEdge(a.id, sid) // the hub owns its spokes
    }

    const docs: { source: string; md: string }[] = []
    if (hub) docs.push({ source: a.id, md: hub })
    for (const name of spokeNames) docs.push({ source: spokeIdBySlug.get(slugKey(name))!, md: spokes[name] })

    for (const doc of docs) {
      for (const target of parseWikiLinks(doc.md)) {
        const slug = slugKey(target)
        if (!slug) continue
        const to = spokeIdBySlug.get(slug) ?? appBySlug.get(slug)
        if (to) { addEdge(doc.source, to); continue }
        addEdge(doc.source, `ghost:${slug}`)
        const cs = concepts.get(slug) || { label: target, mentions: 0, apps: new Set<string>() }
        cs.mentions++; cs.apps.add(a.id)
        concepts.set(slug, cs)
      }
    }
  }

  for (const [slug, cs] of concepts) {
    nodes.push({ id: `ghost:${slug}`, label: cs.label, kind: 'concept', lines: 0, empty: true })
  }

  const stats: ConceptStat[] = [...concepts.entries()]
    .map(([slug, cs]) => ({ slug, label: cs.label, mentions: cs.mentions, apps: cs.apps.size, isError: slug.startsWith('err-') }))
    .sort((x, y) => y.apps - x.apps || y.mentions - x.mentions || x.slug.localeCompare(y.slug))

  return { nodes, edges, concepts: stats }
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
