import { useEffect } from 'react'

const editors = new Set<symbol>()

// Each mounted editor owns its registration, including editors hidden by a tab.
export function useUnsavedChanges(dirty: boolean) {
  useEffect(() => {
    if (!dirty) return
    const id = Symbol('unsaved-editor')
    editors.add(id)
    const warn = (event: BeforeUnloadEvent) => {
      event.preventDefault()
      event.returnValue = ''
    }
    window.addEventListener('beforeunload', warn)
    return () => {
      editors.delete(id)
      window.removeEventListener('beforeunload', warn)
    }
  }, [dirty])
}

export function confirmLeaveWorkspace() {
  return editors.size === 0 || window.confirm('Leave this project and discard unsaved file or memory edits?')
}
