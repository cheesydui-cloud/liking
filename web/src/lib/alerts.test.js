import { test } from 'node:test'
import assert from 'node:assert/strict'
import { alertCardParts } from './alerts.js'

test('alert cards split name and reason', () => {
  const used = alertCardParts({ text: 'kelly 流量已用尽', to: '/users', kind: 'danger' })
  assert.equal(used.scope, '用户')
  assert.equal(used.name, 'kelly')
  assert.equal(used.reason, '流量已用尽')
  assert.equal(used.tone, '严重')

  const spaced = alertCardParts({ text: '干杯云 3050 离线', to: '/servers', kind: 'warn' })
  assert.equal(spaced.scope, '实例')
  assert.equal(spaced.name, '干杯云 3050')
  assert.equal(spaced.reason, '离线')
  assert.equal(spaced.tone, '注意')

  const cert = alertCardParts({ text: '证书 主域名 已过期', to: '/settings?tab=certs', kind: 'danger' })
  assert.equal(cert.scope, '证书')
  assert.equal(cert.name, '主域名')
  assert.equal(cert.reason, '已过期')

  const soon = alertCardParts({ text: '香港 01 将于 3 天后到期', to: '/servers', kind: 'warn' })
  assert.equal(soon.name, '香港 01')
  assert.equal(soon.reason, '将于 3 天后到期')

  const upgrade = alertCardParts({ text: 'nbnet-907 Agent 可升级到 0.3.47', to: '/servers', kind: 'warn' })
  assert.equal(upgrade.name, 'nbnet-907')
  assert.equal(upgrade.reason, 'Agent 可升级到 0.3.47')

  const more = alertCardParts({ text: '还有 3 条报警', to: '/users', kind: 'warn' })
  assert.equal(more.scope, '更多')
  assert.equal(more.name, '还有 3 条报警')
  assert.equal(more.reason, '查看其余')
})
