import { test } from 'node:test'
import assert from 'node:assert/strict'
import { landingSummary } from './share.js'

test('landing summary keeps protocol host and port', () => {
  const encoded = 'ss://2022-blake3-aes-128-gcm%3Akey%3D%3D@203.0.113.15:43162#pro'
  assert.equal(landingSummary(encoded), 'SS2022  203.0.113.15:43162')
  assert.equal(landingSummary(encoded).includes('key'), false)
  assert.equal(
    landingSummary('vless://11111111-1111-1111-1111-111111111111@ex.example:443?type=tcp#n'),
    'VLESS  ex.example:443',
  )
  assert.equal(landingSummary('socks5://u:p@198.51.100.8:1080'), 'SK5  198.51.100.8:1080')
  assert.equal(landingSummary('not a link'), '')
})
