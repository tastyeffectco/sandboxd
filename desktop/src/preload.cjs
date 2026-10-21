const { contextBridge, ipcRenderer } = require('electron')
if (process.isMainFrame) {
  const subscribe = (channel, callback) => {
    const listener = (_event, value) => callback(value)
    ipcRenderer.on(channel, listener)
    return () => ipcRenderer.removeListener(channel, listener)
  }
  contextBridge.exposeInMainWorld(
    'sandboxdDesktop',
    Object.freeze({
      state: () => ipcRenderer.invoke('desktop:state'),
      connect: (connection) => ipcRenderer.invoke('desktop:connect', connection),
      disconnect: (forget = false) => ipcRenderer.invoke('desktop:disconnect', forget),
      openExternal: (url) => ipcRenderer.invoke('desktop:external', url),
      installEngine: () => ipcRenderer.invoke('desktop:install-engine'),
      onEngineProgress: (callback) => subscribe('desktop:engine-progress', callback),
      onConnections: (callback) => subscribe('desktop:connections', callback),
      onNavigate: (callback) => subscribe('desktop:navigate', callback),
      onSearch: (callback) => subscribe('desktop:search', callback),
    }),
  )
}
