const FIXED_REASONS = [
  '已达流量上限，节点已停用',
  'Agent 太旧，请用安装命令重装',
  '下发失败',
  '流量已用尽',
  '已到期',
  '今天到期',
  '离线',
  '已过期',
  '即将到期',
]

const PATTERN_REASONS = [
  /^(.*) (流量已用 \d+%)$/,
  /^(.*) (磁盘剩余 \d+%)$/,
  /^(.*) (将于 \d+ 天后到期)$/,
  /^(.*) (Agent 可升级到 \S+)$/,
]

export function alertCardParts(item) {
  const text = String(item?.text || '').trim()
  const to = item?.to || '/servers'
  const fault = item?.kind === 'danger'
  let scope = '实例'
  if (to.startsWith('/users')) scope = '用户'
  else if (to.startsWith('/settings')) scope = '证书'
  const split = splitAlert(text)
  if (split.more) scope = '更多'
  return {
    scope,
    name: split.name || text,
    reason: split.reason,
    tone: fault ? '严重' : '注意',
    fault,
    to,
    text,
  }
}

function splitAlert(text) {
  if (/^还有 \d+ 条报警$/.test(text)) {
    return { name: text, reason: '查看其余', more: true }
  }
  if (text.startsWith('证书 ')) {
    const body = text.slice(3).trim()
    const hit = takeReason(body, ['已过期', '即将到期'])
    if (hit) return { name: hit.name, reason: hit.reason }
  }
  const hit = takeReason(text, FIXED_REASONS)
  if (hit) return hit
  for (const re of PATTERN_REASONS) {
    const m = text.match(re)
    if (m && m[1]) return { name: m[1], reason: m[2] }
  }
  return { name: text, reason: '' }
}

function takeReason(text, reasons) {
  for (const reason of reasons) {
    const mark = ' ' + reason
    if (text.endsWith(mark) && text.length > mark.length) {
      return { name: text.slice(0, -mark.length), reason }
    }
  }
  return null
}
