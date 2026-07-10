// Types for the Vite build-time env used by demo mode (VITE_DEMO=1).
interface ImportMetaEnv {
  readonly VITE_DEMO?: string
}
interface ImportMeta {
  readonly env: ImportMetaEnv
}
