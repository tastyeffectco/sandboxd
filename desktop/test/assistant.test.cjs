// The server assistant: download and verification of OpenCode, its config, a
// conversation driven through a fake OpenCode, and the MCP bridge end to end.
const { test } = require('node:test')
const assert = require('node:assert/strict')
const { execFileSync } = require('node:child_process')
const { createHash } = require('node:crypto')
const { EventEmitter, once } = require('node:events')
const fs = require('node:fs/promises')
const http = require('node:http')
const os = require('node:os')
const path = require('node:path')
const { PassThrough } = require('node:stream')
const {
  createAssistant,
  createInstaller,
  createToolEndpoint,
  opencodeConfig,
  opencodePackage,
  chatEvent,
  failureMessage,
} = require('../src/assistant.cjs')

const tmp = async (t) => {
  const dir = await fs.mkdtemp(path.join(os.tmpdir(), 'sandboxd-assistant-'))
  t.after(() => fs.rm(dir, { recursive: true, force: true }))
  return dir
}

// A package shaped like npm's: package/package.json + package/bin/<binary>,
// with a long path so tar writes a pax header.
async function makePackage(dir, binary = 'opencode', content = '#!/bin/sh\necho 1.19.5\n') {
  const root = path.join(dir, 'src')
  await fs.mkdir(path.join(root, 'package', 'bin'), { recursive: true })
  await fs.mkdir(path.join(root, 'package', 'x'.repeat(120)), { recursive: true })
  await fs.writeFile(path.join(root, 'package', 'x'.repeat(120), 'pad.txt'), 'pad')
  await fs.writeFile(path.join(root, 'package', 'package.json'), '{}')
  await fs.writeFile(path.join(root, 'package', 'bin', binary), content)
  const tgz = path.join(dir, 'pkg.tgz')
  execFileSync('tar', ['--format=pax', '-czf', tgz, '-C', root, 'package'])
  const bytes = await fs.readFile(tgz)
  return { tgz, bytes, integrity: 'sha512-' + createHash('sha512').update(bytes).digest('base64') }
}

async function serve(t, bytes) {
  const server = http.createServer((req, res) => {
    server.hits++
    if (req.url === '/redirect') {
      res.writeHead(302, { location: '/pkg.tgz' })
      return res.end()
    }
    res.writeHead(200, { 'content-length': bytes.length })
    res.end(bytes)
  })
  server.hits = 0
  server.listen(0, '127.0.0.1')
  await once(server, 'listening')
  t.after(() => new Promise((r) => server.close(r)))
  return { server, url: (p = '/pkg.tgz') => `http://127.0.0.1:${server.address().port}${p}` }
}

test('every desktop platform has a pinned OpenCode package', () => {
  for (const [platform, arch, name, binary] of [
    ['win32', 'x64', 'opencode-windows-x64', 'opencode.exe'],
    ['win32', 'arm64', 'opencode-windows-arm64', 'opencode.exe'],
    ['darwin', 'arm64', 'opencode-darwin-arm64', 'opencode'],
    ['darwin', 'x64', 'opencode-darwin-x64', 'opencode'],
  ]) {
    const pkg = opencodePackage(platform, arch)
    assert.equal(pkg.name, name)
    assert.equal(pkg.binary, binary)
    assert.match(pkg.integrity, /^sha512-[A-Za-z0-9+/]{86}==$/)
    assert.equal(pkg.url, `https://registry.npmjs.org/${name}/-/${name}-1.19.5.tgz`)
  }
  assert.equal(opencodePackage('freebsd', 'x64'), null)
})

test('installs OpenCode once, only when its hash matches', async (t) => {
  const dir = await tmp(t)
  const { bytes, integrity } = await makePackage(dir)
  const { server, url } = await serve(t, bytes)
  const pkg = { name: 'opencode-test', integrity, url: url('/redirect'), binary: 'opencode' }
  const progress = []
  const installer = createInstaller({ dataDir: path.join(dir, 'data'), pkg, onProgress: (p) => progress.push(p) })
  assert.equal(await installer.installed(), false)
  const [a, b] = await Promise.all([installer.ensure(), installer.ensure()])
  assert.equal(a, b)
  assert.equal(server.hits, 2, 'one download (plus its redirect)')
  assert.equal(await fs.readFile(a, 'utf8'), '#!/bin/sh\necho 1.19.5\n')
  assert.equal(execFileSync(a, { encoding: 'utf8' }), '1.19.5\n')
  assert(progress.at(-1).received === bytes.length)
  assert.equal(await installer.installed(), true)
  await installer.ensure()
  assert.equal(server.hits, 2, 'installed: no second download')
  const left = await fs.readdir(path.dirname(a))
  assert.deepEqual(left.sort(), ['installed', 'opencode'])

  // A different file at the same address is refused and leaves nothing behind.
  const other = createInstaller({
    dataDir: path.join(dir, 'other'),
    pkg: { ...pkg, integrity: 'sha512-' + 'A'.repeat(86) + '==' },
  })
  await assert.rejects(other.ensure(), { code: 'integrity' })
  assert.deepEqual(await fs.readdir(path.join(dir, 'other', 'opencode', '1.19.5')), [])
  assert.equal(await other.installed(), false)
})

