const fs = require('node:fs/promises')
const path = require('node:path')
const { spawn } = require('node:child_process')
const { randomBytes, createHash } = require('node:crypto')
const net = require('node:net')

const ENGINE_URL = 'http://127.0.0.1:9090'
const ENGINE_COMMIT = '1a860a57b4d0ca27bcebbfd712a7e45e6fa4a6c9'
// Canonical's versioned Ubuntu Base images; hashes from its published SHA256SUMS.
const ROOTFS = {
  x64: { arch: 'amd64', sha256: 'e77b6f10c2590cef872b33ee9f635a0e3fd1f57fb074c0e52b5c7f56147a0c86' },
  arm64: { arch: 'arm64', sha256: 'a91d5a93010193712d346d761372b7c9db6dfcf093893161c64ca107f05914f2' },
}
const STAGES = {
  packages: 'Installing engine prerequisites…',
  docker: 'Starting the container runtime…',
  source: 'Downloading sandboxd…',
  engine: 'Building your engine and coding tools. The first setup can take 15–30 minutes…',
  starting: 'Starting your engine…',
  health: 'Waiting for the engine to be ready…',
  ready: 'Checking the connection from Windows…',
}
function decode(buffer) {
  return buffer.includes(0) ? buffer.toString('utf16le').replace(/^\uFEFF/, '') : buffer.toString('utf8')
}
function run(command, args, { input, timeout = 120000, onLine = () => {} } = {}) {
  return new Promise((resolve, reject) => {
    const child = spawn(command, args, { windowsHide: true, shell: false, stdio: ['pipe', 'pipe', 'pipe'] })
    const chunks = []
    let bytes = 0,
      pending = '',
      timedOut = false
    const timer = setTimeout(() => {
      timedOut = true
      child.kill()
    }, timeout)
    for (const stream of [child.stdout, child.stderr])
      stream.on('data', (chunk) => {
        // Bounded output: native command diagnostics only; engine logs stay in Linux.
        chunks.push(chunk)
        bytes += chunk.length
        while (bytes > 65536 && chunks.length > 1) bytes -= chunks.shift().length
        pending += chunk.toString('utf8')
        const lines = pending.split(/\r?\n/)
        pending = lines.pop().slice(-4096)
        for (const line of lines) onLine(line)
      })
    child.stdin.on('error', () => {})
    child.once('error', (error) => {
      clearTimeout(timer)
      reject(error)
    })
    child.once('close', (code) => {
      clearTimeout(timer)
      if (timedOut)
        return reject(new Error('Setup timed out. Retry to continue; existing engine data is preserved.'))
      resolve({ code, output: decode(Buffer.concat(chunks)) })
    })
    child.stdin.end(input)
  })
}
async function download(url, file, digest, progress) {
  const response = await fetch(url, { redirect: 'error', signal: AbortSignal.timeout(15 * 60 * 1000) })
  if (!response.ok)
    throw new Error(`Ubuntu download failed (HTTP ${response.status}). Retry when the network is available.`)
  const handle = await fs.open(file, 'w', 0o600)
  const hash = createHash('sha256')
  let size = 0,
    last = 0
  try {
    for await (const chunk of response.body) {
      size += chunk.length
      if (size > 128 * 1024 * 1024) throw new Error('Ubuntu download exceeded the expected size.')
      hash.update(chunk)
      await handle.writeFile(chunk)
      if (size - last >= 1024 * 1024) {
        last = size
        progress(Math.floor(size / 1024 / 1024))
      }
    }
    if (hash.digest('hex') !== digest)
      throw new Error('Ubuntu checksum verification failed. Nothing was installed from this download.')
  } finally {
    await handle.close()
  }
}
function portOpen(port) {
  return new Promise((resolve) => {
    const socket = net.connect({ host: '127.0.0.1', port })
    const finish = (value) => {
      socket.destroy()
      resolve(value)
    }
    socket.once('connect', () => finish(true))
    socket.once('error', () => finish(false))
    socket.setTimeout(1500, () => finish(false))
  })
}
// Elevation is limited to Microsoft's WSL installer. No app path, engine script,
// user input or renderer-supplied command is executed with Windows admin rights.
const ENABLE_WSL = String.raw`$ErrorActionPreference='Stop'; try { $p=Start-Process -FilePath "$env:SystemRoot\System32\wsl.exe" -ArgumentList '--install','--no-distribution','--web-download' -Verb RunAs -Wait -PassThru; exit $p.ExitCode } catch { Write-Output 'WSL setup was cancelled or could not request administrator permission.'; exit 1 }`

