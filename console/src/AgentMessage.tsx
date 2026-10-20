import { Fragment, ReactNode } from 'react'
import { splitInline } from './brain'

const inline = (text: string) =>
  splitInline(text).map((part, index) =>
    part.kind === 'code' ? (
      <code key={index}>{part.v}</code>
    ) : part.kind === 'bold' ? (
      <strong key={index}>{part.v}</strong>
    ) : (
      <Fragment key={index}>{part.kind === 'wiki' ? `[[${part.v}]]` : part.v}</Fragment>
    ),
  )

// Render the common structures returned by coding agents as React text.
// Raw HTML, embedded media and executable links are never interpreted.
export function AgentMessage({ text }: { text: string }) {
  const blocks: ReactNode[] = []
  const lines = text.split('\n')
  for (let i = 0; i < lines.length; i++) {
    const line = lines[i]
    if (!line.trim()) continue
    const fence = line.match(/^\s*(`{3,}|~{3,})(.*)$/)
    if (fence) {
      const code: string[] = []
      const marker = fence[1]
      while (++i < lines.length && !lines[i].trim().startsWith(marker)) code.push(lines[i])
      blocks.push(
        <div className="message-code" key={i}>
          {fence[2].trim() && <span>{fence[2].trim()}</span>}
          <pre>
            <code>{code.join('\n')}</code>
          </pre>
        </div>,
      )
    } else if (/^#{1,6}\s/.test(line)) {
      blocks.push(<h4 key={i}>{inline(line.replace(/^#{1,6}\s+/, ''))}</h4>)
    } else if (/^\s*([-*]|\d+[.)])\s/.test(line)) {
      const ordered = /^\s*\d/.test(line)
      const items: ReactNode[] = []
      const pattern = ordered ? /^\s*\d+[.)]\s+/ : /^\s*[-*]\s+/
      do {
        items.push(<li key={i}>{inline(lines[i].replace(pattern, ''))}</li>)
        i++
      } while (i < lines.length && pattern.test(lines[i]))
      i--
      blocks.push(ordered ? <ol key={i}>{items}</ol> : <ul key={i}>{items}</ul>)
    } else {
      blocks.push(<p key={i}>{inline(line)}</p>)
    }
  }
  return <div className="message-content">{blocks}</div>
}
