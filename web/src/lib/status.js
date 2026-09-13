export function serverHasCore(s, core) {
  if (!s?.cores) return true
  const have = String(s.cores).split(',').map(x => x.trim().toLowerCase().replace('sing-box', 'singbox')).filter(Boolean)
  if (!have.length) return true
  const want = String(core || 'xray').toLowerCase().replace('sing-box', 'singbox')
  return have.includes(want)
}

export function isDirectNode(inb) {
  if (!inb) return false
  if (inb.profile === 'port-forward') return false
  if (inb.line_kind === 'chain') return false
  if (inb.exit_uri) return false
  return true
}

export function serverInstalled(s) {
  return !!(s && (s.agent_ver || s.last_seen))
}

export function serverStatus(s) {
  if (!serverInstalled(s)) return '未安装'
  if (s.last_error) return '故障'
  if (s.online) return '在线'
  return '离线'
}

export function nodeStatus(inb, server, opts = {}) {
  if (!inb || inb.enabled === false) return '停用'
  if (!serverInstalled(server)) return '未安装'
  if (!server.online) return '离线'
  if (server.last_error) return '故障'
  if (!opts.skipLanding && inb.line_kind === 'chain' && !inb.exit_uri) {
    const id = Number(inb.exit_inbound_id)
    if (!id) return '故障'
    if (opts.byID && !opts.byID.get(id)) return '故障'
  }
  if (!serverHasCore(server, inb.core)) return '未安装'
  return '正常'
}

export function hopStatus(h, byID, serversByID) {
  if (!h) return '故障'
  if (h.kind === 'socks' || h.uri) return ''
  const land = byID?.get(Number(h.inbound_id))
  if (!land) return '故障'
  const srv = serversByID?.get(Number(land.server_id))
  return nodeStatus(land, srv)
}

export function statusClass(status) {
  switch (status) {
    case '正常':
    case '在线':
      return 'is-live'
    case '故障':
      return 'is-fault'
    case '未安装':
      return 'is-warn'
    case '停用':
      return 'is-mute'
    default:
      return 'is-off'
  }
}
