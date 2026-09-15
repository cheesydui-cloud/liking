export function startPoll(fn, ms = 5000, opts = {}) {
  let stopped = false
  let inflight = false
  let delay = ms
  let timer = 0
  const ac = typeof AbortController !== 'undefined' ? new AbortController() : null

  const tick = async () => {
    if (stopped || inflight) return
    if (typeof document !== 'undefined' && document.hidden) return
    inflight = true
    try {
      const ret = fn(ac?.signal)
      if (ret && typeof ret.then === 'function') await ret
      delay = ms
    } catch (e) {
      if (e?.name !== 'AbortError' && e?.code !== 20) {
        delay = Math.min(Math.max(delay, ms) * 2, 30000)
      }
    } finally {
      inflight = false
    }
  }

  const loop = async () => {
    await tick()
    if (stopped) return
    timer = setTimeout(loop, delay)
  }

  if (opts.immediate !== false) loop()
  else timer = setTimeout(loop, ms)

  const onVis = () => {
    if (!document.hidden) tick()
  }
  document.addEventListener('visibilitychange', onVis)
  return () => {
    stopped = true
    clearTimeout(timer)
    ac?.abort()
    document.removeEventListener('visibilitychange', onVis)
  }
}
