export const KB_PER_MBPS = 125
export const SPEED_MAX_KBPS = 10000 * KB_PER_MBPS

export function formatSpeedLimit(kbps) {
  const n = Number(kbps) || 0
  if (n <= 0) return ''
  if (n % KB_PER_MBPS === 0) return `${n / KB_PER_MBPS} Mbps`
  return `${n} KB/s`
}

export function speedFormFromKbps(kbps) {
  const n = Number(kbps) || 0
  if (n <= 0) return { value: '', unit: 'mbps' }
  if (n % KB_PER_MBPS === 0) return { value: String(n / KB_PER_MBPS), unit: 'mbps' }
  return { value: String(Math.round(n)), unit: 'kbps' }
}

export function kbpsFromForm(value, unit) {
  if (value == null || String(value).trim() === '') return 0
  const n = Number(value)
  if (!Number.isFinite(n) || n < 0) return NaN
  const kbps = unit === 'mbps' ? Math.round(n * KB_PER_MBPS) : Math.round(n)
  if (n > 0 && kbps < 1) return NaN
  if (kbps > SPEED_MAX_KBPS) return NaN
  return kbps
}

export function switchSpeedUnit(value, unit, next) {
  if (next === unit) return { value, unit }
  const raw = String(value ?? '').trim()
  if (raw === '') return { value: '', unit: next }
  const n = Number(raw)
  if (!Number.isFinite(n)) return { value, unit: next }
  if (unit === 'mbps' && next === 'kbps') return { value: String(Math.round(n * KB_PER_MBPS)), unit: next }
  if (unit === 'kbps' && next === 'mbps') {
    const mbps = Math.round((n / KB_PER_MBPS) * 1000) / 1000
    return { value: String(mbps), unit: next }
  }
  return { value, unit: next }
}
