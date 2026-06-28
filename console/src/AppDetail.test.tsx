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
  gitDiffFixture,
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

  it('renders advisory runtime detection + manifest status/validation/effective', async () => {
    render(<AppDetail appId="01APPAAAAAAAAAAAAAAAAAAAAA" onError={noop} onInfo={noop} />)
    const panel = await screen.findByTestId('runtime-inspect')
    expect(panel.textContent).toMatch(/nextjs/)
    expect(panel.textContent).toMatch(/high/)
    expect(panel.textContent).toMatch(/suggested/) // default_suggestion marker
    expect(panel.textContent).toMatch(/detect-only/) // astro is detect-only
    expect(panel.textContent).toMatch(/4321/)
    expect(panel.textContent).toMatch(/Advisory/i)
    // current sandbox.yaml status + effective view
    expect(screen.getByTestId('ri-manifest-status').textContent).toMatch(/valid/)
    expect(screen.getByTestId('ri-effective').textContent).toMatch(/port: 3000/)
  })

  it('shows invalid manifest errors/warnings', async () => {
    installFetch((m, p) => {
      if (/\/v1\/apps\/[^/]+\/runtime\/manifest/.test(p)) {
        return {
          present: true,
          source: 'sandbox.yaml',
          manifest: 'command: x\n',
          validation: {
            valid: false,
            errors: ["top-level 'command' is not valid — put it under web.command"],
            warnings: ['unknown top-level key "foo" (ignored)'],
            // effective omitted: invalid manifests are never presented as runnable
          },
        }
      }
      return appDetailRoutes(webSandboxFixture)(m, p)
    })
    render(<AppDetail appId="01APPAAAAAAAAAAAAAAAAAAAAA" onError={noop} onInfo={noop} />)
    expect((await screen.findByTestId('ri-manifest-status')).textContent).toMatch(/invalid/)
    expect(screen.getByTestId('ri-errors').textContent).toMatch(/web\.command/)
    expect(screen.getByTestId('ri-warnings').textContent).toMatch(/unknown top-level/)
  })

  it('shows missing manifest state', async () => {
    installFetch((m, p) => {
      if (/\/v1\/apps\/[^/]+\/runtime\/manifest/.test(p)) return { present: false, source: 'default', reason: 'none' }
      return appDetailRoutes(webSandboxFixture)(m, p)
    })
    render(<AppDetail appId="01APPAAAAAAAAAAAAAAAAAAAAA" onError={noop} onInfo={noop} />)
    expect((await screen.findByTestId('ri-manifest-status')).textContent).toMatch(/missing/)
  })

  it('shows suggested YAML; Copy and Ask-agent copy without submitting a task', async () => {
    const writes: string[] = []
    Object.assign(navigator, { clipboard: { writeText: (t: string) => { writes.push(t); return Promise.resolve() } } })
    let taskSubmitted = false
    installFetch((m, p) => {
      if (m === 'POST' && /\/tasks/.test(p)) taskSubmitted = true
      return appDetailRoutes(webSandboxFixture)(m, p)
    })
    render(<AppDetail appId="01APPAAAAAAAAAAAAAAAAAAAAA" onError={noop} onInfo={noop} />)
    // astro suggestion carries advisory YAML
    const yaml = await screen.findByTestId('ri-suggested-yaml-astro')
    expect(yaml.textContent).toMatch(/astro dev/)
    expect(yaml.textContent).toMatch(/--host 0\.0\.0\.0/)
    // Copy YAML
    fireEvent.click(screen.getByTestId('ri-copy-astro'))
    expect(writes[writes.length - 1]).toMatch(/astro dev/)
    // Ask agent -> copies a prompt with schema + suggested YAML; submits NO task
    fireEvent.click(screen.getByTestId('ri-ask-astro'))
    const prompt = writes[writes.length - 1]
    expect(prompt).toMatch(/sandbox\.yaml schema/i)
    expect(prompt).toMatch(/web:/)
    expect(prompt).toMatch(/astro dev/)
    expect(prompt).toMatch(/allowedHosts/) // astro note carried into the prompt
    expect(taskSubmitted).toBe(false)
  })

  it('lists user vs generated files and lazily loads a per-file diff on click', async () => {
    let diffPath: string | null = null
    installFetch((m, p) => {
      const mm = p.match(/\/v1\/apps\/[^/]+\/git\/diff\?path=(.+)$/)
      if (mm) {
        diffPath = decodeURIComponent(mm[1])
        return gitDiffFixture
      }
      return appDetailRoutes(webSandboxFixture)(m, p)
    })
    render(<AppDetail appId="01APPAAAAAAAAAAAAAAAAAAAAA" onError={noop} onInfo={noop} />)
    const files = await screen.findByTestId('git-files') // waits for status to load
    const panel = screen.getByTestId('git-panel')
    expect(panel.textContent).toMatch(/Git review/i)
    expect(panel.textContent).toMatch(/main/) // branch
    // user files listed; runtime files in their own group, labelled not-your-edits
    expect(files.textContent).toMatch(/src\/App\.tsx/)
    const rt = screen.getByTestId('git-runtime-files')
    expect(rt.textContent).toMatch(/sandbox\.yaml/)
    expect(rt.textContent).toMatch(/not your edits/i)
    expect(screen.getByTestId('git-files').textContent).not.toMatch(/sandbox\.yaml/)
    // no push controls for a non-imported app
    expect(screen.queryByTestId('git-push-box')).toBeNull()
    // clicking a file fetches THAT file's diff and renders it
    fireEvent.click(screen.getByTestId('git-filerow-src/App.tsx'))
    const fd = await screen.findByTestId('git-filediff-src/App.tsx')
    expect(fd.textContent).toMatch(/const x = 1/)
    expect(diffPath).toBe('src/App.tsx') // path-specific diff, not the whole repo
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
    expect(screen.getByTestId('git-committed-sha').textContent).toMatch(/deadbee/)
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

  it('select all / none toggles every user file', async () => {
    render(<AppDetail appId="01APPAAAAAAAAAAAAAAAAAAAAA" onError={noop} onInfo={noop} />)
    await screen.findByTestId('git-files')
    const sel = screen.getByTestId('git-selectall')
    expect(sel.textContent).toMatch(/select none/i) // all checked by default
    fireEvent.click(sel) // -> none
    expect((screen.getByTestId('git-pick-src/App.tsx') as HTMLInputElement).checked).toBe(false)
    expect((screen.getByTestId('git-pick-notes.md') as HTMLInputElement).checked).toBe(false)
    expect(screen.getByTestId('git-selectall').textContent).toMatch(/select all/i)
    fireEvent.click(screen.getByTestId('git-selectall')) // -> all
    expect((screen.getByTestId('git-pick-src/App.tsx') as HTMLInputElement).checked).toBe(true)
  })

  it('renders truncated and binary diff states', async () => {
    installFetch((m, p) => {
      const mm = p.match(/\/git\/diff\?path=(.+)$/)
      if (mm) {
        const path = decodeURIComponent(mm[1])
        if (path === 'src/App.tsx') return { available: true, diff: '@@ -1 +1 @@\n+x', truncated: true }
        return { available: true, diff: 'Binary files a/notes.md and b/notes.md differ', truncated: false }
      }
      return appDetailRoutes(webSandboxFixture)(m, p)
    })
    render(<AppDetail appId="01APPAAAAAAAAAAAAAAAAAAAAA" onError={noop} onInfo={noop} />)
    await screen.findByTestId('git-files')
    fireEvent.click(screen.getByTestId('git-filerow-src/App.tsx'))
    expect(await screen.findByTestId('git-filediff-trunc-src/App.tsx')).toBeTruthy()
    fireEvent.click(screen.getByTestId('git-filerow-notes.md'))
    expect((await screen.findByTestId('git-filediff-notes.md')).textContent).toMatch(/binary file/i)
  })

  it('shows a friendly message when a per-file diff fails to load', async () => {
    installFetch((m, p) => {
      if (/\/git\/diff\?path=/.test(p)) return undefined // -> 404 -> rejected promise
      return appDetailRoutes(webSandboxFixture)(m, p)
    })
    render(<AppDetail appId="01APPAAAAAAAAAAAAAAAAAAAAA" onError={noop} onInfo={noop} />)
    await screen.findByTestId('git-files')
    fireEvent.click(screen.getByTestId('git-filerow-src/App.tsx'))
    const fd = await screen.findByTestId('git-filediff-src/App.tsx')
    expect(fd.textContent).toMatch(/couldn’t load this diff/i)
  })

  it('git push: hidden until commit, then explicit confirm posts the branch', async () => {
    let pushed: { branch?: string } | null = null
    installFetch((m, p) => {
      if (m === 'POST' && /\/git\/commit/.test(p)) {
        return { committed: true, sha: 'abc1234', branch: 'main', files_committed: ['src/App.tsx'] }
      }
      if (m === 'POST' && /\/git\/push/.test(p)) {
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
    await screen.findByTestId('git-commit-box')
    // push hidden before any commit; helper text explains why
    expect(screen.queryByTestId('git-push-box')).toBeNull()
    expect(screen.getByTestId('git-push-help')).toBeTruthy()
    // commit -> push appears
    fireEvent.change(screen.getByTestId('git-commit-message'), { target: { value: 'wip' } })
    fireEvent.click(screen.getByTestId('git-commit'))
    const box = await screen.findByTestId('git-push-box')
    expect(box.textContent).toMatch(/new branch/i)
    expect(box.textContent).toMatch(/github\.com/)
    // explicit confirm required
    fireEvent.change(screen.getByTestId('git-push-branch'), { target: { value: 'my-feature' } })
    fireEvent.click(screen.getByTestId('git-push-start'))
    expect(pushed).toBeNull()
    fireEvent.click(screen.getByTestId('git-push-confirm-yes'))
    await screen.findByTestId('git-push-result')
    expect(pushed!.branch).toBe('my-feature')
  })

  it('maps push reasons to friendly text', async () => {
    installFetch((m, p) => {
      if (m === 'POST' && /\/git\/commit/.test(p)) {
        return { committed: true, sha: 'abc1234', branch: 'main', files_committed: ['src/App.tsx'] }
      }
      if (m === 'POST' && /\/git\/push/.test(p)) return { pushed: false, reason: 'branch_exists' }
      if (/\/v1\/apps\/[^/]+$/.test(p)) {
        return { ...appsFixture[0], git: { repo_url: 'https://github.com/o/r', branch: 'main' } }
      }
      return appDetailRoutes(webSandboxFixture)(m, p)
    })
    render(<AppDetail appId="01APPAAAAAAAAAAAAAAAAAAAAA" onError={noop} onInfo={noop} />)
    await screen.findByTestId('git-commit-box')
    fireEvent.change(screen.getByTestId('git-commit-message'), { target: { value: 'wip' } })
    fireEvent.click(screen.getByTestId('git-commit'))
    await screen.findByTestId('git-push-box')
    fireEvent.click(screen.getByTestId('git-push-start'))
    fireEvent.click(screen.getByTestId('git-push-confirm-yes'))
    const reason = await screen.findByTestId('git-push-reason')
    expect(reason.textContent).toMatch(/branch name already exists/i) // not the raw "branch_exists"
    expect(reason.textContent).not.toMatch(/branch_exists/)
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
