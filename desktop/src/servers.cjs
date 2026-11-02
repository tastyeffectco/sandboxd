// Your servers: add a Linux server over SSH, install the engine on it, then
// reach its API through an SSH tunnel as the limited `sandboxd` user.
//
// - One ed25519 key per device. The private key is encrypted with the OS
//   credential store (Electron safeStorage) and never logged.
// - The admin password is used once, in memory, to install. It is never written
//   anywhere; sudo receives it on stdin, never on a command line.
// - The server's host key is pinned on first contact (SHA256 fingerprint). A
//   different key later stops every connection until the user trusts it again.
// - After install the device key may only forward to the engine API and run
//   `frontdoor …` (enforced on the server by authorized_keys options).
const { Client, utils } = require('ssh2')
const { createHash, randomBytes } = require('node:crypto')
const fs = require('node:fs/promises')
const http = require('node:http')
const net = require('node:net')
const path = require('node:path')
const { ENGINE_TAG } = require('./windows-engine.cjs')

const REPO = 'tastyeffectco/sandboxd'
const SERVER_ID = /^srv_[a-f0-9]{12}$/
const RESULT_PREFIX = 'SANDBOXD_INSTALL_RESULT '
const LIMITED_USER = 'sandboxd'
const MIN_DISK_KB = 5 * 1024 * 1024
// Providers sell "1 GB" machines that report a little less once the kernel
// reserves its share.
const MIN_MEMORY_KB = 900 * 1024
const FRONTDOOR = new Set(['facts', 'apply', 'remove', 'status', 'enable-caddy-import'])
const FRONTS = new Set(['caddy', 'existing', 'manual'])
const PROXIES = new Set(['nginx', 'caddy', 'apache', 'traefik', 'coolify', 'none'])

const STEPS = {
  connecting: 'Connecting to your server…',
  checking: 'Checking your server…',
  docker: 'Installing Docker (this takes a couple of minutes)…',
  installing: 'Installing sandboxd (this takes a few minutes)…',
  securing: 'Securing the connection…',
  done: 'Done',
}
const MESSAGES = {
  identity:
    "This server's identity changed since you added it. If you didn't reinstall it, someone may be impersonating it.",
  password: "The server didn't accept this password. Check the login details your provider sent you.",
  notFound: "We couldn't find a server at this address. Check it for typos.",
  noAnswer: "Your server didn't answer. Check that it is turned on and that the address is right.",
  offline: "Can't reach this server right now. sandboxd keeps trying.",
  broken: "sandboxd couldn't open this server's saved connection. Remove it from your list and add it again.",
  keyRefused:
    'This server no longer accepts this computer. Remove it from your list and add it again with its password.',
}

class ServerError extends Error {
  constructor(code, message, extra = {}) {
    super(message)
    this.code = code
    Object.assign(this, extra)
  }
}

// OpenSSH's own format, so users can compare it with `ssh-keygen -lf`.
const fingerprint = (key) => 'SHA256:' + createHash('sha256').update(key).digest('base64').replace(/=+$/, '')
const shellQuote = (value) => `'${String(value).replace(/'/g, `'\\''`)}'`

function installRef(env = process.env) {
  const ref = env.SANDBOXD_SERVER_REF || ENGINE_TAG
  if (!/^[A-Za-z0-9][A-Za-z0-9._/-]{0,99}$/.test(ref) || ref.includes('..'))
    throw new ServerError('invalid_ref', 'SANDBOXD_SERVER_REF is not a valid git branch or tag.')
  return ref
}

// The engine install, exactly as documented by install.sh's contract. Release
// tags pull signed images; any other ref (a branch for testing) builds them.
function installCommand(ref, desktopKey) {
  if (!/^ssh-ed25519 [A-Za-z0-9+/=]+ [A-Za-z0-9._-]+$/.test(desktopKey))
    throw new ServerError('invalid_key', 'The device key is invalid.')
  const url = `https://raw.githubusercontent.com/${REPO}/${ref}/install.sh`
  const mode = /^v\d/.test(ref) ? '--pull' : '--build'
  return (
    `set -o pipefail; ` +
    `if command -v curl >/dev/null 2>&1; then curl -fsSL ${url}; else wget -qO- ${url}; fi | ` +
    `SANDBOXD_REF=${ref} bash -s -- ${mode} --no-console --no-desktop --front=auto --desktop-key="${desktopKey}"`
  )
}
const DOCKER_COMMAND =
  'set -o pipefail; if command -v curl >/dev/null 2>&1; then curl -fsSL https://get.docker.com; else wget -qO- https://get.docker.com; fi | sh'
