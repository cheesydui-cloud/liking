export const CORE_OPTIONS = [
  { id: 'xray', label: 'Xray' },
  { id: 'singbox', label: 'sing-box' },
  { id: 'mita', label: 'Mita' },
]

export function parseCores(s) {
  return String(s || '')
    .split(',')
    .map(x => x.trim().toLowerCase().replace(/^sing-box$/, 'singbox'))
    .filter(Boolean)
}

export function coreLabel(id) {
  const x = CORE_OPTIONS.find(c => c.id === id)
  return x ? x.label : id
}

export function fmtResetDay(n) {
  const d = Number(n) || 0
  if (d < 1 || d > 31) return '未设置'
  return `每月 ${d} 日`
}

export function ymd(ts) {
  if (!ts) return ''
  const d = new Date(Number(ts) * 1000)
  if (Number.isNaN(d.getTime())) return ''
  const z = n => String(n).padStart(2, '0')
  return `${d.getFullYear()}-${z(d.getMonth() + 1)}-${z(d.getDate())}`
}

export function ymdToUnix(s) {
  if (!s) return 0
  const d = new Date(`${s}T23:59:59`)
  const n = d.getTime()
  return Number.isNaN(n) ? 0 : Math.floor(n / 1000)
}

export function fmtExpires(ts) {
  return ymd(ts) || '未设置'
}

export function isExpired(ts) {
  return !!(ts && Number(ts) * 1000 < Date.now())
}
