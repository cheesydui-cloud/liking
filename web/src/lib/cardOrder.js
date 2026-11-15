// Pick the slot whose center is nearer the dragged card than its current hole.
// A small margin keeps the hole from flickering when the pointer sits on a boundary.
export function targetIndexFromRects(rects, x, y, from, margin = 8) {
  if (!Array.isArray(rects) || from < 0 || from >= rects.length) return from
  const dist = (rect) => {
    const dx = x - (rect.left + rect.width / 2)
    const dy = y - (rect.top + rect.height / 2)
    return Math.hypot(dx, dy)
  }
  let to = from
  let best = Infinity
  for (let i = 0; i < rects.length; i++) {
    if (i === from) continue
    const d = dist(rects[i])
    if (d < best) {
      best = d
      to = i
    }
  }
  if (to === from) return from
  if (dist(rects[from]) - best < margin) return from
  return to
}

export function reorderList(list, from, to) {
  if (from === to || from < 0 || to < 0 || from >= list.length || to >= list.length) return list
  const next = list.slice()
  const [item] = next.splice(from, 1)
  next.splice(to, 0, item)
  return next
}

function idKey(item) {
  return String(item?.id)
}

// subset is the new order of the cards on screen. Hidden rows keep their slots.
export function spliceById(full, subset) {
  const source = Array.isArray(full) ? full : []
  const moving = Array.isArray(subset) ? subset : []
  const byId = new Map(source.map(item => [idKey(item), item]))
  const ids = moving.map(idKey)
  const seen = new Set(ids)
  let i = 0
  return source.map(item => {
    if (!seen.has(idKey(item))) return item
    const next = byId.get(ids[i])
    i += 1
    return next || item
  })
}

export function mergeKeepOrder(prev, next) {
  const incoming = Array.isArray(next) ? next : []
  const prior = Array.isArray(prev) ? prev : []
  if (!prior.length) return incoming
  const byId = new Map(incoming.map(item => [idKey(item), item]))
  const seen = new Set()
  const out = []
  for (const item of prior) {
    const key = idKey(item)
    if (!byId.has(key)) continue
    out.push(byId.get(key))
    seen.add(key)
  }
  for (const item of incoming) {
    const key = idKey(item)
    if (!seen.has(key)) out.push(item)
  }
  return out
}