const PREFLIGHT =
  `printf 'os=%s\\n' "$(uname -s)"; printf 'arch=%s\\n' "$(uname -m)"; printf 'uid=%s\\n' "$(id -u)"; ` +
  `printf 'mem=%s\\n' "$(awk '/^MemTotal:/{print $2}' /proc/meminfo 2>/dev/null)"; ` +
  `printf 'disk=%s\\n' "$(df -Pk /var/lib 2>/dev/null | awk 'NR==2{print $4}')"; ` +
  `if command -v docker >/dev/null 2>&1; then echo docker=1; else echo docker=0; fi; ` +
  `if command -v curl >/dev/null 2>&1 || command -v wget >/dev/null 2>&1; then echo fetch=1; else echo fetch=0; fi; ` +
  `if command -v sudo >/dev/null 2>&1; then echo sudo=1; else echo sudo=0; fi`

function parseFacts(output) {
  const facts = {}
  for (const line of output.split('\n')) {
    const match = /^([a-z]+)=(.*)$/.exec(line.trim())
    if (match) facts[match[1]] = match[2]
  }
  return facts
}

// Plain-language reasons a server can't host sandboxd.
function checkFacts(facts) {
  if (facts.os !== 'Linux')
    throw new ServerError(
      'unsupported',
      'This server is not running Linux. sandboxd needs a Linux server, such as Ubuntu or Debian.',
    )
  if (!['x86_64', 'amd64', 'aarch64', 'arm64'].includes(facts.arch))
    throw new ServerError(
      'unsupported',
      'This server uses a processor sandboxd does not support. Choose an Intel/AMD (x86) or ARM64 server.',
    )
  const disk = Number(facts.disk)
  if (Number.isFinite(disk) && facts.disk !== '' && disk < MIN_DISK_KB)
    throw new ServerError(
      'unsupported',
      `This server has ${(disk / 1024 / 1024).toFixed(1)} GB of free disk space. sandboxd needs at least 5 GB. Free some space or choose a bigger server.`,
    )
  const memory = Number(facts.mem)
  if (Number.isFinite(memory) && facts.mem !== '' && memory < MIN_MEMORY_KB)
    throw new ServerError(
      'unsupported',
      'This server has less than 1 GB of memory. Choose a server with at least 1 GB.',
    )
  if (facts.fetch !== '1')
    throw new ServerError(
      'unsupported',
      'This server cannot download files (curl or wget is missing). Install curl on it, then try again.',
    )
  if (facts.uid !== '0' && facts.sudo !== '1')
    throw new ServerError(
      'needs_admin',
      "This login can't install software on the server. Use the root login, or a user that can use sudo.",
    )
}

