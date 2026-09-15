const mem = new Map()
let gen = 0

export function cacheGen() {
  return gen
}

export function peekList(key) {
  return mem.has(key) ? mem.get(key) : undefined
}

export function putList(key, value, g) {
  if (g !== undefined && g !== gen) return value
  mem.set(key, value)
  return value
}

export function clearLists() {
  gen += 1
  mem.clear()
}
