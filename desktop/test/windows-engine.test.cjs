const test = require('node:test')
const assert = require('node:assert/strict')
const fs = require('node:fs/promises')
const os = require('node:os')
const path = require('node:path')
const {
  createWindowsEngine,
  decode,
  download,
  ROOTFS,
  ENGINE_TAG,
  ENGINE_COMMIT,
} = require('../src/windows-engine.cjs')

async function fixture(t, overrides = {}) {
  const directory = await fs.mkdtemp(path.join(os.tmpdir(), 'sandboxd-setup-test-'))
  t.after(() => fs.rm(directory, { recursive: true, force: true }))
  const commands = [],
    events = [],
    downloads = [],
    keepalives = [],
    shortcuts = []
  let probes = 0
  const manager = createWindowsEngine({
    dataDir: directory,
    platform: 'win32',
    arch: 'x64',
    probe: async () => ++probes > 1,
    execute: async (command, args, options) => {
      commands.push({ command, args, options })
      if (args.includes('bash')) options.onLine('SANDBOXD_SETUP:engine')
      return { code: 0, output: '' }
    },
    fetchRootfs: async (...args) => downloads.push(args),
    isPortOpen: async () => false,
    keepAlive: (...args) => keepalives.push(args),
    removeShortcut: async (distro) => shortcuts.push(distro),
    onChange: (value) => events.push(value),
    sleep: async () => {},
    ...overrides,
  })
  return { manager, directory, commands, events, downloads, keepalives, shortcuts }
}

test('new setup pins downloads, imports an isolated distro, streams stages and verifies Windows connectivity', async (t) => {
  const f = await fixture(t)
  const first = f.manager.start()
  assert.equal(f.manager.start(), first, 'double click must share one setup')
  const result = await first
  assert.equal(result.phase, 'ready')
  assert.equal(result.busy, false)
  assert.equal(result.installed, true)
  assert.equal(f.downloads.length, 1)
  assert.equal(f.downloads[0][2], ROOTFS.x64.sha256)
  assert.match(f.downloads[0][0], /^https:\/\/cdimage\.ubuntu\.com\/.*24\.04\.5-base-amd64\.tar\.gz$/)
  const imported = f.commands.find((c) => c.args.includes('--import'))
  assert.match(imported.args[1], /^sandboxd-desktop-[a-f0-9]{16}$/)
  assert.deepEqual(imported.args.slice(-2), ['--version', '2'])
  const bootstrap = f.commands.find((c) => c.args.includes('bash'))
  assert.deepEqual(bootstrap.args.slice(-2), [ENGINE_TAG, ENGINE_COMMIT])
  assert.match(ENGINE_TAG, /^v\d+\.\d+\.\d+$/)
  assert.equal(bootstrap.args[bootstrap.args.indexOf('--cd') + 1], '/')
  // Signed release images only: never fall back to a long source build.
  assert.match(bootstrap.options.input, /bash install.sh --pull --no-desktop --no-console/)
  assert.doesNotMatch(bootstrap.options.input, /--build/)
  assert.match(bootstrap.options.input, /sha256sum -c/)
  // The engine must come back after a Windows restart: WSL boot commands run without PATH.
  assert.match(bootstrap.options.input, /START'\n#!\/bin\/sh\n(#.*\n)*export PATH=\/usr\/local\/sbin:/)
  // An interrupted download is started over, and a damaged base image fails setup.
  assert.match(bootstrap.options.input, /images-started \]; then\n(.*\n){1,2}\s+docker system prune -af/)
  assert.match(bootstrap.options.input, /test -s \/usr\/bin\/node/)
  // Unpacking images must not fill the VM's file cache, which Windows cannot reclaim.
  assert.match(bootstrap.options.input, /echo 1 > \/proc\/sys\/vm\/drop_caches; done \) 9>&- >\/dev\/null 2>&1 &/)
  assert(f.events.some((e) => e.message === 'Downloading your engine and coding tools…'))
  assert.deepEqual(
    f.shortcuts,
    [imported.args[1], imported.args[1]],
    'the engine is not listed as an app in Start, after import or first boot',
  )
  assert.equal(f.keepalives.length, 1)
  assert(f.keepalives[0][1].includes('flock'))
  assert(f.commands.every((c) => !c.args.includes('--unregister') && !c.args.includes('--shutdown')))
  const persisted = JSON.parse(await fs.readFile(path.join(f.directory, 'local-engine/engine.json')))
  assert.equal(persisted.installed, true)
  assert.equal(persisted.distro, imported.args[1])
})

test('an existing local API connects without installing or changing Windows', async (t) => {
  const f = await fixture(t, { probe: async () => true })
  assert.equal((await f.manager.start()).phase, 'ready')
  assert.equal(f.commands.length, 0)
  assert.equal(f.downloads.length, 0)
})

test('a conflicting local port fails without touching WSL', async (t) => {
  const f = await fixture(t, { probe: async () => false, isPortOpen: async () => true })
  const result = await f.manager.start()
  assert.equal(result.phase, 'error')
  assert.match(result.message, /port 9090/)
  assert.equal(f.commands.length, 0)
})

