import { describe, it, expect, beforeEach } from 'vitest'
import { render, screen, fireEvent } from '@testing-library/react'
import { AppDetail } from './AppDetail'
import {
  installFetch,
  appDetailRoutes,
  webSandboxFixture,
  workerSandboxFixture,
  unhealthySandboxFixture,
  gitStatusPristineFixture,
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

  it('shows read-only Git status, splits user vs runtime files, and loads a diff', async () => {
    render(<AppDetail appId="01APPAAAAAAAAAAAAAAAAAAAAA" onError={noop} onInfo={noop} />)
    const panel = await screen.findByTestId('git-panel')
    expect(panel.textContent).toMatch(/read-only/i) // clearly labelled
    expect(panel.textContent).toMatch(/main/)        // branch
    // user files listed
    expect(await screen.findByTestId('git-files')).toBeTruthy()
    expect(screen.getByTestId('git-files').textContent).toMatch(/src\/App\.tsx/)
    // runtime-generated files are shown SEPARATELY, labelled as not-your-edits
    const rt = screen.getByTestId('git-runtime-files')
    expect(rt.textContent).toMatch(/sandbox\.yaml/)
    expect(rt.textContent).toMatch(/not your edits/i)
    // sandbox.yaml must NOT appear among user files
    expect(screen.getByTestId('git-files').textContent).not.toMatch(/sandbox\.yaml/)
    // no commit/push controls in this slice
    expect(screen.queryByText(/commit/i)).toBeNull()
    expect(screen.queryByText(/push/i)).toBeNull()
    // diff loads on demand (the fixed diff endpoint)
    fireEvent.click(screen.getByTestId('git-view-diff'))
    expect((await screen.findByTestId('git-diff')).textContent).toMatch(/const x = 1/)
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
