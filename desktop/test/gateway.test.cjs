const { test } = require('node:test')
const assert = require('node:assert/strict')
const http = require('node:http')
const net = require('node:net')
const { once } = require('node:events')
const fs = require('node:fs/promises')
const os = require('node:os')
const path = require('node:path')
const { createGateway } = require('../src/gateway.cjs')

async function fixture(t, handler = (_req, res) => res.end('{}'), options = {}) {
  const uiRoot = await fs.mkdtemp(path.join(os.tmpdir(), 'sandboxd-gateway-'))
  await fs.writeFile(path.join(uiRoot, 'index.html'), '<h1>Packaged UI</h1>')
  const engine = http.createServer(handler)
  const sockets = new Set()
  engine.on('connection', (socket) => {
    sockets.add(socket)
    socket.once('close', () => sockets.delete(socket))
  })
  engine.listen(0, '127.0.0.1')
  await once(engine, 'listening')
  const origin = `http://127.0.0.1:${engine.address().port}`
  const saved = []
  const gateway = await createGateway({
    uiRoot,
    engine: origin,
    onSession: (...args) => saved.push(args),
    ...options,
  })
  t.after(async () => {
    await gateway.close()
    for (const socket of sockets) socket.destroy()
    await new Promise((r) => engine.close(r))
    await fs.rm(uiRoot, { recursive: true })
  })
  const request = (route, headers = {}, method = 'GET') =>
    new Promise((resolve, reject) => {
      const req = http.request(
        gateway.origin + route,
        { method, headers: { ...gateway.authorization, ...headers } },
        (response) => {
          let body = ''
          response.on('data', (data) => (body += data))
          response.on('end', () => resolve({ status: response.statusCode, headers: response.headers, body }))
          response.on('close', () => response.complete || reject(new Error(`response cut: ${body}`)))
        },
      )
      req.on('error', reject)
      req.end()
    })
  return { gateway, engine, origin, saved, request }
}
test('private gateway rejects forged capabilities, Host, Origin and cross-site requests', async (t) => {
  const { request, gateway } = await fixture(t)
  for (const headers of [
    { 'x-sandboxd-desktop': '' },
    { 'x-sandboxd-desktop': 'é'.repeat(64) },
    { host: 'evil.example' },
    { origin: 'https://evil.example' },
    { 'sec-fetch-site': 'cross-site' },
    { 'sec-fetch-site': 'same-site' },
  ])
    assert.equal((await request('/', headers)).status, 403)
  const page = await request('/', { origin: gateway.origin, 'sec-fetch-site': 'same-origin' })
  assert.equal(page.status, 200)
  assert.match(page.body, /Packaged UI/)
  assert(page.headers['content-security-policy'].includes(`script-src ${gateway.origin}/assets/`))
  assert.equal(page.headers['access-control-allow-origin'], undefined)
})
test('sessions remain in the host, logout clears them, and no ambient headers reach the engine', async (t) => {
  const received = []
  const { request, saved, origin, gateway } = await fixture(t, (req, res) => {
    received.push(req.headers)
    if (req.url === '/v1/auth/login')
      res.setHeader('set-cookie', ['sbx_session=secret; Secure; HttpOnly; SameSite=Lax', 'other=ignored'])
    if (req.url === '/v1/auth/logout') res.setHeader('set-cookie', 'sbx_session=; Max-Age=0')
    res.end('{}')
  })
  const login = await request(
    '/v1/auth/login',
    { cookie: 'browser=private', authorization: 'Bearer ambient', 'x-forwarded-host': 'evil.example' },
    'POST',
  )
  assert.equal(login.headers['set-cookie'], undefined)
  await request('/v1/apps')
  await request('/v1/auth/logout', {}, 'POST')
  await request('/v1/apps')
  assert.equal(received[0].cookie, undefined)
  assert.equal(received[0].authorization, undefined)
  assert.equal(received[0]['x-forwarded-host'], undefined)
  assert.equal(received[0]['x-sandboxd-desktop'], undefined)
  assert.equal(received[1].cookie, 'sbx_session=secret')
  assert.equal(received[3].cookie, undefined)
  assert.equal(received[0].origin, origin)
  assert.deepEqual(saved, [
    [origin, 'secret'],
    [origin, ''],
  ])
  gateway.setEngine('')
  assert.equal((await request('/v1/apps')).status, 503)
})
test('only bundled static files and selected-origin API paths are available; redirects never escape', async (t) => {
  const { request } = await fixture(t, (_req, res) => {
    res.writeHead(302, { location: 'https://evil.example', 'set-cookie': 'sbx_session=bad' })
    res.end()
  })
  const redirect = await request('/v1/apps')
  assert.equal(redirect.status, 502)
  assert.equal(redirect.headers.location, undefined)
  for (const route of [
    '/missing',
    '/%2e%2e%2fpackage.json',
    '/%2e%2e%5cpackage.json',
    '/%00',
    '/v1/../index.html/no',
  ])
    assert.equal((await request(route)).status, 404)
  assert.equal((await request('/', {}, 'POST')).status, 405)
})
test('SSE is streamed before the upstream response closes and downloads preserve their headers', async (t) => {
  let finish
  const { request, gateway } = await fixture(t, (req, res) => {
    if (req.url.endsWith('/export')) {
      res.writeHead(200, {
        'content-type': 'application/zip',
        'content-disposition': 'attachment; filename="workspace.zip"',
      })
      res.end('ZIP')
      return
    }
    res.writeHead(200, { 'content-type': 'text/event-stream' })
    res.write('data: first\n\n')
    finish = () => res.end('data: last\n\n')
  })
  const stream = await new Promise((resolve, reject) => {
    http
      .get(gateway.origin + '/v1/events', { headers: gateway.authorization }, (res) => {
        res.once('data', (data) => {
          assert.match(data.toString(), /first/)
          resolve(res)
        })
      })
      .on('error', reject)
  })
  finish()
  stream.resume()
  await once(stream, 'end')
  const download = await request('/v1/sandboxes/test/export')
  assert.equal(download.body, 'ZIP')
  assert.match(download.headers['content-disposition'], /workspace.zip/)
})
test('terminal upgrade preserves private engine auth and closes on switching engines', async (t) => {
  const { gateway, engine, origin } = await fixture(t)
  gateway.setEngine(origin, 'terminal-session')
  let upstreamHeaders
  engine.on('upgrade', (req, socket) => {
    upstreamHeaders = req.headers
    socket.on('error', () => {})
    socket.write(
      'HTTP/1.1 101 Switching Protocols\r\nUpgrade: websocket\r\nConnection: Upgrade\r\nSec-WebSocket-Accept: test\r\n\r\n',
    )
    socket.on('data', (data) => socket.write(data))
  })
  const target = new URL(gateway.origin)
  const socket = net.connect(Number(target.port), target.hostname)
  t.after(() => socket.destroy())
  await once(socket, 'connect')
  socket.write(
    `GET /v1/sandboxes/test/terminal HTTP/1.1\r\nHost: ${target.host}\r\nOrigin: ${gateway.origin}\r\nX-Sandboxd-Desktop: ${gateway.authorization['x-sandboxd-desktop']}\r\nConnection: Upgrade\r\nUpgrade: websocket\r\nSec-WebSocket-Key: test\r\nSec-WebSocket-Version: 13\r\n\r\n`,
  )
  assert.match((await once(socket, 'data'))[0].toString(), /101 Switching/)
  assert.equal(upstreamHeaders.cookie, 'sbx_session=terminal-session')
  assert.equal(upstreamHeaders['x-sandboxd-desktop'], undefined)
  socket.write('terminal bytes')
  assert.equal((await once(socket, 'data'))[0].toString(), 'terminal bytes')
  const closed = once(socket, 'close')
  gateway.setEngine('')
  await closed
})

