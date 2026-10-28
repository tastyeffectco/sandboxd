import { afterEach, describe, expect, it, vi } from 'vitest'
import { cleanup, render, screen } from '@testing-library/react'
import { PreviewPane } from './AppView'
import { webSandboxFixture } from './test/fixtures'

afterEach(cleanup)

describe('PreviewPane', () => {
  it('loads the preview again when an agent task ends', () => {
    globalThis.fetch = vi.fn(() => new Promise<Response>(() => {})) as unknown as typeof fetch
    const { rerender } = render(<PreviewPane sb={webSandboxFixture} reloadKey={0} />)
    const before = screen.getByTestId('preview')
    rerender(<PreviewPane sb={webSandboxFixture} reloadKey={0} />)
    expect(screen.getByTestId('preview')).toBe(before)
    rerender(<PreviewPane sb={webSandboxFixture} reloadKey={1} />)
    expect(screen.getByTestId('preview')).not.toBe(before)
  })
})
