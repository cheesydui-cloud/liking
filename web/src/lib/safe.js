export function asArray(v) {
  return Array.isArray(v) ? v : []
}

export function isAbort(e) {
  return e?.name === 'AbortError' || e?.code === 20
}