test('task event streams survive quiet agent steps while other requests still time out', async (t) => {
  const { request } = await fixture(
    t,
    (req, res) => {
      if (req.url.endsWith('/events')) {
        res.writeHead(200, { 'content-type': 'text/event-stream' })
        res.write('event: status\ndata: {}\n\n')
        // Silent for longer than the gateway's idle limit, like a long install.
        setTimeout(() => res.end('event: done\ndata: {}\n\n'), 400)
        return
      }
      // A normal API call that never answers.
    },
    { idleTimeout: 150 },
  )
  const events = await request('/v1/sandboxes/sb/tasks/task/events', { accept: 'text/event-stream' })
  assert.equal(events.status, 200)
  assert.match(events.body, /event: status/)
  assert.match(events.body, /event: done/)
  assert.equal((await request('/v1/apps')).status, 502)
})

test('native account import is bound to the chosen engine and never follows redirects', async (t) => {
  let received
  const { gateway, origin } = await fixture(t, (req, res) => {
    let body = ''
    req.on('data', (chunk) => {
      body += chunk
    })
    req.on('end', () => {
      received = { url: req.url, headers: req.headers, body }
      res.end('{}')
    })
  })
  gateway.setEngine(origin, 'engine-session')
  await gateway.importAgentLogin('codex', 'private-login', origin)
  assert.equal(received.url, '/v1/agents/codex/import')
  assert.equal(received.headers.cookie, 'sbx_session=engine-session')
  assert.equal(received.headers.origin, origin)
  assert.deepEqual(JSON.parse(received.body), { credentials: 'private-login' })
  await assert.rejects(gateway.importAgentLogin('../elsewhere', 'private-login', origin), /Unsupported/)
  await assert.rejects(
    gateway.importAgentLogin('codex', 'private-login', 'https://another.example'),
    /changed/,
  )
  gateway.setEngine('http://remote.example', '', true)
  await assert.rejects(gateway.importAgentLogin('codex', 'private-login', 'http://remote.example'), /HTTPS/)
  const redirected = await fixture(t, (_req, res) => {
    res.writeHead(307, { location: origin })
    res.end()
  })
  await assert.rejects(redirected.gateway.importAgentLogin('codex', 'private-login', redirected.origin))
})

