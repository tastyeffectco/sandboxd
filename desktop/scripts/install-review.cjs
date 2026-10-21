// CI only: exercise the generated installer on a disposable hosted runner.
// Never bypass signature checks in the shipping download installers.
const fs = require('node:fs/promises')
const path = require('node:path')
const { spawn, spawnSync } = require('node:child_process')
const { version } = require('../package.json')

async function install(command, args, target) {
  const child = spawn(command, args, { stdio: 'inherit' })
  const progress = setInterval(async () => {
    const files = await fs.readdir(target).catch(() => [])
    console.log(`Installer still running; destination entries: ${files.join(', ') || '(empty)'}`)
    if (process.platform === 'win32')
      spawnSync(
        'powershell.exe',
        [
          '-NoProfile',
          '-Command',
          "Get-Process | Where-Object { $_.ProcessName -like '*sandboxd*' } | Select-Object Id, ProcessName, CPU, MainWindowTitle | Format-Table -AutoSize",
        ],
        { stdio: 'inherit', timeout: 10000 },
      )
  }, 30000)
  let timedOut = false
  const timeout = setTimeout(() => {
    timedOut = true
    child.kill()
  }, 300000)
  try {
    const result = await new Promise((resolve, reject) => {
      child.once('error', reject)
      child.once('exit', resolve)
    })
    if (timedOut || result !== 0)
      throw new Error(`Installer ${timedOut ? 'timed out after 5 minutes' : `exited with ${result}`}`)
  } finally {
    clearInterval(progress)
    clearTimeout(timeout)
  }
}
async function main() {
  if (!process.env.GITHUB_ACTIONS || !process.env.RUNNER_TEMP || !process.env.GITHUB_ENV)
    throw new Error('Installer smoke runs only on disposable GitHub Actions runners.')
  const target = await fs.mkdtemp(path.join(process.env.RUNNER_TEMP, 'sandboxd-installed-'))
  const release = path.resolve(__dirname, '../release')
  let executable
  if (process.platform === 'darwin') {
    await install(
      'ditto',
      ['-x', '-k', path.join(release, `sandboxd-desktop-${version}-mac-${process.arch}.zip`), target],
      target,
    )
    executable = path.join(target, 'sandboxd.app/Contents/MacOS/sandboxd')
  } else if (process.platform === 'win32') {
    await install(
      path.join(release, `sandboxd-desktop-${version}-win-${process.arch}.exe`),
      ['/S', `/D=${target}`],
      target,
    )
    executable = path.join(target, 'sandboxd.exe')
  } else throw new Error('No published Linux desktop installer.')
  await fs.access(executable)
  await fs.appendFile(process.env.GITHUB_ENV, `SANDBOXD_PACKAGE_EXECUTABLE=${executable}\n`)
  console.log(`PASS: ${process.platform}/${process.arch} installer produced the native application`)
}
main().catch((error) => {
  console.error(error)
  process.exitCode = 1
})
