// Real WSL integration on disposable Windows runners. Never run on a user's PC.
const fs = require('node:fs/promises')
const path = require('node:path')
const assert = require('node:assert/strict')
const { spawnSync } = require('node:child_process')
const { randomBytes } = require('node:crypto')
const { createWindowsEngine, ENGINE_URL } = require('../src/windows-engine.cjs')

async function main() {
  assert(
    process.platform === 'win32' && process.env.GITHUB_ACTIONS && process.env.RUNNER_TEMP,
    'This destructive fixture cleanup is restricted to disposable Windows CI runners.',
  )
  const directory = await fs.mkdtemp(path.join(process.env.RUNNER_TEMP, 'sandboxd-wsl-smoke-'))
  let lastProbe = ''
  const probe = async () => {
    try {
      const r = await fetch(ENGINE_URL + '/v1/auth/status', { signal: AbortSignal.timeout(3000) })
      const body = await r.json()
      lastProbe = `HTTP ${r.status}; enabled=${typeof body.enabled}; authenticated=${typeof body.authenticated}`
      return r.ok && typeof body.enabled === 'boolean' && typeof body.authenticated === 'boolean'
    } catch (error) {
      lastProbe = `${error.message}; ${error.cause?.code || ''}`
      return false
    }
  }
  const manager = createWindowsEngine({
    dataDir: directory,
    probe,
    onChange: (state) => console.log(`${state.phase}: ${state.message}`),
  })
  let manifest
  try {
    assert.equal(await probe(), false, 'Runner must not have an existing engine')
    const installed = await manager.start()
    console.log('Last Windows API probe:', lastProbe)
    assert.equal(installed.phase, 'ready', installed.message)
    assert.equal(installed.installed, true)
    manifest = JSON.parse(await fs.readFile(path.join(directory, 'local-engine/engine.json')))
    assert.equal((await fetch(ENGINE_URL + '/readyz')).status, 200)
    const password = randomBytes(24).toString('base64url')
    const setup = await fetch(ENGINE_URL + '/v1/auth/setup', {
      method: 'POST',
      headers: { 'content-type': 'application/json' },
      body: JSON.stringify({ password }),
      signal: AbortSignal.timeout(10000),
    })
    assert.equal(setup.status, 204, 'First-run password setup must work from Windows')
    // Stopping only the fixture distro simulates reboot; starting again must reuse it.
    assert.equal(spawnSync('wsl.exe', ['--terminate', manifest.distro], { timeout: 60000 }).status, 0)
    for (let n = 0; n < 30 && (await probe()); n++) await new Promise((r) => setTimeout(r, 1000))
    assert.equal(await probe(), false)
    const resumed = createWindowsEngine({
      dataDir: directory,
      probe,
      onChange: (state) => console.log(`resume ${state.phase}: ${state.message}`),
    })
    assert.equal((await resumed.load()).installed, true)
    assert.equal((await resumed.start()).phase, 'ready')
    const login = await fetch(ENGINE_URL + '/v1/auth/login', {
      method: 'POST',
      headers: { 'content-type': 'application/json' },
      body: JSON.stringify({ password }),
      signal: AbortSignal.timeout(10000),
    })
    assert.equal(login.status, 204, 'The engine password must survive restarting WSL')
    const cookie = login.headers
      .getSetCookie()
      .find((value) => value.startsWith('sbx_session='))
      ?.split(';')[0]
    assert(cookie, 'Login must issue an engine session')
    assert.equal(
      (await fetch(ENGINE_URL + '/v1/apps', { headers: { cookie }, signal: AbortSignal.timeout(10000) }))
        .status,
      200,
    )
    assert.equal(
      JSON.parse(await fs.readFile(path.join(directory, 'local-engine/engine.json'))).distro,
      manifest.distro,
    )
    console.log(
      'PASS: actual WSL import, Docker installation, engine build, Windows localhost connectivity, first-run login, and persistent auth after restart',
    )
  } finally {
    manifest ||= await fs
      .readFile(path.join(directory, 'local-engine/engine.json'), 'utf8')
      .then(JSON.parse)
      .catch(() => null)
    if (manifest && /^sandboxd-desktop-[a-f0-9]{16}$/.test(manifest.distro)) {
      // Logs contain bootstrap keys: do not print or upload them to public Actions.
      const summary = spawnSync(
        'wsl.exe',
        [
          '-d',
          manifest.distro,
          '-u',
          'root',
          '--cd',
          '/',
          '--exec',
          'bash',
          '-c',
          'test -f /var/lib/sandboxd-desktop/packages-ready && echo packages-ready; test -f /var/lib/sandboxd-desktop/ready && echo engine-ready; docker ps --format "{{.Names}}: {{.Status}} {{.Ports}}"; curl -fsS --max-time 3 http://127.0.0.1:9090/v1/auth/status; ps aux | grep -E "docker-proxy|flock|sleep infinity"',
        ],
        { timeout: 20000, encoding: 'utf8' },
      )
      console.log(summary.stdout)
      assert.equal(spawnSync('wsl.exe', ['--unregister', manifest.distro], { timeout: 60000 }).status, 0)
    }
    await fs.rm(directory, { recursive: true, force: true, maxRetries: 5, retryDelay: 200 })
  }
}
main().catch((error) => {
  console.error(error)
  process.exitCode = 1
})
