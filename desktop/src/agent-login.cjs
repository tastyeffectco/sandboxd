const fs = require('node:fs/promises')
const path = require('node:path')
const os = require('node:os')
const { execFile } = require('node:child_process')
const { promisify } = require('node:util')

const files = {
  'claude-code': ['.claude', '.credentials.json'],
  codex: ['.codex', 'auth.json'],
  opencode: ['.local', 'share', 'opencode', 'auth.json'],
  pi: ['.pi', 'agent', 'auth.json'],
}
const limit = 65536
// Only known CLI credential locations are read. No renderer-supplied path,
// command, keychain service or environment expansion is accepted.
function createAgentLogins({
  home = os.homedir(),
  platform = process.platform,
  run = promisify(execFile),
} = {}) {
  async function read(agent) {
    if (!Object.hasOwn(files, agent)) throw new Error('Unsupported coding agent.')
    let raw
    try {
      const file = await fs.open(path.join(home, ...files[agent]), 'r')
      try {
        const stat = await file.stat()
        if (!stat.isFile() || stat.size > limit) throw new Error('Invalid login file.')
        const buffer = Buffer.alloc(limit + 1)
        const { bytesRead } = await file.read(buffer, 0, buffer.length, 0)
        if (bytesRead > limit) throw new Error('Login file is too large.')
        raw = buffer.subarray(0, bytesRead).toString('utf8')
      } finally {
        await file.close()
      }
    } catch (error) {
      if (error.code !== 'ENOENT' || platform !== 'darwin' || agent !== 'claude-code') throw error
      const result = await run(
        '/usr/bin/security',
        ['find-generic-password', '-s', 'Claude Code-credentials', '-w'],
        { timeout: 5000, maxBuffer: limit },
      )
      raw = result.stdout
    }
    const doc = JSON.parse(raw)
    const valid =
      agent === 'claude-code'
        ? doc?.claudeAiOauth?.accessToken
        : agent === 'codex'
          ? doc?.tokens?.access_token
          : doc &&
            typeof doc === 'object' &&
            Object.values(doc).some(
              (c) =>
                c && ((c.type === 'oauth' && c.access) || (['api', 'api_key'].includes(c.type) && c.key)),
            )
    if (!valid) throw new Error('No usable account login found.')
    return raw
  }
  return {
    async available(agent) {
      try {
        await read(agent)
        return { available: true }
      } catch {
        return { available: false }
      }
    },
    async credential(agent) {
      try {
        return await read(agent)
      } catch {
        throw new Error('No usable local login found. Sign in with this coding agent, then retry.')
      }
    },
  }
}
module.exports = { createAgentLogins }
