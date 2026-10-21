// Uses a disposable local fixture, never a user's engine or desktop profile.
const { _electron: electron, expect } = require('../../console/node_modules/@playwright/test')
const http = require('node:http')
const { once } = require('node:events')
const fs = require('node:fs/promises')
const os = require('node:os')
const path = require('node:path')
const assert = require('node:assert/strict')
const { createHash } = require('node:crypto')

async function main() {
  const temp = await fs.mkdtemp(path.join(os.tmpdir(), 'sandboxd-desktop-smoke-'))
  const output = path.resolve(__dirname, '../test-results')
  await fs.mkdir(output, { recursive: true })
  const requests = []
  const engine = http.createServer((req, res) => {
    requests.push({ url: req.url, headers: req.headers })
    res.setHeader('content-type', 'application/json')
    if (req.url === '/v1/desktop-script-probe') {
      res.setHeader('content-type', 'application/javascript')
      return res.end('window.engineScriptExecuted = true')
    }
    if (req.url === '/v1/auth/status')
      return res.end(
        JSON.stringify({
          enabled: true,
          authenticated: req.headers.cookie === 'sbx_session=test-session',
          password_set: true,
        }),
      )
    if (req.url === '/v1/auth/login') {
      res.setHeader('set-cookie', 'sbx_session=test-session; Secure; HttpOnly')
      return res.end('{}')
    }
    if (req.url === '/v1/settings')
      return res.end(
        JSON.stringify({
          version: 'test',
          networking: { preview_domain: 'localhost' },
          auth: { enabled: true },
          runtime: {},
          lifecycle: {},
          egress: {},
          agents: { providers: [], default_models: {} },
          capabilities: {},
          editable: [],
        }),
      )
    if (req.url === '/v1/apps') return res.end('{"apps":[]}')
    if (req.url === '/v1/agents') return res.end('{"providers":[]}')
    if (req.url === '/v1/model-providers') return res.end('{"providers":[]}')
    if (req.url === '/v1/presets') return res.end('{"presets":[]}')
    if (req.url === '/v1/sandboxes/test/export') {
      res.setHeader('content-type', 'application/zip')
      res.setHeader('content-disposition', 'attachment; filename="workspace.zip"')
      return res.end('ZIP')
    }
    if (req.url === '/preview') {
      res.setHeader('content-type', 'text/html')
      return res.end('<h1>Untrusted preview</h1>')
    }
    res.end('{}')
  })
  const sockets = new Set()
  engine.on('connection', (socket) => {
    sockets.add(socket)
    socket.once('close', () => sockets.delete(socket))
  })
  engine.on('upgrade', (req, socket) => {
    assert.equal(req.headers.cookie, 'sbx_session=test-session')
    assert.equal(req.headers['x-sandboxd-desktop'], undefined)
    const accept = createHash('sha1')
      .update(req.headers['sec-websocket-key'] + '258EAFA5-E914-47DA-95CA-C5AB0DC85B11')
      .digest('base64')
    socket.write(
      `HTTP/1.1 101 Switching Protocols\r\nUpgrade: websocket\r\nConnection: Upgrade\r\nSec-WebSocket-Accept: ${accept}\r\n\r\n`,
    )
    const message = Buffer.from('fixture-terminal')
    socket.write(Buffer.concat([Buffer.from([0x81, message.length]), message]))
    socket.on('error', () => {})
    socket.on('data', () => socket.destroy())
  })
  engine.listen(process.platform === 'win32' ? 9090 : 0, '127.0.0.1')
  await once(engine, 'listening')
  let app
  try {
    app = await electron.launch({
      chromiumSandbox: process.env.SANDBOXD_TEST_NO_OS_SANDBOX !== '1',
      executablePath: require('electron'),
      args: [path.resolve(__dirname, '..')],
      env: { ...process.env, SANDBOXD_DESKTOP_TEST_DATA: temp },
      timeout: 30000,
    })
    app.process().stderr.on('data', (data) => process.stderr.write(data))
    const page = await app.firstWindow()
    page.setDefaultTimeout(10000)
    const errors = []
    page.on('pageerror', (error) => errors.push(error.message))
    const origin = `http://127.0.0.1:${engine.address().port}`
    await expect(page.getByRole('heading', { name: 'Connect your engine.' })).toBeVisible()
    await page.screenshot({ path: path.join(output, 'welcome.png') })
    await app.evaluate(({ BrowserWindow }) => BrowserWindow.getAllWindows()[0].setSize(880, 640))
    await page.waitForFunction(() => window.innerWidth <= 880)
    assert(
      await page.evaluate(() => {
        const story = document.querySelector('.desktop-welcome-story').getBoundingClientRect()
        const footer = document.querySelector('.desktop-welcome-story > small').getBoundingClientRect()
        return footer.bottom <= story.bottom && document.documentElement.scrollWidth <= window.innerWidth
      }),
      'Welcome content must fit the minimum window',
    )
    await page.screenshot({ path: path.join(output, 'welcome-small.png') })
    await app.evaluate(({ BrowserWindow }) => BrowserWindow.getAllWindows()[0].setSize(1440, 960))

    assert.deepEqual(
      await page.evaluate(() => ({
        node: typeof require,
        process: typeof process,
        bridge: typeof window.sandboxdDesktop,
      })),
      { node: 'undefined', process: 'undefined', bridge: 'object' },
    )
    const prefs = await app.evaluate(({ BrowserWindow }) => {
      const p = BrowserWindow.getAllWindows()[0].webContents.getLastWebPreferences()
      return {
        sandbox: p.sandbox,
        node: p.nodeIntegration,
        isolation: p.contextIsolation,
        webSecurity: p.webSecurity,
      }
    })
    assert.deepEqual(prefs, { sandbox: true, node: false, isolation: true, webSecurity: true })
    if (process.platform === 'win32') {
      // Exercise the real renderer/preload flow without installing WSL in the UI
      // fixture. Actual WSL lifecycle runs separately in smoke-wsl.cjs.
      await app.evaluate(({ ipcMain }) => {
        let attempts = 0
        ipcMain.removeHandler('desktop:install-engine')
        ipcMain.handle('desktop:install-engine', async (event) => {
          const result = {
            busy: true,
            installed: false,
            phase: 'installing',
            message: 'Preparing your engine…',
            url: 'http://127.0.0.1:9090',
          }
          event.sender.send('desktop:engine-progress', result)
          await new Promise((resolve) => setTimeout(resolve, 150))
          attempts++
          Object.assign(
            result,
            attempts === 1
              ? { phase: 'restart', message: 'Restart Windows, reopen sandboxd, and click Continue setup.' }
              : attempts === 2
                ? { phase: 'error', message: 'Network interrupted. Retry to continue.' }
                : { phase: 'ready', installed: true, message: 'Your local engine is ready.' },
            { busy: false },
          )
          event.sender.send('desktop:engine-progress', result)
          return result
        })
      })
      await page.getByTestId('desktop-install-engine').click()
      await expect(page.getByRole('button', { name: 'Continue setup' })).toBeVisible()
      await page.getByRole('button', { name: 'Continue setup' }).click()
      await expect(page.getByText('Network interrupted. Retry to continue.')).toBeVisible()
      await page.screenshot({ path: path.join(output, 'engine-setup-retry.png') })
      await page.getByRole('button', { name: 'Retry setup' }).click()
    } else {
      await page.getByTestId('desktop-engine-url').fill(origin)
      await page.getByTestId('desktop-connect').click()
    }
    await page.getByTestId('login-password').fill('fixture-password')
    await page.getByTestId('login-submit').click()
    await expect(page.getByTestId('nav-apps')).toBeVisible()
    await page.screenshot({ path: path.join(output, 'workspace.png') })
    assert.equal(await page.evaluate(() => document.cookie), '')
    assert.equal(
      await page.evaluate(
        () =>
          new Promise((resolve) => {
            const script = document.createElement('script')
            script.src = '/v1/desktop-script-probe'
            script.onload = () => resolve('loaded')
            script.onerror = () => resolve('blocked')
            document.head.append(script)
          }),
      ),
      'blocked',
    )
    assert.equal(await page.evaluate(() => typeof window.engineScriptExecuted), 'undefined')
    assert(!requests.some((request) => request.url === '/v1/desktop-script-probe'))

    assert.equal(
      await page.evaluate(
        () =>
          new Promise((resolve, reject) => {
            const socket = new WebSocket(`ws://${location.host}/v1/sandboxes/test/terminal`)
            socket.onmessage = (event) => {
              resolve(event.data)
              socket.close()
            }
            socket.onerror = () => reject(new Error('Terminal WebSocket failed'))
          }),
      ),
      'fixture-terminal',
    )
    assert.equal(
      JSON.stringify(await page.evaluate(() => window.sandboxdDesktop.state())).includes('test-session'),
      false,
    )

    assert(requests.some((r) => r.url === '/v1/apps' && r.headers.cookie === 'sbx_session=test-session'))
    assert(requests.every((r) => !r.headers['x-sandboxd-desktop']))
    // A hostile engine preview has no native bridge and cannot reach the private gateway.
    await page.evaluate((origin) => {
      const f = document.createElement('iframe')
      f.src = origin + '/preview'
      f.id = 'hostile-preview'
      document.body.append(f)
    }, origin)
    const frame = await (await page.locator('#hostile-preview').elementHandle()).contentFrame()
    await expect(frame.getByRole('heading', { name: 'Untrusted preview' })).toBeVisible()
    assert.equal(await frame.evaluate(() => typeof window.sandboxdDesktop), 'undefined')
    assert.equal(
      await frame.evaluate(async (gateway) => {
        try {
          return (await fetch(gateway + '/v1/apps')).status
        } catch {
          return 'blocked'
        }
      }, new URL(page.url()).origin),
      'blocked',
    )
    assert(
      requests
        .filter((r) => r.url === '/preview')
        .every((r) => !r.headers.cookie && !r.headers['x-sandboxd-desktop']),
    )
    await page.locator('#hostile-preview').evaluate((f) => f.remove())
    // Native export uses the authenticated gateway and keeps the workspace open.
    const downloaded = app.evaluate(
      ({ BrowserWindow }, destination) =>
        new Promise((resolve) => {
          const timer = setTimeout(() => resolve('timeout'), 10000)
          BrowserWindow.getAllWindows()[0].webContents.session.once('will-download', (_event, item) => {
            item.setSavePath(destination)
            item.once('done', (_event, state) => {
              clearTimeout(timer)
              resolve(state)
            })
          })
        }),
      path.join(temp, 'workspace.zip'),
    )
    await page.evaluate(() => {
      const link = document.createElement('a')
      link.href = '/v1/sandboxes/test/export'
      link.download = 'workspace.zip'
      link.textContent = 'Export fixture'
      document.body.append(link)
      link.click()
      link.remove()
    })
    assert.equal(await downloaded, 'completed')
    assert.equal(await fs.readFile(path.join(temp, 'workspace.zip'), 'utf8'), 'ZIP')
    await expect(page.getByTestId('nav-apps')).toBeVisible()
    await page.getByRole('button', { name: /This computer/ }).click()
    await page.getByRole('button', { name: 'Disconnect', exact: true }).click()
    await expect(page.getByRole('heading', { name: 'Connect your engine.' })).toBeVisible()
    const profile = await fs.readFile(path.join(temp, 'connections.json'), 'utf8')
    assert(!profile.includes('test-session') && !profile.includes('fixture-password'))
    assert.deepEqual(errors, [])
    console.log(
      'PASS: Electron onboarding, login, session isolation, preview isolation, exports, disconnect and sandbox preferences',
    )
  } finally {
    if (app) await app.close()
    for (const socket of sockets) socket.destroy()
    await new Promise((resolve) => engine.close(resolve))
    await fs.rm(temp, { recursive: true, force: true })
  }
}
main().catch((error) => {
  console.error(error)
  process.exitCode = 1
})
