// Your servers, against a real in-process SSH server (ssh2's Server) and a fake
// engine HTTP API behind it.
const { test } = require('node:test')
const assert = require('node:assert/strict')
const fs = require('node:fs/promises')
const http = require('node:http')
const net = require('node:net')
const os = require('node:os')
const path = require('node:path')
const { once } = require('node:events')
const { Server, utils } = require('ssh2')
const {
  createServers,
  installCommand,
  authorizeCommand,
  unauthorizeCommand,
  installRef,
  parseInstallResult,
  checkFacts,
  generateDeviceKey,
  MESSAGES,
} = require('../src/servers.cjs')

const PASSWORD = 'correct horse battery staple'
const TOKEN = 'sbx_live_0123456789abcdef0123456789abcdef'
const FACTS = {
  os: 'Linux',
  arch: 'x86_64',
  uid: '0',
  mem: '2000000',
  disk: '40000000',
  docker: '1',
  fetch: '1',
  sudo: '1',
}
// Reversible, but never the plain bytes on disk.
const encrypt = (text) => Buffer.from('enc:' + Buffer.from(text).toString('hex').split('').reverse().join(''))
const decrypt = (buffer) =>
  Buffer.from(buffer.toString().slice(4).split('').reverse().join(''), 'hex').toString()
const wait = async (check, timeout = 10000) => {
  const start = Date.now()
  while (!(await check())) {
    if (Date.now() - start > timeout) throw new Error('timed out')
    await new Promise((r) => setTimeout(r, 20))
  }
}

