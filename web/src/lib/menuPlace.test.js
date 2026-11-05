import { test } from 'node:test'
import assert from 'node:assert/strict'
import { placeMoreMenu } from './menuPlace.js'

const view = { width: 1200, height: 700 }

test('opens downward when the menu fits', () => {
  const pos = placeMoreMenu(
    { top: 80, right: 400, bottom: 112, left: 368 },
    { width: 200, height: 180 },
    view,
  )
  assert.equal(pos.top, 116)
  assert.equal(pos.bottom, undefined)
  assert.equal(pos.left, 200)
  assert.ok(pos.maxHeight >= 180)
})

test('opens upward when the lower edge would clip the menu', () => {
  const pos = placeMoreMenu(
    { top: 520, right: 900, bottom: 552, left: 868 },
    { width: 200, height: 280 },
    view,
  )
  assert.equal(pos.top, undefined)
  assert.equal(pos.bottom, view.height - 520 + 4)
  assert.ok(pos.maxHeight >= 280)
})

test('scrolls when neither side can show the whole menu', () => {
  const pos = placeMoreMenu(
    { top: 200, right: 400, bottom: 232, left: 368 },
    { width: 200, height: 500 },
    { width: 800, height: 360 },
  )
  assert.ok(pos.maxHeight < 500)
  assert.ok(pos.maxHeight >= 96)
})

test('keeps the menu inside the left edge', () => {
  const pos = placeMoreMenu(
    { top: 40, right: 40, bottom: 72, left: 8 },
    { width: 200, height: 100 },
    view,
  )
  assert.equal(pos.left, 8)
})
