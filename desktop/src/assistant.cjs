// The server assistant: OpenCode on this computer, on its free model by
// default, whose only tools are the approval-gated server tools
// (server-tools.cjs), which this app serves to it as MCP over loopback HTTP.
//
// - OpenCode is downloaded once, from npm's platform package at a pinned
//   version, and accepted only if its sha512 matches the one pinned here.
// - It runs in its own data folders under the app's data directory, so it
//   neither reads nor changes an OpenCode the user installed themselves.
// - Its own tools (shell, files, web) are switched off: it can only look at and
//   change a server through the sandboxd tools, and every change waits for the
//   user.
// - One conversation per server; each message is one `opencode run`, continuing
//   that server's session.
const { spawn } = require('node:child_process')
const { createHash, randomBytes } = require('node:crypto')
const fs = require('node:fs')
const fsp = require('node:fs/promises')
const https = require('node:https')
const http = require('node:http')
const path = require('node:path')
const zlib = require('node:zlib')

const OPENCODE_VERSION = '1.19.5'
// npm `dist.integrity` of opencode-<platform>@1.19.5.
const OPENCODE_PACKAGES = {
  'win32-x64': 'sha512-xCVbrA8A/qQTAdBIKyhK5tx3/mcq18yF82Mi8phry0MJb2631nnnpxF9m6FFfWAVHyer4qFGrO7hbWXesHQ1Gg==',
  'win32-arm64': 'sha512-aySF/MDji5KO3I5yl9Ec4g8kW/QIx4Jj5FXwBhh/FrDu0w6L8HAOGH7cyjFb3t8TW7pxRkH3Ei3rfkWAE7cScw==',
  'darwin-x64': 'sha512-+DO7IMPTCrX12UJXnL4pL3ckcwR6BR6f6tA9QIlVQ5nZx66T5U7FRMJT4n5rsOnJxLM/pZ2vfX1G8vrnvXO8FA==',
  'darwin-arm64': 'sha512-0MbSbKkmwVHtgP31GJyyECe6QVy4T7W4Wp7s2TsmXHTPPyf01ne3dWSX97X9dkNK28Lc6kt+zMPQUP/9GgDLZg==',
  'linux-x64': 'sha512-i1qh7VooLGML9xX6ud93y0JA8j/1aPeDYRKjsmYXrJX1TjIWHgBBMkEbEJPZthpWbsJg//XfBgR6Kb7+NPaJiQ==',
  'linux-arm64': 'sha512-u2HFRI6v1nNMVKiV34An8XrGBxJeZ+RCg2tYu3SqlGODKqKsr5RoEbaX1tnsfGb+rETh9AJVX9kS8dSZXPufsA==',
}
const FREE_MODEL = 'opencode/big-pickle'
const { handle: handleMcp } = require('./mcp-tools.cjs')
const SERVER_ID = /^srv_[a-f0-9]{12}$/
const MAX_PROMPT = 8000

class AssistantError extends Error {
  constructor(code, message) {
    super(message)
    this.code = code
  }
}

function opencodePackage(platform = process.platform, arch = process.arch) {
  const key = `${platform}-${arch}`
  const integrity = OPENCODE_PACKAGES[key]
  if (!integrity) return null
  const name = `opencode-${platform === 'win32' ? 'windows' : platform}-${arch}`
  return {
    name,
    integrity,
    url: `https://registry.npmjs.org/${name}/-/${name}-${OPENCODE_VERSION}.tgz`,
    binary: platform === 'win32' ? 'opencode.exe' : 'opencode',
  }
}

