export function createOpLock() {
  const busy = new Set()
  return {
    has(key) {
      return busy.has(key)
    },
    async run(key, fn) {
      if (busy.has(key)) return
      busy.add(key)
      try {
        return await fn()
      } finally {
        busy.delete(key)
      }
    },
  }
}
