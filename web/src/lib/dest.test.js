import { test } from 'node:test'
import assert from 'node:assert/strict'
import { readFileSync } from 'node:fs'
import { dirname, join } from 'node:path'
import { fileURLToPath } from 'node:url'
import {
  destHostOf,
  destLabel,
  formatDest,
  serverIsOnline,
  sortDests,
} from './dest.js'

test('formatDest keeps port and defaults 443', () => {
  assert.equal(formatDest('azure.microsoft.com'), 'azure.microsoft.com:443')
  assert.equal(formatDest('j.6sc.co:443'), 'j.6sc.co:443')
  assert.equal(formatDest('  www.cloudflare.com:443  '), 'www.cloudflare.com:443')
})

test('destLabel matches openssl screening', () => {
  assert.equal(destLabel('azure.microsoft.com', { ok: true, latency_ms: 44 }), 'azure.microsoft.com: 44 ms')
  assert.equal(destLabel('j.6sc.co', { ok: true, latency_ms: 49 }), 'j.6sc.co: 49 ms')
  assert.equal(destLabel('assets-www.xbox.com', { ok: false }), 'assets-www.xbox.com: timeout')
  assert.equal(destLabel('cdn.userway.org', null, true), 'cdn.userway.org: …')
  assert.equal(destLabel('www.apple.com', null, false), 'www.apple.com')
})

test('destHostOf strips port', () => {
  assert.equal(destHostOf('azure.microsoft.com:443'), 'azure.microsoft.com')
  assert.equal(destHostOf('azure.microsoft.com'), 'azure.microsoft.com')
})

test('serverIsOnline treats panel online int', () => {
  assert.equal(serverIsOnline({ online: 1 }), true)
  assert.equal(serverIsOnline({ online: 0 }), false)
  assert.equal(serverIsOnline({ online: true }), true)
  assert.equal(serverIsOnline(undefined), false)
  assert.equal(serverIsOnline({}), false)
})

test('sortDests lowest ms first, timeout last', () => {
  const dests = ['www.cloudflare.com:443', 'azure.microsoft.com:443', 'j.6sc.co:443']
  const rows = {
    'azure.microsoft.com:443': { ok: true, latency_ms: 44 },
    'j.6sc.co:443': { ok: true, latency_ms: 49 },
    'www.cloudflare.com:443': { ok: false },
  }
  assert.deepEqual(sortDests(dests, rows), [
    'azure.microsoft.com:443',
    'j.6sc.co:443',
    'www.cloudflare.com:443',
  ])
})

test('dest picker has no blocking hint bar', () => {
  const css = readFileSync(join(dirname(fileURLToPath(import.meta.url)), '../index.css'), 'utf8')
  const dest = css.slice(css.indexOf('.dest-picker-row {'), css.indexOf('.badge {'))
  assert.equal(dest.includes('dest-menu-bar'), false)
  assert.equal(dest.includes('.dest-menu button'), false)
  assert.match(dest, /\.dest-picker-row\s*\{[^}]*display:\s*flex/)
  assert.match(dest, /\.dest-picker-probe\s*\{[^}]*width:\s*auto/)
  assert.match(dest, /\.dest-menu-item\s*\{/)
})
