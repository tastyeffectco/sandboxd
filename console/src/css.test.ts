// @ts-expect-error the console has no Node types; Vitest runs this file in Node.
import { readFileSync } from 'node:fs'
import { describe, expect, it } from 'vitest'

// A lost "}" (easy to drop when resolving a merge) silently disables every
// rule after it. Check each stylesheet opens and closes its blocks in step.
const sheets = ['./design.css', './desktop.css', './design/theme.css']

describe('stylesheets', () => {
  it.each(sheets)('%s has balanced braces', (path) => {
    const source: string = readFileSync(new URL(path, import.meta.url), 'utf8')
    expect(source.length).toBeGreaterThan(100)
    const css = source.replace(/\/\*[\s\S]*?\*\//g, '').replace(/"(?:\\.|[^"\\])*"|'(?:\\.|[^'\\])*'/g, '""')
    let depth = 0
    for (const ch of css) {
      if (ch === '{') depth++
      if (ch === '}') depth--
      expect(depth, 'a "}" closes nothing').toBeGreaterThanOrEqual(0)
    }
    expect(depth, 'a "{" is never closed').toBe(0)
  })
})
