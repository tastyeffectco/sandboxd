import { describe, expect, it } from 'vitest'
import { attachmentPath, splitAttachments, withAttachments } from './chatAttachments'

describe('chat attachments', () => {
  it('saves each file under .attachments with a safe, unique name', () => {
    const p = attachmentPath('my photo (1).png', new Date('2026-11-21T18:05:09Z'))
    expect(p).toBe('.attachments/20261121-180509-my-photo-_1_.png')
    expect(attachmentPath('../../etc/passwd')).toMatch(/^\.attachments\/\d{8}-\d{6}-\.\._\.\._etc_passwd$/)
  })

  it('names the files for the agent and reads them back for the chat', () => {
    const sent = withAttachments('Use this logo', ['.attachments/20261121-180509-logo.png'], 'this project')
    expect(sent).toBe('Use this logo\n\nAttached files (saved in this project): .attachments/20261121-180509-logo.png')
    expect(splitAttachments(sent)).toEqual({ text: 'Use this logo', files: ['logo.png'] })
  })

  it('sends files with no words, and leaves plain messages alone', () => {
    expect(withAttachments('', ['.attachments/a.pdf'], 'your workspace')).toMatch(/^Here are some files\./)
    expect(splitAttachments('hello')).toEqual({ text: 'hello', files: [] })
  })
})
