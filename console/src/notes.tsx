// Minimal markdown renderer for release notes. It understands what GitHub's
// generated notes and hand-written changelogs use — headings, bullet lists,
// paragraphs, bold, inline code, links — and nothing else. Everything is
// emitted as React elements, so text is always escaped; only http(s) links
// become anchors, anything else stays literal text.
import type { ReactNode } from 'react'

export type Block =
  | { kind: 'heading'; level: number; text: string }
  | { kind: 'list'; items: string[] }
  | { kind: 'para'; text: string }

export function parseNotes(md: string): Block[] {
  const blocks: Block[] = []
  let para: string[] = []
  let list: string[] | null = null
  const flush = () => {
    if (para.length) { blocks.push({ kind: 'para', text: para.join(' ') }); para = [] }
    if (list) { blocks.push({ kind: 'list', items: list }); list = null }
  }
  for (const raw of md.replace(/\r\n/g, '\n').split('\n')) {
    const line = raw.trim()
    if (!line) { flush(); continue }
    const h = /^(#{1,6})\s+(.*?)\s*#*$/.exec(line)
    if (h) { flush(); blocks.push({ kind: 'heading', level: h[1].length, text: h[2] }); continue }
    const li = /^[-*+]\s+(.*)$/.exec(line)
    if (li) {
      if (para.length) { blocks.push({ kind: 'para', text: para.join(' ') }); para = [] }
      ;(list ??= []).push(li[1])
      continue
    }
    if (list) { blocks.push({ kind: 'list', items: list }); list = null }
    para.push(line)
  }
  flush()
  return blocks
}

const INLINE = /(`[^`]+`)|(\*\*[^*]+\*\*)|(\[[^\]]+\]\((https?:\/\/[^)\s]+)\))|(https?:\/\/[^\s<>)]+)/g

// Inline markup → React nodes. Anything not matched is plain (escaped) text.
export function renderInline(text: string): ReactNode[] {
  const out: ReactNode[] = []
  let last = 0
  let k = 0
  for (const m of text.matchAll(INLINE)) {
    const at = m.index ?? 0
    if (at > last) out.push(text.slice(last, at))
    const tok = m[0]
    if (m[1]) out.push(<code key={k++} style={{ fontFamily: 'var(--mono)', fontSize: '0.92em', padding: '0 5px', borderRadius: 5, background: 'rgba(255,255,255,.07)' }}>{tok.slice(1, -1)}</code>)
    else if (m[2]) out.push(<b key={k++}>{tok.slice(2, -2)}</b>)
    else if (m[3]) out.push(<a key={k++} href={m[4]} target="_blank" rel="noreferrer">{tok.slice(1, tok.indexOf(']'))}</a>)
    else out.push(<a key={k++} href={tok} target="_blank" rel="noreferrer">{shortUrl(tok)}</a>)
    last = at + tok.length
  }
  if (last < text.length) out.push(text.slice(last))
  return out
}

// GitHub notes end every line with a full PR URL; show it as "#123".
function shortUrl(u: string): string {
  const pr = /github\.com\/[^/]+\/[^/]+\/pull\/(\d+)$/.exec(u)
  return pr ? `#${pr[1]}` : u
}

export function Notes({ md, testid }: { md: string; testid?: string }) {
  return (
    <div data-testid={testid} style={{ fontSize: 12.5, lineHeight: 1.5 }}>
      {parseNotes(md).map((b, i) => {
        if (b.kind === 'heading') {
          const size = b.level <= 2 ? 13.5 : 12.5
          return <div key={i} style={{ fontWeight: 600, fontSize: size, margin: '10px 0 4px' }}>{renderInline(b.text)}</div>
        }
        if (b.kind === 'list') {
          return (
            <ul key={i} style={{ margin: '2px 0 6px', paddingLeft: 18 }}>
              {b.items.map((it, j) => <li key={j}>{renderInline(it)}</li>)}
            </ul>
          )
        }
        return <p key={i} style={{ margin: '4px 0' }}>{renderInline(b.text)}</p>
      })}
    </div>
  )
}
