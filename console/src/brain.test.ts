import { describe, it, expect } from 'vitest'
import { parseWikiLinks, slugKey, buildBrainGraph, brainExcerpt, splitInline, AppBrain } from './brain'

const hb = (hub: string | null, spokes: Record<string, string> = {}): AppBrain => ({ hub, spokes })

describe('parseWikiLinks', () => {
  it('finds links, trims, keeps order', () => {
    expect(parseWikiLinks('uses [[Auth Brick]] and [[ api-gateway ]]')).toEqual(['Auth Brick', 'api-gateway'])
  })
  it('ignores empty and multiline targets', () => {
    expect(parseWikiLinks('[[  ]] and [[a\nb]]')).toEqual([])
  })
  it('returns nothing for plain markdown', () => {
    expect(parseWikiLinks('# title\n[normal](link)')).toEqual([])
  })
})

describe('slugKey', () => {
  it('matches names case/space/punctuation-insensitively', () => {
    expect(slugKey('API Gateway')).toBe(slugKey('api-gateway'))
    expect(slugKey('  Auth  Brick! ')).toBe('auth-brick')
  })
})

describe('buildBrainGraph', () => {
  const apps = [
    { id: 'a1', name: 'api-gateway' },
    { id: 'a2', name: 'Todo App' },
    { id: 'a3', name: 'no-brain-yet' },
  ]

  it('links brains to apps by name and creates concept ghosts for unresolved targets', () => {
    const { nodes, edges } = buildBrainGraph(apps, {
      a1: hb('talks to [[Todo App]] and shares [[Auth Brick]]'),
      a2: hb('fronted by [[API Gateway]]'),
      a3: null,
    })
    expect(edges).toContainEqual({ from: 'a1', to: 'a2' })
    expect(edges).toContainEqual({ from: 'a2', to: 'a1' })
    expect(edges).toContainEqual({ from: 'a1', to: 'ghost:auth-brick' })
    const ghost = nodes.find((n) => n.id === 'ghost:auth-brick')
    expect(ghost?.kind).toBe('concept')
    expect(ghost?.empty).toBe(true)
    expect(ghost?.label).toBe('Auth Brick')
  })

  it('marks apps without brains as empty but never invents edges from them', () => {
    const { nodes, edges } = buildBrainGraph(apps, { a1: hb('x'), a2: hb('y'), a3: null })
    expect(nodes.find((n) => n.id === 'a3')?.empty).toBe(true)
    expect(edges).toEqual([])
  })

  it('drops self-links and duplicate edges', () => {
    const { edges } = buildBrainGraph(apps, {
      a1: hb('[[api-gateway]] then [[Todo App]] and again [[todo app]]'),
      a2: null, a3: null,
    })
    expect(edges).toEqual([{ from: 'a1', to: 'a2' }])
  })

  it('adds spoke nodes with an implicit hub→spoke edge', () => {
    const { nodes, edges } = buildBrainGraph(apps, {
      a1: hb('details in [[decisions]]', { decisions: '- D1: chose postgres' }),
      a2: null, a3: null,
    })
    const spoke = nodes.find((n) => n.kind === 'spoke')
    expect(spoke?.id).toBe('a1:decisions')
    expect(spoke?.appId).toBe('a1')
    // one implicit ownership edge; the [[decisions]] wikilink dedupes into it
    expect(edges.filter((e) => e.from === 'a1' && e.to === 'a1:decisions')).toHaveLength(1)
  })

  it('resolves own spokes before app names and lets spokes link outward', () => {
    const { edges } = buildBrainGraph(apps, {
      a1: hb('see [[notes]]', { notes: 'depends on [[Todo App]] and hits [[err-pg-hang]]' }),
      a2: null, a3: null,
    })
    expect(edges).toContainEqual({ from: 'a1:notes', to: 'a2' })
    expect(edges).toContainEqual({ from: 'a1:notes', to: 'ghost:err-pg-hang' })
  })

  it('unifies the same concept across apps and ranks shared/error concepts', () => {
    const { nodes, concepts } = buildBrainGraph(apps, {
      a1: hb('hit [[err-pg-hang]] again, uses [[postgres]]'),
      a2: hb('also hit [[err-pg-hang]]'),
      a3: null,
    })
    // one shared node, not two
    expect(nodes.filter((n) => n.id === 'ghost:err-pg-hang')).toHaveLength(1)
    const err = concepts.find((c) => c.slug === 'err-pg-hang')
    expect(err).toMatchObject({ apps: 2, mentions: 2, isError: true })
    // shared (2 apps) ranks above single-app postgres
    expect(concepts[0].slug).toBe('err-pg-hang')
    expect(concepts.find((c) => c.slug === 'postgres')?.isError).toBe(false)
  })
})

describe('brainExcerpt', () => {
  it('takes the Current state section, skipping comments and blanks', () => {
    const md = '# Brain\n\n## Current state\n<!-- hint -->\nAdd works.\nNext: persistence.\n\n## Decisions\n- D1'
    expect(brainExcerpt(md)).toBe('Add works. · Next: persistence.')
  })
  it('falls back to first content lines when the section is renamed', () => {
    const md = '# Brain\nSome intro line.\n## Où on en est\nnope'
    expect(brainExcerpt(md)).toContain('Some intro line.')
  })
})

describe('splitInline', () => {
  it('splits text, code, bold and wikilinks', () => {
    expect(splitInline('run `make` for **speed** with [[Auth Brick]]')).toEqual([
      { kind: 'text', v: 'run ' },
      { kind: 'code', v: 'make' },
      { kind: 'text', v: ' for ' },
      { kind: 'bold', v: 'speed' },
      { kind: 'text', v: ' with ' },
      { kind: 'wiki', v: 'Auth Brick' },
    ])
  })
  it('passes plain lines through untouched', () => {
    expect(splitInline('nothing special')).toEqual([{ kind: 'text', v: 'nothing special' }])
  })
})
