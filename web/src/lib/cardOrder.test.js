import { test } from 'node:test'
import assert from 'node:assert/strict'
import { mergeKeepOrder, reorderList, spliceById } from './cardOrder.js'

test('splice keeps hidden cards in their slots', () => {
  const full = [{ id: 1 }, { id: 2 }, { id: 3 }, { id: 4 }, { id: 5 }]
  const next = spliceById(full, [{ id: 5 }, { id: 1 }, { id: 3 }])
  assert.deepEqual(next.map(x => x.id), [5, 2, 1, 4, 3])
})

test('reorder moves an item inside the visible list', () => {
  const next = reorderList([{ id: 'a' }, { id: 'b' }, { id: 'c' }], 0, 2)
  assert.deepEqual(next.map(x => x.id), ['b', 'c', 'a'])
  assert.equal(reorderList(next, 1, 1).map(x => x.id).join(), 'b,c,a')
})

test('merge keeps the local order and picks up new rows', () => {
  const prev = [{ id: 3, name: 'old' }, { id: 1, name: 'old' }]
  const incoming = [{ id: 1, name: 'new' }, { id: 2, name: 'fresh' }, { id: 3, name: 'new' }]
  const next = mergeKeepOrder(prev, incoming)
  assert.deepEqual(next.map(x => [x.id, x.name]), [[3, 'new'], [1, 'new'], [2, 'fresh']])
})