async function fakeServer(t, options = {}) {
  const engine = http.createServer((req, res) => {
    engine.requests.push({ url: req.url, authorization: req.headers.authorization })
    if (req.headers.authorization !== `Bearer ${TOKEN}`) {
      res.writeHead(401)
      return res.end('{}')
    }
    res.setHeader('content-type', 'application/json')
    if (req.url === '/v1/auth/status') return res.end('{"enabled":true,"authenticated":true}')
    let size = 0
    req.on('data', (chunk) => (size += chunk.length))
    req.on('end', () => res.end(JSON.stringify({ url: req.url, size })))
  })
  engine.requests = []
  engine.listen(0, '127.0.0.1')
  await once(engine, 'listening')
  const state = {
    facts: { ...FACTS, ...options.facts },
    admin: options.admin || 'root',
    authorized: [],
    commands: [],
    stdin: [],
    clients: new Set(),
    installExit: options.installExit ?? 0,
    result:
      options.result ??
      `SANDBOXD_INSTALL_RESULT {"front":"caddy","domain":"203-0-113-10.sslip.io","proxy":"none","api_token":"${TOKEN}","api":"127.0.0.1:9090"}`,
  }
  let ssh
  const listen = async (port = 0) => {
    const hostKey = generateDeviceKey('host').private
    ssh = new Server({ hostKeys: [hostKey] }, (client) => {
      let user = ''
      let byKey = false
      state.clients.add(client)
      client.on('close', () => state.clients.delete(client))
      client.on('error', () => {})
      client.on('authentication', (ctx) => {
        if (ctx.username === state.admin && ctx.method === 'password' && ctx.password === PASSWORD) {
          user = ctx.username
          return ctx.accept()
        }
        if (ctx.username === state.admin && ctx.method === 'publickey') {
          const key = state.authorized
            .map((line) => utils.parseKey(line))
            .find((k) => k.type === ctx.key.algo && k.getPublicSSH().equals(ctx.key.data))
          if (key && (!ctx.signature || key.verify(ctx.blob, ctx.signature, ctx.hashAlgo) === true)) {
            user = ctx.username
            byKey = true
            return ctx.accept()
          }
        }
        ctx.reject(['password', 'publickey'])
      })
      client.on('ready', () => {
        client.on('session', (accept) => {
          accept().on('exec', (accept, _reject, info) => {
            const stream = accept()
            let input = ''
            stream.on('data', (d) => (input += d))
            const finish = (code, text = '') => {
              if (text) stream.write(text)
              stream.exit(code)
              stream.end()
            }
            const command = info.command
            state.commands.push({ user, command, byKey })
            if (command.startsWith('umask 077; mkdir -p ~/.ssh')) {
              const line = /printf '%s\\n' '([^']+)' >>/.exec(command)[1]
              if (!state.authorized.includes(line)) state.authorized.push(line)
              return finish(0)
            }
            if (command.startsWith('f=~/.ssh/authorized_keys')) {
              const blob = /grep -vF ' ([^ ]+) sandboxd-desktop:'/.exec(command)[1]
              state.authorized = state.authorized.filter((line) => !line.includes(` ${blob} sandboxd-desktop:`))
              return finish(0)
            }
            if (command === 'echo hello') return finish(0, 'hello\n')
            if (command.startsWith("printf 'os="))
              return finish(
                0,
                Object.entries(state.facts)
                  .map(([k, v]) => `${k}=${v}\n`)
                  .join(''),
              )
            const sudo = command.startsWith("sudo -S -p '' ")
            const run = () => {
              if (sudo) state.stdin.push(input)
              if (sudo && input !== PASSWORD + '\n')
                return finish(1, 'Sorry, try again.\nsudo: 1 incorrect password attempt\n')
              if (command.includes("bash -c 'id -u'")) return finish(0, '0\n')
              if (command.includes('get.docker.com')) {
                state.facts.docker = '1'
                return finish(0, 'docker installed\n')
              }
              if (command.includes('/install.sh')) {
                if (state.installExit !== 0) return finish(state.installExit, 'step 1\nERROR: disk full\n')
                return finish(0, `==> pulling images\n==> starting\n${state.result}\n`)
              }
              finish(127, 'unknown command\n')
            }
            const passwordless = command.startsWith('sudo -n ')
            if (passwordless) return finish(1, 'sudo: a password is required\n')
            if (sudo) stream.once('end', run)
            else run()
          })
        })
        client.on('tcpip', (accept, reject, info) => {
          if (!byKey || info.destIP !== '127.0.0.1' || info.destPort !== 9090) return reject()
          const channel = accept()
          const socket = net.connect(engine.address().port, '127.0.0.1')
          socket.on('error', () => channel.close())
          channel.on('error', () => socket.destroy())
          channel.pipe(socket).pipe(channel)
        })
      })
    })
    ssh.listen(port, '127.0.0.1')
    await once(ssh, 'listening')
    return ssh.address().port
  }
  const port = await listen()
  const stopSsh = async () => {
    for (const client of state.clients) client.end()
    await new Promise((r) => ssh.close(r))
  }
  t.after(async () => {
    await stopSsh().catch(() => {})
    engine.closeAllConnections?.()
    await new Promise((r) => engine.close(r))
  })
  return {
    port,
    state,
    engine,
    stopSsh,
    // Same address, new host key: a reinstalled (or impersonated) server.
    async replaceHostKey() {
      await stopSsh()
      await listen(port)
    },
  }
}

async function manager(t, options = {}) {
  const dataDir = await fs.mkdtemp(path.join(os.tmpdir(), 'sandboxd-servers-'))
  const statuses = []
  const servers = createServers({
    dataDir,
    encrypt,
    decrypt,
    connectTimeout: 4000,
    backoff: [50, 100],
    keepaliveInterval: 0,
    onStatus: (id, status) => statuses.push({ id, ...status }),
    env: {},
    ...options,
  })
  t.after(async () => {
    await servers.close()
    await fs.rm(dataDir, { recursive: true, force: true })
  })
  await servers.load()
  return { servers, dataDir, statuses }
}

const get = (port, route, init = {}) =>
  new Promise((resolve, reject) => {
    const req = http.request({ host: '127.0.0.1', port, path: route, ...init }, (res) => {
      let body = ''
      res.on('data', (d) => (body += d))
      res.on('end', () => resolve({ status: res.statusCode, body }))
    })
    req.on('error', reject)
    req.end(init.body)
  })

async function allFiles(directory) {
  const out = []
  for (const entry of await fs.readdir(directory, { withFileTypes: true })) {
    const full = path.join(directory, entry.name)
    if (entry.isDirectory()) out.push(...(await allFiles(full)))
    else
      out.push({
        name: entry.name,
        text: await fs.readFile(full, 'utf8'),
        mode: (await fs.stat(full)).mode & 0o777,
      })
  }
  return out
}

