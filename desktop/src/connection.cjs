function normalizeEngineURL(value, allowInsecure = false) {
  if (typeof value !== 'string' || value.length > 2048) throw new Error('Enter your engine’s address, such as https://sandboxd.example.com.')
  let url
  try {
    url = new URL(value.trim())
  } catch {
    throw new Error('Enter the full address, starting with https://, such as https://sandboxd.example.com.')
  }
  if (
    !['http:', 'https:'].includes(url.protocol) ||
    url.username ||
    url.password ||
    url.search ||
    url.hash ||
    !['/', '/v1', '/v1/'].includes(url.pathname)
  ) {
    throw new Error('Enter just the engine’s address, such as https://sandboxd.example.com, with nothing added to it.')
  }
  const local = ['localhost', '127.0.0.1', '[::1]'].includes(url.hostname)
  if (url.protocol === 'http:' && !local && !allowInsecure)
    throw new Error('Use an address that starts with https://, or turn on Allow unencrypted HTTP.')
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
