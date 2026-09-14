import { formatPortRange, serverPortRange } from './ports'
import { parseShareURI, shareLabel, shareProto, isSocksURI } from './share'

export const LAND_PROFILES = ['vless-reality', 'vless-reality-vision', 'vless-xhttp-tls', 'trojan-tls', 'ss2022', 'socks5']
export const MAX_HOPS = 5

export function inboundSettings(inb) {
  const st = inb?.settings
  if (!st) return {}
  if (typeof st === 'string') {
    try { return JSON.parse(st) || {} } catch { return {} }
  }
  return st
}

export function forwardKind(inb) {
  if (!inb) return ''
  if (inb.profile === 'port-forward') return 'port'
  if (inb.exit_uri || inb.line_kind === 'chain') return 'chain'
  return ''
}

export function kindLabel(k) {
  if (k === 'chain') return '链式'
  if (k === 'port') return '端口'
  return ''
}

export function protoShort(profile) {
  switch (profile) {
    case 'vless-reality-vision': return 'Vision'
    case 'vless-reality': return 'REALITY'
    case 'vless-xhttp-tls': return 'XHTTP'
    case 'trojan-tls': return 'Trojan'
    case 'ss2022': return 'SS2022'
    case 'anytls': return 'AnyTLS'
    case 'mieru': return 'Mieru'
    case 'socks5': return 'SOCKS5'
    case 'port-forward': return '中转'
    default: return profile || ''
  }
}

export function parseSocks(uri) {
  const out = { host: '', port: '1080', user: '', pass: '' }
  if (!uri) return out
  try {
    const u = new URL(uri)
    out.host = u.hostname || ''
    out.port = u.port || '1080'
    out.user = decodeURIComponent(u.username || '')
    out.pass = decodeURIComponent(u.password || '')
  } catch { /* ignore */ }
  return out
}

export function formatSocks(host, port, user, pass) {
  host = String(host || '').trim()
  if (!host) return ''
  const p = Number(port) || 1080
  const hp = host.includes(':') && !host.startsWith('[') ? `[${host}]:${p}` : `${host}:${p}`
  const u = String(user || '').trim()
  const pw = String(pass || '')
  if (u || pw) return `socks5://${encodeURIComponent(u)}:${encodeURIComponent(pw)}@${hp}`
  return `socks5://${hp}`
}

export function socksHostPort(uri) {
  const s = parseSocks(uri)
  if (!s.host) return 'SK5'
  return `${s.host}:${s.port || 1080}`
}

export function hopURI(h) {
  if (!h) return ''
  if (h.uri) return h.uri
  if (h.kind === 'socks') return formatSocks(h.sk5_host, h.sk5_port, h.sk5_user, h.sk5_pass)
  return ''
}

export function hopText(h, byID) {
  if (!h) return '—'
  const uri = hopURI(h)
  if (h.kind === 'uri' || h.kind === 'socks' || uri) {
    const t = parseShareURI(uri)
    if (t.ok) {
      if (t.proto === 'SK5') return `${t.host}:${t.port}`
      return t.label
    }
    if (h.kind === 'socks') return socksHostPort(uri)
    return shareLabel(uri)
  }
  const land = byID.get(Number(h.inbound_id))
  return land ? land.name : '落地已删除'
}

export function hopProto(h, byID) {
  if (!h) return '—'
  const uri = hopURI(h)
  if (h.kind === 'uri' || h.kind === 'socks' || uri) return shareProto(uri)
  const land = byID.get(Number(h.inbound_id))
  return land ? protoShort(land.profile) : '—'
}

export function pathHops(inb) {
  const st = inboundSettings(inb)
  const hops = []
  const raw = Array.isArray(st.hops) ? st.hops : []
  for (const h of raw) hops.push(h)
  if (inb.exit_uri) hops.push({ kind: 'socks', uri: inb.exit_uri })
  else if (inb.exit_inbound_id) hops.push({ kind: 'panel', inbound_id: inb.exit_inbound_id })
  return hops
}

export function landingText(inb, byID) {
  const k = forwardKind(inb)
  if (k === 'port') {
    const st = inboundSettings(inb)
    return `${st.dest_host || '?'}:${st.dest_port || '?'}`
  }
  const hops = pathHops(inb)
  const last = hops[hops.length - 1]
  return hopText(last, byID)
}

export function usedPortsText(server, list, excludeId = 0) {
  const serverId = server?.id
  const { min, max } = serverPortRange(server)
  const ports = (list || [])
    .filter(x => Number(x.server_id) === Number(serverId) && Number(x.id) !== Number(excludeId))
    .map(x => x.port)
  const used = ports.length ? `已用 ${[...new Set(ports)].sort((a, b) => a - b).join('、')}` : '这台实例还没有节点'
  return `不填则在 ${min}–${max} 随机。${used}`
}

export function hopFromSaved(h) {
  const hop = { kind: 'panel', inbound_id: '', sk5_host: '', sk5_port: '1080', sk5_user: '', sk5_pass: '', uri: '' }
  if (!h) return hop
  const uri = h.uri || ''
  if (h.kind === 'uri' || (uri && !isSocksURI(uri))) {
    hop.kind = 'uri'
    hop.uri = uri
    return hop
  }
  if (h.kind === 'socks' || uri) {
    const sk = parseSocks(uri)
    hop.kind = 'socks'
    hop.uri = uri
    hop.sk5_host = sk.host
    hop.sk5_port = sk.port
    hop.sk5_user = sk.user
    hop.sk5_pass = sk.pass
    return hop
  }
  hop.kind = 'panel'
  hop.inbound_id = h.inbound_id || ''
  return hop
}

export { formatPortRange, isSocksURI }
