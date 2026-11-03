// The sandboxd tools as MCP (JSON-RPC 2.0) for the desktop's OpenCode, which
// reaches them over loopback HTTP (assistant.cjs serves them). Nothing here
// holds a key or a password: every call goes to server-tools.cjs, which signs
// in to the server and asks the user before any change.
const why = {
  type: 'string',
  description:
    'One plain sentence for the user, saying what this does and why (they read it before allowing it). No jargon.',
}
const admin = { type: 'boolean', description: 'Run as root (sudo). Only when the change needs it.' }

const TOOLS = [
  {
    name: 'look',
    description:
      "Survey the user's server, read-only: OS, login and sudo, what listens on ports 80/443, installed web servers (nginx, Caddy, Apache) and their site files, Docker containers, sandboxd's settings, disk and memory. Start here.",
    inputSchema: { type: 'object', properties: {}, additionalProperties: false },
  },
  {
    name: 'read',
    description:
      'Read one text file on the server (up to 256 KB). Web server configuration under /etc/nginx, /etc/caddy, /etc/apache2 and system facts are read directly; any other file asks the user first.',
    inputSchema: {
      type: 'object',
      properties: { path: { type: 'string', description: 'Absolute path' }, why },
      required: ['path'],
      additionalProperties: false,
    },
  },
  {
    name: 'run',
    description:
      'Run one shell command on the server (bash). The user sees `why` and the exact command and must allow it first. Prefer checks before changes (for example `nginx -t` before reloading). Returns the exit code and output.',
    inputSchema: {
      type: 'object',
      properties: { command: { type: 'string' }, why, admin },
      required: ['command', 'why'],
      additionalProperties: false,
    },
  },
  {
    name: 'write',
    description:
      'Create or replace one text file on the server (up to 64 KB). The user sees `why` and the before/after and must allow it first. The previous file is backed up; the result has a change id that `undo` uses.',
    inputSchema: {
      type: 'object',
      properties: { path: { type: 'string', description: 'Absolute path' }, content: { type: 'string' }, why, admin },
      required: ['path', 'content', 'why'],
      additionalProperties: false,
    },
  },
  {
    name: 'undo',
    description: 'Put back a file changed by `write`, using its change id. Asks the user first.',
    inputSchema: {
      type: 'object',
      properties: { change: { type: 'string' }, why, admin },
      required: ['change'],
      additionalProperties: false,
    },
  },
]

// One JSON-RPC message → its response, or null for a notification. `call`
// runs a tool: (name, args) => result object.
async function handle(message, call) {
  const { id, method, params } = message || {}
  const reply = (body) => (id === undefined || id === null ? null : { jsonrpc: '2.0', id, ...body })
  if (method === 'initialize')
    return reply({
      result: {
        protocolVersion: typeof params?.protocolVersion === 'string' ? params.protocolVersion : '2025-06-18',
        capabilities: { tools: {} },
        serverInfo: { name: 'sandboxd', version: '1' },
      },
    })
  if (method === 'ping') return reply({ result: {} })
  if (method === 'tools/list') return reply({ result: { tools: TOOLS } })
  if (method === 'tools/call') {
    const name = params?.name
    if (!TOOLS.some((t) => t.name === name))
      return reply({ result: { isError: true, content: [{ type: 'text', text: `Unknown tool: ${name}` }] } })
    let result
    try {
      result = await call(name, params?.arguments || {})
    } catch (error) {
      result = { ok: false, message: error?.message || 'The tool failed.' }
    }
    return reply({
      result: { isError: result?.ok === false, content: [{ type: 'text', text: JSON.stringify(result, null, 1) }] },
    })
  }
  return reply({ error: { code: -32601, message: `Unknown method: ${method}` } })
}

module.exports = { TOOLS, handle }
