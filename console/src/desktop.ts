export type DesktopConnection = { url: string; name: string; allowInsecure: boolean }
export type LocalEngineState = {
  phase: 'idle' | 'checking' | 'wsl' | 'download' | 'installing' | 'starting' | 'ready' | 'restart' | 'error'
  busy: boolean
  installed: boolean
  message: string
  url: string
}
export type DesktopState = {
  platform: string
  version: string
  connections: DesktopConnection[]
  selected: string
  canRemember: boolean
  localEngine?: LocalEngineState
}
// Your servers: Linux servers the desktop installed sandboxd on over SSH.
export type ServerStatus = {
  state: 'connected' | 'connecting' | 'offline'
  // Plain-language reason when not connected.
  reason: string
  // The server presents a different SSH identity than the one pinned when it
  // was added. Nothing is sent until the user trusts it (trustIdentity).
  identityChanged?: boolean
  presentedSha256?: string
}
export type DesktopServer = {
  id: string
  name: string
  host: string
  port: number
  hostKeySha256: string
  front: 'caddy' | 'existing' | 'manual'
  domain: string
  proxy: 'nginx' | 'caddy' | 'apache' | 'traefik' | 'coolify' | 'none'
  addedAt: string
  status: ServerStatus
}
export type AddServerInput = {
  host: string
  password: string
  port?: number
  username?: string
  name?: string
  // Only after the user agreed: lets the desktop install Docker first.
  installDocker?: boolean
}
export type AddServerStep = 'connecting' | 'checking' | 'docker' | 'installing' | 'securing' | 'done'
export type AddServerProgress = { step: AddServerStep; message: string }
export type AddServerResult =
  | { ok: true; server: DesktopServer }
  | {
      ok: false
      code:
        | 'invalid'
        | 'busy'
        | 'exists'
        | 'auth'
        | 'not_found'
        | 'unreachable'
        | 'timeout'
        | 'unsupported'
        | 'needs_admin'
        | 'needs_docker'
        | 'docker_failed'
        | 'install_failed'
        | 'verify'
        | 'no_keychain'
        | string
      message: string
      // The last lines of the installer output, for support.
      detail?: string
    }
// Each frontdoor command ends with this JSON line (facts returns server facts).
export type FrontdoorResult = { ok: boolean; step: string; message: string; [key: string]: unknown }
export interface DesktopServers {
  list(): Promise<DesktopServer[]>
  status(): Promise<Record<string, ServerStatus>>
  status(id: string): Promise<ServerStatus>
  add(input: AddServerInput): Promise<AddServerResult>
  remove(id: string): Promise<{ removed: true; note: string }>
  frontdoor(
    id: string,
    args:
      | ['facts']
      | ['apply', string, ...string[]]
      | ['remove', string]
      | ['status', string]
      | ['enable-caddy-import'],
  ): Promise<FrontdoorResult>
  // Shows a native confirmation; resolves trusted:false when the user cancels.
  trustIdentity(id: string): Promise<{ trusted: boolean; server?: DesktopServer }>
  onProgress(callback: (progress: AddServerProgress) => void): () => void
  onChange(callback: (change: { id: string; status: ServerStatus }) => void): () => void
}
// A server engine's /v1 API, through the desktop gateway and that server's
// SSH tunnel: fetch(serverApi(id, '/v1/apps')).
export const serverApi = (id: string, path: string) => `/servers/${encodeURIComponent(id)}${path}`
export interface DesktopBridge {
  agentLogin?(agent: string): Promise<{ available: boolean }>
  importAgentLogin?(agent: string): Promise<{ imported: boolean }>
  state(): Promise<DesktopState>
  connect(connection: DesktopConnection): Promise<DesktopState>
  disconnect(forget?: boolean): Promise<DesktopState>
  openExternal(url: string): Promise<void>
  installEngine(): Promise<LocalEngineState>
  // Restores the session for the engine this app installed (no password prompt).
  signIn(): Promise<void>
  onEngineProgress(callback: (state: LocalEngineState) => void): () => void
  onConnections(callback: () => void): () => void
  onNavigate(callback: (route: string) => void): () => void
  onSearch(callback: () => void): () => void
  servers?: DesktopServers
}
declare global {
  interface Window {
    sandboxdDesktop?: DesktopBridge
  }
}
