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
      agentLogin: (agent) => ipcRenderer.invoke('desktop:agent-login', agent),
      importAgentLogin: (agent) => ipcRenderer.invoke('desktop:import-agent-login', agent),
      state: () => ipcRenderer.invoke('desktop:state'),
      connect: (connection) => ipcRenderer.invoke('desktop:connect', connection),
      disconnect: (forget = false) => ipcRenderer.invoke('desktop:disconnect', forget),
      openExternal: (url) => ipcRenderer.invoke('desktop:external', url),
      installEngine: () => ipcRenderer.invoke('desktop:install-engine'),
      signIn: () => ipcRenderer.invoke('desktop:sign-in'),
      onEngineProgress: (callback) => subscribe('desktop:engine-progress', callback),
      onConnections: (callback) => subscribe('desktop:connections', callback),
      onNavigate: (callback) => subscribe('desktop:navigate', callback),
      onSearch: (callback) => subscribe('desktop:search', callback),
      // Your servers (see servers.cjs). Server engines are reached through the
      // gateway at /servers/<id>/v1/...
      servers: Object.freeze({
        list: () => ipcRenderer.invoke('servers:list'),
        status: (id) => ipcRenderer.invoke('servers:status', id),
        add: (input) => ipcRenderer.invoke('servers:add', input),
        remove: (id) => ipcRenderer.invoke('servers:remove', id),
        trustIdentity: (id) => ipcRenderer.invoke('servers:trust-identity', id),
        onProgress: (callback) => subscribe('servers:progress', callback),
        onChange: (callback) => subscribe('servers:changed', callback),
      }),
      // The server assistant (assistant.cjs, server-tools.cjs).
      assistant: Object.freeze({
        send: (id, message) => ipcRenderer.invoke('assistant:send', id, message),
        stop: (id) => ipcRenderer.invoke('assistant:stop', id),
        reset: (id) => ipcRenderer.invoke('assistant:reset', id),
        pending: () => ipcRenderer.invoke('assistant:pending'),
        activity: (id) => ipcRenderer.invoke('assistant:activity', id),
        decide: (approvalId, decision) => ipcRenderer.invoke('assistant:decide', approvalId, decision),
        onEvent: (callback) => subscribe('assistant:event', callback),
        onTools: (callback) => subscribe('assistant:tools', callback),
        onSetup: (callback) => subscribe('assistant:setup', callback),
      }),
    }),
  )
}
