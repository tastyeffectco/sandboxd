function normalizeEngineURL(value, allowInsecure = false) {
  if (typeof value !== 'string' || value.length > 2048) throw new Error('Enter a valid engine URL.')
  let url
  try {
    url = new URL(value.trim())
  } catch {
    throw new Error('Use a complete URL, such as http://127.0.0.1:9090.')
  }
  if (
    !['http:', 'https:'].includes(url.protocol) ||
    url.username ||
    url.password ||
    url.search ||
    url.hash ||
    !['/', '/v1', '/v1/'].includes(url.pathname)
  ) {
    throw new Error('Use the engine origin without credentials, a query, or a path.')
  }
  const local = ['localhost', '127.0.0.1', '[::1]'].includes(url.hostname)
  if (url.protocol === 'http:' && !local && !allowInsecure)
    throw new Error('Use HTTPS for a remote engine, or explicitly allow an unencrypted connection.')
  return url.origin
}

function externalURL(value) {
  try {
    const url = new URL(value)
    return ['https:', 'http:'].includes(url.protocol) && !url.username && !url.password ? url.href : null
  } catch {
    return null
  }
}

module.exports = { normalizeEngineURL, externalURL }
