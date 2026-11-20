import { useMemo, useRef } from 'react'
import CodeMirror from '@uiw/react-codemirror'
import { EditorView, keymap } from '@codemirror/view'
import { Prec, type Extension } from '@codemirror/state'
import { javascript } from '@codemirror/lang-javascript'
import { python } from '@codemirror/lang-python'
import { html } from '@codemirror/lang-html'
import { css } from '@codemirror/lang-css'
import { json } from '@codemirror/lang-json'
import { markdown } from '@codemirror/lang-markdown'

// Language selection by file extension. Unknown types fall back to plain text
// (still line-numbered + editable). Add packs here to grow coverage.
function langFor(path: string): Extension[] {
  const ext = path.split('.').pop()?.toLowerCase() || ''
  if (['js', 'jsx', 'mjs', 'cjs'].includes(ext)) return [javascript({ jsx: true })]
  if (['ts', 'tsx'].includes(ext)) return [javascript({ jsx: ext === 'tsx', typescript: true })]
  if (ext === 'py') return [python()]
  if (['html', 'htm', 'vue', 'svelte'].includes(ext)) return [html()]
  if (['css', 'scss', 'less'].includes(ext)) return [css()]
  if (ext === 'json') return [json()]
  if (['md', 'markdown', 'mdx'].includes(ext)) return [markdown()]
  return []
}

// The editor in the console's dark theme (syntax colours from One Dark,
// surfaces and selection from the design tokens).
const theme = EditorView.theme(
  {
    '&': { fontSize: '13px', backgroundColor: 'var(--bg-2)', color: 'var(--fg-2)' },
    '&.cm-focused': { outline: 'none' },
    '.cm-scroller': { fontFamily: 'var(--mono)', lineHeight: '1.65' },
    '.cm-gutters': {
      backgroundColor: 'var(--bg-2)',
      color: 'var(--faint)',
      border: 'none',
      borderRight: '1px solid var(--border)',
    },
    '.cm-activeLineGutter': { backgroundColor: 'rgba(155, 123, 255, 0.08)', color: 'var(--fg-2)' },
    '.cm-activeLine': { backgroundColor: 'rgba(255, 255, 255, 0.025)' },
    '.cm-selectionBackground, &.cm-focused .cm-selectionBackground, ::selection': {
      backgroundColor: 'rgba(155, 123, 255, 0.32) !important',
    },
    '.cm-cursor': { borderLeftColor: 'var(--violet)' },
  },
  { dark: true },
)

// CodeEditor — a real code editor (CodeMirror 6) with syntax highlighting,
// bracket matching, search, and Cmd/Ctrl+S to save. Fully client-side; no
// sandboxd-core change. Save fires through a ref so the keymap always calls the
// latest handler without re-creating the editor.
export function CodeEditor({ path, value, onChange, onSave, height = '560px' }: {
  path: string
  value: string
  onChange: (v: string) => void
  onSave: () => void
  height?: string
}) {
  const saveRef = useRef(onSave)
  saveRef.current = onSave

  const extensions = useMemo<Extension[]>(() => [
    ...langFor(path),
    theme,
    Prec.highest(keymap.of([{ key: 'Mod-s', preventDefault: true, run: () => { saveRef.current(); return true } }])),
  ], [path])

  return (
    <CodeMirror
      value={value}
      onChange={onChange}
      extensions={extensions}
      theme="dark"
      height={height}
      basicSetup={{ highlightActiveLine: true, foldGutter: false, autocompletion: false }}
    />
  )
}
