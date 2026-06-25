import { defineConfig } from 'vitest/config'
import react from '@vitejs/plugin-react'

// Lightweight client smoke tests. sandboxd is the contract; these render the
// console against fixtures that mirror real /v1 responses (see src/test).
export default defineConfig({
  plugins: [react()],
  test: {
    environment: 'jsdom',
    setupFiles: ['./src/test/setup.ts'],
    include: ['src/**/*.test.{ts,tsx}'],
  },
})