function createWindowsEngine({
  dataDir,
  probe,
  onChange = () => {},
  platform = process.platform,
  arch = process.arch,
  execute = run,
  fetchRootfs = download,
  isPortOpen = portOpen,
  keepAlive = (command, args) => {
    const child = spawn(command, args, { detached: true, windowsHide: true, stdio: 'ignore', shell: false })
    child.on('error', () => {})
    child.unref()
  },
  sleep = (ms) => new Promise((resolve) => setTimeout(resolve, ms)),
}) {
  const directory = path.join(dataDir, 'local-engine')
  const manifestPath = path.join(directory, 'engine.json')
  const system = process.env.SystemRoot || 'C:\\Windows'
  const wsl = path.win32.join(system, 'System32', 'wsl.exe')
  const powershell = path.win32.join(system, 'System32', 'WindowsPowerShell', 'v1.0', 'powershell.exe')
  let manifest, active
  let state = { phase: 'idle', busy: false, installed: false, message: '', url: ENGINE_URL }
  const set = (patch) => {
    state = { ...state, ...patch }
    onChange({ ...state })
  }
  async function load() {
    try {
      const value = JSON.parse(await fs.readFile(manifestPath, 'utf8'))
      if (!/^sandboxd-desktop-[a-f0-9]{16}$/.test(value.distro))
        throw new Error('Invalid managed engine configuration.')
      manifest = value
      state.installed = value.installed === true
      if (value.needsRestart && !state.busy) {
        state.phase = 'restart'
        state.message = 'After restarting Windows, click Continue setup.'
      }
    } catch (error) {
      if (error.code !== 'ENOENT') {
        set({
          phase: 'error',
          message:
            'Could not read your managed engine configuration. Restore local-engine/engine.json before retrying.',
        })
        throw error
      }
    }
    return { ...state }
  }
  async function save() {
    await fs.mkdir(directory, { recursive: true, mode: 0o700 })
    await fs.writeFile(manifestPath + '.tmp', JSON.stringify(manifest), { mode: 0o600 })
    await fs.rename(manifestPath + '.tmp', manifestPath)
  }
  async function setup() {
    if (platform !== 'win32') throw new Error('Automatic engine setup is available on Windows.')
    if (!ROOTFS[arch]) throw new Error('This Windows architecture is not supported.')
    set({ phase: 'checking', busy: true, message: 'Checking this computer…' })
    try {
      if (await probe()) {
        set({ phase: 'ready', message: 'Your local engine is ready.' })
        return { ...state }
      }
      await load()
      // WSL may retain its localhost listener while our installed engine restarts.
      // Starting the owned distro is safe; never stop an unrelated port owner.
      if (!manifest?.installed && (await isPortOpen(9090)))
        throw new Error(
          'Another app is using port 9090. Close it or connect to your existing engine using its API address.',
        )
      if (!manifest) {
        manifest = { distro: `sandboxd-desktop-${randomBytes(8).toString('hex')}`, installed: false }
        await save()
      }
      let check = await execute(wsl, ['--status']).catch(() => ({ code: 1 }))
      if (check.code !== 0) {
        manifest.needsRestart = true
        await save()
        set({
          phase: 'wsl',
          message:
            'Enabling Windows Subsystem for Linux. Approve the Windows administrator prompt to continue…',
        })
        const result = await execute(
          powershell,
          [
            '-NoProfile',
            '-NonInteractive',
            '-EncodedCommand',
            Buffer.from(ENABLE_WSL, 'utf16le').toString('base64'),
          ],
          { timeout: 20 * 60 * 1000 },
        )
        if (![0, 3010].includes(result.code))
          throw new Error(
            'Windows could not enable WSL, or permission was declined. Retry and approve the Windows prompt. Hardware virtualization must be enabled.',
          )
        check = await execute(wsl, ['--status']).catch(() => ({ code: 1 }))
        if (result.code === 3010 || check.code !== 0) {
          set({
            phase: 'restart',
            message: 'Restart Windows, reopen sandboxd, and click Continue setup. Your progress is saved.',
          })
          return { ...state }
        }
      }
      if (manifest.needsRestart) {
        manifest.needsRestart = false
        await save()
      }
      const listed = await execute(wsl, ['--list', '--quiet'])
      if (listed.code !== 0) throw new Error('Cannot list WSL environments. Restart Windows and retry setup.')
      const names = listed.output.split(/\r?\n/).map((name) => name.trim().toLowerCase())
      if (!names.includes(manifest.distro)) {
        if (manifest.installed)
          throw new Error(
            'Your managed WSL environment is missing. Restore it before continuing; setup will not replace an engine containing your work.',
          )
        const image = ROOTFS[arch]
        const archive = path.join(directory, 'ubuntu.tar.gz')
        set({ phase: 'download', message: 'Downloading the Linux environment…' })
        try {
          await fetchRootfs(
            `https://cdimage.ubuntu.com/ubuntu-base/releases/24.04/release/ubuntu-base-24.04.5-base-${image.arch}.tar.gz`,
            archive,
            image.sha256,
            (mb) => set({ message: `Downloading the Linux environment… ${mb} MB` }),
          )
          set({ phase: 'installing', message: 'Creating your private engine environment…' })
          const imported = await execute(
            wsl,
            ['--import', manifest.distro, path.join(directory, 'disk'), archive, '--version', '2'],
            { timeout: 10 * 60 * 1000 },
          )
          if (imported.code !== 0)
            throw new Error(
              'WSL could not create the engine environment. Restart Windows and retry. Check that hardware virtualization is enabled and disk space is available.',
            )
        } finally {
          await fs.rm(archive, { force: true })
        }
      }
      if (!manifest.installed && (await isPortOpen(8080)))
        throw new Error(
          'Another app is using preview port 8080. Close it, then retry setup. Your partial setup is preserved.',
        )
      set({ phase: 'installing', message: 'Preparing your engine…' })
      const script = await fs.readFile(path.join(__dirname, 'windows-engine-bootstrap.sh'), 'utf8')
      const result = await execute(
        wsl,
        [
          '--distribution',
          manifest.distro,
          '--user',
          'root',
          '--cd',
          '/',
          '--exec',
          'bash',
          '-s',
          '--',
          ENGINE_COMMIT,
        ],
        {
          input: script.replace(/\r/g, ''),
          timeout: 60 * 60 * 1000,
          onLine: (line) => {
            const stage = line.trim().replace(/^SANDBOXD_SETUP:/, '')
            if (line.startsWith('SANDBOXD_SETUP:') && STAGES[stage]) set({ message: STAGES[stage] })
          },
        },
      )
      if (result.code !== 0)
        throw new Error(
          'Engine setup did not finish. Retry to continue. Details: /var/log/sandboxd-setup.log in your managed WSL environment.',
        )
      manifest.installed = true
      await save()
      set({ installed: true })
      // One detached keepalive per distro; its Linux lock prevents duplicates.
      // Closing Electron must not stop the engine or its workloads.
      keepAlive(wsl, [
        '--distribution',
        manifest.distro,
        '--user',
        'root',
        '--cd',
        '/',
        '--exec',
        'flock',
        '-n',
        '/run/sandboxd-desktop.keepalive',
        'sleep',
        'infinity',
      ])
      set({ phase: 'starting', message: 'Connecting to your engine…' })
      for (let attempt = 0; attempt < 45; attempt++) {
        if (await probe()) {
          set({ phase: 'ready', installed: true, message: 'Your local engine is ready.' })
          return { ...state }
        }
        await sleep(2000)
      }
      throw new Error(
        'The engine started, but Windows cannot reach localhost:9090. Check VPN/firewall settings and WSL localhost forwarding, then retry.',
      )
    } catch (error) {
      set({ phase: 'error', message: error.message })
      return { ...state }
    } finally {
      set({ busy: false })
    }
  }
  return {
    state: () => ({ ...state }),
    load,
    start: () => {
      if (active) return active
      active = setup()
        .then(() => ({ ...state }))
        .finally(() => {
          active = null
        })
      return active
    },
  }
}
module.exports = { createWindowsEngine, download, decode, ROOTFS, ENGINE_COMMIT, ENGINE_URL }