// GET with a few redirects, into a file, hashing as it goes.
function download(url, target, { onProgress = () => {}, redirects = 5 } = {}) {
  return new Promise((resolve, reject) => {
    const client = url.startsWith('http:') ? http : https
    const request = client.get(url, { headers: { 'user-agent': 'sandboxd-desktop' } }, (response) => {
      if ([301, 302, 303, 307, 308].includes(response.statusCode) && response.headers.location && redirects > 0) {
        response.resume()
        return resolve(download(new URL(response.headers.location, url).href, target, { onProgress, redirects: redirects - 1 }))
      }
      if (response.statusCode !== 200) {
        response.resume()
        return reject(new AssistantError('download', `The download failed (HTTP ${response.statusCode}).`))
      }
      const total = Number(response.headers['content-length']) || 0
      const hash = createHash('sha512')
      let received = 0
      const out = fs.createWriteStream(target, { mode: 0o600 })
      response.on('data', (chunk) => {
        hash.update(chunk)
        received += chunk.length
        onProgress({ received, total })
      })
      response.on('error', reject)
      out.on('error', reject)
      out.on('finish', () => resolve('sha512-' + hash.digest('base64')))
      response.pipe(out)
    })
    request.setTimeout(60000, () => request.destroy(new AssistantError('download', 'The download stalled.')))
    request.on('error', (error) =>
      reject(error instanceof AssistantError ? error : new AssistantError('download', 'The download failed.')),
    )
  })
}

// Streams one regular file out of a .tgz (ustar, with pax headers) into `target`.
function extractOne(tgz, member, target) {
  return new Promise((resolve, reject) => {
    let buffer = Buffer.alloc(0)
    let state = 'header'
    let remaining = 0
    let pad = 0
    let out = null
    let paxPath = ''
    let found = false
    let collectPax = null
    const input = fs.createReadStream(tgz).pipe(zlib.createGunzip())
    const fail = (error) => {
      input.destroy()
      out?.destroy()
      reject(error)
    }
    const parseOctal = (field) => parseInt(field.toString('ascii').replace(/\0.*$/s, '').trim() || '0', 8)
    input.on('data', (chunk) => {
      buffer = buffer.length ? Buffer.concat([buffer, chunk]) : chunk
      while (buffer.length) {
        if (state === 'header') {
          if (buffer.length < 512) return
          const header = buffer.subarray(0, 512)
          buffer = buffer.subarray(512)
          if (header.every((b) => b === 0)) continue
          const name =
            paxPath ||
            [header.subarray(345, 500), header.subarray(0, 100)]
              .map((f) => f.toString('utf8').replace(/\0.*$/s, ''))
              .filter(Boolean)
              .join('/')
          paxPath = ''
          const type = String.fromCharCode(header[156] || 48)
          remaining = parseOctal(header.subarray(124, 136))
          pad = (512 - (remaining % 512)) % 512
          collectPax = type === 'x' ? [] : null
          if ((type === '0' || type === '\0') && name === member) {
            found = true
            out = fs.createWriteStream(target, { mode: 0o700 })
            out.on('error', fail)
          }
          state = 'body'
        } else if (state === 'body') {
          const take = Math.min(remaining, buffer.length)
          const piece = buffer.subarray(0, take)
          buffer = buffer.subarray(take)
          remaining -= take
          if (out) out.write(piece)
          if (collectPax) collectPax.push(piece)
          if (remaining === 0) {
            if (collectPax) {
              const match = /(?:^|\n)\d+ path=([^\n]*)\n/.exec(Buffer.concat(collectPax).toString('utf8'))
              if (match) paxPath = match[1]
              collectPax = null
            }
            if (out) {
              const done = out
              out = null
              input.pause()
              done.end(() => input.resume())
            }
            state = 'pad'
          }
        } else {
          const take = Math.min(pad, buffer.length)
          buffer = buffer.subarray(take)
          pad -= take
          if (pad === 0) state = 'header'
        }
      }
    })
    input.on('error', () => fail(new AssistantError('download', 'The downloaded file is damaged.')))
    input.on('end', () => {
      if (!found) return fail(new AssistantError('download', 'The download did not contain OpenCode.'))
      if (out) out.end(() => resolve())
      else resolve()
    })
  })
}