test('/servers/<id>/v1 goes through that server tunnel with its token, streams bodies, rejects unknown ids', async (t) => {
  const received = []
  let onData = () => {}
  const serverEngine = http.createServer((req, res) => {
    received.push({ url: req.url, headers: req.headers })
    if (req.url.endsWith('/terminal')) return
    let size = 0
    req.on('data', (chunk) => {
      size += chunk.length
      onData(size)
    })
    req.on('end', () => {
      res.setHeader('set-cookie', 'sbx_session=server-cookie')
      res.end(JSON.stringify({ size }))
    })
  })
  const upgraded = new Set()
  serverEngine.on('upgrade', (req, socket) => {
    upgraded.add(socket)
    received.push({ url: req.url, headers: req.headers })
    socket.on('error', () => {})
    socket.write('HTTP/1.1 101 Switching Protocols\r\nUpgrade: websocket\r\nConnection: Upgrade\r\n\r\n')
    socket.on('data', (data) => socket.write(data))
  })
  serverEngine.listen(0, '127.0.0.1')
  await once(serverEngine, 'listening')
  t.after(() => {
    for (const socket of upgraded) socket.destroy()
    serverEngine.closeAllConnections()
    return new Promise((r) => serverEngine.close(r))
  })
  const id = 'srv_0123456789ab'
  const tunnel = `http://127.0.0.1:${serverEngine.address().port}`
  const { request, gateway, saved } = await fixture(t, undefined, {
    session: 'local-session',
    servers: { resolve: (value) => (value === id ? { origin: tunnel, token: 'server-token' } : null) },
  })

  const listed = await request(`/servers/${id}/v1/apps?label=env%3Dlive`, { cookie: 'ambient=1' })
  assert.equal(listed.status, 200)
  const first = received.at(-1)
  assert.equal(first.url, '/v1/apps?label=env%3Dlive')
  assert.equal(first.headers.authorization, 'Bearer server-token')
  assert.equal(first.headers.cookie, undefined)
  assert.equal(first.headers['x-sandboxd-desktop'], undefined)
  // A server's cookies never replace the selected engine's session.
  assert.deepEqual(saved, [])

  for (const route of [
    '/servers/srv_ffffffffffff/v1/apps',
    '/servers/evil.example/v1/apps',
    `/servers/${id}/apps`,
    `/servers/${id}`,
  ])
    assert.equal((await request(route)).status, 404, route)
  // Raw paths (the HTTP client would normalize these before sending).
  const raw = async (route) => {
    const target = new URL(gateway.origin)
    const socket = net.connect(Number(target.port), target.hostname)
    await once(socket, 'connect')
    socket.write(`GET ${route} HTTP/1.1\r\nHost: ${target.host}\r\nX-Sandboxd-Desktop: ${gateway.authorization['x-sandboxd-desktop']}\r\nConnection: close\r\n\r\n`)
    let text = ''
    for await (const data of socket) text += data
    return Number(/^HTTP\/1\.1 (\d+)/.exec(text)[1])
  }
  assert.equal(await raw('/servers/%2e%2e/v1/apps'), 404)
  assert.equal(await raw(`/servers/${id}/v1/%2e%2e/%2e%2e/healthz`), 400)
  assert.equal(await raw(`/servers/${id}/v1/../../healthz`), 400)
  assert.equal(received.length, 1)

  // A large release bundle reaches the server while it is still being sent.
  const chunk = Buffer.alloc(1024 * 1024, 1)
  const total = 24
  const upload = await new Promise((resolve, reject) => {
    const req = http.request(
      `${gateway.origin}/servers/${id}/v1/apps/a/releases`,
      {
        method: 'POST',
        headers: { ...gateway.authorization, 'content-type': 'application/zstd', 'content-length': chunk.length * total },
      },
      (res) => {
        let body = ''
        res.on('data', (d) => (body += d))
        res.on('end', () => resolve(JSON.parse(body)))
      },
    )
    req.on('error', reject)
    let streamed = false
    req.write(chunk)
    onData = (size) => {
      if (streamed || size < chunk.length) return
      streamed = true
      for (let i = 1; i < total; i++) req.write(chunk)
      req.end()
    }
  })
  assert.equal(upload.size, chunk.length * total)

  // Terminals work the same way.
  const target = new URL(gateway.origin)
  const socket = net.connect(Number(target.port), target.hostname)
  t.after(() => socket.destroy())
  await once(socket, 'connect')
  socket.write(
    `GET /servers/${id}/v1/sandboxes/app/terminal HTTP/1.1\r\nHost: ${target.host}\r\nOrigin: ${gateway.origin}\r\nX-Sandboxd-Desktop: ${gateway.authorization['x-sandboxd-desktop']}\r\nConnection: Upgrade\r\nUpgrade: websocket\r\nSec-WebSocket-Key: test\r\nSec-WebSocket-Version: 13\r\n\r\n`,
  )
  assert.match((await once(socket, 'data'))[0].toString(), /101 Switching/)
  assert.equal(received.at(-1).url, '/v1/sandboxes/app/terminal')
  assert.equal(received.at(-1).headers.authorization, 'Bearer server-token')
  socket.write('hello')
  assert.equal((await once(socket, 'data'))[0].toString(), 'hello')

  const unknown = net.connect(Number(target.port), target.hostname)
  await once(unknown, 'connect')
  unknown.write(
    `GET /servers/srv_ffffffffffff/v1/sandboxes/app/terminal HTTP/1.1\r\nHost: ${target.host}\r\nX-Sandboxd-Desktop: ${gateway.authorization['x-sandboxd-desktop']}\r\nConnection: Upgrade\r\nUpgrade: websocket\r\n\r\n`,
  )
  await once(unknown, 'close')
})

test('without the servers feature, server routes are not found', async (t) => {
  const { request } = await fixture(t)
  assert.equal((await request('/servers/srv_0123456789ab/v1/apps')).status, 404)
})
