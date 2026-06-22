import { defineConfig } from '@playwright/test'

// Runs against a live console (which proxies to a live sandboxd). Bring
// the stack up first:  docker compose --profile console up -d
export default defineConfig({
  testDir: './e2e',
  timeout: 120_000,
  expect: { timeout: 10_000 },
  reporter: 'list',
  use: {
    baseURL: process.env.CONSOLE_URL || 'http://127.0.0.1:8787',
    trace: 'on-first-retry',
  },
})