// The OpenCode binary, downloading it the first time. Concurrent callers share
// one download.
function createInstaller({
  dataDir,
  platform = process.platform,
  arch = process.arch,
  onProgress = () => {},
  pkg = opencodePackage(platform, arch),
}) {
  const dir = path.join(dataDir, 'opencode', OPENCODE_VERSION)
  const binary = pkg && path.join(dir, pkg.binary)
  let running = null
  const installed = async () => {
    try {
      return (await fsp.readFile(path.join(dir, 'installed'), 'utf8')).trim() === pkg.integrity
    } catch {
      return false
    }
  }
  return {
    binary,
    supported: !!pkg,
    installed: async () => !!pkg && (await installed()),
    ensure(url = pkg?.url) {
      if (!pkg)
        return Promise.reject(new AssistantError('unsupported', 'The assistant is not available on this computer yet.'))
      running ||= (async () => {
        if (await installed()) return binary
        await fsp.mkdir(dir, { recursive: true })
        const tgz = path.join(dir, `download-${randomBytes(4).toString('hex')}.tgz`)
        try {
          const integrity = await download(url, tgz, { onProgress })
          if (integrity !== pkg.integrity)
            throw new AssistantError('integrity', 'The downloaded assistant did not match the expected file, so it was not used.')
          await extractOne(tgz, `package/bin/${pkg.binary}`, binary + '.tmp')
          await fsp.rename(binary + '.tmp', binary)
          if (platform !== 'win32') await fsp.chmod(binary, 0o755)
          await fsp.writeFile(path.join(dir, 'installed'), pkg.integrity)
          return binary
        } finally {
          await fsp.rm(tgz, { force: true })
          await fsp.rm(binary + '.tmp', { force: true })
        }
      })().finally(() => {
        running = null
      })
      return running
    },
  }
}

// OpenCode's configuration: only the sandboxd tools, the instructions, no
// sharing or self-update.
function opencodeConfig({ mcpUrl, headers, instructions, model = FREE_MODEL }) {
  return {
    $schema: 'https://opencode.ai/config.json',
    model,
    share: 'disabled',
    autoupdate: false,
    instructions: [instructions],
    tools: { '*': false, 'sandboxd_*': true },
    mcp: {
      sandboxd: {
        type: 'remote',
        url: mcpUrl,
        headers,
        oauth: false,
        enabled: true,
        // A tool call waits for the user's approval (server-tools.cjs gives up
        // after 15 minutes).
        timeout: 16 * 60 * 1000,
      },
    },
  }
}

// One line of `opencode run --format json` → what the chat shows, or null.
function chatEvent(line) {
  let event
  try {
    event = JSON.parse(line)
  } catch {
    return null
  }
  const part = event?.part || {}
  if (event.type === 'text' && typeof part.text === 'string' && part.text.trim())
    return { type: 'text', text: part.text }
  if (event.type === 'tool_use' && typeof part.tool === 'string') {
    const tool = part.tool.replace(/^sandboxd_/, '')
    return { type: 'tool', tool, status: part.state?.status || '' }
  }
  if (event.type === 'error')
    return { type: 'error', text: event.error?.data?.message || event.error?.message || 'The assistant stopped.' }
  return null
}

// Plain words for what OpenCode prints when it can't answer.
function failureMessage(stderr) {
  if (/free usage limit|rate limit|quota|429/i.test(stderr))
    return 'The free assistant is busy or has reached its limit for now. Try again in a while, or connect your own AI account in Settings.'
  if (/ENOTFOUND|EAI_AGAIN|ECONNREFUSED|ETIMEDOUT|network/i.test(stderr))
    return 'The assistant could not reach the internet. Check your connection and try again.'
  return 'The assistant stopped before it finished. Try again.'
}

