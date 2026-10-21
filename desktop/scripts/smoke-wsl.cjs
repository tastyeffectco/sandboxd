// Real WSL integration on disposable Windows runners. Never run on a user's PC.
const fs = require('node:fs/promises')
const path = require('node:path')
const assert = require('node:assert/strict')
const { spawnSync } = require('node:child_process')
const { createWindowsEngine, ENGINE_URL } = require('../src/windows-engine.cjs')

async function main() {
  assert(
    process.platform === 'win32' && process.env.GITHUB_ACTIONS && process.env.RUNNER_TEMP,
    'This destructive fixture cleanup is restricted to disposable Windows CI runners.',
  )
  const directory = await fs.mkdtemp(path.join(process.env.RUNNER_TEMP, 'sandboxd-wsl-smoke-'))
  const probe = async () => {
    try {
      const r = await fetch(ENGINE_URL + '/v1/auth/status', { signal: AbortSignal.timeout(3000) })
      const body = await r.json()
      return r.ok && typeof body.enabled === 'boolean' && typeof body.authenticated === 'boolean'
    } catch {
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
    assert.equal(installed.phase, 'ready', installed.message)
    assert.equal(installed.installed, true)
    manifest = JSON.parse(await fs.readFile(path.join(directory, 'local-engine/engine.json')))
    assert.equal((await fetch(ENGINE_URL + '/readyz')).status, 200)
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
    assert.equal(
      JSON.parse(await fs.readFile(path.join(directory, 'local-engine/engine.json'))).distro,
      manifest.distro,
    )
    console.log(
      'PASS: actual WSL import, Docker installation, engine build, Windows localhost connectivity, and restart recovery',
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
          'test -f /var/lib/sandboxd-desktop/packages-ready && echo packages-ready; test -f /var/lib/sandboxd-desktop/ready && echo engine-ready; docker ps --format "{{.Names}}: {{.Status}}"',
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
