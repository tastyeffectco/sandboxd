// Server tools for the assistant: what it may do on one of your servers, and
// the user's approval in front of every change.
//
// - `look` runs a fixed, read-only survey (no approval; shown in the activity).
// - `read` reads one text file. Web server configuration and system facts are
//   read freely; anything else asks first, because the file goes to the model.
// - `run` and `write` always ask. The approval shows the assistant's reason in
//   its words, then exactly what will run or change. Nothing runs if the user
//   says no, closes the app, or doesn't answer.
// - Every file `write` replaces is copied to /var/backups/sandboxd/<change>/
//   first, and `undo` puts it back (also after approval).
//
// The assistant never sees the SSH key or the server password: the desktop
// signs in for it, and a password typed into an approval is used for that one
// command only.
const { randomBytes } = require('node:crypto')

const FREE_READ = [
  /^\/etc\/(nginx|caddy|apache2|httpd|letsencrypt\/renewal)\/[A-Za-z0-9._/-]+$/,
  /^\/etc\/(os-release|hostname|hosts)$/,
]
const PATH = /^\/[A-Za-z0-9._/@+-]{1,1023}$/
const MAX_READ = 256 * 1024
// The content travels base64-encoded inside one `bash -c` argument, and Linux
// caps one argument at 128 KB.
const MAX_WRITE = 64 * 1024
const MAX_LOG = 200
const APPROVAL_TIMEOUT = 15 * 60 * 1000

// Read-only facts the assistant starts from. Every probe tolerates absence.
const LOOK = [
  `printf '== system\\n'; . /etc/os-release 2>/dev/null; printf 'os=%s %s\\narch=%s\\nuser=%s uid=%s\\n' "$NAME" "$VERSION_ID" "$(uname -m)" "$(id -un)" "$(id -u)"`,
  `printf 'sudo_without_password=%s\\n' "$( [ "$(id -u)" = 0 ] && echo root || { sudo -n true 2>/dev/null && echo yes || echo no; })"`,
  `printf '== listening on 80/443\\n'; (ss -ltnpH '( sport = :80 or sport = :443 )' 2>/dev/null || ss -ltnH '( sport = :80 or sport = :443 )' 2>/dev/null) | awk '{print $4, $6}'`,
  `printf '== web servers installed\\n'; for b in nginx caddy apache2 httpd certbot; do command -v $b >/dev/null 2>&1 && printf '%s %s\\n' $b "$($b -v 2>&1 | head -n1)"; done; true`,
  `printf '== nginx sites\\n'; ls -1 /etc/nginx/sites-enabled /etc/nginx/conf.d 2>/dev/null; true`,
  `printf '== caddy\\n'; ls -1 /etc/caddy 2>/dev/null; true`,
  `printf '== docker containers\\n'; docker ps --format '{{.Names}}\\t{{.Image}}\\t{{.Ports}}' 2>/dev/null || echo 'docker not reachable as this user'`,
  `printf '== sandboxd\\n'; for d in /opt/sandboxd "$HOME/sandboxd" /root/sandboxd; do [ -f "$d/.env" ] && { printf 'dir=%s\\n' "$d"; grep -E '^(SANDBOXD_FRONT|PREVIEW_DOMAIN|PREVIEW_URL_SCHEME|SANDBOXD_HTTP_BIND|HTTP_PORT)=' "$d/.env" 2>/dev/null; }; done; true`,
  `printf '== disk and memory\\n'; df -h / 2>/dev/null | tail -n1; free -h 2>/dev/null | sed -n 2p`,
].join('; ')

class ToolError extends Error {
  constructor(code, message) {
    super(message)
    this.code = code
  }
}

