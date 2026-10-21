const http = require('node:http')
const https = require('node:https')
const { randomBytes, timingSafeEqual } = require('node:crypto')
const fs = require('node:fs/promises')
const path = require('node:path')
const { normalizeEngineURL } = require('./connection.cjs')

const MIME = {
  '.html': 'text/html; charset=utf-8',
  '.js': 'text/javascript; charset=utf-8',
  '.css': 'text/css; charset=utf-8',
  '.svg': 'image/svg+xml',
  '.png': 'image/png',
  '.webp': 'image/webp',
  '.ico': 'image/x-icon',
  '.woff2': 'font/woff2',
}
const CSP =
  "default-src 'none'; script-src 'self'; style-src 'self' 'unsafe-inline' https://fonts.googleapis.com; font-src 'self' https://fonts.gstatic.com; img-src 'self' data: https:; connect-src 'self' http: https: ws: wss:; frame-src http: https:; object-src 'none'; base-uri 'none'; form-action 'self'; frame-ancestors 'none'"
const equal = (a, b) =>
  typeof a === 'string' &&
  Buffer.byteLength(a) === Buffer.byteLength(b) &&
  timingSafeEqual(Buffer.from(a), Buffer.from(b))

// A private transport for the packaged UI. Only the engine API is proxied;
// engines can never supply desktop scripts. Each app process has a random
// capability header injected by Electron, and upstream login cookies stay here.
async function createGateway({ uiRoot, engine = '', session = '', onSession = () => {} }) {
  const token = randomBytes(32).toString('hex')
  let origin = '',
    host = '',
    current = engine,
    engineSession = session,
    generation = 0
  const streams = new Set()
  const sockets = new Set()
  const terminals = new Set()
  const root = path.resolve(uiRoot)
  const authorized = (request) => {
    return (
      request.headers.host === host &&
      equal(request.headers['x-sandboxd-desktop'], token) &&
      (!request.headers.origin || request.headers.origin === origin) &&
      (!request.headers['sec-fetch-site'] ||
        ['same-origin', 'none'].includes(request.headers['sec-fetch-site']))
    )
  }
  const error = (response, status, message) => {
    if (response.headersSent) return response.destroy()
    response.writeHead(status, { 'content-type': 'application/json', 'cache-control': 'no-store' })
    response.end(JSON.stringify({ error: { code: 'desktop_connection', message } }))
  }
  const upstream = (request) => {
    if (!current) throw new Error('Connect an engine to continue.')
    const target = new URL(request.url, current)
    if (target.origin !== current || !target.pathname.startsWith('/v1/')) throw new Error('Invalid API path.')
    // Do not forward browser cookies, capability tokens, proxy headers or
    // ambient credentials. The selected engine owns its own login session.
    const headers = {}
    for (const key of [
      'accept',
      'content-type',
      'content-length',
      'last-event-id',
      'range',
      'if-none-match',
    ]) {
      if (request.headers[key] !== undefined) headers[key] = request.headers[key]
    }
    if (engineSession) headers.cookie = `sbx_session=${engineSession}`
    headers.origin = target.origin
    return { target, headers }
  }
  const track = (request) => {
    streams.add(request)
    request.once('close', () => streams.delete(request))
    return request
  }
  const server = http.createServer(async (request, response) => {
    response.setHeader('x-content-type-options', 'nosniff')
    response.setHeader('referrer-policy', 'no-referrer')
    // Script sources are restricted to bundled assets, excluding every API path.
    response.setHeader(
      'content-security-policy',
      CSP.replace("script-src 'self'", `script-src ${origin}/assets/`),
    )
    response.setHeader('cache-control', 'no-store')
    if (!authorized(request)) return error(response, 403, 'This connection belongs to the desktop app.')
    let url
    try {
      url = new URL(request.url, origin)
    } catch {
      return error(response, 400, 'Invalid URL.')
    }
    if (url.pathname.startsWith('/v1/')) {
      let target, headers
      try {
        ;({ target, headers } = upstream(request))
      } catch (e) {
        return error(response, 503, e.message)
      }
      const connectedGeneration = generation
      const proxy = track(
        (target.protocol === 'https:' ? https : http).request(
          target,
          { method: request.method, headers },
          (remote) => {
            // Redirects are never followed or handed to the renderer: credentials
            // must not travel to another origin or to a non-API path.
            if (remote.statusCode >= 300 && remote.statusCode < 400 && remote.statusCode !== 304) {
              remote.resume()
              return error(response, 502, 'The engine redirected this request. Use its final API origin.')
            }
            if (connectedGeneration === generation) {
              for (const cookie of remote.headers['set-cookie'] || []) {
                const match = /^sbx_session=([^;]*)/i.exec(cookie)
                if (match) {
                  engineSession = /max-age=0\b|max-age=-1\b/i.test(cookie) ? '' : match[1]
                  onSession(current, engineSession)
                }
              }
            }
            for (const key of [
              'content-type',
              'content-disposition',
              'etag',
              'content-range',
              'accept-ranges',
            ]) {
              if (remote.headers[key]) response.setHeader(key, remote.headers[key])
            }
            response.setHeader(
              'content-security-policy',
              "default-src 'none'; sandbox; frame-ancestors 'none'",
            )
            response.writeHead(remote.statusCode || 502)
            remote.on('error', () => response.destroy())
            response.on('close', () => remote.destroy())
            remote.pipe(response)
          },
        ),
      )
      proxy.on('error', () =>
        error(response, 502, 'Cannot reach the engine. Check the address and whether sandboxd is running.'),
      )
      proxy.setTimeout(120000, () => proxy.destroy())
      request.on('aborted', () => proxy.destroy())
      response.on('close', () => proxy.destroy())
      request.pipe(proxy)
      return
    }
    if (!['GET', 'HEAD'].includes(request.method)) return error(response, 405, 'Method not allowed.')
    try {
      const decoded = decodeURIComponent(url.pathname)
      const relative = decoded === '/' ? 'index.html' : decoded.slice(1)
      const filename = path.resolve(root, relative)
      if (!filename.startsWith(root + path.sep)) return error(response, 404, 'Not found.')
      const body = await fs.readFile(filename)
      response.setHeader('content-type', MIME[path.extname(filename)] || 'application/octet-stream')
      response.end(request.method === 'HEAD' ? undefined : body)
    } catch {
      error(response, 404, 'Not found.')
    }
  })
  server.on('connection', (socket) => {
    sockets.add(socket)
    socket.once('close', () => sockets.delete(socket))
  })
  server.on('upgrade', (request, socket, head) => {
    if (!authorized(request) || !/^\/v1\/sandboxes\/[^/?]+\/terminal$/.test(request.url))
      return socket.destroy()
    let target, headers
    try {
      ;({ target, headers } = upstream(request))
    } catch {
      return socket.destroy()
    }
    for (const key of ['sec-websocket-key', 'sec-websocket-version', 'sec-websocket-protocol'])
      if (request.headers[key]) headers[key] = request.headers[key]
    headers.connection = 'Upgrade'
    headers.upgrade = 'websocket'
    const proxy = track((target.protocol === 'https:' ? https : http).request(target, { headers }))
    proxy.on('upgrade', (response, remote, remoteHead) => {
      const responseHeaders = ['HTTP/1.1 101 Switching Protocols']
      for (const key of ['upgrade', 'connection', 'sec-websocket-accept', 'sec-websocket-protocol'])
        if (response.headers[key]) responseHeaders.push(`${key}: ${response.headers[key]}`)
      socket.write(responseHeaders.join('\r\n') + '\r\n\r\n')
      if (remoteHead.length) socket.write(remoteHead)
      if (head.length) remote.write(head)
      sockets.add(remote)
      terminals.add(remote)
      remote.once('close', () => terminals.delete(remote))
      remote.once('close', () => {
        sockets.delete(remote)
        socket.destroy()
      })
      socket.once('close', () => remote.destroy())
      remote.on('error', () => socket.destroy())
      socket.on('error', () => remote.destroy())
      socket.pipe(remote).pipe(socket)
    })
    proxy.on('response', (response) => {
      response.resume()
      socket.destroy()
    })
    proxy.on('error', () => socket.destroy())
    socket.on('close', () => proxy.destroy())
    proxy.end()
  })
  await new Promise((resolve, reject) => {
    server.once('error', reject)
    server.listen(0, '127.0.0.1', resolve)
  })
  host = `127.0.0.1:${server.address().port}`
  origin = `http://${host}`
  return {
    origin,
    authorization: { 'x-sandboxd-desktop': token },
    get engine() {
      return current
    },
    setEngine(value, session = '', allowInsecure = false) {
      const next = value ? normalizeEngineURL(value, allowInsecure) : ''
      for (const stream of streams) stream.destroy()
      // Close terminal connections; keep idle renderer HTTP connections usable.
      for (const socket of terminals) socket.destroy()
      generation++
      current = next
      engineSession = session
    },
    // Main-process-only import: credentials never pass through renderer IPC.
    async importAgentLogin(agent, credentials, expectedEngine) {
      if (current !== expectedEngine || !current) throw new Error('Engine changed. Retry the import.')
      if (!['claude-code', 'codex', 'opencode', 'pi'].includes(agent))
        throw new Error('Unsupported coding agent.')
      const target = new URL(current)
      if (target.protocol !== 'https:' && !['127.0.0.1', 'localhost', '[::1]'].includes(target.hostname))
        throw new Error('Use HTTPS before sending an account login to a remote engine.')
      const response = await fetch(current + '/v1/agents/' + agent + '/import', {
        method: 'POST',
        redirect: 'error',
        signal: AbortSignal.timeout(20000),
        headers: {
          'content-type': 'application/json',
          origin: current,
          ...(engineSession ? { cookie: `sbx_session=${engineSession}` } : {}),
        },
        body: JSON.stringify({ credentials }),
      })
      await response.body?.cancel()
      if (!response.ok)
        throw new Error(`Could not import login (HTTP ${response.status}). Sign in to the engine and retry.`)
    },
    async close() {
      for (const stream of streams) stream.destroy()
      for (const socket of sockets) socket.destroy()
      await new Promise((resolve) => server.close(resolve))
    },
  }
}
module.exports = { createGateway }
