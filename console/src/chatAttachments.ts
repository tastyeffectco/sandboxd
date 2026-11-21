// Files attached in a project chat (the sandboxd agent's, or an app's): saved
// in the project's hidden .attachments folder, then named at the end of the
// message so the coding agent can open them. No engine change: the agent
// already reads its own workspace.
export const ATTACH_DIR = '.attachments'
export const MAX_FILES = 10
export const MAX_FILE_BYTES = 25 * 1024 * 1024
const MARK = /\n\nAttached files \(saved in [^)]*\): (.+)$/s

// A safe, unique file name inside .attachments.
export function attachmentPath(name: string, now = new Date()) {
  const stamp = now.toISOString().replace(/[-:]/g, '').replace(/\..*$/, '').replace('T', '-')
  const safe = (name || 'file').replace(/[^\w.\- ]+/g, '_').replace(/\s+/g, '-').slice(-80) || 'file'
  return `${ATTACH_DIR}/${stamp}-${safe}`
}

// The message the agent gets: the person's words, then where the files are.
export function withAttachments(text: string, paths: string[], where: string) {
  const words = text.trim() || 'Here are some files.'
  if (!paths.length) return words
  return `${words}\n\nAttached files (saved in ${where}): ${paths.join(', ')}`
}

// Split a sent message back into the person's words and the files' names.
export function splitAttachments(prompt: string): { text: string; files: string[] } {
  const m = MARK.exec(prompt)
  if (!m) return { text: prompt, files: [] }
  return {
    text: prompt.slice(0, m.index),
    files: m[1].split(', ').map((p) => p.replace(/^.*\//, '').replace(/^\d{8}-\d{6}-/, '')),
  }
}

export function fileSize(n: number): string {
  if (n >= 1024 * 1024) return `${(n / 1024 / 1024).toFixed(1)} MB`
  if (n >= 1024) return `${Math.round(n / 1024)} KB`
  return `${n} bytes`
}