const shellQuote = (value) => `'${String(value).replace(/'/g, `'\\''`)}'`
const text = (value, max) => (typeof value === 'string' ? value.trim().slice(0, max) : '')

function checkPath(path) {
  if (typeof path !== 'string' || !PATH.test(path) || path.split('/').includes('..'))
    throw new ToolError('invalid', 'The file path must be absolute, without "..".')
  return path
}

function createServerTools({
  servers,
  approvalTimeout = APPROVAL_TIMEOUT,
  now = () => new Date(),
  backupRoot = '/var/backups/sandboxd',
}) {
  const pending = new Map()
  const logs = new Map()
  const listeners = new Set()
  const emit = (event) => {
    for (const listener of listeners) listener(event)
  }
  const record = (serverId, entry) => {
    const log = logs.get(serverId) || []
    log.push({ at: now().toISOString(), ...entry })
    logs.set(serverId, log.slice(-MAX_LOG))
    emit({ type: 'activity', serverId, entry: log.at(-1) })
  }
  const serverName = (serverId) => {
    const server = servers.list().find((s) => s.id === serverId)
    if (!server) throw new ToolError('unknown', 'This server isn’t in your list.')
    return server
  }

  // Resolves {allow, password} once the user answers; a timeout is a no.
  function ask(serverId, request) {
    const server = serverName(serverId)
    const id = 'ap_' + randomBytes(8).toString('hex')
    const approval = {
      id,
      serverId,
      serverName: server.name,
      login: server.username,
      needsPassword: !!request.admin && server.username !== 'root',
      ...request,
    }
    return new Promise((resolve) => {
      const timer = setTimeout(() => decide(id, { allow: false, reason: 'timeout' }), approvalTimeout)
      pending.set(id, { approval, resolve, timer })
      emit({ type: 'approval', approval })
    })
  }

  function decide(id, { allow, password, reason } = {}) {
    const entry = pending.get(id)
    if (!entry) return false
    pending.delete(id)
    clearTimeout(entry.timer)
    emit({ type: 'decided', id, allow: !!allow })
    entry.resolve({
      allow: allow === true,
      password: typeof password === 'string' && password ? password : undefined,
      reason: reason || (allow ? 'allowed' : 'declined'),
    })
    return true
  }

  const declined = (decision) =>
    decision.reason === 'timeout'
      ? 'The user did not answer, so nothing was changed. Ask again when they are back.'
      : 'The user said no, so nothing was changed. Ask what they would prefer, or explain another way.'

  // Runs after approval; a refused sudo password asks once more.
  async function runApproved(serverId, command, { admin, password, timeout }) {
    try {
      return await servers.run(serverId, command, { admin, password, timeout })
    } catch (error) {
      if (error.code === 'needs_password') throw new ToolError('needs_password', error.message)
      throw error
    }
  }

  const outcome = (result) => ({
    ok: result.code === 0,
    exitCode: result.code,
    output: result.output.slice(-16000),
  })

  const tools = {
    async look({ server }) {
      serverName(server)
      const result = await servers.run(server, LOOK, { timeout: 60000 })
      record(server, { kind: 'look', title: 'Looked at the server', ok: result.code === 0 })
      return { ok: true, facts: result.output.slice(-16000) }
    },

    async read({ server, path, why }) {
      const file = checkPath(path)
      if (!FREE_READ.some((rule) => rule.test(file))) {
        const decision = await ask(server, {
          kind: 'read',
          title: text(why, 300) || `Read ${file}`,
          detail: `Read the file ${file} and send it to the assistant.`,
          path: file,
        })
        if (!decision.allow) return { ok: false, message: declined(decision) }
      }
      const result = await servers.run(
        server,
        `f=${shellQuote(file)}; [ -f "$f" ] || { echo "no such file: $f" >&2; exit 2; }; head -c ${MAX_READ} "$f"`,
        { timeout: 30000 },
      )
      record(server, { kind: 'read', title: `Read ${file}`, ok: result.code === 0 })
      if (result.code !== 0) return { ok: false, message: result.output.trim().slice(-2000) }
      return { ok: true, content: result.output }
    },

    async run({ server, command, why, admin = false }) {
      if (typeof command !== 'string' || !command.trim() || command.length > 20000)
        throw new ToolError('invalid', 'Give the command to run.')
      const title = text(why, 300)
      if (!title) throw new ToolError('invalid', 'Say in plain words why this command is needed.')
      const decision = await ask(server, { kind: 'run', title, command, admin: !!admin })
      if (!decision.allow) {
        record(server, { kind: 'run', title, command, declined: true })
        return { ok: false, message: declined(decision) }
      }
      const result = await runApproved(server, command, {
        admin: !!admin,
        password: decision.password,
        timeout: 15 * 60 * 1000,
      })
      record(server, { kind: 'run', title, command, ok: result.code === 0 })
      return outcome(result)
    },

    // Replaces one file. The old one, if any, is backed up first; the change id
    // undoes it.
    async write({ server, path, content, why, admin = false }) {
      const file = checkPath(path)
      if (typeof content !== 'string' || Buffer.byteLength(content) > MAX_WRITE)
        throw new ToolError('invalid', 'The file content must be text up to 64 KB.')
      const title = text(why, 300)
      if (!title) throw new ToolError('invalid', 'Say in plain words why this file changes.')
      const current = await servers.run(
        server,
        `f=${shellQuote(file)}; if [ -f "$f" ]; then head -c ${MAX_WRITE} "$f"; else echo '<<sandboxd: new file>>'; fi`,
        { admin: false, timeout: 30000 },
      )
      const before = current.code === 0 ? current.output : ''
      const change = 'ch_' + now().toISOString().replace(/[-:.TZ]/g, '').slice(0, 14) + randomBytes(3).toString('hex')
      const decision = await ask(server, {
        kind: 'write',
        title,
        path: file,
        before: before === '<<sandboxd: new file>>\n' ? null : before,
        after: content,
        admin: !!admin,
      })
      if (!decision.allow) {
        record(server, { kind: 'write', title, path: file, declined: true })
        return { ok: false, message: declined(decision) }
      }
      const backup = `${backupRoot}/${change}`
      const data = Buffer.from(content).toString('base64')
      const command =
        `set -e; f=${shellQuote(file)}; b=${shellQuote(backup)}; ` +
        `mkdir -p "$b"; if [ -e "$f" ]; then cp -a "$f" "$b/file"; echo "$f" > "$b/path"; else echo "$f" > "$b/new"; fi; ` +
        `mkdir -p "$(dirname "$f")"; t="$(mktemp "$f.sandboxd.XXXXXX")"; ` +
        `printf '%s' ${shellQuote(data)} | base64 -d > "$t"; ` +
        // An existing file is overwritten in place, which keeps its owner and
        // mode on any POSIX system; a new one is moved into place.
        `if [ -e "$f" ]; then cat "$t" > "$f"; rm -f "$t"; else chmod 644 "$t"; mv -f "$t" "$f"; fi; ` +
        `echo "changed $f (undo: ${change})"`
      const result = await runApproved(server, command, {
        admin: !!admin,
        password: decision.password,
        timeout: 60000,
      })
      record(server, { kind: 'write', title, path: file, change, ok: result.code === 0 })
      return { ...outcome(result), change: result.code === 0 ? change : undefined }
    },

    async undo({ server, change, why, admin = false }) {
      if (typeof change !== 'string' || !/^ch_[0-9]{14}[a-f0-9]{6}$/.test(change))
        throw new ToolError('invalid', 'Give the change id that `write` returned.')
      const title = text(why, 300) || 'Put a file back the way it was'
      const decision = await ask(server, { kind: 'undo', title, change, admin: !!admin })
      if (!decision.allow) return { ok: false, message: declined(decision) }
      const backup = `${backupRoot}/${change}`
      const command =
        `set -e; b=${shellQuote(backup)}; ` +
        `if [ -f "$b/path" ]; then f="$(cat "$b/path")"; cat "$b/file" > "$f"; echo "restored $f"; ` +
        `elif [ -f "$b/new" ]; then f="$(cat "$b/new")"; rm -f "$f"; echo "removed $f (it was new)"; ` +
        `else echo "no backup for ${change}" >&2; exit 2; fi`
      const result = await runApproved(server, command, {
        admin: !!admin,
        password: decision.password,
        timeout: 60000,
      })
      record(server, { kind: 'undo', title, change, ok: result.code === 0 })
      return outcome(result)
    },
  }

  return {
    tools,
    async call(name, args) {
      if (!Object.hasOwn(tools, name)) throw new ToolError('invalid', `Unknown tool: ${name}`)
      if (!args || typeof args !== 'object') throw new ToolError('invalid', 'Missing arguments.')
      return tools[name](args)
    },
    decide,
    pending: () => [...pending.values()].map((p) => p.approval),
    activity: (serverId) => [...(logs.get(serverId) || [])],
    subscribe(listener) {
      listeners.add(listener)
      return () => listeners.delete(listener)
    },
    // App quit: every open question is a no.
    close() {
      for (const id of [...pending.keys()]) decide(id, { allow: false, reason: 'closed' })
    },
  }
}

module.exports = { createServerTools, ToolError, FREE_READ, LOOK }
