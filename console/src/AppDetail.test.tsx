import { describe, it, expect, beforeEach } from 'vitest'
import { render, screen, fireEvent } from '@testing-library/react'
import { AppDetail } from './AppDetail'
import { installFetch, appDetailRoutes, webSandboxFixture, workerSandboxFixture, unhealthySandboxFixture } from './test/fixtures'

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

  it('shows read-only Git status + changed files, and loads a diff on demand', async () => {
    render(<AppDetail appId="01APPAAAAAAAAAAAAAAAAAAAAA" onError={noop} onInfo={noop} />)
    const panel = await screen.findByTestId('git-panel')
    expect(panel.textContent).toMatch(/read-only/i) // clearly labelled
    expect(panel.textContent).toMatch(/main/)        // branch
    expect(await screen.findByTestId('git-files')).toBeTruthy()
    expect(panel.textContent).toMatch(/src\/App\.tsx/)
    // no commit/push controls in this slice
    expect(screen.queryByText(/commit/i)).toBeNull()
    expect(screen.queryByText(/push/i)).toBeNull()
    // diff loads on demand
    fireEvent.click(screen.getByTestId('git-view-diff'))
    expect((await screen.findByTestId('git-diff')).textContent).toMatch(/const x = 1/)
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