test('missing WSL requests only the fixed Microsoft installer and exposes restart state', async (t) => {
  const calls = []
  const f = await fixture(t, {
    execute: async (command, args) => {
      calls.push({ command, args })
      return { code: args.includes('-EncodedCommand') ? 3010 : 1, output: '' }
    },
  })
  const result = await f.manager.start()
  assert.equal(result.phase, 'restart')
  assert.equal(result.busy, false)
  assert.equal(f.downloads.length, 0)
  const elevated = calls.find((c) => c.args.includes('-EncodedCommand'))
  const script = Buffer.from(elevated.args.at(-1), 'base64').toString('utf16le')
  assert.match(script, /\$env:SystemRoot\\System32\\wsl\.exe/)
  assert.match(script, /--no-distribution/)
  assert.match(script, /-Verb RunAs/)
  assert.doesNotMatch(script, /Restart-Computer|shutdown|ExecutionPolicy|sandboxd/)
  const reopened = createWindowsEngine({ dataDir: f.directory, platform: 'win32', probe: async () => false })
  assert.equal((await reopened.load()).phase, 'restart', 'restart guidance must survive closing the app')
})

test('declining Windows elevation produces a retryable failure', async (t) => {
  const f = await fixture(t, { execute: async () => ({ code: 1, output: 'cancelled' }) })
  const result = await f.manager.start()
  assert.equal(result.phase, 'error')
  assert.match(result.message, /permission was declined/)
  assert.equal(f.downloads.length, 0)
})

test('a failed bootstrap retries the same distro and ignores unrecognized progress', async (t) => {
  let distro,
    runs = 0,
    probes = 0
  const f = await fixture(t, {
    probe: async () => ++probes > 2,
    execute: async (_command, args, options) => {
      if (args.includes('--list')) return { code: 0, output: distro || '' }
      if (args.includes('--import')) distro = args[1]
      if (args.includes('bash')) {
        options.onLine('SANDBOXD_SETUP:arbitrary injected message')
        return { code: ++runs === 1 ? 1 : 0, output: 'sk_secret-should-not-reach-renderer' }
      }
      return { code: 0, output: '' }
    },
  })
  const failed = await f.manager.start()
  assert.equal(failed.phase, 'error')
  // Plain language: what to do next, no Linux paths.
  assert.match(failed.message, /close some apps, then click Retry setup/)
  assert.doesNotMatch(failed.message, /\/var\/log|WSL/)
  assert.equal((await f.manager.start()).phase, 'ready')
  assert.equal(f.downloads.length, 1)
  assert.doesNotMatch(JSON.stringify(f.events), /sk_secret|arbitrary injected/)
})

test('an installed environment that disappears is not silently replaced', async (t) => {
  const f = await fixture(t, { probe: async () => false })
  await fs.mkdir(path.join(f.directory, 'local-engine'))
  await fs.writeFile(
    path.join(f.directory, 'local-engine/engine.json'),
    JSON.stringify({ distro: 'sandboxd-desktop-0123456789abcdef', installed: true }),
  )
  assert.match((await f.manager.start()).message, /environment is missing/)
  assert.equal(f.downloads.length, 0)
})

test('Windows UTF-16 distro output decodes without losing names', () => {
  assert.equal(
    decode(Buffer.from('sandboxd-desktop-0123456789abcdef\r\n', 'utf16le')),
    'sandboxd-desktop-0123456789abcdef\r\n',
  )
})

test('checksum mismatch prevents import', async (t) => {
  const f = await fixture(t, {
    fetchRootfs: async () => {
      throw new Error('Ubuntu checksum verification failed.')
    },
  })
  assert.equal((await f.manager.start()).phase, 'error')
  assert(!f.commands.some((c) => c.args.includes('--import')))
})

test('downloader hashes the actual response bytes and rejects corrupt archives', async (t) => {
  const original = global.fetch
  t.after(() => {
    global.fetch = original
  })
  global.fetch = async () => new Response('corrupt archive')
  const directory = await fs.mkdtemp(path.join(os.tmpdir(), 'sandboxd-download-test-'))
  t.after(() => fs.rm(directory, { recursive: true, force: true }))
  await assert.rejects(
    download('https://example.invalid/archive', path.join(directory, 'archive'), ROOTFS.x64.sha256, () => {}),
    /checksum verification failed/,
  )
})

test('an installed engine can restart while WSL retains its localhost listener', async (t) => {
  const distro = 'sandboxd-desktop-0123456789abcdef'
  let bootstrapped = false
  const f = await fixture(t, {
    probe: async () => bootstrapped,
    isPortOpen: async () => true,
    execute: async (_command, args) => {
      if (args.includes('--list')) return { code: 0, output: distro }
      if (args.includes('bash')) bootstrapped = true
      return { code: 0, output: '' }
    },
  })
  await fs.mkdir(path.join(f.directory, 'local-engine'))
  await fs.writeFile(
    path.join(f.directory, 'local-engine/engine.json'),
    JSON.stringify({ distro, installed: true }),
  )
  assert.equal((await f.manager.start()).phase, 'ready')
  assert.equal(bootstrapped, true)
  assert.equal(f.downloads.length, 0)
})