function createAssistant({
  dataDir,
  installer,
  instructionsFile,
  toolPort,
  toolToken,
  serverInfo,
  spawnProcess = spawn,
  env = process.env,
  timeout = 30 * 60 * 1000,
}) {
  const running = new Map()

  async function send(serverId, prompt, onEvent = () => {}) {
    if (typeof prompt !== 'string' || !prompt.trim() || prompt.length > MAX_PROMPT)
      throw new AssistantError('invalid', 'Write a message for the assistant.')
    const server = serverInfo(serverId)
    if (!server) throw new AssistantError('unknown', 'This server isn’t in your list.')
    if (running.has(serverId))
      throw new AssistantError('busy', 'The assistant is still working on your last message.')
    const marker = {}
    running.set(serverId, marker)
    try {
      onEvent({ type: 'status', text: 'Getting the assistant ready…' })
      const binary = await installer.ensure()
      const home = path.join(dataDir, 'assistant')
      const work = path.join(home, 'servers', serverId)
      const xdg = (name) => path.join(home, name)
      await Promise.all([work, xdg('data'), xdg('config'), xdg('cache'), xdg('state')].map((d) => fsp.mkdir(d, { recursive: true })))
      const configFile = path.join(home, 'opencode.json')
      await fsp.writeFile(
        configFile,
        JSON.stringify(
          opencodeConfig({
            mcpUrl: `http://127.0.0.1:${toolPort()}/mcp`,
            // This conversation's tools act on its own server only.
            headers: { authorization: `Bearer ${toolToken}`, 'x-sandboxd-server': serverId },
            instructions: instructionsFile,
          }),
          null,
          2,
        ),
        { mode: 0o600 },
      )
      const started = await fsp
        .stat(path.join(work, '.started'))
        .then(() => true)
        .catch(() => false)
      const brief = started
        ? prompt
        : `You are helping with the server "${server.name}" (${server.host}, login ${server.username}). ` +
          `sandboxd's front door there: ${server.front}${server.proxy !== 'none' ? `, existing web server: ${server.proxy}` : ''}` +
          `${server.domain ? `, app addresses under ${server.domain}` : ''}.\n\n---\n\n${prompt}`
      const args = ['run', '--format', 'json', '--print-logs', '--log-level', 'WARN', '--model', FREE_MODEL]
      if (started) args.push('--continue')
      args.push('--', brief)
      // Only what OpenCode needs from the environment; no inherited secrets.
      const keep = ['PATH', 'SystemRoot', 'SYSTEMROOT', 'windir', 'TEMP', 'TMP', 'USERPROFILE', 'APPDATA', 'LOCALAPPDATA', 'HOMEDRIVE', 'HOMEPATH', 'LANG', 'HTTPS_PROXY', 'HTTP_PROXY', 'NO_PROXY', 'https_proxy', 'http_proxy', 'no_proxy']
      const childEnv = Object.fromEntries(keep.filter((k) => env[k] !== undefined).map((k) => [k, env[k]]))
      Object.assign(childEnv, {
        HOME: home,
        XDG_DATA_HOME: xdg('data'),
        XDG_CONFIG_HOME: xdg('config'),
        XDG_CACHE_HOME: xdg('cache'),
        XDG_STATE_HOME: xdg('state'),
        OPENCODE_CONFIG: configFile,
        OPENCODE_DISABLE_AUTOUPDATE: '1',
        PWD: work,
      })
      onEvent({ type: 'status', text: 'Thinking…' })
      const child = spawnProcess(binary, args, { cwd: work, env: childEnv, stdio: ['ignore', 'pipe', 'pipe'], windowsHide: true })
      marker.child = child
      let stderr = ''
      let partial = ''
      let said = false
      child.stdout.setEncoding('utf8')
      child.stdout.on('data', (chunk) => {
        const lines = (partial + chunk).split('\n')
        partial = lines.pop()
        for (const line of lines) {
          const event = chatEvent(line)
          if (!event) continue
          if (event.type === 'text') said = true
          onEvent(event)
        }
      })
      child.stderr.setEncoding('utf8')
      child.stderr.on('data', (chunk) => {
        stderr = (stderr + chunk).slice(-20000)
      })
      const timer = setTimeout(() => child.kill(), timeout)
      const code = await new Promise((resolve) => {
        child.on('error', () => resolve(-1))
        child.on('close', (c) => resolve(c))
      })
      clearTimeout(timer)
      if (partial) {
        const event = chatEvent(partial)
        if (event?.type === 'text') said = true
        if (event) onEvent(event)
      }
      if (marker.cancelled) return { ok: false, message: 'Stopped.' }
      if (code !== 0 || !said) {
        const message = failureMessage(stderr)
        onEvent({ type: 'error', text: message })
        return { ok: false, message }
      }
      await fsp.writeFile(path.join(work, '.started'), new Date().toISOString())
      return { ok: true }
    } finally {
      running.delete(serverId)
    }
  }

  return {
    send,
    busy: (serverId) => running.has(serverId),
    stop(serverId) {
      const marker = running.get(serverId)
      if (marker?.child) {
        marker.cancelled = true
        marker.child.kill()
      }
    },
    // A new conversation for this server (the old session stays on disk).
    async reset(serverId) {
      await fsp.rm(path.join(dataDir, 'assistant', 'servers', serverId, '.started'), { force: true })
    },
    close() {
      for (const marker of running.values()) marker.child?.kill()
    },
  }
}

