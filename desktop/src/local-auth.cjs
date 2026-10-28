const { randomBytes } = require('node:crypto')

// Signs in to the engine this app installed, so a first-time user goes straight
// from setup to the project builder. Other engines keep their own login screen.
// Returns the engine session to use, '' when the engine has login disabled, or
// null when the user must sign in themselves (e.g. they changed the password).
async function signInManagedEngine({
  url,
  session = '',
  password = '',
  savePassword,
  request = fetch,
  generate = () => randomBytes(24).toString('base64url'),
}) {
  const call = (path, { method = 'GET', cookie, body } = {}) =>
    request(url + path, {
      method,
      redirect: 'error',
      signal: AbortSignal.timeout(10000),
      headers: {
        origin: url,
        ...(cookie ? { cookie: `sbx_session=${cookie}` } : {}),
        ...(body ? { 'content-type': 'application/json' } : {}),
      },
      body: body ? JSON.stringify(body) : undefined,
    })
  const status = async (cookie) => {
    const response = await call('/v1/auth/status', { cookie })
    if (!response.ok) throw new Error(`Engine sign-in check failed (HTTP ${response.status}).`)
    return response.json()
  }
  const issued = (response) => {
    const cookies = response.headers.getSetCookie?.() || []
    for (const cookie of cookies) {
      const match = /^sbx_session=([^;]+)/i.exec(cookie)
      if (match) return match[1]
    }
    return null
  }

  if (session && (await status(session)).authenticated) return session
  const current = await status()
  if (current.enabled === false) return ''
  if (!current.password_set) {
    const generated = generate()
    // Persist first: a crash after setup must never leave an unknown password.
    await savePassword(generated)
    const response = await call('/v1/auth/setup', { method: 'POST', body: { password: generated } })
    return response.ok ? issued(response) : null
  }
  if (!password) return null
  const response = await call('/v1/auth/login', { method: 'POST', body: { password } })
  return response.ok ? issued(response) : null
}

module.exports = { signInManagedEngine }
