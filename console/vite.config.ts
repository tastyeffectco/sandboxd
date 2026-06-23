import { defineConfig } from 'vite'
import react from '@vitejs/plugin-react'

// In dev, proxy the public API to a local sandboxd so the SPA can use
// same-origin relative `/v1` paths (no CORS, no auth in the single-user
// default). Override the target with SANDBOXD_URL. In the container the
// same `/v1` paths are proxied by nginx (see nginx.conf).
const target = process.env.SANDBOXD_URL || 'http://127.0.0.1:9090'

export default defineConfig({
  plugins: [react()],
  server: {
    host: true,
    port: 8787,
    proxy: {
      '/v1': { target, changeOrigin: true },
    },
  },
})
