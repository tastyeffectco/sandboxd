import { describe, it, expect } from 'vitest'
import { parseWikiLinks, slugKey, buildBrainGraph, brainExcerpt, splitInline } from './brain'

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

  it('links brains to apps by name and creates ghosts for unresolved targets', () => {
    const { nodes, edges } = buildBrainGraph(apps, {
      a1: 'talks to [[Todo App]] and shares [[Auth Brick]]',
      a2: 'fronted by [[API Gateway]]',
      a3: null,
    })
    expect(edges).toContainEqual({ from: 'a1', to: 'a2' })
    expect(edges).toContainEqual({ from: 'a2', to: 'a1' })
    expect(edges).toContainEqual({ from: 'a1', to: 'ghost:auth-brick' })
    const ghost = nodes.find((n) => n.id === 'ghost:auth-brick')
    expect(ghost?.ghost).toBe(true)
    expect(ghost?.label).toBe('Auth Brick')
  })

  it('marks apps without brains as ghost nodes but never invents edges from them', () => {
    const { nodes, edges } = buildBrainGraph(apps, { a1: 'x', a2: 'y', a3: null })
    expect(nodes.find((n) => n.id === 'a3')?.ghost).toBe(true)
    expect(edges).toEqual([])
  })

  it('drops self-links and duplicate edges', () => {
    const { edges } = buildBrainGraph(apps, {
      a1: '[[api-gateway]] then [[Todo App]] and again [[todo app]]',
      a2: null, a3: null,
    })
    expect(edges).toEqual([{ from: 'a1', to: 'a2' }])
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
