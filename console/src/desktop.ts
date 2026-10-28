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
export interface DesktopBridge {
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
}
declare global {
  interface Window {
    sandboxdDesktop?: DesktopBridge
  }
}
