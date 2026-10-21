export type DesktopConnection = { url: string; name: string; allowInsecure: boolean }
export type DesktopState = {
  platform: string
  version: string
  connections: DesktopConnection[]
  selected: string
  canRemember: boolean
}
export interface DesktopBridge {
  agentLogin?(agent: string): Promise<{ available: boolean }>
  importAgentLogin?(agent: string): Promise<{ imported: boolean }>
  state(): Promise<DesktopState>
  connect(connection: DesktopConnection): Promise<DesktopState>
  disconnect(forget?: boolean): Promise<DesktopState>
  openExternal(url: string): Promise<void>
  onConnections(callback: () => void): () => void
  onNavigate(callback: (route: string) => void): () => void
  onSearch(callback: () => void): () => void
}
declare global {
  interface Window {
    sandboxdDesktop?: DesktopBridge
  }
}
