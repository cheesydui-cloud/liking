import { test } from 'node:test'
import assert from 'node:assert/strict'
import { cnAgentInstallCommand, normalizeGhProxy } from './installCmd.js'

test('normalizeGhProxy requires https and a trailing slash', () => {
  assert.equal(normalizeGhProxy('https://gh-proxy.com'), 'https://gh-proxy.com/')
  assert.equal(normalizeGhProxy('http://gh-proxy.com/'), '')
  assert.equal(normalizeGhProxy('https://gh-proxy.com/; rm -rf'), '')
})

test('cn install command does not fetch the script from the panel', () => {
  const cmd = cnAgentInstallCommand({
    panelURL: 'https://liking.example',
    token: '00112233445566778899aabbccddeeff',
    version: '0.3.44',
    proxy: 'https://gh-proxy.com',
    insecure: false,
  })
  assert.match(cmd, /^curl -fsSL --http1\.1 --retry 5 /)
  assert.match(cmd, /https:\/\/gh-proxy\.com\/https:\/\/raw\.githubusercontent\.com\/cheesydui-cloud\/liking\/v0\.3\.44\/internal\/installscript\/install-agent\.sh/)
  assert.match(cmd, /--panel-url 'https:\/\/liking\.example'/)
  assert.match(cmd, /--release 'v0\.3\.44'/)
  assert.match(cmd, /--gh-proxy 'https:\/\/gh-proxy\.com\/'/)
  assert.equal(cmd.includes('liking.example/v1/install-agent'), false)
})

test('cn install command refuses an incomplete payload', () => {
  assert.equal(cnAgentInstallCommand({
    panelURL: 'https://liking.example',
    token: 'abc',
    version: '',
    proxy: 'https://gh-proxy.com/',
  }), '')
})
