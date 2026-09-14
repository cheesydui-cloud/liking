const PROTO = {
  vless: 'VLESS',
  ss: 'SS',
  trojan: 'Trojan',
  socks5: 'SK5',
  socks: 'SK5',
  socks5h: 'SK5',
}

export function isSocksScheme(scheme) {
  return scheme === 'socks' || scheme === 'socks5' || scheme === 'socks5h'
}

function firstLine(raw) {
  return String(raw || '').split('\n').map(s => s.trim()).find(Boolean) || ''
}

function hostPortLabel(host, port) {
  const h = String(host || '')
  const hp = h.includes(':') ? `[${h}]:${port}` : `${h}:${port}`
  return hp
}

function fail(msg) {
  return { ok: false, scheme: '', host: '', port: 0, name: '', proto: '', label: '', error: msg }
}

function ok(scheme, host, port, name) {
  const proto = PROTO[scheme] || String(scheme || '').toUpperCase()
  const label = name ? `${name} · ${proto}` : `${proto} ${hostPortLabel(host, port)}`
  return { ok: true, scheme, host, port, name, proto, label, error: '' }
}

function fragmentName(hash) {
  const s = String(hash || '').replace(/^#/, '')
  if (!s) return ''
  try { return decodeURIComponent(s) } catch { return s }
}

function schemeOf(line) {
  const i = line.indexOf('://')
  if (i <= 0) return ''
  return line.slice(0, i).toLowerCase()
}

function decodeB64(s) {
  const t = String(s || '').replace(/\s/g, '').replace(/-/g, '+').replace(/_/g, '/')
  if (!t) return ''
  const pad = t + '==='.slice((t.length + 3) % 4)
  try { return atob(pad) } catch { return '' }
}

function splitHostPort(hp, fallback) {
  const s = String(hp || '').trim()
  if (!s) return null
  if (s.startsWith('[')) {
    const end = s.indexOf(']')
    if (end < 0) return null
    const host = s.slice(1, end)
    const rest = s.slice(end + 1)
    const port = rest.startsWith(':') ? Number(rest.slice(1)) : fallback
    if (!host || !Number.isInteger(port) || port < 1 || port > 65535) return null
    return { host, port }
  }
  const colon = s.lastIndexOf(':')
  if (colon < 0) {
    if (!Number.isInteger(fallback) || fallback < 1) return null
    return { host: s, port: fallback }
  }
  const host = s.slice(0, colon)
  const port = Number(s.slice(colon + 1))
  if (!host || !Number.isInteger(port) || port < 1 || port > 65535) return null
  return { host, port }
}

function parseLegacySS(payload, name) {
  let body = String(payload || '').trim()
  if (body.includes('#')) {
    const i = body.indexOf('#')
    if (!name) name = fragmentName(body.slice(i))
    body = body.slice(0, i)
  }
  if (body.includes('?')) body = body.slice(0, body.indexOf('?'))
  const decoded = decodeB64(body)
  if (!decoded) return fail('SS 链接无法解码')
  const at = decoded.lastIndexOf('@')
  if (at < 0) return fail('SS 链接缺少主机')
  const user = decoded.slice(0, at)
  const hp = splitHostPort(decoded.slice(at + 1), 8388)
  const cut = user.indexOf(':')
  if (cut < 1 || !hp) return fail('SS 用户信息无效')
  return ok('ss', hp.host, hp.port, name)
}

export function parseShareURI(raw) {
  const line = firstLine(raw)
  if (!line) return fail('链接为空')
  const scheme = schemeOf(line)
  if (!scheme) return fail('无法识别链接，请粘贴 vless://、ss://、trojan:// 或 socks5://')
  if (!['vless', 'ss', 'trojan', 'socks', 'socks5', 'socks5h'].includes(scheme)) {
    return fail(`暂不支持 ${scheme} 链接，请用 vless / ss / trojan / socks5`)
  }

  let u
  try { u = new URL(line) } catch { u = null }

  if (scheme === 'ss') {
    const plugin = u ? (u.searchParams.get('plugin') || '') : ''
    if (plugin.trim()) return fail('暂不支持带插件的 SS 链接')
    let rest = line.slice(line.toLowerCase().indexOf('ss://') + 5)
    let name = u ? fragmentName(u.hash) : ''
    if (rest.includes('#')) {
      const i = rest.indexOf('#')
      if (!name) name = fragmentName(rest.slice(i))
      rest = rest.slice(0, i)
    }
    if (rest.includes('?')) {
      const i = rest.indexOf('?')
      const qs = new URLSearchParams(rest.slice(i + 1))
      if ((qs.get('plugin') || '').trim()) return fail('暂不支持带插件的 SS 链接')
      rest = rest.slice(0, i)
    }
    if (rest.includes('@')) {
      if (u && u.hostname) {
        const port = Number(u.port) || 8388
        if (!Number.isInteger(port) || port < 1 || port > 65535) return fail('端口无效')
        return ok('ss', u.hostname, port, name)
      }
      const at = rest.lastIndexOf('@')
      const hp = splitHostPort(rest.slice(at + 1).replace(/\/$/, ''), 8388)
      if (!hp) return fail('SS 主机无效')
      return ok('ss', hp.host, hp.port, name)
    }
    return parseLegacySS(rest, name)
  }

  if (!u) return fail('无法识别链接，请粘贴 vless://、ss://、trojan:// 或 socks5://')
  const host = u.hostname || ''
  if (!host) return fail('链接缺少主机')
  const fallback = isSocksScheme(scheme) ? 1080 : 443
  const port = Number(u.port) || fallback
  if (!Number.isInteger(port) || port < 1 || port > 65535) return fail('端口无效')
  if (scheme === 'vless' && !(u.username || '').trim()) return fail('VLESS 缺少 UUID')
  if (scheme === 'trojan' && !(u.username || '').trim()) return fail('Trojan 缺少密码')
  return ok(scheme, host, port, fragmentName(u.hash))
}

export function shareLabel(uri) {
  const t = parseShareURI(uri)
  return t.ok ? t.label : '出口链接'
}

export function shareProto(uri) {
  const t = parseShareURI(uri)
  return t.ok ? t.proto : '链接'
}

export function isSocksURI(uri) {
  const t = parseShareURI(uri)
  return t.ok && isSocksScheme(t.scheme)
}
