// Server tools: approvals in front of every change, and write/undo run for real
// in a local shell against a temporary folder (standing in for the server).
const { test } = require('node:test')
const assert = require('node:assert/strict')
const { execFile } = require('node:child_process')
const fs = require('node:fs/promises')
const os = require('node:os')
const path = require('node:path')
const { createServerTools } = require('../src/server-tools.cjs')

const SERVER = { id: 'srv_0123456789ab', name: 'My VPS', username: 'ubuntu' }
// The tools drive a Linux server's shell; these tests stand one in with the
// local POSIX shell, which Windows doesn't have.
const posix = { skip: process.platform === 'win32' && 'needs a POSIX shell' }
const testPosix = (name, fn) => test(name, posix, fn)

// servers.run, executed by the local bash. `admin` is recorded, not elevated.
function localServers({ needsPassword = false } = {}) {
  const calls = []
  return {
    calls,
    list: () => [SERVER],
    run(id, command, options = {}) {
      calls.push({ id, command, ...options })
      if (id !== SERVER.id) return Promise.reject(Object.assign(new Error('unknown'), { code: 'unknown' }))
      if (options.admin && needsPassword && !options.password)
        return Promise.reject(Object.assign(new Error('needs password'), { code: 'needs_password' }))
      return new Promise((resolve) =>
        execFile('bash', ['-c', command], { maxBuffer: 1 << 24 }, (error, stdout, stderr) =>
          resolve({ code: error ? (error.code ?? 1) : 0, output: stdout + stderr }),
        ),
      )
    },
  }
}

async function setup(t, options = {}) {
  const root = await fs.mkdtemp(path.join(os.tmpdir(), 'sandboxd-tools-'))
  t.after(() => fs.rm(root, { recursive: true, force: true }))
  const servers = localServers(options)
  const tools = createServerTools({ servers, backupRoot: path.join(root, 'backups'), ...options })
  const events = []
  tools.subscribe((event) => events.push(event))
  // Answers the next approval as told.
  const answer = (decision) =>
    new Promise((resolve) => {
      const stop = tools.subscribe((event) => {
        if (event.type !== 'approval') return
        stop()
        resolve(event.approval)
        tools.decide(event.approval.id, decision)
      })
    })
  return { root, servers, tools, events, answer }
}

testPosix('run asks first, shows the reason and the exact command, and runs only when allowed', async (t) => {
  const { tools, servers, answer, root } = await setup(t)
  const marker = path.join(root, 'ran')
  const asked = answer({ allow: false })
  const no = await tools.call('run', { server: SERVER.id, command: `touch ${marker}`, why: 'Test it' })
  const approval = await asked
  assert.equal(approval.title, 'Test it')
  assert.equal(approval.command, `touch ${marker}`)
  assert.equal(approval.serverName, 'My VPS')
  assert.equal(no.ok, false)
  assert.match(no.message, /said no/)
  assert.equal(servers.calls.length, 0)
  await assert.rejects(fs.stat(marker))

  answer({ allow: true })
  const yes = await tools.call('run', { server: SERVER.id, command: `touch ${marker}; echo done`, why: 'Test it' })
  assert.deepEqual([yes.ok, yes.output], [true, 'done\n'])
  await fs.stat(marker)
  assert.deepEqual(
    tools.activity(SERVER.id).map((e) => [e.kind, !!e.declined, e.ok]),
    [
      ['run', true, undefined],
      ['run', false, true],
    ],
  )
})

testPosix('a reason is required, and unanswered questions are a no', async (t) => {
  const { tools, servers } = await setup(t, { approvalTimeout: 50 })
  await assert.rejects(tools.call('run', { server: SERVER.id, command: 'ls' }), /why this command/)
  const result = await tools.call('run', { server: SERVER.id, command: 'ls', why: 'List' })
  assert.equal(result.ok, false)
  assert.match(result.message, /did not answer/)
  assert.equal(servers.calls.length, 0)
  await assert.rejects(tools.call('nope', {}), /Unknown tool/)
})

testPosix('closing the app declines everything still open', async (t) => {
  const { tools } = await setup(t)
  const pending = tools.call('run', { server: SERVER.id, command: 'ls', why: 'List' })
  await new Promise((r) => setImmediate(r))
  assert.equal(tools.pending().length, 1)
  tools.close()
  assert.equal((await pending).ok, false)
  assert.equal(tools.pending().length, 0)
})

