export const DEFAULT_PORT_MIN = 10000
export const DEFAULT_PORT_MAX = 59999

export function serverPortRange(s) {
  const min = Number(s?.port_min)
  const max = Number(s?.port_max)
  return {
    min: Number.isInteger(min) && min >= 1 && min <= 65535 ? min : DEFAULT_PORT_MIN,
    max: Number.isInteger(max) && max >= 1 && max <= 65535 ? max : DEFAULT_PORT_MAX,
  }
}

export function formatPortRange(s) {
  const { min, max } = serverPortRange(s)
  return `${min}–${max}`
}

export function parsePort(v) {
  const n = Number(String(v ?? '').trim())
  if (!Number.isInteger(n) || n < 1 || n > 65535) return null
  return n
}
