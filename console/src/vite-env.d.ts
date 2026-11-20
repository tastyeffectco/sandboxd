// Types for the Vite build-time env used by demo mode (VITE_DEMO=1).
interface ImportMetaEnv {
  readonly VITE_DESKTOP?: string
  readonly VITE_DEMO?: string
}
interface ImportMeta {
  readonly env: ImportMetaEnv
  // Vite: the files matching a pattern, known at build time (design/Avatar.tsx).
  glob(pattern: string, options: { eager: true; query: string; import: string }): Record<string, unknown>
}

// `import text from './file?raw'`: the file's content as a string (Vite).
declare module '*?raw' {
  const content: string
  export default content
}