// The installer's last line: SANDBOXD_INSTALL_RESULT {json}.
function parseInstallResult(line) {
  if (typeof line !== 'string' || !line.startsWith(RESULT_PREFIX)) return null
  let result
  try {
    result = JSON.parse(line.slice(RESULT_PREFIX.length))
  } catch {
    return null
  }
  if (!result || typeof result !== 'object') return null
  const api = typeof result.api === 'string' ? result.api : '127.0.0.1:9090'
  const apiMatch = /^127\.0\.0\.1:(\d{1,5})$/.exec(api)
  if (
    typeof result.api_token !== 'string' ||
    !/^[\x21-\x7e]{16,512}$/.test(result.api_token) ||
    !apiMatch ||
    Number(apiMatch[1]) < 1 ||
    Number(apiMatch[1]) > 65535
  )
    return null
  const text = (value, max = 253) => (typeof value === 'string' && value.length <= max ? value : '')
  return {
    front: FRONTS.has(result.front) ? result.front : 'manual',
    domain: /^[A-Za-z0-9.-]{0,253}$/.test(text(result.domain)) ? text(result.domain) : '',
    proxy: PROXIES.has(result.proxy) ? result.proxy : 'none',
    apiToken: result.api_token,
    api,
  }
}

function validateInput(input) {
  if (!input || typeof input !== 'object')
    throw new ServerError('invalid', 'Enter your server’s address and password.')
  const host = typeof input.host === 'string' ? input.host.trim().replace(/^\[(.*)\]$/, '$1') : ''
  if (
    !host ||
    host.length > 253 ||
    !(/^[A-Za-z0-9.-]+$/.test(host) || net.isIPv6(host)) ||
    host.startsWith('-')
  )
    throw new ServerError(
      'invalid',
      'Enter the server address, such as 203.0.113.10 or myserver.example.com.',
    )
  const port = input.port === undefined || input.port === '' ? 22 : Number(input.port)
  if (!Number.isInteger(port) || port < 1 || port > 65535)
    throw new ServerError('invalid', 'The SSH port is a number between 1 and 65535 (usually 22).')
  const username = input.username === undefined || input.username === '' ? 'root' : input.username
  if (typeof username !== 'string' || !/^[a-z_][a-z0-9_.-]{0,31}$/i.test(username))
    throw new ServerError('invalid', 'Enter a valid user name (usually root).')
  if (
    typeof input.password !== 'string' ||
    !input.password ||
    input.password.length > 1024 ||
    /[\r\n]/.test(input.password)
  )
    throw new ServerError('invalid', 'Enter the server password.')
  const name = typeof input.name === 'string' && input.name.trim() ? input.name.trim().slice(0, 80) : host
  return { host, port, username, password: input.password, name, installDocker: input.installDocker === true }
}

function connectionError(error) {
  if (error instanceof ServerError) return error
  if (error?.level === 'client-authentication') return new ServerError('auth', MESSAGES.password)
  if (['ENOTFOUND', 'EAI_AGAIN'].includes(error?.code)) return new ServerError('not_found', MESSAGES.notFound)
  if (error?.level || error?.code) return new ServerError('unreachable', MESSAGES.noAnswer)
  return new ServerError('error', 'Something went wrong while adding the server. Try again.')
}

// Opens an SSH connection. The host key must match `expected` when given;
// otherwise the presented fingerprint is reported for pinning.
function sshConnect(options, { expected, connectTimeout = 20000 } = {}) {
  return new Promise((resolve, reject) => {
    const client = new Client()
    let presented = ''
    let settled = false
    const done = (error) => {
      if (settled) return
      settled = true
      if (error) {
        client.end()
        reject(error)
      } else resolve({ client, fingerprint: presented })
    }
    client.on('ready', () => done())
    client.on('error', (error) =>
      done(
        presented && expected && presented !== expected
          ? new ServerError('identity', MESSAGES.identity, { fingerprint: presented })
          : connectionError(error),
      ),
    )
    client.on('close', () => done(new ServerError('unreachable', MESSAGES.noAnswer)))
    if (options.password)
      client.on('keyboard-interactive', (_name, _instructions, _lang, prompts, finish) =>
        finish(prompts.map(() => options.password)),
      )
    client.connect({
      ...options,
      readyTimeout: connectTimeout,
      tryKeyboard: !!options.password,
      hostVerifier: (key) => {
        presented = fingerprint(key)
        return !expected || presented === expected
      },
    })
  })
}

