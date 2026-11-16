const http = require('node:http')
const crypto = require('node:crypto')

// "Connect OpenRouter": the person signs in on openrouter.ai in their own
// browser and comes back with an API key, without copying anything. This is
// OpenRouter's documented PKCE flow with a one-time loopback callback, the
// standard way for desktop apps (RFC 8252).
const AUTH = 'https://openrouter.ai/auth'
const KEYS = 'https://openrouter.ai/api/v1/auth/keys'
const KEY_INFO = 'https://openrouter.ai/api/v1/key'

const b64url = (buffer) => buffer.toString('base64url')
const page = (title, text) =>
  `<!doctype html><meta charset="utf-8"><title>${title}</title>` +
  '<body style="font-family:system-ui,sans-serif;display:grid;place-items:center;height:90vh;margin:0;color:#1f2a24">' +
  `<div style="text-align:center"><h1 style="font-size:22px">${title}</h1><p>${text}</p></div>`

function createOpenRouterSignIn({ open, fetch = globalThis.fetch, timeoutMs = 10 * 60 * 1000 } = {}) {
  let pending = null

  async function exchange(code, verifier) {
    const res = await fetch(KEYS, {
      method: 'POST',
      headers: { 'Content-Type': 'application/json' },
      body: JSON.stringify({ code, code_verifier: verifier, code_challenge_method: 'S256' }),
    })
    const body = await res.json().catch(() => ({}))
    if (!res.ok || typeof body.key !== 'string' || !body.key)
      throw new Error('OpenRouter did not finish the sign-in. Please try again.')
    return body.key
  }

  // New accounts have no credit: they start on OpenRouter's free models.
  async function freeTier(key) {
    try {
      const res = await fetch(KEY_INFO, { headers: { Authorization: `Bearer ${key}` } })
      const body = await res.json()
      return body?.data?.is_free_tier !== false
    } catch {
      return true
    }
  }

  function signIn() {
    pending?.cancel(new Error('A newer sign-in started.'))
    const verifier = b64url(crypto.randomBytes(32))
    const challenge = b64url(crypto.createHash('sha256').update(verifier).digest())
    const state = b64url(crypto.randomBytes(16))
    return new Promise((resolve, reject) => {
      let timer
      const server = http.createServer((req, res) => {
        const url = new URL(req.url, 'http://localhost')
        if (url.pathname !== '/callback') {
          res.writeHead(404).end()
          return
        }
        const code = url.searchParams.get('code')
        if (!code || url.searchParams.get('state') !== state) {
          res.writeHead(400, { 'Content-Type': 'text/html; charset=utf-8' })
          res.end(page('Not connected', 'The sign-in was cancelled. You can close this tab and try again in sandboxd.'))
          finish(new Error('The OpenRouter sign-in was cancelled.'))
          return
        }
        exchange(code, verifier)
          .then(async (key) => {
            res.writeHead(200, { 'Content-Type': 'text/html; charset=utf-8' })
            res.end(page('Connected to sandboxd', 'You can close this tab and go back to sandboxd.'))
            finish(null, { key, free: await freeTier(key) })
          })
          .catch((error) => {
            res.writeHead(502, { 'Content-Type': 'text/html; charset=utf-8' })
            res.end(page('Not connected', error.message))
            finish(error)
          })
      })
      const finish = (error, value) => {
        clearTimeout(timer)
        server.close()
        server.closeAllConnections?.()
        if (pending?.server === server) pending = null
        if (error) reject(error)
        else resolve(value)
      }
      pending = { server, cancel: finish }
      server.on('error', (error) => finish(error))
      server.listen(0, '127.0.0.1', () => {
        const callback = `http://localhost:${server.address().port}/callback`
        const url = new URL(AUTH)
        url.searchParams.set('callback_url', callback)
        url.searchParams.set('code_challenge', challenge)
        url.searchParams.set('code_challenge_method', 'S256')
        url.searchParams.set('state', state)
        url.searchParams.set('key_label', 'sandboxd')
        timer = setTimeout(() => finish(new Error('The OpenRouter sign-in took too long. Please try again.')), timeoutMs)
        Promise.resolve(open(url.toString())).catch((error) => finish(error))
      })
    })
  }

  return { signIn }
}

module.exports = { createOpenRouterSignIn }
