const { test } = require('node:test')
const assert = require('node:assert/strict')
const crypto = require('node:crypto')
const { createOpenRouterSignIn } = require('../src/openrouter.cjs')

// A fake OpenRouter: records the exchange and answers like the real API.
function fakeFetch({ free = true, fail = false } = {}) {
  const calls = []
  const fetch = async (url, init = {}) => {
    calls.push({ url, init })
    if (url.endsWith('/auth/keys')) {
      if (fail) return { ok: false, json: async () => ({ error: 'invalid code' }) }
      return { ok: true, json: async () => ({ key: 'sk-or-new' }) }
    }
    return { ok: true, json: async () => ({ data: { is_free_tier: free } }) }
  }
  return { fetch, calls }
}
// Plays the browser: follows the sign-in URL back to the loopback callback.
const browser = (mutate = (u) => u) => (authURL) => (browser.done = visit(authURL, mutate))
async function visit(authURL, mutate) {
  const auth = new URL(authURL)
  assert.equal(auth.origin + auth.pathname, 'https://openrouter.ai/auth')
  const callback = new URL(auth.searchParams.get('callback_url'))
  assert.equal(callback.hostname, 'localhost')
  callback.hostname = '127.0.0.1'
  callback.searchParams.set('code', 'one-time-code')
  callback.searchParams.set('state', auth.searchParams.get('state'))
  const res = await fetch(mutate(callback).toString())
  browser.last = { auth, status: res.status, page: await res.text() }
}

test('signs in with PKCE and returns the key and the account tier', async () => {
  const { fetch, calls } = fakeFetch({ free: false })
  const signIn = createOpenRouterSignIn({ open: browser(), fetch })
  const result = await signIn.signIn()
  assert.deepEqual(result, { key: 'sk-or-new', free: false })
  await browser.done
  const { auth, status, page } = browser.last
  assert.equal(status, 200)
  assert.match(page, /go back to sandboxd/)
  const body = JSON.parse(calls[0].init.body)
  assert.equal(body.code, 'one-time-code')
  assert.equal(body.code_challenge_method, 'S256')
  const challenge = crypto.createHash('sha256').update(body.code_verifier).digest('base64url')
  assert.equal(auth.searchParams.get('code_challenge'), challenge)
  assert.equal(calls[1].init.headers.Authorization, 'Bearer sk-or-new')
})

test('rejects a callback with the wrong state', async () => {
  const { fetch, calls } = fakeFetch()
  const signIn = createOpenRouterSignIn({
    open: browser((u) => (u.searchParams.set('state', 'forged'), u)),
    fetch,
  })
  await assert.rejects(signIn.signIn(), /cancelled/)
  assert.equal(calls.length, 0)
})

test('a failed exchange is a plain error', async () => {
  const signIn = createOpenRouterSignIn({ open: browser(), fetch: fakeFetch({ fail: true }).fetch })
  await assert.rejects(signIn.signIn(), /did not finish the sign-in/)
})

test('gives up after the timeout', async () => {
  const signIn = createOpenRouterSignIn({ open: () => {}, fetch: fakeFetch().fetch, timeoutMs: 50 })
  await assert.rejects(signIn.signIn(), /took too long/)
})