// Runs one command, streaming its output line by line. Only a bounded tail of
// output is kept, for error details.
function execute(client, command, { stdin, onLine = () => {}, timeout = 60000, maxOutput = 1 << 20 } = {}) {
  return new Promise((resolve, reject) => {
    client.exec(command, (error, stream) => {
      if (error) return reject(new ServerError('unreachable', MESSAGES.offline))
      let output = ''
      let partial = ''
      const timer = setTimeout(() => {
        stream.close()
        reject(new ServerError('timeout', 'Your server took too long to answer. Try again.'))
      }, timeout)
      const take = (chunk) => {
        const text = chunk.toString('utf8')
        output = (output + text).slice(-maxOutput)
        const lines = (partial + text).split('\n')
        partial = lines.pop().slice(-maxOutput)
        for (const line of lines) onLine(line.replace(/\r$/, ''))
      }
      stream.on('data', take)
      stream.stderr.on('data', take)
      stream.on('close', (code, signal) => {
        clearTimeout(timer)
        if (partial) onLine(partial.replace(/\r$/, ''))
        resolve({ code: typeof code === 'number' ? code : null, signal, output })
      })
      if (stdin !== undefined) stream.end(stdin)
      else stream.end()
    })
  })
}

const lastLines = (output, count = 15) =>
  output
    .split('\n')
    .map((line) => line.replace(/\r$/, ''))
    .filter((line) => line.trim() && !line.startsWith(RESULT_PREFIX))
    .slice(-count)
    .join('\n')

