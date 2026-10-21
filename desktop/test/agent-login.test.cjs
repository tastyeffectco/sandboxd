const { test } = require('node:test')
const assert = require('node:assert/strict')
const fs = require('node:fs/promises')
const os = require('node:os')
const path = require('node:path')
const { createAgentLogins } = require('../src/agent-login.cjs')

test('local discovery returns only availability and never accepts arbitrary paths', async (t) => {
  const home = await fs.mkdtemp(path.join(os.tmpdir(), 'sandboxd-agent-login-'))
  t.after(() => fs.rm(home, { recursive: true }))
  await fs.mkdir(path.join(home, '.codex'))
  const credential = '{"tokens":{"access_token":"private-token"}}'
  await fs.writeFile(path.join(home, '.codex', 'auth.json'), credential)
  const login = createAgentLogins({ home, platform: 'linux' })
  assert.deepEqual(await login.available('codex'), { available: true })
  assert.equal(await login.credential('codex'), credential)
  assert.deepEqual(await login.available('../../secret'), { available: false })
  await assert.rejects(login.credential('../../secret'), /No usable local login/)
  await fs.writeFile(path.join(home, '.codex', 'auth.json'), 'x'.repeat(70000))
  assert.deepEqual(await login.available('codex'), { available: false })
})

test('macOS Claude keychain lookup uses one fixed service and keeps tokens out of discovery', async (t) => {
  const home = await fs.mkdtemp(path.join(os.tmpdir(), 'sandboxd-agent-keychain-'))
  t.after(() => fs.rm(home, { recursive: true }))
  const login = createAgentLogins({
    home,
    platform: 'darwin',
    run: async (command, args) => {
      assert.equal(command, '/usr/bin/security')
      assert.deepEqual(args, ['find-generic-password', '-s', 'Claude Code-credentials', '-w'])
      return { stdout: '{"claudeAiOauth":{"accessToken":"private"}}' }
    },
  })
  assert.deepEqual(await login.available('claude-code'), { available: true })
  assert.deepEqual(await login.available('codex'), { available: false })
})
