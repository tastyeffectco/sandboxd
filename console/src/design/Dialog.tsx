import { ReactNode, useEffect, useId, useRef } from 'react'
import { Icon } from './Icon'

export function Dialog({
  title,
  children,
  close,
}: {
  title: string
  children: ReactNode
  close: () => void
}) {
  const ref = useRef<HTMLDialogElement>(null)
  const label = useId()
  useEffect(() => {
    const previous = document.activeElement as HTMLElement | null
    ref.current?.showModal()
    return () => {
      ref.current?.close()
      previous?.focus()
    }
  }, [])
  return (
    <dialog
      ref={ref}
      className="studio-dialog"
      aria-labelledby={label}
      onCancel={(e) => {
        e.preventDefault()
        close()
      }}
      onClick={(e) => {
        if (e.target === e.currentTarget) {
          const r = e.currentTarget.getBoundingClientRect()
          if (e.clientX < r.left || e.clientX > r.right || e.clientY < r.top || e.clientY > r.bottom) close()
        }
      }}
    >
      <header>
        <h2 id={label}>{title}</h2>
        <button type="button" className="icon-button" aria-label="Close dialog" onClick={close}>
          <Icon name="close" />
        </button>
      </header>
      {children}
    </dialog>
  )
}
