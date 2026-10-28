import { afterEach, describe, expect, it, vi } from 'vitest'
import { cleanup, fireEvent, render, screen, waitFor } from '@testing-library/react'
import { ClaudeSignIn } from './ClaudeSignIn'
import { api } from './api'

vi.mock('./api', () => ({
  api: {
    claudeSignInStatus: vi.fn(),
    startClaudeSignIn: vi.fn(),
    finishClaudeSignIn: vi.fn(),
    signOutClaude: vi.fn(),
  },
}))

afterEach(cleanup)

describe('ClaudeSignIn', () => {
  it("signs Claude Code in on Claude's own page and reports the result", async () => {
    vi.mocked(api.claudeSignInStatus).mockResolvedValue({ logged_in: false })
    vi.mocked(api.startClaudeSignIn).mockResolvedValue({ url: 'https://claude.com/cai/oauth/authorize?x=1' })
    vi.mocked(api.finishClaudeSignIn).mockResolvedValue({ logged_in: true, method: 'claude.ai' })
    const signedIn = vi.fn()
    render(<ClaudeSignIn sandboxId="sb" onSignedIn={signedIn} />)
    fireEvent.click(await screen.findByTestId('claude-sign-in-start'))
    const link = (await screen.findByTestId('claude-sign-in-link')) as HTMLAnchorElement
    expect(link.href).toBe('https://claude.com/cai/oauth/authorize?x=1')
    expect(link.target).toBe('_blank')
    fireEvent.change(screen.getByTestId('claude-sign-in-code'), { target: { value: ' code-from-claude ' } })
    fireEvent.click(screen.getByTestId('claude-sign-in-finish'))
    await screen.findByTestId('claude-signed-in')
    expect(api.finishClaudeSignIn).toHaveBeenCalledWith('sb', 'code-from-claude')
    expect(signedIn).toHaveBeenLastCalledWith(true)
  })

  it('signs out and offers signing in again', async () => {
    vi.mocked(api.claudeSignInStatus).mockResolvedValue({ logged_in: true, method: 'claude.ai' })
    vi.mocked(api.signOutClaude).mockResolvedValue({ logged_in: false })
    const signedIn = vi.fn()
    render(<ClaudeSignIn sandboxId="sb" onSignedIn={signedIn} />)
    fireEvent.click(await screen.findByText('Sign out'))
    await screen.findByTestId('claude-sign-in-start')
    await waitFor(() => expect(signedIn).toHaveBeenLastCalledWith(false))
  })

  it('shows why a sign-in failed', async () => {
    vi.mocked(api.claudeSignInStatus).mockResolvedValue({ logged_in: false })
    vi.mocked(api.startClaudeSignIn).mockRejectedValue(new Error('start the project to sign in with Claude'))
    render(<ClaudeSignIn sandboxId="sb" onSignedIn={() => {}} />)
    fireEvent.click(await screen.findByTestId('claude-sign-in-start'))
    expect((await screen.findByRole('alert')).textContent).toContain(
      'start the project to sign in with Claude',
    )
  })
})
