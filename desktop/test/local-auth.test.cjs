const { test } = require('node:test')
const assert = require('node:assert/strict')
const { signInManagedEngine } = require('../src/local-auth.cjs')

const url = 'http://127.0.0.1:9090'
function engine({ enabled = true, password = '', valid = [] } = {}) {
  const calls = []
  const sessions = new Set(valid)
  const reply = (status, body, cookie) => ({
    ok: status < 300,
    status,
    json: async () => body,
    headers: { getSetCookie: () => (cookie ? [`sbx_session=${cookie}; Path=/; HttpOnly`] : []) },
  })
  const request = async (target, init) => {
    const path = new URL(target).pathname
    const cookie = /sbx_session=([^;]+)/.exec(init.headers.cookie || '')?.[1]
    const body = init.body ? JSON.parse(init.body) : undefined
    calls.push({ path, method: init.method, body, headers: init.headers })
    if (path === '/v1/auth/status')
      return reply(200, { enabled, authenticated: sessions.has(cookie), password_set: !!password })
    if (path === '/v1/auth/setup') {
      if (password) return reply(409, {})
      password = body.password
      sessions.add('created')
      return reply(204, null, 'created')
    }
    if (path === '/v1/auth/login') {
      if (body.password !== password) return reply(401, {})
      sessions.add('renewed')
      return reply(204, null, 'renewed')
    }
    return reply(404, {})
  }
  return { request, calls, password: () => password }
}

test('a fresh managed engine gets a saved password before setup', async () => {
  const fixture = engine()
  const saved = []
  const session = await signInManagedEngine({
    url,
    request: fixture.request,
    generate: () => 'generated-password',
    savePassword: async (value) => saved.push([value, fixture.password()]),
  })
  assert.equal(session, 'created')
  assert.deepEqual(saved, [['generated-password', '']])
  assert.equal(fixture.password(), 'generated-password')
  assert.ok(fixture.calls.every((c) => c.headers.origin === url))
})

test('a valid session is reused without another login', async () => {
  const fixture = engine({ password: 'secret', valid: ['current'] })
  const session = await signInManagedEngine({
    url,
    session: 'current',
    password: 'secret',
    request: fixture.request,
    savePassword: async () => assert.fail('must not create a password'),
  })
  assert.equal(session, 'current')
  assert.deepEqual(
    fixture.calls.map((c) => c.path),
    ['/v1/auth/status'],
  )
})

test('an expired session signs in again with the saved password', async () => {
  const fixture = engine({ password: 'secret' })
  const session = await signInManagedEngine({
    url,
    session: 'expired',
    password: 'secret',
    request: fixture.request,
    savePassword: async () => assert.fail('must not replace the password'),
  })
  assert.equal(session, 'renewed')
})

test('a changed or unknown password falls back to the login screen', async () => {
  for (const password of ['old-secret', '']) {
    const fixture = engine({ password: 'changed-by-user' })
    const session = await signInManagedEngine({
      url,
      password,
      request: fixture.request,
      savePassword: async () => assert.fail('must not replace the password'),
    })
    assert.equal(session, null)
  }
})

test('an engine with login disabled needs no session', async () => {
  const fixture = engine({ enabled: false })
  const session = await signInManagedEngine({
    url,
    request: fixture.request,
    savePassword: async () => assert.fail('must not create a password'),
  })
  assert.equal(session, '')
})

test('a failed password save stops before the engine is changed', async () => {
  const fixture = engine()
  await assert.rejects(
    signInManagedEngine({
      url,
      request: fixture.request,
      savePassword: async () => {
        throw new Error('disk full')
      },
    }),
    /disk full/,
  )
  assert.equal(fixture.password(), '')
})