test('the config allows only the sandboxd tools', () => {
  const config = opencodeConfig({ mcpUrl: 'http://127.0.0.1:1/mcp', headers: { a: 'b' }, instructions: '/x.md' })
  assert.deepEqual(config.tools, { '*': false, 'sandboxd_*': true })
  assert.equal(config.model, 'opencode/big-pickle')
  assert.equal(config.share, 'disabled')
  assert.equal(config.autoupdate, false)
  assert.deepEqual(config.instructions, ['/x.md'])
  assert.deepEqual(
    [config.mcp.sandboxd.type, config.mcp.sandboxd.url, config.mcp.sandboxd.oauth],
    ['remote', 'http://127.0.0.1:1/mcp', false],
  )
  assert(config.mcp.sandboxd.timeout > 15 * 60 * 1000, 'outlasts an approval')
})

test('chat events and plain failure messages', () => {
  assert.deepEqual(chatEvent('{"type":"text","part":{"text":"Hello"}}'), { type: 'text', text: 'Hello' })
  assert.deepEqual(chatEvent('{"type":"tool_use","part":{"tool":"sandboxd_look","state":{"status":"completed"}}}'), {
    type: 'tool',
    tool: 'look',
    status: 'completed',
  })
  assert.equal(chatEvent('not json'), null)
  assert.equal(chatEvent('{"type":"step_start"}'), null)
  assert.match(failureMessage('AI_APICallError: Free usage limit reached'), /free assistant is busy/)
  assert.match(failureMessage('getaddrinfo ENOTFOUND opencode.ai'), /internet/)
  assert.match(failureMessage(''), /stopped before it finished/)
})

// A stand-in for the OpenCode binary: records how it was started, prints events.
function fakeSpawn(script) {
  const calls = []
  const spawnProcess = (binary, args, options) => {
    calls.push({ binary, args, options })
    const child = new EventEmitter()
    child.stdout = new PassThrough()
    child.stderr = new PassThrough()
    // Like a real child: 'close' comes after its output has been read.
    const emit = child.emit.bind(child)
    child.emit = (name, ...rest) =>
      name === 'close' && !child.killed ? setTimeout(() => emit(name, ...rest), 10) : emit(name, ...rest)
    child.kill = () => {
      child.killed = true
      child.emit('close', null)
    }
    setImmediate(() => script(child, calls.length))
    return child
  }
  return { calls, spawnProcess }
}

const SERVER = { name: 'My VPS', host: '203.0.113.10', username: 'ubuntu', front: 'existing', proxy: 'nginx', domain: '203-0-113-10.sslip.io' }

async function assistant(t, script, extra = {}) {
  const dir = await tmp(t)
  const fake = fakeSpawn(script)
  const installs = []
  const a = createAssistant({
    dataDir: dir,
    installer: { ensure: async () => (installs.push(1), '/bin/opencode') },
    instructionsFile: '/ops.md',
    toolPort: () => 4321,
    toolToken: 'tok',
    serverInfo: (id) => (id === 'srv_0123456789ab' ? SERVER : null),
    spawnProcess: fake.spawnProcess,
    env: { PATH: '/usr/bin', SECRET_TOKEN: 'leak', OPENAI_API_KEY: 'leak' },
    ...extra,
  })
  return { dir, a, ...fake, installs }
}

test('a conversation: briefed once, continued after, isolated from the user’s own OpenCode', async (t) => {
  const { dir, a, calls } = await assistant(t, (child) => {
    child.stdout.write('{"type":"tool_use","part":{"tool":"sandboxd_look","state":{"status":"completed"}}}\n')
    child.stdout.end('{"type":"text","part":{"text":"Your server runs nginx."}}')
    child.emit('close', 0)
  })
  const events = []
  const first = await a.send('srv_0123456789ab', 'Put my shop online', (e) => events.push(e))
  assert.equal(first.ok, true)
  assert.deepEqual(
    events.filter((e) => e.type !== 'status'),
    [
      { type: 'tool', tool: 'look', status: 'completed' },
      { type: 'text', text: 'Your server runs nginx.' },
    ],
  )
  const [{ binary, args, options }] = calls
  assert.equal(binary, '/bin/opencode')
  assert.deepEqual(args.slice(0, 8), ['run', '--format', 'json', '--print-logs', '--log-level', 'WARN', '--model', 'opencode/big-pickle'])
  assert(!args.includes('--continue'))
  assert.match(args.at(-1), /^You are helping with the server "My VPS" \(203\.0\.113\.10, login ubuntu\)/)
  assert.match(args.at(-1), /existing web server: nginx/)
  assert.match(args.at(-1), /Put my shop online$/)
  assert.equal(options.cwd, path.join(dir, 'assistant', 'servers', 'srv_0123456789ab'))
  assert.equal(options.env.XDG_CONFIG_HOME, path.join(dir, 'assistant', 'config'))
  assert.equal(options.env.HOME, path.join(dir, 'assistant'))
  assert.equal(options.env.SECRET_TOKEN, undefined)
  assert.equal(options.env.OPENAI_API_KEY, undefined)
  const config = JSON.parse(await fs.readFile(options.env.OPENCODE_CONFIG, 'utf8'))
  assert.equal(config.mcp.sandboxd.url, 'http://127.0.0.1:4321/mcp')
  assert.deepEqual(config.mcp.sandboxd.headers, { authorization: 'Bearer tok', 'x-sandboxd-server': 'srv_0123456789ab' })

  await a.send('srv_0123456789ab', 'And the blog?')
  assert(calls[1].args.includes('--continue'))
  assert.equal(calls[1].args.at(-1), 'And the blog?')
  await a.reset('srv_0123456789ab')
  await a.send('srv_0123456789ab', 'Start over')
  assert(!calls[2].args.includes('--continue'))
})

