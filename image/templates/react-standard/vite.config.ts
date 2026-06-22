import { defineConfig } from 'vite'
import react from '@vitejs/plugin-react'

// The preview is served through Traefik on a per-sandbox host
// (s-<id>-3000.preview.<domain>), so the dev server must listen on all
// interfaces and accept that forwarded Host header.
export default defineConfig({
  plugins: [react()],
  server: {
    host: true,
    port: 3000,
    strictPort: true,
    allowedHosts: true,
  },
})
