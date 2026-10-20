import { render, screen } from '@testing-library/react'
import { describe, expect, it } from 'vitest'
import { AgentMessage } from './AgentMessage'

describe('agent response rendering', () => {
  it('renders headings, lists, inline code and an incomplete streaming fence', () => {
    const { container } = render(
      <AgentMessage
        text={'## Result\n- Updated **layout**\n- Changed `src/App.tsx`\n```tsx\nconst ready = true'}
      />,
    )
    expect(screen.getByRole('heading', { name: 'Result' })).toBeTruthy()
    expect(screen.getAllByRole('listitem')).toHaveLength(2)
    expect(container.querySelector('strong')?.textContent).toBe('layout')
    expect(container.querySelector('pre')?.textContent).toBe('const ready = true')
  })

  it('keeps HTML and executable URLs inert', () => {
    const text =
      '<img src=x onerror=alert(1)>\n[click](javascript:alert(1))\n```html\n<script>bad()</script>\n```'
    const { container } = render(<AgentMessage text={text} />)
    expect(container.querySelectorAll('img, script, a')).toHaveLength(0)
    expect(container.textContent).toContain('<img src=x onerror=alert(1)>')
    expect(container.textContent).toContain('<script>bad()</script>')
  })
})
