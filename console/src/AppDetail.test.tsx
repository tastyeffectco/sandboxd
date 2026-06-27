import { describe, it, expect, beforeEach, vi } from 'vitest'
import { render, screen, fireEvent } from '@testing-library/react'
import { AppDetail } from './AppDetail'
import {
  installFetch,
  appDetailRoutes,
  webSandboxFixture,
  workerSandboxFixture,
  unhealthySandboxFixture,
  gitStatusPristineFixture,
  appsFixture,
} from './test/fixtures'

const noop = () => {}

describe('app detail — web app', () => {
  beforeEach(() => installFetch(appDetailRoutes(webSandboxFixture)))

  it('renders preview/endpoint, processes, activity, and config sections', async () => {
    render(<AppDetail appId="01APPAAAAAAAAAAAAAAAAAAAAA" onError={noop} onInfo={noop} />)
    expect(await screen.findByTestId('processes-panel')).toBeTruthy()
    expect(await screen.findByTestId('activity-panel')).toBeTruthy()
    expect(await screen.findByTestId('config-panel')).toBeTruthy()
    // "Preview / endpoint" (not just "Preview") — API/worker apps are valid too
    expect(screen.getByText(/Preview \/ endpoint/i)).toBeTruthy()
    // the web process is listed
    expect(await screen.findByTestId('processes-list')).toBeTruthy()
  })

  it('config panel redacts the sensitive value but shows the non-sensitive one', async () => {
    render(<AppDetail appId="01APPAAAAAAAAAAAAAAAAAAAAA" onError={noop} onInfo={noop} />)
    await screen.findByTestId('config-list')
    expect(screen.getByText('API_KEY')).toBeTruthy()   // present...
    expect(screen.queryByText('super-secret')).toBeNull() // ...but no plaintext secret
    expect(screen.getByText('debug')).toBeTruthy()        // non-sensitive value shown
  })

  it('renders advisory runtime detection (suggestion, confidence, detect-only + warning)', async () => {
    render(<AppDetail appId="01APPAAAAAAAAAAAAAAAAAAAAA" onError={noop} onInfo={noop} />)
    const panel = await screen.findByTestId('runtime-inspect')
    expect(panel.textContent).toMatch(/nextjs/)
    expect(panel.textContent).toMatch(/high/)
    expect(panel.textContent).toMatch(/suggested/) // default_suggestion marker
    // astro is detect-only and warns; never presented as a runnable default
    expect(panel.textContent).toMatch(/detect-only/)
    expect(panel.textContent).toMatch(/4321/)
    expect(panel.textContent).toMatch(/Advisory only/i)
  })

  it('shows Git status, splits user vs runtime files, and loads a diff', async () => {
    render(<AppDetail appId="01APPAAAAAAAAAAAAAAAAAAAAA" onError={noop} onInfo={noop} />)
    const panel = await screen.findByTestId('git-panel')
    expect(panel.textContent).toMatch(/no push yet/i) // local commit only; clearly no push
    expect(panel.textContent).toMatch(/main/)         // branch
    // user files listed
    expect(await screen.findByTestId('git-files')).toBeTruthy()
    expect(screen.getByTestId('git-files').textContent).toMatch(/src\/App\.tsx/)
    // runtime-generated files are shown SEPARATELY, labelled as not-your-edits
    const rt = screen.getByTestId('git-runtime-files')
    expect(rt.textContent).toMatch(/sandbox\.yaml/)
    expect(rt.textContent).toMatch(/not your edits/i)
    // sandbox.yaml must NOT appear among user files
    expect(screen.getByTestId('git-files').textContent).not.toMatch(/sandbox\.yaml/)
    // commit exists (B1) but NO push control yet
    expect(screen.getByTestId('git-commit')).toBeTruthy()
    expect(screen.queryByRole('button', { name: /push/i })).toBeNull()
    // diff loads on demand (the fixed diff endpoint)
    fireEvent.click(screen.getByTestId('git-view-diff'))
    expect((await screen.findByTestId('git-diff')).textContent).toMatch(/const x = 1/)
  })

  it('commits selected user files (default), runtime unchecked, posts the right body, shows sha', async () => {
    let posted: { message: string; paths: string[]; runtime_paths: string[] } | null = null
    installFetch((m, p) => {
      if (m === 'POST' && /\/v1\/apps\/[^/]+\/git\/commit/.test(p)) {
        return { committed: true, sha: 'deadbeefcafe', branch: 'main', files_committed: ['src/App.tsx'] }
      }
      return appDetailRoutes(webSandboxFixture)(m, p)
    })
    const realFetch = globalThis.fetch
    globalThis.fetch = vi.fn(async (input: unknown, init?: { method?: string; body?: string }) => {
      if ((init?.method || 'GET').toUpperCase() === 'POST' && String(input).includes('/git/commit')) {
        posted = init?.body ? JSON.parse(init.body) : null
      }
      return realFetch(input as never, init as never)
    }) as unknown as typeof fetch
    render(<AppDetail appId="01APPAAAAAAAAAAAAAAAAAAAAA" onError={noop} onInfo={noop} />)
    await screen.findByTestId('git-commit-box')
    // user file is checked by default; runtime file is NOT
    expect((screen.getByTestId('git-pick-src/App.tsx') as HTMLInputElement).checked).toBe(true)
    expect((screen.getByTestId('git-rtpick-sandbox.yaml') as HTMLInputElement).checked).toBe(false)
    // commit disabled until a message is entered
    expect((screen.getByTestId('git-commit') as HTMLButtonElement).disabled).toBe(true)
    fireEvent.change(screen.getByTestId('git-commit-message'), { target: { value: 'my change' } })
    expect((screen.getByTestId('git-commit') as HTMLButtonElement).disabled).toBe(false)
    fireEvent.click(screen.getByTestId('git-commit'))
    await screen.findByTestId('git-committed-sha')
    expect(posted!.message).toBe('my change')
    expect(posted!.paths).toContain('src/App.tsx')
    expect(posted!.paths).toContain('notes.md')
    expect(posted!.runtime_paths).toEqual([]) // runtime excluded by default
    expect(screen.getByTestId('git-committed-sha').textContent).toMatch(/deadbeef/)
  })

  it('represents a pristine import honestly (clean to the user, runtime files surfaced)', async () => {
    installFetch((m, p) => {
      if (/\/v1\/apps\/[^/]+\/git\/status/.test(p)) return gitStatusPristineFixture
      return appDetailRoutes(webSandboxFixture)(m, p)
    })
    render(<AppDetail appId="01APPAAAAAAAAAAAAAAAAAAAAA" onError={noop} onInfo={noop} />)
    await screen.findByTestId('git-panel')
    // user-facing state is "clean" (no user edits) even though raw repo isn't
    expect(await screen.findByTestId('git-clean')).toBeTruthy()
    // no user-file list, but the runtime files are disclosed
    expect(screen.queryByTestId('git-files')).toBeNull()
    expect(screen.getByTestId('git-runtime-files').textContent).toMatch(/pnpm-lock\.yaml/)
  })

  it('double-clicking Commit sends only one request', async () => {
    let commitCalls = 0
    installFetch((m, p) => {
      if (m === 'POST' && /\/v1\/apps\/[^/]+\/git\/commit/.test(p)) {
        commitCalls++
        return { committed: true, sha: 'abc12345', branch: 'main', files_committed: ['src/App.tsx'] }
      }
      return appDetailRoutes(webSandboxFixture)(m, p)
    })
    render(<AppDetail appId="01APPAAAAAAAAAAAAAAAAAAAAA" onError={noop} onInfo={noop} />)
    await screen.findByTestId('git-commit-box')
    fireEvent.change(screen.getByTestId('git-commit-message'), { target: { value: 'one commit' } })
    const btn = screen.getByTestId('git-commit')
    fireEvent.click(btn)
    fireEvent.click(btn) // immediate second click before the request resolves
    await screen.findByTestId('git-committed-sha')
    expect(commitCalls).toBe(1)
  })

  it('git push: shown for imported apps, requires explicit confirm, posts branch, shows result', async () => {
    let pushed: { branch?: string } | null = null
    installFetch((m, p) => {
      if (m === 'POST' && /\/v1\/apps\/[^/]+\/git\/push/.test(p)) {
        return { pushed: true, branch: 'my-feature', commits: 2, remote_url: 'https://github.com/o/r' }
      }
      if (/\/v1\/apps\/[^/]+$/.test(p)) {
        return { ...appsFixture[0], git: { repo_url: 'https://github.com/o/r', branch: 'main' } }
      }
      return appDetailRoutes(webSandboxFixture)(m, p)
    })
    const realFetch = globalThis.fetch
    globalThis.fetch = vi.fn(async (input: unknown, init?: { method?: string; body?: string }) => {
      if ((init?.method || 'GET').toUpperCase() === 'POST' && String(input).includes('/git/push')) {
        pushed = init?.body ? JSON.parse(init.body) : null
      }
      return realFetch(input as never, init as never)
    }) as unknown as typeof fetch

    render(<AppDetail appId="01APPAAAAAAAAAAAAAAAAAAAAA" onError={noop} onInfo={noop} />)
    const box = await screen.findByTestId('git-push-box')
    expect(box.textContent).toMatch(/new branch/i) // explicit remote-write framing
    expect(box.textContent).toMatch(/github\.com/)
    // requires explicit confirm: no push happens on the first click
    fireEvent.change(screen.getByTestId('git-push-branch'), { target: { value: 'my-feature' } })
    fireEvent.click(screen.getByTestId('git-push-start'))
    expect(pushed).toBeNull()
    expect(screen.getByTestId('git-push-confirm')).toBeTruthy()
    // confirm -> posts
    fireEvent.click(screen.getByTestId('git-push-confirm-yes'))
    await screen.findByTestId('git-push-result')
    expect(pushed!.branch).toBe('my-feature')
    expect(screen.getByTestId('git-push-result').textContent).toMatch(/my-feature/)
  })

  it('delete control says it removes the workspace (v1 DELETE purges)', async () => {
    render(<AppDetail appId="01APPAAAAAAAAAAAAAAAAAAAAA" onError={noop} onInfo={noop} />)
    const del = await screen.findByTestId('delete-sandbox')
    // v1 DELETE purges the workspace, so the wording must say so — not just "Delete sandbox".
    expect(del.textContent).toMatch(/delete sandbox and workspace/i)
  })
})

