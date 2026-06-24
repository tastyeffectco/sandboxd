import { describe, it, expect, beforeEach } from 'vitest'
import { render, screen } from '@testing-library/react'
import { AppDetail } from './AppDetail'
import { installFetch, appDetailRoutes, webSandboxFixture, workerSandboxFixture } from './test/fixtures'

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

  it('shows a Delete sandbox control (wording surfaced for review)', async () => {
    render(<AppDetail appId="01APPAAAAAAAAAAAAAAAAAAAAA" onError={noop} onInfo={noop} />)
    const del = await screen.findByTestId('delete-sandbox')
    // NOTE: v1 DELETE purges the workspace; the button currently reads
    // "Delete sandbox" and does not say "purge" — see known limitations.
    expect(del.textContent).toMatch(/delete/i)
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