// One SSH connection per server as the limited user, with a local listener on
// 127.0.0.1:<ephemeral> forwarding to the engine API. Reconnects with backoff.
function createTunnel({
  server,
  privateKey,
  onStatus = () => {},
  backoff = [1000, 2000, 5000, 10000, 30000, 60000],
  connectTimeout = 20000,
  keepaliveInterval = 15000,
}) {
  const [apiHost, apiPort] = server.api.split(':')
  let status = { state: 'connecting', reason: '' }
  let client = null
  let ready = false
  let stopped = false
  let timer = null
  let attempt = 0
  let changedIdentity = ''
  let pinned = server.hostKeySha256
  const waiting = new Set()
  const active = new Set()
  const readyWaiters = new Set()
  const set = (next) => {
    status = next
    onStatus(status)
  }
  const forward = (socket) => {
    client.forwardOut('127.0.0.1', 0, apiHost, Number(apiPort), (error, stream) => {
      if (error || socket.destroyed) {
        stream?.close()
        return socket.destroy()
      }
      socket.on('error', () => stream.close())
      stream.on('error', () => socket.destroy())
      stream.on('close', () => socket.destroy())
      socket.on('close', () => stream.close())
      socket.pipe(stream).pipe(socket)
    })
  }
  const listener = net.createServer((socket) => {
    active.add(socket)
    socket.once('close', () => active.delete(socket))
    socket.on('error', () => {})
    if (changedIdentity) return socket.destroy()
    if (ready) return forward(socket)
    waiting.add(socket)
    const drop = setTimeout(() => socket.destroy(), connectTimeout)
    socket.once('close', () => {
      clearTimeout(drop)
      waiting.delete(socket)
    })
    if (!client && !changedIdentity) connectNow()
  })
  const connectNow = () => {
    if (stopped || client) return
    clearTimeout(timer)
    timer = null
    if (status.state !== 'connected') set({ state: 'connecting', reason: '' })
    const connection = new Client()
    client = connection
    let lastError = null
    let presented = ''
    connection.on('ready', () => {
      if (client !== connection) return
      ready = true
      attempt = 0
      set({ state: 'connected', reason: '' })
      for (const socket of waiting) forward(socket)
      waiting.clear()
      for (const resolve of readyWaiters) resolve()
      readyWaiters.clear()
    })
    connection.on('error', (error) => {
      lastError = error
    })
    connection.on('close', () => {
      if (client !== connection) return
      client = null
      ready = false
      if (stopped) return
      if (presented && presented !== pinned) {
        changedIdentity = presented
        for (const socket of waiting) socket.destroy()
        waiting.clear()
        return set({
          state: 'offline',
          reason: MESSAGES.identity,
          identityChanged: true,
          presentedSha256: presented,
        })
      }
      const refused = lastError?.level === 'client-authentication'
      const delay = refused ? 300000 : backoff[Math.min(attempt, backoff.length - 1)]
      attempt++
      set({ state: 'offline', reason: refused ? MESSAGES.keyRefused : MESSAGES.offline })
      timer = setTimeout(connectNow, delay)
      timer.unref?.()
    })
    connection.connect({
      host: server.host,
      port: server.port,
      username: LIMITED_USER,
      privateKey,
      readyTimeout: connectTimeout,
      keepaliveInterval,
      keepaliveCountMax: 3,
      hostVerifier: (key) => {
        presented = fingerprint(key)
        return presented === pinned
      },
    })
  }
  const whenReady = (timeout = connectTimeout) =>
    ready
      ? Promise.resolve()
      : new Promise((resolve, reject) => {
          if (changedIdentity) return reject(new ServerError('identity', MESSAGES.identity))
          const fail = setTimeout(() => {
            readyWaiters.delete(done)
            reject(new ServerError('unreachable', status.reason || MESSAGES.offline))
          }, timeout)
          const done = () => {
            clearTimeout(fail)
            resolve()
          }
          readyWaiters.add(done)
          connectNow()
        })
  return {
    async start() {
      await new Promise((resolve, reject) => {
        listener.once('error', reject)
        listener.listen(0, '127.0.0.1', resolve)
      })
      connectNow()
    },
    get port() {
      return listener.address()?.port
    },
    status: () => ({ ...status }),
    // After sleep or a network change: drop a possibly dead connection and
    // reconnect now instead of waiting for keepalives or the backoff.
    refresh() {
      if (stopped || changedIdentity) return
      attempt = 0
      if (client) {
        const old = client
        client = null
        ready = false
        old.end()
      }
      connectNow()
    },
    trust(next) {
      pinned = next
      server.hostKeySha256 = next
      changedIdentity = ''
      attempt = 0
      connectNow()
    },
    async exec(command, options) {
      await whenReady()
      return execute(client, command, options)
    },
    whenReady,
    async stop() {
      stopped = true
      clearTimeout(timer)
      client?.end()
      client = null
      ready = false
      for (const socket of active) socket.destroy()
      await new Promise((resolve) => listener.close(() => resolve()))
    },
  }
}

// Confirms the installed engine answers through the tunnel and accepts the token.
function verifyEngine(port, token, timeout = 20000) {
  return new Promise((resolve, reject) => {
    const request = http.get(
      {
        host: '127.0.0.1',
        port,
        path: '/v1/auth/status',
        headers: { authorization: `Bearer ${token}` },
        timeout,
      },
      (response) => {
        let body = ''
        response.setEncoding('utf8')
        response.on('data', (chunk) => {
          body += chunk
          if (body.length > 65536) response.destroy()
        })
        response.on('end', () => {
          try {
            const status = JSON.parse(body)
            if (response.statusCode === 200 && status.authenticated !== false) return resolve()
          } catch {
            /* rejected below */
          }
          reject(
            new ServerError(
              'verify',
              'sandboxd was installed, but its secure connection did not work. Try adding the server again.',
            ),
          )
        })
      },
    )
    request.on('timeout', () => request.destroy())
    request.on('error', () =>
      reject(
        new ServerError(
          'verify',
          'sandboxd was installed, but its secure connection did not work. Try adding the server again.',
        ),
      ),
    )
  })
}

