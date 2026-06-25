import { afterEach, vi } from 'vitest'
import { cleanup } from '@testing-library/react'

// Unmount React trees and reset the fetch stub between tests.
afterEach(() => {
  cleanup()
  vi.restoreAllMocks()
})
