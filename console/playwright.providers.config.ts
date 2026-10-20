import { defineConfig } from '@playwright/test'

// Browser coverage for the console's provider workflow. API fixtures keep this
// independent of Docker; Go tests cover the real API/proxy/runtime boundaries.
export default defineConfig({
  testDir: './e2e',
  testMatch: ['model-providers.spec.ts', 'builder.spec.ts'],
  timeout: 30_000,
  use: {
    baseURL: 'http://127.0.0.1:18787',
    viewport: { width: 1440, height: 1080 },
    trace: 'retain-on-failure',
  },
  webServer: {
    command: 'pnpm dev --host 127.0.0.1 --port 18787',
    url: 'http://127.0.0.1:18787',
    reuseExistingServer: !process.env.CI,
  },
})
