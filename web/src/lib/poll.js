export function startPoll(fn, ms, opts = {}) {
  const tick = () => {
    if (typeof document !== 'undefined' && document.hidden) return
    try {
      const ret = fn()
      if (ret && typeof ret.catch === 'function') ret.catch(() => {})
    } catch {
      /* page fetch errors stay in the caller */
    }
  }
  if (opts.immediate !== false) tick()
  const id = setInterval(tick, ms)
  const onVis = () => {
    if (!document.hidden) tick()
  }
  document.addEventListener('visibilitychange', onVis)
  return () => {
    clearInterval(id)
    document.removeEventListener('visibilitychange', onVis)
  }
}
