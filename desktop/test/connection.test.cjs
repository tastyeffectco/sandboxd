const { test } = require('node:test')
const assert = require('node:assert/strict')
const { normalizeEngineURL, externalURL } = require('../src/connection.cjs')

test('engine origins are normalized without accepting credentials or arbitrary paths', () => {
  assert.equal(normalizeEngineURL(' https://engine.example/v1/ '), 'https://engine.example')
  for (const value of ['http://localhost:9090', 'http://127.0.0.1:9090', 'http://[::1]:9090'])
    assert.equal(normalizeEngineURL(value), value)
  for (const value of [
    null,
    {},
    'file:///tmp/app',
    'javascript:alert(1)',
    'https://user:secret@engine.example',
    'https://engine.example/preview',
    'https://engine.example/?key=secret',
    'https://engine.example/#secret',
    'http://localhost.evil.example',
    'http://192.168.1.1',
  ])
    assert.throws(() => normalizeEngineURL(value))
  assert.equal(normalizeEngineURL('http://192.168.1.1:9090', true), 'http://192.168.1.1:9090')
})
test('external links cannot open executable or local-file schemes', () => {
  for (const value of [
    'file:///etc/passwd',
    'javascript:alert(1)',
    'data:text/html,test',
    'ms-settings:',
    'https://user:secret@example.com',
    null,
  ])
    assert.equal(externalURL(value), null)
  assert.equal(externalURL('https://example.com/docs'), 'https://example.com/docs')
})