// The sandboxd tools as MCP over loopback HTTP (the Streamable HTTP transport,
// answered with plain JSON). One bearer token per app run; the server header
// pins each conversation's calls to its own server.
function createToolEndpoint({ tools, token = randomBytes(24).toString('hex') }) {
  const server = http.createServer((req, res) => {
    const reply = (status, body) => {
      res.writeHead(status, body === undefined ? {} : { 'content-type': 'application/json' })
      res.end(body === undefined ? undefined : JSON.stringify(body))
    }
    const serverId = req.headers['x-sandboxd-server']
    if (
      req.url !== '/mcp' ||
      req.headers.authorization !== `Bearer ${token}` ||
      typeof serverId !== 'string' ||
      !SERVER_ID.test(serverId) ||
      // Loopback only, and never a browser page (DNS rebinding, CSRF).
      req.headers.origin !== undefined
    )
      return reply(403, { error: 'forbidden' })
    if (req.method !== 'POST') return reply(405, { error: 'POST only' })
    let body = ''
    req.setEncoding('utf8')
    req.on('data', (chunk) => {
      body += chunk
      if (body.length > 200000) req.destroy()
    })
    req.on('end', async () => {
      let message
      try {
        message = JSON.parse(body)
      } catch {
        return reply(400, { jsonrpc: '2.0', id: null, error: { code: -32700, message: 'Parse error' } })
      }
      const call = (name, args) => tools.call(name, { ...args, server: serverId })
      if (Array.isArray(message)) {
        const answers = (await Promise.all(message.map((m) => handleMcp(m, call)))).filter(Boolean)
        return answers.length ? reply(200, answers) : reply(202)
      }
      const answer = await handleMcp(message, call)
      return answer ? reply(200, answer) : reply(202)
    })
  })
  return {
    token,
    async start() {
      await new Promise((resolve, reject) => {
        server.once('error', reject)
        server.listen(0, '127.0.0.1', resolve)
      })
    },
    port: () => server.address()?.port,
    close: () =>
      new Promise((resolve) => {
        server.closeAllConnections?.()
        server.close(() => resolve())
      }),
  }
}

module.exports = {
  createAssistant,
  createInstaller,
  createToolEndpoint,
  opencodeConfig,
  opencodePackage,
  chatEvent,
  failureMessage,
  extractOne,
  OPENCODE_VERSION,
  FREE_MODEL,
}
