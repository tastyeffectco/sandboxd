import { afterEach, describe, expect, it, vi } from 'vitest'
import { renderHook } from '@testing-library/react'
import { confirmLeaveWorkspace, useUnsavedChanges } from './unsavedChanges'

const unload = () => {
  const event = new Event('beforeunload', { cancelable: true })
  window.dispatchEvent(event)
  return event.defaultPrevented
}

afterEach(() => vi.restoreAllMocks())

describe('unsaved changes', () => {
  it('leaves without asking when nothing is being edited', () => {
    const confirm = vi.spyOn(window, 'confirm')
    renderHook(() => useUnsavedChanges(false))
    expect(confirmLeaveWorkspace()).toBe(true)
    expect(confirm).not.toHaveBeenCalled()
    expect(unload()).toBe(false)
  })

  it('asks before leaving with unsaved edits, and warns when the page closes', () => {
    const confirm = vi.spyOn(window, 'confirm').mockReturnValue(false)
    const editor = renderHook(({ dirty }) => useUnsavedChanges(dirty), { initialProps: { dirty: true } })
    expect(confirmLeaveWorkspace()).toBe(false)
    expect(confirm).toHaveBeenCalledWith('Leave this project and discard unsaved file or memory edits?')
    confirm.mockReturnValue(true)
    expect(confirmLeaveWorkspace()).toBe(true)
    expect(unload()).toBe(true)

    // Saved: no more questions.
    editor.rerender({ dirty: false })
    confirm.mockClear()
    expect(confirmLeaveWorkspace()).toBe(true)
    expect(confirm).not.toHaveBeenCalled()
    expect(unload()).toBe(false)
    editor.unmount()
  })

  it('keeps asking while any editor, even a hidden one, still has edits', () => {
    const confirm = vi.spyOn(window, 'confirm').mockReturnValue(false)
    const files = renderHook(() => useUnsavedChanges(true))
    const memory = renderHook(() => useUnsavedChanges(true))
    files.unmount()
    expect(confirmLeaveWorkspace()).toBe(false)
    expect(unload()).toBe(true)
    memory.unmount()
    confirm.mockClear()
    expect(confirmLeaveWorkspace()).toBe(true)
    expect(confirm).not.toHaveBeenCalled()
    expect(unload()).toBe(false)
  })
})