test('failures are said plainly, one message at a time', async (t) => {
  const { a } = await assistant(t, (child) => {
    child.stderr.end('ERROR AI_APICallError: Free usage limit reached\n')
    child.stdout.end()
    child.emit('close', 1)
  })
  const events = []
  const result = await a.send('srv_0123456789ab', 'Hello', (e) => events.push(e))
  assert.equal(result.ok, false)
  assert.match(result.message, /free assistant is busy/)
  assert.equal(events.at(-1).type, 'error')
  await assert.rejects(a.send('srv_ffffffffffff', 'Hello'), /isn’t in your list/)
  await assert.rejects(a.send('srv_0123456789ab', '   '), /Write a message/)

  const slow = await assistant(t, () => {})
  const pending = slow.a.send('srv_0123456789ab', 'Hello')
  await new Promise((r) => setTimeout(r, 20))
  await assert.rejects(slow.a.send('srv_0123456789ab', 'Again'), /still working/)
  slow.a.stop('srv_0123456789ab')
  assert.deepEqual(await pending, { ok: false, message: 'Stopped.' })
})

test('the tools endpoint speaks MCP, pinned to the conversation’s server, with the token', async (t) => {
  const calls = []
  const endpoint = createToolEndpoint({
    tools: {
      call: async (tool, args) => {
        calls.push({ tool, args })
        if (tool === 'run') throw new Error('Give the command to run.')
        return { ok: true, facts: 'os=Ubuntu' }
      },
    },
  })
  await endpoint.start()
  t.after(() => endpoint.close())
  const post = (body, headers = {}) =>
    new Promise((resolve, reject) => {
      const req = http.request(
        `http://127.0.0.1:${endpoint.port()}/mcp`,
        {
          method: 'POST',
          headers: {
            authorization: `Bearer ${endpoint.token}`,
            'x-sandboxd-server': 'srv_0123456789ab',
            'content-type': 'application/json',
            ...headers,
          },
        },
        (res) => {
          let text = ''
          res.on('data', (d) => (text += d))
          res.on('end', () => resolve({ status: res.statusCode, body: text ? JSON.parse(text) : null }))
        },
      )
      req.on('error', reject)
      req.end(typeof body === 'string' ? body : JSON.stringify(body))
    })
  let next = 1
  const rpc = async (method, params) => (await post({ jsonrpc: '2.0', id: next++, method, params })).body

  const init = await rpc('initialize', { protocolVersion: '2025-11-25', capabilities: {} })
  assert.equal(init.result.protocolVersion, '2025-11-25')
  assert.equal((await post({ jsonrpc: '2.0', method: 'notifications/initialized' })).status, 202)
  const list = await rpc('tools/list', {})
  assert.deepEqual(list.result.tools.map((tool) => tool.name), ['look', 'read', 'run', 'write', 'undo'])
  assert(list.result.tools.find((tool) => tool.name === 'run').inputSchema.required.includes('why'))

  const look = await rpc('tools/call', { name: 'look', arguments: { server: 'srv_ffffffffffff' } })
  assert.equal(look.result.isError, false)
  assert.deepEqual(JSON.parse(look.result.content[0].text), { ok: true, facts: 'os=Ubuntu' })
  // The conversation's own server wins over anything in the arguments.
  assert.equal(calls[0].args.server, 'srv_0123456789ab')
  const failed = await rpc('tools/call', { name: 'run', arguments: {} })
  assert.equal(failed.result.isError, true)
  assert.match(failed.result.content[0].text, /Give the command/)
  assert.equal((await rpc('tools/call', { name: 'shell', arguments: {} })).result.isError, true)
  assert.equal((await rpc('resources/list', {})).error.code, -32601)
  assert.equal((await post('not json')).status, 400)

  // No token, a wrong server id, or a browser page: refused.
  assert.equal((await post({ jsonrpc: '2.0', id: 9, method: 'tools/list' }, { authorization: 'Bearer nope' })).status, 403)
  assert.equal((await post({ jsonrpc: '2.0', id: 9, method: 'tools/list' }, { 'x-sandboxd-server': '../x' })).status, 403)
  assert.equal(
    (await post({ jsonrpc: '2.0', id: 9, method: 'tools/list' }, { origin: 'https://evil.example' })).status,
    403,
  )
  assert.equal(calls.length, 2)
})
