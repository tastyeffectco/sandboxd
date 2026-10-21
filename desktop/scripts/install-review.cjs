// CI only: exercise the generated installer on a disposable hosted runner.
// Never bypass signature checks in the shipping download installers.
const fs = require('node:fs/promises')
const path = require('node:path')
const { spawnSync } = require('node:child_process')
const { version } = require('../package.json')

async function main() {
  if (!process.env.GITHUB_ACTIONS || !process.env.RUNNER_TEMP || !process.env.GITHUB_ENV)
    throw new Error('Installer smoke runs only on disposable GitHub Actions runners.')
  const target = await fs.mkdtemp(path.join(process.env.RUNNER_TEMP, 'sandboxd-installed-'))
  const release = path.resolve(__dirname, '../release')
  let executable, result
  if (process.platform === 'darwin') {
    result = spawnSync(
      'ditto',
      ['-x', '-k', path.join(release, `sandboxd-desktop-${version}-mac-${process.arch}.zip`), target],
      { stdio: 'inherit', timeout: 120000 },
    )
    executable = path.join(target, 'sandboxd.app/Contents/MacOS/sandboxd')
  } else if (process.platform === 'win32') {
    result = spawnSync(
      path.join(release, `sandboxd-desktop-${version}-win-${process.arch}.exe`),
      ['/S', `/D=${target}`],
      { stdio: 'inherit', timeout: 120000 },
    )
    executable = path.join(target, 'sandboxd.exe')
  } else throw new Error('No published Linux desktop installer.')
  if (result.error || result.status !== 0)
    throw new Error(`Installer failed: ${result.error?.message || result.status}`)
  await fs.access(executable)
  await fs.appendFile(process.env.GITHUB_ENV, `SANDBOXD_PACKAGE_EXECUTABLE=${executable}\n`)
  console.log(`PASS: ${process.platform}/${process.arch} installer produced the native application`)
}
main().catch((error) => {
  console.error(error)
  process.exitCode = 1
})
