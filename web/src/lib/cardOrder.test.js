import { test } from 'node:test'
import assert from 'node:assert/strict'
import { mergeKeepOrder, reorderList, spliceById, targetIndexFromRects } from './cardOrder.js'

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

test('target index follows the nearer card center', () => {
  const rects = [
    { left: 0, top: 0, width: 100, height: 80 },
    { left: 120, top: 0, width: 100, height: 80 },
    { left: 0, top: 100, width: 100, height: 80 },
  ]
  assert.equal(targetIndexFromRects(rects, 20, 20, 0), 0)
  assert.equal(targetIndexFromRects(rects, 111, 40, 0), 0)
  assert.equal(targetIndexFromRects(rects, 170, 40, 0), 1)
  assert.equal(targetIndexFromRects(rects, 20, 170, 0), 2)
  assert.equal(targetIndexFromRects(rects, 20, 20, 2), 0)
  assert.equal(targetIndexFromRects([], 0, 0, 0), 0)
})

test('merge keeps the local order and picks up new rows', () => {
  const prev = [{ id: 3, name: 'old' }, { id: 1, name: 'old' }]
  const incoming = [{ id: 1, name: 'new' }, { id: 2, name: 'fresh' }, { id: 3, name: 'new' }]
  const next = mergeKeepOrder(prev, incoming)
  assert.deepEqual(next.map(x => [x.id, x.name]), [[3, 'new'], [1, 'new'], [2, 'fresh']])
})
