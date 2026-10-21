const { app, BrowserWindow, Menu, ipcMain, shell, session, safeStorage, dialog } = require('electron')
const path = require('node:path')
const fs = require('node:fs/promises')
const { createGateway } = require('./gateway.cjs')
const { normalizeEngineURL, externalURL } = require('./connection.cjs')

let window,
  gateway,
  desktopSession,
  quitting = false
let config = { connections: [], selected: '', sessions: {} }
let writes = Promise.resolve()
let connecting = false
const memorySessions = new Map()
const configPath = () => path.join(app.getPath('userData'), 'connections.json')
const canRemember = () =>
  safeStorage.isEncryptionAvailable() &&
  (process.platform !== 'linux' || safeStorage.getSelectedStorageBackend() !== 'basic_text')
const publicState = () => ({
  platform: process.platform,
  version: app.getVersion(),
  connections: config.connections,
  selected: config.selected,
  canRemember: canRemember(),
})
const persist = () => {
  const json = JSON.stringify(config)
  writes = writes
    .catch(() => {})
    .then(async () => {
      await fs.mkdir(app.getPath('userData'), { recursive: true })
      await fs.writeFile(configPath() + '.tmp', json, { mode: 0o600 })
      await fs.rename(configPath() + '.tmp', configPath())
    })
  return writes
}
function trusted(event) {
  if (!window || event.sender !== window.webContents || event.senderFrame !== window.webContents.mainFrame)
    throw new Error('Unavailable outside the desktop window.')
  const url = new URL(event.senderFrame.url)
  if (url.origin !== gateway.origin || !['/', '/index.html'].includes(url.pathname))
    throw new Error('Untrusted desktop page.')
}
function sessionFor(url) {
  if (memorySessions.has(url)) return memorySessions.get(url)
  try {
    return canRemember() && config.sessions[url]
      ? safeStorage.decryptString(Buffer.from(config.sessions[url], 'base64'))
      : ''
  } catch {
    return ''
  }
}
async function openExternal(value) {
  const url = externalURL(value)
  if (url) await shell.openExternal(url)
}
async function probeEngine(value, allowInsecure) {
  const origin = normalizeEngineURL(value, allowInsecure)
  let response
  try {
    response = await fetch(origin + '/v1/auth/status', {
      redirect: 'error',
      signal: AbortSignal.timeout(7000),
    })
  } catch {
    throw new Error('Cannot reach this engine. Check the address, network and whether sandboxd is running.')
  }
  if (!response.ok)
    throw new Error(`The engine returned HTTP ${response.status}. Check that this is its API address.`)
  let body = null
  try {
    const chunks = []
    let size = 0
    for await (const chunk of response.body) {
      size += chunk.length
      if (size > 65536) throw new Error('Engine status is too large.')
      chunks.push(chunk)
    }
    body = JSON.parse(Buffer.concat(chunks).toString('utf8'))
  } catch {
    /* Invalid or oversized status: reject the connection below. */
  }
  if (!body || typeof body.enabled !== 'boolean' || typeof body.authenticated !== 'boolean')
    throw new Error('This address did not return the sandboxd API. Use the engine URL, not an app preview.')
  return origin
}
async function createWindow() {
  window = new BrowserWindow({
    width: 1440,
    height: 960,
    minWidth: 880,
    minHeight: 640,
    show: false,
    title: 'sandboxd',
    backgroundColor: '#f5f6f3',
    titleBarStyle: 'hidden',
    ...(process.platform === 'darwin'
      ? { trafficLightPosition: { x: 18, y: 18 } }
      : { titleBarOverlay: { color: '#f0f2ee', symbolColor: '#284638', height: 48 } }),
    webPreferences: {
      preload: path.join(__dirname, 'preload.cjs'),
      session: desktopSession,
      contextIsolation: true,
      sandbox: true,
      nodeIntegration: false,
      nodeIntegrationInSubFrames: false,
      webSecurity: true,
      allowRunningInsecureContent: false,
      webviewTag: false,
    },
  })
  window.webContents.setWindowOpenHandler(({ url }) => {
    const target = new URL(url)
    if (target.origin === gateway.origin && /^\/v1\/sandboxes\/[^/]+\/export$/.test(target.pathname))
      window.webContents.downloadURL(url)
    else if (target.origin !== gateway.origin) void openExternal(url).catch(() => {})
    return { action: 'deny' }
  })
  window.webContents.on('will-navigate', (event, url) => {
    const target = new URL(url)
    if (target.origin !== gateway.origin || !['/', '/index.html'].includes(target.pathname)) {
      event.preventDefault()
      if (target.origin === gateway.origin && /^\/v1\/sandboxes\/[^/]+\/export$/.test(target.pathname))
        window.webContents.downloadURL(url)
    }
  })
  window.webContents.on('will-attach-webview', (event) => event.preventDefault())
  window.webContents.on('will-prevent-unload', (event) => {
    const answer = dialog.showMessageBoxSync(window, {
      type: 'question',
      buttons: ['Keep editing', 'Discard and close'],
      defaultId: 0,
      cancelId: 0,
      title: 'Unsaved changes',
      message: 'Discard unsaved workspace edits?',
    })
    if (answer === 1) event.preventDefault()
    else quitting = false
  })
  window.once('ready-to-show', () => window.show())
  window.on('closed', () => {
    window = null
  })
  await window.loadURL(gateway.origin)
}
async function start() {
  try {
    const stored = JSON.parse(await fs.readFile(configPath(), 'utf8'))
    config.connections = Array.isArray(stored.connections)
      ? stored.connections
          .filter((c) => {
            try {
              return (
                normalizeEngineURL(c.url, c.allowInsecure) === c.url &&
                typeof c.name === 'string' &&
                c.name.length <= 80 &&
                typeof c.allowInsecure === 'boolean'
              )
            } catch {
              return false
            }
          })
          .slice(0, 20)
          .map(({ url, name, allowInsecure }) => ({ url, name, allowInsecure }))
      : []
    config.selected = config.connections.some((c) => c.url === stored.selected) ? stored.selected : ''
    config.sessions = stored.sessions && typeof stored.sessions === 'object' ? stored.sessions : {}
  } catch {
    /* Fresh install, or invalid config: offer connection setup. */
  }
  gateway = await createGateway({
    uiRoot: path.join(__dirname, '../ui'),
    engine: config.selected,
    session: sessionFor(config.selected),
    onSession: (url, token) => {
      memorySessions.set(url, token)
      try {
        if (!token || !canRemember()) delete config.sessions[url]
        else config.sessions[url] = safeStorage.encryptString(token).toString('base64')
      } catch {
        delete config.sessions[url]
      }
      void persist().catch(() => {})
    },
  })
  desktopSession = session.fromPartition(`sandboxd-${process.pid}`)
  desktopSession.setPermissionRequestHandler((_contents, _permission, callback) => callback(false))
  desktopSession.setPermissionCheckHandler(() => false)
  // Cookies are shared across ports. Use a main-process-only header so a
  // local preview cannot receive the gateway capability by sharing its host.
  desktopSession.webRequest.onBeforeSendHeaders((details, callback) => {
    const target = new URL(details.url)
    const isGateway =
      target.host === new URL(gateway.origin).host && ['http:', 'ws:'].includes(target.protocol)
    const ownFrame = details.webContentsId === window?.webContents.id
    const ownOrigin = details.initiatorOrigin === gateway.origin
    const navigation = details.resourceType === 'mainFrame' && !details.initiatorOrigin
    const download = !details.initiatorOrigin && /^\/v1\/sandboxes\/[^/]+\/export$/.test(target.pathname)
    if (isGateway && (!ownFrame || (!ownOrigin && !navigation && !download)))
      return callback({ cancel: true })
    const headers = { ...details.requestHeaders }
    for (const key of Object.keys(headers))
      if (key.toLowerCase() === 'x-sandboxd-desktop') delete headers[key]
    if (isGateway) Object.assign(headers, gateway.authorization)
    callback({ requestHeaders: headers })
  })
  ipcMain.handle('desktop:state', (event) => {
    trusted(event)
    return publicState()
  })
  ipcMain.handle('desktop:connect', async (event, input) => {
    trusted(event)
    if (
      !input ||
      typeof input.name !== 'string' ||
      input.name.length > 80 ||
      typeof input.allowInsecure !== 'boolean'
    )
      throw new Error('Invalid connection.')
    if (connecting) throw new Error('A connection is already in progress.')
    connecting = true
    try {
      const url = await probeEngine(input.url, input.allowInsecure)
      const connection = {
        url,
        name:
          input.name.trim() ||
          (new URL(url).hostname === '127.0.0.1' ? 'This computer' : new URL(url).hostname),
        allowInsecure: input.allowInsecure,
      }
      const previous = config
      config = {
        ...config,
        connections: [connection, ...config.connections.filter((c) => c.url !== url)].slice(0, 20),
        selected: url,
      }
      try {
        await persist()
      } catch {
        config = previous
        throw new Error('Could not save this connection. Check available disk space and profile permissions.')
      }
      gateway.setEngine(url, sessionFor(url), input.allowInsecure)
      return publicState()
    } finally {
      connecting = false
    }
  })
  ipcMain.handle('desktop:disconnect', async (event, forget) => {
    trusted(event)
    if (connecting) throw new Error('Wait for the connection to finish.')
    const previous = config
    config = {
      ...config,
      connections: [...config.connections],
      sessions: { ...config.sessions },
      selected: '',
    }
    if (forget === true) {
      config.connections = config.connections.filter((c) => c.url !== previous.selected)
      delete config.sessions[previous.selected]
    }
    try {
      await persist()
    } catch {
      config = previous
      throw new Error('Could not save the disconnection. Check available disk space and profile permissions.')
    }
    if (forget === true) memorySessions.delete(previous.selected)
    gateway.setEngine('')
    return publicState()
  })
  ipcMain.handle('desktop:external', async (event, url) => {
    trusted(event)
    await openExternal(url)
  })
  Menu.setApplicationMenu(
    Menu.buildFromTemplate([
      ...(process.platform === 'darwin' ? [{ role: 'appMenu' }] : []),
      {
        label: 'File',
        submenu: [
          {
            label: 'New Project',
            accelerator: 'CmdOrCtrl+N',
            click: () => window?.webContents.send('desktop:navigate', 'apps'),
          },
          {
            label: 'Engine Connections…',
            accelerator: 'CmdOrCtrl+Shift+E',
            click: () => window?.webContents.send('desktop:connections'),
          },
          { type: 'separator' },
          process.platform === 'darwin' ? { role: 'close' } : { role: 'quit' },
        ],
      },
      { role: 'editMenu' },
      {
        label: 'View',
        submenu: [
          {
            label: 'Quick Search',
            accelerator: 'CmdOrCtrl+K',
            click: () => window?.webContents.send('desktop:search'),
          },
          { type: 'separator' },
          { role: 'resetZoom' },
          { role: 'zoomIn' },
          { role: 'zoomOut' },
          { role: 'togglefullscreen' },
        ],
      },
      { role: 'windowMenu' },
      {
        role: 'help',
        submenu: [
          {
            label: 'Documentation',
            click: () =>
              void openExternal('https://github.com/tastyeffectco/sandboxd/blob/main/docs/desktop.md'),
          },
          {
            label: 'Report an Issue',
            click: () => void openExternal('https://github.com/tastyeffectco/sandboxd/issues/new/choose'),
          },
          ...(process.platform === 'darwin' ? [] : [{ role: 'about' }]),
        ],
      },
    ]),
  )
  app.setAboutPanelOptions({
    applicationName: 'sandboxd',
    applicationVersion: app.getVersion(),
    copyright: 'MIT · sandboxd contributors',
  })
  await createWindow()
}
if (process.env.SANDBOXD_DESKTOP_TEST_DATA && !app.isPackaged)
  app.setPath('userData', process.env.SANDBOXD_DESKTOP_TEST_DATA)
if (!app.requestSingleInstanceLock()) app.quit()
else {
  app.on('second-instance', () => {
    if (window?.isMinimized()) window.restore()
    window?.focus()
  })
  app
    .whenReady()
    .then(start)
    .catch((error) => {
      dialog.showErrorBox('sandboxd could not start', error.message)
      app.quit()
    })
  app.on('activate', () => {
    if (!window && gateway && !quitting) void createWindow()
  })
  app.on('window-all-closed', () => {
    if (process.platform !== 'darwin') app.quit()
  })
  app.on('before-quit', (event) => {
    if (quitting) return
    event.preventDefault()
    void writes
      .catch(() => {})
      .then(() => {
        quitting = true
        app.quit()
      })
  })
  app.on('will-quit', () => {
    void gateway?.close()
  })
}