describe('app detail — unhealthy sandbox', () => {
  beforeEach(() => installFetch(appDetailRoutes(unhealthySandboxFixture)))

  it('does not present a stopped/unhealthy sandbox as running', async () => {
    render(<AppDetail appId="01APPAAAAAAAAAAAAAAAAAAAAA" onError={noop} onInfo={noop} />)
    await screen.findByTestId('processes-list')
    // no live preview iframe for a non-serving sandbox
    expect(screen.queryByTestId('preview')).toBeNull()
    expect(screen.getByTestId('preview-empty')).toBeTruthy()
    expect(screen.getByText(/Sandbox not running/i)).toBeTruthy()
    // the web process is shown stopped, never running
    const row = screen.getByTestId('process-web')
    expect(row.textContent).toMatch(/stopped/i)
    expect(row.textContent).not.toMatch(/\brunning\b/i)
  })
})

describe('app detail — worker-only app', () => {
  beforeEach(() => installFetch(appDetailRoutes(workerSandboxFixture)))

  it('renders the no-public-endpoint state as valid (not an error)', async () => {
    render(<AppDetail appId="01APPAAAAAAAAAAAAAAAAAAAAA" onError={noop} onInfo={noop} />)
    expect(await screen.findByTestId('preview-empty')).toBeTruthy()
    expect(screen.getByText(/No public endpoint/i)).toBeTruthy()
    // the worker process still shows up
    expect(await screen.findByTestId('processes-list')).toBeTruthy()
  })
})
