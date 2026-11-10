import { afterEach, describe, expect, it, vi } from 'vitest'
import { api, setOnUnauthorized } from './api'

const respond = (status: number, body: string, contentType = 'application/json', statusText = '') =>
  vi.spyOn(globalThis, 'fetch').mockResolvedValue(
    new Response(body, { status, statusText, headers: { 'content-type': contentType } }),
  )

afterEach(() => {
  vi.restoreAllMocks()
  setOnUnauthorized(() => {})
})

describe('api requests', () => {
  it('sends JSON bodies and reads JSON answers', async () => {
    const fetch = respond(200, JSON.stringify({ apps: [{ id: 'app_1' }] }))
    expect(await api.listApps()).toEqual([{ id: 'app_1' }])
    expect(fetch).toHaveBeenCalledWith('/v1/apps', { method: 'GET', headers: undefined, body: undefined })

    const patch = respond(200, '{}')
    await api.patchSettings({ lifecycle: { idle_reap_enabled: false } })
    expect(patch.mock.lastCall).toEqual([
      '/v1/settings',
      {
        method: 'PATCH',
        headers: { 'content-type': 'application/json' },
        body: JSON.stringify({ lifecycle: { idle_reap_enabled: false } }),
      },
    ])
  })

  it('shows the engine’s own error message, with the status attached', async () => {
    respond(409, JSON.stringify({ error: { message: 'An app with this name already exists.' } }))
    await expect(api.getSettings()).rejects.toMatchObject({
      message: 'An app with this name already exists.',
      status: 409,
    })
  })

  it('falls back to the HTTP status when the error has no message', async () => {
    respond(502, '<html>Bad gateway</html>', 'text/html', 'Bad Gateway')
    await expect(api.getSettings()).rejects.toMatchObject({ message: '502 Bad Gateway', status: 502 })
  })

  it('signs the user out when the session has expired (401), and only then', async () => {
    const signedOut = vi.fn()
    setOnUnauthorized(signedOut)
    respond(403, JSON.stringify({ error: { message: 'Forbidden' } }))
    await expect(api.getSettings()).rejects.toMatchObject({ status: 403 })
    expect(signedOut).not.toHaveBeenCalled()

    respond(401, JSON.stringify({ error: { message: 'Sign in again.' } }))
    await expect(api.getSettings()).rejects.toMatchObject({ message: 'Sign in again.', status: 401 })
    expect(signedOut).toHaveBeenCalledTimes(1)
  })
})
