export const DEST_PRESETS = [
  'azure.microsoft.com:443',
  'j.6sc.co:443',
  'logx.optimizely.com:443',
  'assets-www.xbox.com:443',
  'devblogs.microsoft.com:443',
  'go.microsoft.com:443',
  'visualstudio.microsoft.com:443',
  'cdn.userway.org:443',
  'gray-wowt-prod.gtv-cdn.com:443',
  'vscjava.gallerycdn.vsassets.io:443',
  'www.cloudflare.com:443',
  'www.microsoft.com:443',
  'dl.google.com:443',
  'www.samsung.com:443',
  'www.apple.com:443',
]

export function formatDest(s) {
  const t = String(s || '').trim()
  if (!t) return ''
  if (t.startsWith('[')) return t
  const cut = t.lastIndexOf(':')
  if (cut > 0 && /^\d+$/.test(t.slice(cut + 1))) return t
  return `${t}:443`
}

export function destHostOf(s) {
  const t = formatDest(s)
  if (!t) return ''
  const cut = t.lastIndexOf(':')
  if (cut > 0 && /^\d+$/.test(t.slice(cut + 1))) return t.slice(0, cut)
  return t
}

export function destLabel(host, row, busy) {
  if (row?.ok) return `${host}: ${row.latency_ms} ms`
  if (row && !row.ok) return `${host}: timeout`
  if (busy) return `${host}: …`
  return host
}

export function serverIsOnline(s) {
  return Number(s?.online) > 0
}

export function sortDests(dests, rows) {
  return [...dests].sort((a, b) => {
    const ra = rows[a]
    const rb = rows[b]
    const sa = ra?.ok ? Number(ra.latency_ms) : 1e9
    const sb = rb?.ok ? Number(rb.latency_ms) : 1e9
    if (sa !== sb) return sa - sb
    return destHostOf(a).localeCompare(destHostOf(b))
  })
}
