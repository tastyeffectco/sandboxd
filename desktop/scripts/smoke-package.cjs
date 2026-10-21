// Launch the actual fused package; no Node inspector or main-process test hooks.
const { chromium, expect } = require('../../console/node_modules/@playwright/test')
const { spawn } = require('node:child_process')
const fs = require('node:fs/promises')
const path = require('node:path')
const os = require('node:os')
const { once } = require('node:events')
const assert = require('node:assert/strict')
const { getCurrentFuseWire, FuseV1Options } = require('@electron/fuses')
const { listPackage } = require('@electron/asar')

async function main() {
  const output = path.resolve(__dirname, '../release')
  const candidates =
    process.platform === 'darwin'
      ? [`mac${process.arch === 'arm64' ? '-arm64' : ''}/sandboxd.app/Contents/MacOS/sandboxd`]
      : process.platform === 'win32'
        ? [`win${process.arch === 'arm64' ? '-arm64' : ''}-unpacked/sandboxd.exe`]
        : [`linux${process.arch === 'arm64' ? '-arm64' : ''}-unpacked/sandboxd-desktop`]
  const executable = process.env.SANDBOXD_PACKAGE_EXECUTABLE || path.join(output, candidates[0])
  await fs.access(executable)
  const fuses = await getCurrentFuseWire(executable)
  for (const name of [
    'RunAsNode',
    'EnableNodeOptionsEnvironmentVariable',
    'EnableNodeCliInspectArguments',
    'GrantFileProtocolExtraPrivileges',
  ])
    assert.equal(fuses[FuseV1Options[name]], 48, `${name} must be off`)
  for (const name of ['OnlyLoadAppFromAsar', 'EnableEmbeddedAsarIntegrityValidation'])
    assert.equal(fuses[FuseV1Options[name]], 49, `${name} must be on`)
  const archive =
    process.platform === 'darwin'
      ? path.resolve(executable, '../../Resources/app.asar')
      : path.join(path.dirname(executable), 'resources/app.asar')
  const files = listPackage(archive)
  assert(
    files.some((file) => /[\\/]ui[\\/]index.html$/.test(file)),
    'Packaged UI missing',
  )
  assert(!files.some((file) => /[\\/]node_modules[\\/]/.test(file)), 'Build dependencies must not ship')
  const temp = await fs.mkdtemp(path.join(os.tmpdir(), 'sandboxd-package-'))
  const args = [`--user-data-dir=${temp}`, '--remote-debugging-port=0']
  if (process.env.SANDBOXD_TEST_NO_OS_SANDBOX === '1') args.push('--no-sandbox')
  const child = spawn(executable, args, { stdio: ['ignore', 'pipe', 'pipe'] })
  let browser
  try {
    const endpoint = await new Promise((resolve, reject) => {
      let log = ''
      const timer = setTimeout(() => reject(new Error('Packaged app did not start: ' + log)), 30000)
      child.once('error', (error) => {
        clearTimeout(timer)
        reject(error)
      })
      child.once('exit', (code) => {
        clearTimeout(timer)
        reject(new Error(`Packaged app exited ${code}: ${log}`))
      })
      child.stderr.on('data', (data) => {
        log += data
        const match = /DevTools listening on (ws:\/\/[^\s]+)/.exec(log)
        if (match) {
          clearTimeout(timer)
          resolve(match[1])
        }
      })
    })
    browser = await chromium.connectOverCDP(endpoint)
    const context = browser.contexts()[0]
    const page = context.pages()[0] || (await context.waitForEvent('page'))
    await expect(page.getByRole('heading', { name: 'Connect your engine.' })).toBeVisible({ timeout: 15000 })
    if ((await page.evaluate(() => typeof require)) !== 'undefined')
      throw new Error('Node exposed in packaged renderer')
    await fs.mkdir(path.resolve(__dirname, '../test-results'), { recursive: true })
    await page.screenshot({ path: path.resolve(__dirname, '../test-results/packaged.png') })
    console.log(
      `PASS: packaged ${process.platform}/${process.arch} client launches with bundled UI and no renderer Node access`,
    )
  } finally {
    if (child.exitCode === null && child.signalCode === null) {
      const exited = once(child, 'exit')
      child.kill()
      const force = setTimeout(() => child.kill('SIGKILL'), 3000)
      await exited
      clearTimeout(force)
    }
    // Closing the last Mac window keeps an application alive. Stop the owned
    // process before closing CDP, which otherwise waits indefinitely for exit.
    if (browser) await browser.close()
    await fs.rm(temp, { recursive: true, force: true, maxRetries: 5, retryDelay: 200 })
  }
}
main().catch((error) => {
  console.error(error)
  process.exitCode = 1
})