function createServers({
  dataDir,
  encrypt,
  decrypt,
  canEncrypt = () => true,
  onStatus = () => {},
  env = process.env,
  connectTimeout = 20000,
  installTimeout = 45 * 60 * 1000,
  backoff,
  keepaliveInterval,
}) {
  const file = path.join(dataDir, 'servers.json')
  const keyFile = path.join(dataDir, 'device-key.json')
  let servers = []
  const tunnels = new Map()
  const tokens = new Map()
  let writes = Promise.resolve()
  let device = null
  let adding = false

  const writeAtomic = (target, value) => {
    writes = writes
      .catch(() => {})
      .then(async () => {
        await fs.mkdir(dataDir, { recursive: true })
        await fs.writeFile(target + '.tmp', JSON.stringify(value, null, 2), { mode: 0o600 })
        await fs.rename(target + '.tmp', target)
      })
    return writes
  }
  const persist = () =>
    writeAtomic(file, {
      version: 1,
      servers: servers.map(
        ({ id, name, host, port, hostKeySha256, front, domain, proxy, api, apiTokenEnc, addedAt }) => ({
          id,
          name,
          host,
          port,
          hostKeySha256,
          front,
          domain,
          proxy,
          api,
          apiTokenEnc,
          addedAt,
        }),
      ),
    })
  const requireStorage = () => {
    if (!canEncrypt())
      throw new ServerError(
        'no_keychain',
        'Adding a server needs your computer’s secure password storage, which is not available. Sign in to your computer’s keychain and try again.',
      )
  }
  async function deviceKey() {
    if (device) return device
    requireStorage()
    try {
      const stored = JSON.parse(await fs.readFile(keyFile, 'utf8'))
      const privateKey = decrypt(Buffer.from(stored.privateEnc, 'base64'))
      const parsed = utils.parseKey(privateKey)
      if (parsed instanceof Error || parsed.type !== 'ssh-ed25519') throw parsed
      device = { publicKey: stored.public, privateKey }
      return device
    } catch {
      // No key yet, or one this keychain can no longer read (a reset keychain,
      // another OS user): create one. Servers added with an unreadable key
      // must be added again anyway.
    }
    const pair = utils.generateKeyPairSync('ed25519', { comment: 'sandboxd-desktop' })
    await writeAtomic(keyFile, {
      version: 1,
      public: pair.public,
      privateEnc: encrypt(pair.private).toString('base64'),
    })
    device = { publicKey: pair.public, privateKey: pair.private }
    return device
  }
  const publicInfo = (server) => ({
    id: server.id,
    name: server.name,
    host: server.host,
    port: server.port,
    hostKeySha256: server.hostKeySha256,
    front: server.front,
    domain: server.domain,
    proxy: server.proxy,
    addedAt: server.addedAt,
    status: tunnels.get(server.id)?.status() || { state: 'offline', reason: MESSAGES.broken },
  })
  const find = (id) => {
    if (typeof id !== 'string' || !SERVER_ID.test(id))
      throw new ServerError('unknown', 'This server isn’t in your list.')
    const server = servers.find((s) => s.id === id)
    if (!server) throw new ServerError('unknown', 'This server isn’t in your list.')
    return server
  }
  async function startTunnel(server) {
    const { privateKey } = await deviceKey()
    const tunnel = createTunnel({
      server,
      privateKey,
      connectTimeout,
      backoff,
      keepaliveInterval,
      onStatus: (status) => onStatus(server.id, status),
    })
    tunnels.set(server.id, tunnel)
    await tunnel.start()
    return tunnel
  }

  async function install(input, progress) {
    const step = (name) => progress({ step: name, message: STEPS[name] })
    step('connecting')
    const known = servers.find((s) => s.host === input.host && s.port === input.port)
    if (known) throw new ServerError('exists', `${known.name} is already in your list.`)
    const { publicKey } = await deviceKey()
    const { client, fingerprint: hostKeySha256 } = await sshConnect(
      { host: input.host, port: input.port, username: input.username, password: input.password },
      { connectTimeout },
    )
    let result = null
    try {
      step('checking')
      const facts = parseFacts((await execute(client, PREFLIGHT, { timeout: 60000 })).output)
      checkFacts(facts)
      const root = facts.uid === '0'
      // sudo reads the password from stdin (never argv); `-p ''` keeps the
      // prompt out of the output.
      const admin = (script) =>
        root ? `bash -c ${shellQuote(script)}` : `sudo -S -p '' bash -c ${shellQuote(script)}`
      const stdin = root ? undefined : input.password + '\n'
      const failed = (output, code) => {
        if (
          !root &&
          /incorrect password|not in the sudoers|may not run sudo|a password is required/i.test(output)
        )
          return new ServerError(
            'needs_admin',
            "This login can't install software on the server. Use the root login, or a user that can use sudo.",
          )
        return new ServerError(
          code,
          code === 'docker_failed'
            ? 'Docker could not be installed on this server. Try again; if it keeps failing, install Docker yourself and add the server again.'
            : 'The installation on your server stopped before it finished. Try again; if it keeps failing, share the details below with support.',
          { detail: lastLines(output) },
        )
      }
      if (facts.docker !== '1') {
        if (!input.installDocker)
          throw new ServerError(
            'needs_docker',
            'This server doesn’t have Docker yet. sandboxd needs it to run your apps and can install it for you (about 2 minutes).',
          )
        step('docker')
        const docker = await execute(client, admin(DOCKER_COMMAND), { stdin, timeout: installTimeout })
        if (docker.code !== 0) throw failed(docker.output, 'docker_failed')
      }
      step('installing')
      const run = await execute(client, admin(installCommand(installRef(env), publicKey)), {
        stdin,
        timeout: installTimeout,
        onLine: (line) => {
          const parsed = parseInstallResult(line.trim())
          if (parsed) result = parsed
        },
      })
      if (run.code !== 0 || !result) throw failed(run.output, 'install_failed')
    } finally {
      client.end()
    }
    return { hostKeySha256, result }
  }

  return {
    MESSAGES,
    async load() {
      try {
        const stored = JSON.parse(await fs.readFile(file, 'utf8'))
        servers = (Array.isArray(stored.servers) ? stored.servers : [])
          .filter(
            (s) =>
              s &&
              SERVER_ID.test(s.id) &&
              typeof s.host === 'string' &&
              Number.isInteger(s.port) &&
              typeof s.hostKeySha256 === 'string' &&
              /^127\.0\.0\.1:\d+$/.test(s.api) &&
              typeof s.apiTokenEnc === 'string',
          )
          .slice(0, 50)
      } catch {
        servers = []
      }
      if (!servers.length || !canEncrypt()) return
      for (const server of servers) {
        try {
          tokens.set(server.id, decrypt(Buffer.from(server.apiTokenEnc, 'base64')))
          await startTunnel(server)
        } catch {
          /* Listed as offline; removing and adding it again repairs it. */
        }
      }
    },
    list: () => servers.map(publicInfo),
    status(id) {
      if (id === undefined) return Object.fromEntries(servers.map((s) => [s.id, publicInfo(s).status]))
      return publicInfo(find(id)).status
    },
    // Resolves {ok:true, server} or {ok:false, code, message, ...}; never throws
    // so error codes reach the renderer intact.
    async add(rawInput, onProgress = () => {}) {
      if (adding)
        return { ok: false, code: 'busy', message: 'A server is already being added. Wait for it to finish.' }
      adding = true
      let input
      try {
        requireStorage()
        input = validateInput(rawInput)
        const { hostKeySha256, result } = await install(input, onProgress)
        input.password = null
        onProgress({ step: 'securing', message: STEPS.securing })
        const server = {
          id: 'srv_' + randomBytes(6).toString('hex'),
          name: input.name,
          host: input.host,
          port: input.port,
          hostKeySha256,
          front: result.front,
          domain: result.domain,
          proxy: result.proxy,
          api: result.api,
          apiTokenEnc: encrypt(result.apiToken).toString('base64'),
          addedAt: new Date().toISOString(),
        }
        const tunnel = await startTunnel(server)
        try {
          await tunnel.whenReady(connectTimeout)
          await verifyEngine(tunnel.port, result.apiToken, connectTimeout)
        } catch (error) {
          tunnels.delete(server.id)
          await tunnel.stop()
          throw error instanceof ServerError && error.code === 'verify'
            ? error
            : new ServerError(
                'verify',
                'sandboxd was installed, but its secure connection did not work. Try adding the server again.',
              )
        }
        tokens.set(server.id, result.apiToken)
        servers.push(server)
        await persist()
        onProgress({ step: 'done', message: STEPS.done })
        return { ok: true, server: publicInfo(server) }
      } catch (error) {
        const failure = connectionError(error)
        return {
          ok: false,
          code: failure.code,
          message: failure.message,
          ...(failure.detail ? { detail: failure.detail } : {}),
        }
      } finally {
        if (input) input.password = null
        adding = false
      }
    },
    // Forgets the server here. The engine, its apps and this device's limited
    // key stay on the server (the key can only reach the engine API).
    async remove(id) {
      const server = find(id)
      const tunnel = tunnels.get(id)
      tunnels.delete(id)
      tokens.delete(id)
      servers = servers.filter((s) => s !== server)
      await tunnel?.stop()
      await persist()
      return {
        removed: true,
        note: `${server.name} was removed from this computer. sandboxd and your apps keep running on the server.`,
      }
    },
    async frontdoor(id, args) {
      const tunnel = tunnels.get(find(id).id)
      if (!tunnel) throw new ServerError('unreachable', MESSAGES.offline)
      if (
        !Array.isArray(args) ||
        !args.length ||
        args.length > 32 ||
        !FRONTDOOR.has(args[0]) ||
        !args.every((arg) => typeof arg === 'string' && /^[A-Za-z0-9._:-]{1,253}$/.test(arg))
      )
        throw new ServerError('invalid', 'Invalid front door request.')
      const { output } = await tunnel.exec(['frontdoor', ...args].join(' '), { timeout: 10 * 60 * 1000 })
      const line =
        output
          .split('\n')
          .map((l) => l.trim())
          .filter(Boolean)
          .pop() || ''
      try {
        return JSON.parse(line)
      } catch {
        return {
          ok: false,
          step: args[0],
          message: 'The server sent an answer sandboxd did not understand. Try again.',
        }
      }
    },
    // Only after the user confirmed: pin the identity the server now presents.
    async trustIdentity(id, next) {
      const server = find(id)
      if (typeof next !== 'string' || !/^SHA256:[A-Za-z0-9+/]{43}$/.test(next))
        throw new ServerError('invalid', 'Invalid server identity.')
      server.hostKeySha256 = next
      await persist()
      tunnels.get(id)?.trust(next)
      return publicInfo(server)
    },
    refreshAll() {
      for (const tunnel of tunnels.values()) tunnel.refresh()
    },
    // For the gateway: where a server's engine is reachable, and its token.
    resolve(id) {
      if (typeof id !== 'string' || !SERVER_ID.test(id)) return null
      const tunnel = tunnels.get(id)
      const token = tokens.get(id)
      return tunnel && token ? { origin: `http://127.0.0.1:${tunnel.port}`, token } : null
    },
    async close() {
      await Promise.all([...tunnels.values()].map((tunnel) => tunnel.stop()))
      tunnels.clear()
      await writes.catch(() => {})
    },
  }
}

module.exports = {
  createServers,
  createTunnel,
  installCommand,
  installRef,
  parseInstallResult,
  parseFacts,
  checkFacts,
  fingerprint,
  SERVER_ID,
  STEPS,
  MESSAGES,
}
