const mem = new Map()

export function peekList(key) {
  return mem.has(key) ? mem.get(key) : undefined
}

export function putList(key, value) {
  mem.set(key, value)
  return value
}

export function clearLists() {
  mem.clear()
}