testPosix('admin changes on a sudo login ask for the password, used for that command only', async (t) => {
  const { tools, servers, answer } = await setup(t, { needsPassword: true })
  const asked = answer({ allow: true, password: 'hunter2' })
  const result = await tools.call('run', { server: SERVER.id, command: 'echo ok', why: 'Reload', admin: true })
  assert.equal((await asked).needsPassword, true)
  assert.equal(result.ok, true)
  assert.equal(servers.calls.at(-1).password, 'hunter2')
  assert(!JSON.stringify(tools.activity(SERVER.id)).includes('hunter2'))
  // Allowed without the password: the server refuses, said plainly.
  answer({ allow: true })
  await assert.rejects(
    tools.call('run', { server: SERVER.id, command: 'echo ok', why: 'Reload', admin: true }),
    { code: 'needs_password' },
  )
})

testPosix('web server configuration is read freely; other files ask first', async (t) => {
  const { tools, root, answer, events } = await setup(t)
  const secret = path.join(root, 'secret.txt')
  await fs.writeFile(secret, 'token=abc')
  const asked = answer({ allow: false })
  const refused = await tools.call('read', { server: SERVER.id, path: secret, why: 'Check it' })
  assert.equal((await asked).kind, 'read')
  assert.equal(refused.ok, false)
  answer({ allow: true })
  assert.equal((await tools.call('read', { server: SERVER.id, path: secret })).content, 'token=abc')
  const approvals = events.filter((e) => e.type === 'approval').length
  await tools.call('read', { server: SERVER.id, path: '/etc/hostname' })
  assert.equal(events.filter((e) => e.type === 'approval').length, approvals, '/etc/hostname needs no approval')
  await assert.rejects(tools.call('read', { server: SERVER.id, path: '/etc/../shadow' }), /without/)
  await assert.rejects(tools.call('read', { server: SERVER.id, path: 'relative' }), /absolute/)
})

testPosix('write shows before/after, backs up, keeps the mode, and undo puts it back', async (t) => {
  const { tools, root, answer } = await setup(t)
  const site = path.join(root, 'nginx', 'shop.conf')
  await fs.mkdir(path.dirname(site))
  await fs.writeFile(site, 'old config\n')
  await fs.chmod(site, 0o640)

  const asked = answer({ allow: true })
  const content = "server { server_name shop.example.com; } # it's $quoted `stuff`\n"
  const written = await tools.call('write', { server: SERVER.id, path: site, content, why: 'Publish the shop' })
  const approval = await asked
  assert.deepEqual([approval.before, approval.after, approval.path], ['old config\n', content, site])
  assert.equal(written.ok, true, written.output)
  assert.match(written.change, /^ch_\d{14}[a-f0-9]{6}$/)
  assert.equal(await fs.readFile(site, 'utf8'), content)
  assert.equal((await fs.stat(site)).mode & 0o777, 0o640)

  answer({ allow: true })
  const undone = await tools.call('undo', { server: SERVER.id, change: written.change })
  assert.equal(undone.ok, true, undone.output)
  assert.equal(await fs.readFile(site, 'utf8'), 'old config\n')

  // A new file: shown as new, and undo removes it.
  const fresh = path.join(root, 'caddy', 'new.caddy')
  const askedNew = answer({ allow: true })
  const created = await tools.call('write', { server: SERVER.id, path: fresh, content: 'x {}\n', why: 'Add' })
  assert.equal((await askedNew).before, null)
  answer({ allow: true })
  await tools.call('undo', { server: SERVER.id, change: created.change })
  await assert.rejects(fs.stat(fresh))

  await assert.rejects(
    tools.call('write', { server: SERVER.id, path: site, content: 'x'.repeat(70000), why: 'Big' }),
    /64 KB/,
  )
  await assert.rejects(tools.call('undo', { server: SERVER.id, change: '../../etc' }), /change id/)
})

testPosix('declined writes change nothing', async (t) => {
  const { tools, root, answer } = await setup(t)
  const site = path.join(root, 'a.conf')
  await fs.writeFile(site, 'keep\n')
  answer({ allow: false })
  const result = await tools.call('write', { server: SERVER.id, path: site, content: 'new\n', why: 'Change' })
  assert.equal(result.ok, false)
  assert.equal(await fs.readFile(site, 'utf8'), 'keep\n')
  await assert.rejects(fs.stat(path.join(root, 'backups')))
})

testPosix('look runs the read-only survey without asking', async (t) => {
  const { tools, events } = await setup(t)
  const result = await tools.call('look', { server: SERVER.id })
  assert.equal(result.ok, true)
  assert.match(result.facts, /== system/)
  assert.match(result.facts, /== listening on 80\/443/)
  assert.equal(events.filter((e) => e.type === 'approval').length, 0)
  await assert.rejects(tools.call('look', { server: 'srv_ffffffffffff' }), /isn’t in your list/)
})
