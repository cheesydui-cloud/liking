const mem = new Map()

export function peekProbe(id) {
  return mem.has(id) ? mem.get(id) : undefined
}

export function putProbe(id, value) {
  mem.set(id, value)
  return value
}
