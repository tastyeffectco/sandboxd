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
  // The login the desktop signs in with (its own key, added at setup).
  username: string
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
export interface DesktopServers {
  list(): Promise<DesktopServer[]>
  status(): Promise<Record<string, ServerStatus>>
  status(id: string): Promise<ServerStatus>
  add(input: AddServerInput): Promise<AddServerResult>
  remove(id: string): Promise<{ removed: true; keyRemoved: boolean; note: string }>
  // Shows a native confirmation; resolves trusted:false when the user cancels.
  trustIdentity(id: string): Promise<{ trusted: boolean; server?: DesktopServer }>
  onProgress(callback: (progress: AddServerProgress) => void): () => void
  onChange(callback: (change: { id: string; status: ServerStatus }) => void): () => void
}
// The server assistant (desktop/src/assistant.cjs, server-tools.cjs): a chat
// per server; every change it wants to make waits for an approval here.
export type AssistantEvent =
  | { serverId: string; type: 'status'; text: string }
  | { serverId: string; type: 'text'; text: string }
  | { serverId: string; type: 'tool'; tool: string; status: string }
  | { serverId: string; type: 'error'; text: string }
export type AssistantApproval = {
  id: string
  serverId: string
  serverName: string
  login: string
  kind: 'read' | 'run' | 'write' | 'undo'
  // The assistant's reason, in its words.
  title: string
  command?: string
  path?: string
  detail?: string
  // write: the file now (null when it is new) and after.
  before?: string | null
  after?: string
  change?: string
  admin?: boolean
  // A sudo login: the approval asks for the server password.
  needsPassword: boolean
}
export type AssistantActivity = {
  at: string
  kind: string
  title: string
  command?: string
  path?: string
  change?: string
  ok?: boolean
  declined?: boolean
}
export type AssistantToolsEvent =
  | { type: 'approval'; approval: AssistantApproval }
  | { type: 'decided'; id: string; allow: boolean }
  | { type: 'activity'; serverId: string; entry: AssistantActivity }
export interface DesktopAssistant {
  send(id: string, message: string): Promise<{ ok: boolean; code?: string; message?: string }>
  stop(id: string): Promise<void>
  reset(id: string): Promise<void>
  pending(): Promise<AssistantApproval[]>
  activity(id: string): Promise<AssistantActivity[]>
  decide(approvalId: string, decision: { allow: boolean; password?: string }): Promise<boolean>
  onEvent(callback: (event: AssistantEvent) => void): () => void
  onTools(callback: (event: AssistantToolsEvent) => void): () => void
  // First use: OpenCode is downloaded (about 75 MB).
  onSetup(callback: (progress: { received: number; total: number }) => void): () => void
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
  assistant?: DesktopAssistant
}
declare global {
  interface Window {
    sandboxdDesktop?: DesktopBridge
  }
}