test('install command follows the server contract and the pinned engine', () => {
  const tagged = installCommand('v0.3.26')
  assert.match(tagged, /raw\.githubusercontent\.com\/tastyeffectco\/sandboxd\/v0\.3\.26\/install\.sh/)
  assert.match(tagged, /SANDBOXD_REF=v0\.3\.26 bash -s -- --pull --no-console --no-desktop --front=auto$/)
  assert.match(installCommand('release/go-live'), / --build --no-console/)
  assert.equal(installRef({ SANDBOXD_SERVER_REF: 'release/go-live' }), 'release/go-live')
  assert.match(installRef({}), /^v\d/)
  assert.throws(() => installRef({ SANDBOXD_SERVER_REF: 'main; rm -rf /' }))
})

test('the device key is added to and removed from the login’s own authorized_keys', () => {
  const key = generateDeviceKey('sandboxd-desktop:0123456789abcdef').public
  assert.match(authorizeCommand(key), /^umask 077; mkdir -p ~\/\.ssh/)
  assert(!authorizeCommand(key).includes('sudo'))
  assert.match(unauthorizeCommand(key), / sandboxd-desktop:'/)
  for (const bad of ["ssh-ed25519 AAAA' ; reboot ; ' sandboxd-desktop:0123456789abcdef", 'ssh-ed25519 AAAA me@laptop'])
    assert.throws(() => authorizeCommand(bad), /device key is invalid/)
})

test('device keys are valid OpenSSH ed25519 keys every time', () => {
  for (let i = 0; i < 1000; i++) {
    const key = generateDeviceKey()
    const parsed = utils.parseKey(key.private)
    assert(!(parsed instanceof Error), key.private)
    assert.equal(parsed.type, 'ssh-ed25519')
    assert.equal(parsed.getPublicSSH().toString('base64'), key.public.split(' ')[1])
    assert.match(key.public, /^ssh-ed25519 [A-Za-z0-9+/=]+ sandboxd-desktop$/) // default comment
    if (i < 20)
      assert.equal(utils.parseKey(key.public).verify(Buffer.from('x'), parsed.sign(Buffer.from('x'))), true)
  }
})

test('install result parsing accepts only the documented shape', () => {
  assert.deepEqual(
    parseInstallResult(
      `SANDBOXD_INSTALL_RESULT {"front":"existing","domain":"","proxy":"nginx","api_token":"${TOKEN}","api":"127.0.0.1:9090"}`,
    ),
    { front: 'existing', domain: '', proxy: 'nginx', apiToken: TOKEN, api: '127.0.0.1:9090' },
  )
  assert.equal(parseInstallResult('SANDBOXD_INSTALL_RESULT not json'), null)
  assert.equal(
    parseInstallResult(`SANDBOXD_INSTALL_RESULT {"api_token":"${TOKEN}","api":"10.0.0.5:9090"}`),
    null,
  )
  assert.equal(parseInstallResult('SANDBOXD_INSTALL_RESULT {"api_token":"short"}'), null)
  assert.equal(parseInstallResult('==> starting'), null)
  assert.equal(
    parseInstallResult(`SANDBOXD_INSTALL_RESULT {"front":"weird","proxy":"x","api_token":"${TOKEN}"}`).front,
    'manual',
  )
})

test('preflight explains in plain words why a server does not fit', () => {
  assert.throws(() => checkFacts({ ...FACTS, os: 'Darwin' }), /not running Linux/)
  assert.throws(() => checkFacts({ ...FACTS, arch: 'armv7l' }), /processor/)
  assert.throws(
    () => checkFacts({ ...FACTS, disk: String(2 * 1024 * 1024) }),
    /2\.0 GB of free disk space.*at least 5 GB/,
  )
  assert.throws(() => checkFacts({ ...FACTS, mem: '500000' }), /less than 1 GB of memory/)
  assert.throws(() => checkFacts({ ...FACTS, uid: '1000', sudo: '0' }), /root login/)
  assert.doesNotThrow(() => checkFacts({ ...FACTS, arch: 'aarch64', mem: '990000' }))
})

test('adds a server: installs, pins the host key, signs in with this computer’s key, never stores the password', async (t) => {
  const fake = await fakeServer(t)
  const { servers, dataDir } = await manager(t)
  const progress = []
  const result = await servers.add(
    { host: '127.0.0.1', port: fake.port, password: PASSWORD, name: 'My VPS' },
    (p) => progress.push(p),
  )
  assert.equal(result.ok, true, result.message + (result.detail || ''))
  assert.deepEqual(
    progress.map((p) => p.message),
    [
      'Connecting to your server…',
      'Checking your server…',
      'Installing sandboxd (this takes a few minutes)…',
      'Securing the connection…',
      'Done',
    ],
  )
  assert.match(result.server.hostKeySha256, /^SHA256:[A-Za-z0-9+/]{43}$/)
  assert.equal(result.server.front, 'caddy')
  assert.equal(result.server.domain, '203-0-113-10.sslip.io')
  assert.equal(result.server.status.state, 'connected')
  assert.equal(result.server.username, 'root')
  // This computer's key, marked, in the login's own file. The fake forwards to
  // the engine only for key logins, so the verify below proves the tunnel uses it.
  assert.equal(fake.state.authorized.length, 1)
  assert.match(fake.state.authorized[0], /^ssh-ed25519 \S+ sandboxd-desktop:[a-f0-9]{16}$/)
  // Root installs run without sudo; the password never appears in a command.
  assert(fake.state.commands.every((c) => !c.command.includes(PASSWORD)))
  assert(
    fake.state.commands.some(
      (c) => c.user === 'root' && c.command.startsWith('bash -c ') && c.command.includes('/install.sh'),
    ),
  )
  // The engine was reached through the tunnel with the API token.
  assert(
    fake.engine.requests.some((r) => r.url === '/v1/auth/status' && r.authorization === `Bearer ${TOKEN}`),
  )
  const files = await allFiles(dataDir)
  assert.deepEqual(files.map((f) => f.name).sort(), ['device-key.json', 'servers.json'])
  for (const file of files) {
    assert(!file.text.includes(PASSWORD), `${file.name} contains the password`)
    assert(!file.text.includes(TOKEN), `${file.name} contains the API token`)
    assert(!file.text.includes('OPENSSH PRIVATE KEY'), `${file.name} contains the private key`)
    if (process.platform !== 'win32') assert.equal(file.mode, 0o600)
  }
  assert(!JSON.stringify(progress).includes(PASSWORD))
  const saved = JSON.parse(files.find((f) => f.name === 'servers.json').text).servers[0]
  assert.deepEqual(Object.keys(saved).sort(), [
    'addedAt',
    'api',
    'apiTokenEnc',
    'domain',
    'front',
    'host',
    'hostKeySha256',
    'id',
    'name',
    'port',
    'proxy',
    'username',
  ])
  assert.equal(decrypt(Buffer.from(saved.apiTokenEnc, 'base64')), TOKEN)

  // The same server again is refused rather than reinstalled.
  const again = await servers.add({ host: '127.0.0.1', port: fake.port, password: PASSWORD })
  assert.equal(again.code, 'exists')
})

test('tunnel forwards HTTP (including large bodies) to the engine and survives restarts of the app', async (t) => {
  const fake = await fakeServer(t)
  const first = await manager(t)
  const { server } = await first.servers.add({ host: '127.0.0.1', port: fake.port, password: PASSWORD })
  const target = first.servers.resolve(server.id)
  assert.equal(target.token, TOKEN)
  const port = Number(new URL(target.origin).port)
  const body = Buffer.alloc(8 * 1024 * 1024, 7)
  const upload = await get(port, '/v1/apps/x/releases', {
    method: 'POST',
    headers: { authorization: `Bearer ${TOKEN}`, 'content-length': body.length },
    body,
  })
  assert.equal(JSON.parse(upload.body).size, body.length)
  assert.equal(first.servers.resolve('srv_000000000000'), null)
  assert.equal(first.servers.resolve('../etc'), null)

  // A new app session loads the saved server and reconnects with the stored key.
  await first.servers.close()
  const second = createServers({
    dataDir: first.dataDir,
    encrypt,
    decrypt,
    connectTimeout: 4000,
    backoff: [50],
    keepaliveInterval: 0,
  })
  t.after(() => second.close())
  await second.load()
  const again = second.resolve(server.id)
  const status = await get(Number(new URL(again.origin).port), '/v1/auth/status', {
    headers: { authorization: `Bearer ${again.token}` },
  })
  assert.equal(status.status, 200)
  assert.equal(second.status(server.id).state, 'connected')
})

test('reconnects after the connection drops (sleep, network change)', async (t) => {
  const fake = await fakeServer(t)
  const { servers, statuses } = await manager(t)
  const { server } = await servers.add({ host: '127.0.0.1', port: fake.port, password: PASSWORD })
  for (const client of fake.state.clients) client.end()
  await wait(() => statuses.some((s) => s.id === server.id && s.state === 'offline'))
  assert.equal(statuses.find((s) => s.state === 'offline').reason, MESSAGES.offline)
  await wait(() => servers.status(server.id).state === 'connected')
  const port = Number(new URL(servers.resolve(server.id).origin).port)
  assert.equal(
    (await get(port, '/v1/auth/status', { headers: { authorization: `Bearer ${TOKEN}` } })).status,
    200,
  )
  // refreshAll (on resume) reconnects immediately.
  servers.refreshAll()
  await wait(() => servers.status(server.id).state === 'connected')
})

test('a changed host key stops the tunnel until the user trusts the new identity', async (t) => {
  const fake = await fakeServer(t)
  const { servers } = await manager(t)
  const { server } = await servers.add({ host: '127.0.0.1', port: fake.port, password: PASSWORD })
  const pinned = server.hostKeySha256
  await fake.replaceHostKey()
  await wait(() => servers.status(server.id).identityChanged === true)
  const status = servers.status(server.id)
  assert.equal(status.state, 'offline')
  assert.equal(status.reason, MESSAGES.identity)
  assert.notEqual(status.presentedSha256, pinned)
  // Nothing goes through while the identity is unconfirmed.
  await assert.rejects(servers.run(server.id, 'echo hello'), /identity changed/)
  const port = Number(new URL(servers.resolve(server.id).origin).port)
  await assert.rejects(get(port, '/v1/auth/status'))
  await new Promise((r) => setTimeout(r, 300))
  assert.equal(servers.status(server.id).identityChanged, true, 'must not retry on its own')
  const trusted = await servers.trustIdentity(server.id, status.presentedSha256)
  assert.equal(trusted.hostKeySha256, status.presentedSha256)
  await wait(() => servers.status(server.id).state === 'connected')
  assert.equal((await servers.run(server.id, 'echo hello')).output, 'hello\n')
})

test('run executes as the saved login, and as root only with sudo and the password', async (t) => {
  const fake = await fakeServer(t, { admin: 'ubuntu', facts: { uid: '1000' } })
  const { servers } = await manager(t)
  const { server } = await servers.add({ host: '127.0.0.1', port: fake.port, username: 'ubuntu', password: PASSWORD })
  const plain = await servers.run(server.id, 'echo hello')
  assert.deepEqual([plain.code, plain.output], [0, 'hello\n'])
  assert.deepEqual(fake.state.commands.at(-1), { user: 'ubuntu', command: 'echo hello', byKey: true })
  // Admin without a password: passwordless sudo, which this server refuses.
  await assert.rejects(servers.run(server.id, 'id -u', { admin: true }), { code: 'needs_password' })
  const admin = await servers.run(server.id, 'id -u', { admin: true, password: PASSWORD })
  assert.equal(admin.output, '0\n')
  assert.equal(fake.state.commands.at(-1).command, "sudo -S -p '' bash -c 'id -u'")
  assert.equal(fake.state.stdin.at(-1), PASSWORD + '\n')
  await assert.rejects(servers.run(server.id, ''), /Invalid command/)
  await assert.rejects(servers.run(server.id, 'id', { admin: true, password: 'a\nb' }), /Invalid password/)
  await assert.rejects(servers.run('srv_ffffffffffff', 'echo hello'), /isn’t in your list/)
})

test('a sudo user installs with the password on stdin only', async (t) => {
  const fake = await fakeServer(t, { admin: 'ubuntu', facts: { uid: '1000' } })
  const { servers } = await manager(t)
  const result = await servers.add({
    host: '127.0.0.1',
    port: fake.port,
    username: 'ubuntu',
    password: PASSWORD,
  })
  assert.equal(result.ok, true, result.message + (result.detail || ''))
  const install = fake.state.commands.find((c) => c.command.includes('/install.sh'))
  assert(install.command.startsWith("sudo -S -p '' bash -c "))
  assert(!install.command.includes(PASSWORD))
  assert.deepEqual(fake.state.stdin, [PASSWORD + '\n'])
  // The key goes in ubuntu's own file, without sudo.
  const authorize = fake.state.commands.find((c) => c.command.startsWith('umask 077'))
  assert.equal(authorize.user, 'ubuntu')
  assert(!authorize.command.startsWith('sudo'))
})

test('asks before installing Docker, then installs it when allowed', async (t) => {
  const fake = await fakeServer(t, { facts: { docker: '0' } })
  const { servers } = await manager(t)
  const refused = await servers.add({ host: '127.0.0.1', port: fake.port, password: PASSWORD })
  assert.equal(refused.code, 'needs_docker')
  assert.match(refused.message, /doesn’t have Docker yet/)
  assert(!fake.state.commands.some((c) => c.command.includes('get.docker.com')))
  const progress = []
  const result = await servers.add(
    { host: '127.0.0.1', port: fake.port, password: PASSWORD, installDocker: true },
    (p) => progress.push(p.step),
  )
  assert.equal(result.ok, true, result.message + (result.detail || ''))
  assert(progress.includes('docker'))
})

test('plain errors: wrong password, unsuitable server, failed install', async (t) => {
  const fake = await fakeServer(t, { facts: { mem: '400000' } })
  const { servers, dataDir } = await manager(t)
  const wrong = await servers.add({ host: '127.0.0.1', port: fake.port, password: 'nope' })
  assert.equal(wrong.code, 'auth')
  assert.equal(wrong.message, MESSAGES.password)
  const small = await servers.add({ host: '127.0.0.1', port: fake.port, password: PASSWORD })
  assert.equal(small.code, 'unsupported')
  assert.match(small.message, /1 GB of memory/)

  const broken = await fakeServer(t, { installExit: 1 })
  const failed = await servers.add({ host: '127.0.0.1', port: broken.port, password: PASSWORD })
  assert.equal(failed.code, 'install_failed')
  assert.match(failed.detail, /disk full/)

  const noResult = await fakeServer(t, { result: '' })
  assert.equal(
    (await servers.add({ host: '127.0.0.1', port: noResult.port, password: PASSWORD })).code,
    'install_failed',
  )

  const closed = net.createServer()
  closed.listen(0, '127.0.0.1')
  await once(closed, 'listening')
  const closedPort = closed.address().port
  closed.close()
  const unreachable = await servers.add({ host: '127.0.0.1', port: closedPort, password: PASSWORD })
  assert.equal(unreachable.code, 'unreachable')
  assert.equal((await servers.add({ host: 'bad host', password: PASSWORD })).code, 'invalid')
  assert.deepEqual(servers.list(), [])
  for (const file of await allFiles(dataDir)) assert(!file.text.includes(PASSWORD))
})

test('remove forgets the server, takes this computer’s key off it and stops its tunnel', async (t) => {
  const fake = await fakeServer(t)
  const { servers, dataDir } = await manager(t)
  const { server } = await servers.add({ host: '127.0.0.1', port: fake.port, password: PASSWORD })
  const port = Number(new URL(servers.resolve(server.id).origin).port)
  const result = await servers.remove(server.id)
  assert.equal(result.removed, true)
  assert.equal(result.keyRemoved, true)
  assert.deepEqual(fake.state.authorized, [])
  assert.match(result.note, /keep running on the server/)
  assert.deepEqual(servers.list(), [])
  assert.equal(servers.resolve(server.id), null)
  await assert.rejects(get(port, '/v1/auth/status'))
  const saved = JSON.parse(await fs.readFile(path.join(dataDir, 'servers.json'), 'utf8'))
  assert.deepEqual(saved.servers, [])
})

test('refuses to add servers without secure storage', async (t) => {
  const { servers } = await manager(t, { canEncrypt: () => false })
  const result = await servers.add({ host: '127.0.0.1', password: PASSWORD })
  assert.equal(result.code, 'no_keychain')
})
