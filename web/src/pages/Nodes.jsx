import { useEffect, useMemo, useRef, useState } from 'react'
import { Link, useSearchParams } from 'react-router-dom'
import { api } from '../lib/api'
import { copyText } from '../lib/copy'
import { formatPortRange, serverPortRange } from '../lib/ports'
import { isDirectNode, nodeStatus, serverHasCore } from '../lib/status'
import { useToast, useDialog } from '../components/Layout'
import { coreLabel } from '../lib/display'
import { cacheGen, peekList, putList } from '../lib/listCache'
import { startPoll } from '../lib/poll'
import { createOpLock } from '../lib/opLock'
import { asArray, isAbort } from '../lib/safe'
import { Badge, Empty, Field, FilterTabs, Icon, LineStatus, Modal, MoreMenu, PageHead, SearchInput, SkeletonRows, StatusWord, fmtBps, fmtBytes, fmtDateShort } from '../components/ui'

const DEST_PRESETS = [
  'www.cloudflare.com:443',
  'www.microsoft.com:443',
  'dl.google.com:443',
  'www.samsung.com:443',
  'www.apple.com:443',
]

const FINGERPRINTS = ['chrome', 'firefox', 'safari', 'ios', 'android', 'edge', 'qq', 'random', 'randomized']

const emptyLine = {
  server_id: 0, name: '', profile: 'vless-reality-vision', port: '', listen: '0.0.0.0',
  line_kind: 'direct', cert_id: 0, enabled: true,
  dest: 'www.cloudflare.com:443', sni: '', path: '', host: '', mode: 'auto',
  method: '2022-blake3-aes-256-gcm', transport: 'BOTH',
  fingerprint: 'chrome', short_ids: '', xver: 0, spider_x: '',
  min_version: '1.3', alpn: 'h2,http/1.1', reject_unknown_sni: true,
  rotate_keys: false, settings: {},
}

function csv(s) {
  return String(s || '').split(/[,\s]+/).map(x => x.trim()).filter(Boolean)
}

function isReality(profile) {
  return String(profile).startsWith('vless-reality')
}

function needsTLS(profile) {
  return profile === 'vless-xhttp-tls' || profile === 'trojan-tls' || profile === 'anytls'
}

function usedPortsText(server, list, excludeId = 0) {
  const serverId = server?.id
  const { min, max } = serverPortRange(server)
  const ports = list
    .filter(x => Number(x.server_id) === Number(serverId) && Number(x.id) !== Number(excludeId))
    .map(x => x.port)
  const used = ports.length ? `已用 ${[...new Set(ports)].sort((a, b) => a - b).join('、')}` : '这台实例还没有节点'
  return `不填则在 ${min}–${max} 随机，避开已用端口。${used}`
}

function inboundSettings(inb) {
  const st = inb?.settings
  if (!st) return {}
  if (typeof st === 'string') {
    try { return JSON.parse(st) || {} } catch { return {} }
  }
  return st
}

function inboundParamRows(inb) {
  const st = inboundSettings(inb)
  const short = Array.isArray(st.short_ids) ? st.short_ids.filter(Boolean).join(',') : (st.short_id || '')
  const names = Array.isArray(st.server_names) ? st.server_names.filter(Boolean).join(',') : ''
  const alpn = Array.isArray(st.alpn) ? st.alpn.filter(Boolean).join(',') : st.alpn
  return [
    ['名称', inb.name],
    ['节点', [inb.server_name, inb.server_host].filter(Boolean).join(' ')],
    ['协议', inb.profile],
    ['端口', String(inb.port || '')],
    ['dest', st.dest],
    ['server_names', names],
    ['sni', st.sni],
    ['path', st.path],
    ['host', st.host],
    ['mode', st.mode],
    ['public_key', st.public_key],
    ['short_id', short],
    ['fingerprint', st.fingerprint],
    ['spider_x', st.spider_x],
    ['xver', st.xver === 0 || st.xver ? String(st.xver) : ''],
    ['min_version', st.min_version],
    ['alpn', alpn],
    ['reject_unknown_sni', st.reject_unknown_sni === false ? '否' : (st.reject_unknown_sni ? '是' : '')],
    ['method', st.method],
    ['server_password', st.server_password],
    ['transport', st.transport],
    ['dest_host', st.dest_host],
    ['dest_port', st.dest_port],
    ['network', st.network],
  ].filter(([, v]) => v)
}

function hasAdvanced(st) {
  if (!st) return false
  if (st.fingerprint && st.fingerprint !== 'chrome') return true
  if (Array.isArray(st.short_ids) && st.short_ids.length > 1) return true
  if (st.min_version && st.min_version !== '1.3') return true
  if (st.reject_unknown_sni === false) return true
  if (Number(st.xver) > 0) return true
  if (st.spider_x) return true
  if (st.host) return true
  if (st.mode && st.mode !== 'auto') return true
  const alpn = Array.isArray(st.alpn) ? st.alpn.join(',') : String(st.alpn || '')
  if (alpn && alpn !== 'h2,http/1.1') return true
  return false
}

function inboundParamLines(inb) {
  return inboundParamRows(inb).map(([k, v]) => `${k} ${v}`).join('\n')
}

function protoShort(profile) {
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

function coreId(core) {
  return String(core || 'xray').toLowerCase().replace(/sing-box/g, 'singbox')
}

function nodeTone(st) {
  if (st === '正常') return 'is-live'
  if (st === '故障' || st === '流量已满') return 'is-fault'
  return 'is-off'
}

function Metric({ label, value, plain }) {
  return (
    <div className="metric">
      <span className="metric-k">{label}</span>
      <span className={`metric-v${plain ? ' is-plain' : ''}`}>{value}</span>
    </div>
  )
}

function formFromInbound(inb) {
  const st = inboundSettings(inb)
  const names = Array.isArray(st.server_names) ? st.server_names.filter(Boolean).join(',') : ''
  const alpn = Array.isArray(st.alpn) ? st.alpn.filter(Boolean).join(',') : (st.alpn || 'h2,http/1.1')
  const short = Array.isArray(st.short_ids) ? st.short_ids.filter(Boolean).join(',') : (st.short_id || '')
  return {
    server_id: inb.server_id,
    name: inb.name || '',
    profile: inb.profile,
    port: inb.port,
    listen: inb.listen || '0.0.0.0',
    line_kind: 'direct',
    cert_id: inb.cert_id || 0,
    enabled: inb.enabled !== false,
    dest: st.dest || 'www.cloudflare.com:443',
    sni: names || st.sni || '',
    path: st.path || '',
    host: st.host || '',
    mode: st.mode || 'auto',
    method: st.method || '2022-blake3-aes-256-gcm',
    transport: st.transport || 'BOTH',
    fingerprint: st.fingerprint || 'chrome',
    short_ids: short,
    xver: st.xver ?? 0,
    spider_x: st.spider_x || '',
    min_version: st.min_version || '1.3',
    alpn: alpn || 'h2,http/1.1',
    reject_unknown_sni: st.reject_unknown_sni !== false,
    rotate_keys: false,
    settings: st,
  }
}

export default function Nodes() {
  const toast = useToast()
  const dialog = useDialog()
  const [params, setParams] = useSearchParams()
  const serverQ = params.get('server') || ''
  const [servers, setServers] = useState(() => asArray(peekList('servers')))
  const [list, setList] = useState(() => asArray(peekList('inbounds')))
  const [certs, setCerts] = useState(() => asArray(peekList('certs')))
  const [profiles, setProfiles] = useState(() => asArray(peekList('profiles')))
  const [ready, setReady] = useState(() => peekList('servers') !== undefined && peekList('inbounds') !== undefined)
  const [f, setF] = useState(emptyLine)
  const [editId, setEditId] = useState(0)
  const [lineOpen, setLineOpen] = useState(false)
  const [busy, setBusy] = useState(false)
  const [paramInb, setParamInb] = useState(null)
  const [shareText, setShareText] = useState('')
  const [showAdv, setShowAdv] = useState(false)
  const [q, setQ] = useState('')
  const [statusFilter, setStatusFilter] = useState('')
  const [pollErr, setPollErr] = useState('')
  const ops = useRef(createOpLock())

  const load = async () => {
    try {
      const g = cacheGen()
      const [a, b, c, d] = await Promise.all([
        api.get('/servers'), api.get('/inbounds'), api.get('/certs'), api.get('/profiles'),
      ])
      setServers(putList('servers', asArray(a.servers), g))
      setList(putList('inbounds', asArray(b.inbounds), g))
      setCerts(putList('certs', asArray(c.certs), g))
      setProfiles(putList('profiles', asArray(d.profiles), g))
    } catch (e) { toast(e.message, 'error') }
    finally { setReady(true) }
  }
  useEffect(() => {
    load()
    return startPoll(async (signal) => {
      const g = cacheGen()
      try {
        const [a, b] = await Promise.all([
          api.get('/servers', signal),
          api.get('/inbounds', signal),
        ])
        setServers(putList('servers', asArray(a.servers), g))
        setList(putList('inbounds', asArray(b.inbounds), g))
        setPollErr('')
      } catch (e) {
        if (isAbort(e)) return
        setPollErr('实时刷新失败，显示的是上次成功数据')
        throw e
      }
    }, 5000, { immediate: false })
  }, [])

  const meta = profiles.find(p => p.id === f.profile)
  const selectedServer = servers.find(s => Number(s.id) === Number(f.server_id))
  const missingCore = selectedServer && meta && !serverHasCore(selectedServer, meta.core)
  const protoList = profiles.length ? profiles : [{ id: f.profile, title: f.profile, desc: '' }]
  const destHostName = String(f.dest || '').split(':')[0].trim().toLowerCase()
  const destIsSelf = isReality(f.profile) && destHostName && [selectedServer?.public_host, selectedServer?.connect_ip]
    .filter(Boolean)
    .some(h => String(h).split(':')[0].trim().toLowerCase() === destHostName)
  const waitOnline = selectedServer && !selectedServer.online

  const openCreateLine = (serverId) => {
    const sid = Number(serverId) || Number(serverQ) || (servers.length === 1 ? servers[0].id : 0)
    setEditId(0)
    setShowAdv(false)
    setF({ ...emptyLine, server_id: sid })
    setLineOpen(true)
  }

  const startEdit = (inb) => {
    setEditId(inb.id)
    setF(formFromInbound(inb))
    setShowAdv(hasAdvanced(inboundSettings(inb)))
    setLineOpen(true)
  }

  const closeLine = () => {
    setLineOpen(false)
    setEditId(0)
    setShowAdv(false)
    setF(emptyLine)
  }

  const bodyFromForm = () => {
    const settings = { ...(f.settings || {}) }
    const profile = f.profile
    if (isReality(profile)) {
      settings.dest = f.dest
      const names = csv(f.sni)
      if (names.length) settings.server_names = names
      else delete settings.server_names
      settings.fingerprint = f.fingerprint || 'chrome'
      const ids = csv(f.short_ids)
      if (ids.length) settings.short_ids = ids
      else delete settings.short_ids
      settings.xver = Number(f.xver) || 0
      if (String(f.spider_x || '').trim()) settings.spider_x = f.spider_x.trim()
      else delete settings.spider_x
      if (f.rotate_keys) {
        delete settings.private_key
        delete settings.public_key
      }
    }
    if (needsTLS(profile)) {
      if (String(f.sni || '').trim()) settings.sni = f.sni.trim()
      else delete settings.sni
      settings.fingerprint = f.fingerprint || 'chrome'
      settings.min_version = f.min_version || '1.3'
      const alpn = csv(f.alpn)
      if (alpn.length) settings.alpn = alpn
      else delete settings.alpn
      settings.reject_unknown_sni = f.reject_unknown_sni !== false
    }
    if (profile === 'vless-xhttp-tls') {
      if (String(f.path || '').trim()) settings.path = f.path.trim()
      else delete settings.path
      settings.mode = f.mode || 'auto'
      if (String(f.host || '').trim()) settings.host = f.host.trim()
      else delete settings.host
    }
    if (profile === 'ss2022') settings.method = f.method
    if (profile === 'mieru') settings.transport = f.transport
    const body = {
      server_id: Number(f.server_id),
      name: f.name,
      profile,
      port: Number(f.port) || 0,
      listen: f.listen || '0.0.0.0',
      line_kind: 'direct',
      enabled: f.enabled !== false,
      settings,
    }
    if (f.cert_id) body.cert_id = Number(f.cert_id)
    return body
  }

  const submitLine = async (e) => {
    e.preventDefault()
    if (!Number(f.server_id)) {
      toast('请选择实例', 'error')
      return
    }
    const raw = String(f.port ?? '').trim()
    if (raw !== '') {
      const port = Number(raw)
      if (!Number.isInteger(port) || port < 1 || port > 65535) {
        toast(selectedServer ? `端口 1–65535，或不填则在 ${formatPortRange(selectedServer)} 随机` : '端口范围 1–65535，或不填则随机', 'error')
        return
      }
    } else if (editId) {
      toast('编辑时需要填写端口', 'error')
      return
    }
    const wasEdit = !!editId
    if (wasEdit) {
      const prev = list.find(x => Number(x.id) === Number(editId))
      if (prev && Number(f.port) !== Number(prev.port)) {
        if (!(await dialog.confirm({
          title: '修改端口',
          message: `端口将从 ${prev.port} 改为 ${f.port}，现有连接会断开。`,
          danger: true,
          okText: '改端口',
        }))) return
      }
    }
    setBusy(true)
    try {
      const d = wasEdit ? await api.put(`/inbounds/${editId}`, bodyFromForm()) : await api.post('/inbounds', bodyFromForm())
      if (d.apply_error) toast(d.apply_error, 'error')
      else toast(wasEdit ? '已保存' : '已增加节点')
      closeLine()
      load()
    } catch (e) { toast(e.message, 'error') }
    finally { setBusy(false) }
  }

  const delLine = async (id) => {
    if (!(await dialog.confirm({ title: '删除节点', message: '订阅里对应的节点会立刻消失。', danger: true }))) return
    await ops.current.run(`del-${id}`, async () => {
      try { await api.del(`/inbounds/${id}`); if (editId === id) closeLine(); await load() }
      catch (e) { toast(e.message, 'error') }
    })
  }

  const toggle = async (inb) => {
    const next = !inb.enabled
    if (!next) {
      if (!(await dialog.confirm({
        title: '停用节点',
        message: `将停掉「${inb.name}」的入口端口，在线用户会立刻断开。`,
        danger: true,
        okText: '停用',
      }))) return
    }
    await ops.current.run(`tog-${inb.id}`, async () => {
      try {
        const d = await api.put(`/inbounds/${inb.id}`, {
          enabled: next, name: inb.name, port: inb.port, listen: inb.listen,
          settings: inb.settings, line_kind: inb.line_kind, exit_inbound_id: inb.exit_inbound_id, exit_uri: inb.exit_uri || '', cert_id: inb.cert_id,
        })
        if (d.apply_error) toast(d.apply_error, 'error')
        await load()
      } catch (e) { toast(e.message, 'error') }
    })
  }

  const copyParams = async (inb) => {
    try {
      await copyText(inboundParamLines(inb))
      toast('参数已复制')
    } catch {
      setParamInb(inb)
      toast('浏览器不允许自动复制，请手动选中', 'error')
    }
  }

  const copyShare = async (inb) => {
    try {
      const d = await api.get(`/inbounds/${inb.id}/share`)
      if (!d?.uri) {
        toast('这个节点还没有分享链接', 'error')
        return
      }
      try {
        await copyText(d.uri)
        toast('已复制当前账号的节点链接')
      } catch {
        setShareText(d.uri)
        toast('浏览器不允许自动复制，请手动选中链接', 'error')
      }
    } catch (e) {
      toast(e.message, 'error')
    }
  }

  const direct = useMemo(() => list.filter(isDirectNode), [list])
  const groups = useMemo(() => {
    const needle = q.trim().toLowerCase()
    const sid = Number(serverQ) || 0
    const out = []
    for (const s of servers) {
      if (sid && Number(s.id) !== sid) continue
      let nodes = direct.filter(x => Number(x.server_id) === Number(s.id))
      if (statusFilter === '故障') nodes = nodes.filter(x => { const st = nodeStatus(x, s); return st === '故障' || st === '流量已满' })
      else if (statusFilter) nodes = nodes.filter(x => nodeStatus(x, s) === statusFilter)
      if (needle) {
        nodes = nodes.filter(x => [x.name, x.profile, protoShort(x.profile), x.port, s.name, s.public_host]
          .some(v => String(v || '').toLowerCase().includes(needle)))
      }
      if (!sid && (statusFilter || needle) && nodes.length === 0) continue
      out.push({ server: s, nodes })
    }
    return out
  }, [servers, direct, q, statusFilter, serverQ])

  const filteredServer = serverQ ? servers.find(s => String(s.id) === String(serverQ)) : null

  return (
    <div>
      <PageHead
        title="节点"
        desc="按实例分组。链式和端口中转在中转里。"
        actions={
          servers.length > 0 ? (
            <button type="button" className="btn-primary" onClick={() => openCreateLine()}>
              <Icon name="plus" size={15} /> 增加节点
            </button>
          ) : null
        }
      />
      {pollErr ? <div className="alert-row is-warn mb-4">{pollErr}</div> : null}
      {servers.length > 0 && (
        <div className="flex flex-col sm:flex-row sm:items-center gap-3 mb-4">
          <SearchInput value={q} onChange={e => setQ(e.target.value)} placeholder="搜索节点 / 协议 / 端口" />
          <FilterTabs
            value={statusFilter}
            onChange={setStatusFilter}
            items={[['','全部'],['正常','正常'],['停用','停用'],['离线','离线'],['故障','故障'],['流量已满','流量已满']]}
          />
          {filteredServer ? (
            <button type="button" className="row-act sm:ml-auto" onClick={() => setParams({})}>
              {filteredServer.name} · 全部实例
            </button>
          ) : (
            <div className="text-[12px] text-ink-mut font-mono sm:ml-auto whitespace-nowrap">
              {direct.length} 个节点
            </div>
          )}
        </div>
      )}
      {!ready ? (
        <div className="card overflow-hidden"><SkeletonRows /></div>
      ) : servers.length === 0 ? (
        <div className="card overflow-hidden">
          <Empty title="还没有实例" hint="先添加一台实例并装上 Agent，再来挂节点。" action={
            <Link to="/servers" className="btn-primary">去添加实例</Link>
          } />
        </div>
      ) : serverQ && !filteredServer ? (
        <div className="card overflow-hidden">
          <Empty title="没有这台实例" hint="回到全部节点，或去实例页。" action={
            <button type="button" className="btn-ghost" onClick={() => setParams({})}>全部节点</button>
          } />
        </div>
      ) : groups.length === 0 ? (
        <div className="card overflow-hidden">
          <Empty title="没有匹配的节点" hint="换个关键词或筛选。" />
        </div>
      ) : (
        <div>
          {groups.map(({ server: s, nodes }) => (
            <section key={s.id} className="node-cluster">
              <div className="node-cluster-head">
                <span className="machine-name truncate">{s.name}</span>
                <StatusWord online={s.online} fault={!!s.last_error} />
                <span className="node-cluster-count">{nodes.length} 个</span>
              </div>
              {nodes.length === 0 ? (
                <div className="card overflow-hidden">
                  <Empty
                    title={statusFilter || q ? '没有匹配的节点' : '这台实例还没有节点'}
                    hint={statusFilter || q ? '换个关键词或筛选。' : '选协议即可，名称和端口都可以留空。'}
                    action={!statusFilter && !q ? (
                      <button type="button" className="btn-primary" onClick={() => openCreateLine(s.id)}>
                        <Icon name="plus" size={15} /> 增加节点
                      </button>
                    ) : null}
                  />
                </div>
              ) : (
                <div className="machine-grid">
                  {nodes.map(inb => {
                    const st = nodeStatus(inb, s)
                    const used = (inb.used_up || 0) + (inb.used_down || 0)
                    return (
                      <div key={inb.id} className={`machine ${nodeTone(st)}`}>
                        <div className="machine-head">
                          <div className="min-w-0 flex-1">
                            <div className="machine-title">
                              <span className="machine-name truncate">{inb.name}</span>
                              <LineStatus status={st} />
                            </div>
                          </div>
                          <div className="machine-toolbar">
                            <button type="button" className="icon-btn" onClick={() => startEdit(inb)} aria-label="编辑节点" title="编辑">
                              <Icon name="pencil" size={14} />
                            </button>
                            <MoreMenu iconOnly items={[
                              { label: '编辑', onSelect: () => startEdit(inb) },
                              { label: '复制', onSelect: () => copyShare(inb) },
                              { label: '参数', onSelect: () => setParamInb(inb) },
                              { sep: true },
                              { label: inb.enabled ? '停用' : '启用', onSelect: () => toggle(inb) },
                              { sep: true },
                              { label: '删除', danger: true, onSelect: () => delLine(inb.id) },
                            ]} />
                          </div>
                        </div>
                        <div className="machine-metrics">
                          <Metric label="协议" value={protoShort(inb.profile)} plain />
                          <Metric label="端口" value={String(inb.port || '')} />
                          <Metric label="全站" value={fmtBytes(used)} />
                          <Metric label="实时" value={s.online ? `${fmtBps(s.net_up_bps)} / ${fmtBps(s.net_down_bps)}` : '—'} />
                        </div>
                        {st === '离线' ? <div className="text-[12px] text-ink-mut px-3 pb-1">实例离线，累计是上次在线时的数字</div> : null}
                        {st === '流量已满' ? <div className="text-[12px] px-3 pb-1" style={{ color: 'var(--color-danger)' }}>实例流量已满，节点已停用</div> : null}
                        <div className="machine-foot">
                          <button type="button" className="machine-ports" onClick={() => setParamInb(inb)}>参数</button>
                          <button type="button" className="row-act" onClick={() => copyShare(inb)}>复制</button>
                        </div>
                      </div>
                    )
                  })}
                </div>
              )}
            </section>
          ))}
        </div>
      )}

      <Modal open={lineOpen} title={editId ? '编辑节点' : '增加节点'} onClose={closeLine} size="xl" footer={
        <>
          <button type="button" className="btn-ghost" onClick={closeLine}>取消</button>
          <button type="submit" form="line-form" className="btn-primary" disabled={busy || destIsSelf}>{busy ? '保存中…' : (editId ? '保存' : '创建')}</button>
        </>
      }>
        <form id="line-form" onSubmit={submitLine} className="grid grid-cols-1 sm:grid-cols-2 gap-3">
          <Field label="实例" hint={waitOnline ? '实例离线，下发会等上线。' : ''}>
            <select
              className="input-field"
              value={f.server_id || ''}
              onChange={e => setF({ ...f, server_id: e.target.value })}
              required
              disabled={!!editId}
            >
              <option value="">选择实例</option>
              {servers.map(s => (
                <option key={s.id} value={s.id}>
                  {s.name}{s.online ? '' : ' · 离线'}{s.cores ? ` / ${s.cores}` : ''}
                </option>
              ))}
            </select>
          </Field>
          <div className="sm:col-span-2">
            <div className="text-[12px] font-medium text-ink-soft mb-1.5">协议</div>
            <div className="grid grid-cols-1 sm:grid-cols-2 gap-2">
              {protoList.map(p => {
                const on = f.profile === p.id
                return (
                  <button
                    type="button"
                    key={p.id}
                    className={`node-pick ${on ? 'is-on' : ''}`}
                    disabled={!!editId}
                    aria-pressed={on}
                    onClick={() => setF({ ...f, profile: p.id })}
                  >
                    <span className="min-w-0 flex-1 text-left">
                      <span className="flex items-center gap-1.5 flex-wrap">
                        <span className="text-[13px] font-medium">{p.title}</span>
                        {p.id === 'vless-reality-vision' ? <Badge tone="ok">推荐</Badge> : null}
                        {p.need_tls ? <Badge tone="muted">需证书</Badge> : null}
                        {p.landing === false ? <Badge tone="muted">仅入口</Badge> : null}
                      </span>
                      {p.desc ? <span className="block text-[12px] text-ink-mut mt-0.5 leading-snug">{p.desc}</span> : null}
                    </span>
                  </button>
                )
              })}
            </div>
            <p className="text-[11.5px] text-ink-mut mt-1.5">选协议后只显示这一路需要的项。REALITY 不用证书；TLS 协议要先在设置里签发。</p>
          </div>
          <Field label="名称" hint="可留空，保存时按协议和端口生成">
            <input className="input-field" value={f.name} onChange={e => setF({ ...f, name: e.target.value })} placeholder="" autoFocus />
          </Field>
          <Field label="端口" hint={selectedServer ? usedPortsText(selectedServer, list, editId) : '不填则随机，避开已用端口'}>
            <input className="input-field" type="number" min="1" max="65535" value={f.port} onChange={e => setF({ ...f, port: e.target.value })} placeholder={selectedServer ? `随机 ${formatPortRange(selectedServer)}` : '随机'} />
          </Field>
          {meta?.need_tls && (
            <Field label="TLS 证书" hint="在设置里签发或上传">
              <select className="input-field" value={f.cert_id} onChange={e => setF({ ...f, cert_id: e.target.value })}>
                <option value="">选择证书</option>
                {certs.map(c => <option key={c.id} value={c.id}>{c.name}{c.expires_at ? ` / ${fmtDateShort(c.expires_at)}` : ''}</option>)}
              </select>
            </Field>
          )}
          {isReality(f.profile) && (
            <>
              <div>
                <Field label="伪装目标 dest" hint="探测时会看到这个网站。必须是别人的站点，不能填本机。">
                  <input className="input-field" value={f.dest} onChange={e => setF({ ...f, dest: e.target.value })} placeholder="www.cloudflare.com:443" />
                </Field>
                <div className="flex flex-wrap gap-1.5 mt-1.5">
                  {DEST_PRESETS.map(d => (
                    <button
                      type="button"
                      key={d}
                      className={`chip ${f.dest === d ? 'is-on' : ''}`}
                      onClick={() => setF({ ...f, dest: d })}
                    >{d.split(':')[0]}</button>
                  ))}
                </div>
              </div>
              <Field label="SNI / serverNames" hint="客户端校验用。留空则用 dest 的域名。可逗号分隔多个。">
                <input className="input-field" value={f.sni} onChange={e => setF({ ...f, sni: e.target.value })} placeholder={destHostName || 'www.cloudflare.com'} />
              </Field>
            </>
          )}
          {needsTLS(f.profile) && (
            <Field label="SNI" hint="客户端校验的域名。留空则用实例公开地址。">
              <input className="input-field" value={f.sni} onChange={e => setF({ ...f, sni: e.target.value })} placeholder={selectedServer?.public_host || ''} />
            </Field>
          )}
          {f.profile === 'vless-xhttp-tls' && (
            <Field label="Path" hint="可留空自动生成。建议随机路径，不要用 /">
              <input className="input-field" value={f.path} onChange={e => setF({ ...f, path: e.target.value })} placeholder="/随机" />
            </Field>
          )}
          {f.profile === 'ss2022' && (
            <Field label="加密" hint="256 更稳妥。已有节点改方法会让旧订阅失效。">
              <select className="input-field" value={f.method} onChange={e => setF({ ...f, method: e.target.value })}>
                <option value="2022-blake3-aes-256-gcm">2022-blake3-aes-256-gcm</option>
                <option value="2022-blake3-aes-128-gcm">2022-blake3-aes-128-gcm</option>
              </select>
            </Field>
          )}
          {f.profile === 'mieru' && (
            <Field label="传输" hint="BOTH 同时开 TCP 和 UDP。只当入口，不能当链式落地。">
              <select className="input-field" value={f.transport} onChange={e => setF({ ...f, transport: e.target.value })}>
                <option value="BOTH">BOTH（TCP + UDP）</option>
                <option value="TCP">仅 TCP</option>
                <option value="UDP">仅 UDP</option>
              </select>
            </Field>
          )}
          {f.profile === 'anytls' && (
            <div className="notice sm:col-span-2">AnyTLS 走 sing-box，需要证书。不能当链式落地。</div>
          )}
          {f.profile === 'mieru' && (
            <div className="notice sm:col-span-2">Mieru 只当入口。链式转发时请把它放在入口机，落地用 VLESS / Trojan / SS2022 / SOCKS5。</div>
          )}
          {f.profile === 'socks5' && (
            <div className="notice sm:col-span-2">SOCKS5 走 sing-box，用户名密码认证，可 UDP。浏览器和 Clash 可直连，也可当链式落地。</div>
          )}
          {destIsSelf && (
            <div className="notice sm:col-span-2">dest 指向了这台机器自己，伪装会失效，也无法保存。请改成 microsoft / apple 这类真实网站。</div>
          )}
          <div className="sm:col-span-2">
            <button type="button" className="row-act" onClick={() => setShowAdv(v => !v)} aria-expanded={showAdv}>
              {showAdv ? '收起高级选项' : '高级选项 / 指纹 / 密钥 / TLS'}
            </button>
          </div>
          {showAdv && (
            <>
              <Field label="监听地址" hint="一般保持 0.0.0.0">
                <input className="input-field" value={f.listen} onChange={e => setF({ ...f, listen: e.target.value })} />
              </Field>
              {(isReality(f.profile) || needsTLS(f.profile)) && (
                <Field label="uTLS 指纹" hint="客户端伪装成这种浏览器的 TLS 握手。">
                  <select className="input-field" value={f.fingerprint} onChange={e => setF({ ...f, fingerprint: e.target.value })}>
                    {FINGERPRINTS.map(x => <option key={x} value={x}>{x}</option>)}
                  </select>
                </Field>
              )}
              {isReality(f.profile) && (
                <>
                  <Field label="shortId" hint="偶数位十六进制，最长 16 位。可逗号分隔多个。留空则自动生成。">
                    <input className="input-field font-mono" value={f.short_ids} onChange={e => setF({ ...f, short_ids: e.target.value })} placeholder="自动" />
                  </Field>
                  <Field label="xver" hint="PROXY protocol 版本。0 关闭，1 / 2 仅在 dest 支持时打开。">
                    <select className="input-field" value={f.xver} onChange={e => setF({ ...f, xver: Number(e.target.value) })}>
                      <option value={0}>0 关闭</option>
                      <option value={1}>1</option>
                      <option value={2}>2</option>
                    </select>
                  </Field>
                  <Field label="spiderX" hint="客户端爬虫路径，写入订阅。可留空。">
                    <input className="input-field font-mono" value={f.spider_x} onChange={e => setF({ ...f, spider_x: e.target.value })} placeholder="/" />
                  </Field>
                  {editId ? (
                    <div className="sm:col-span-2">
                      <button
                        type="button"
                        className={`row-act ${f.rotate_keys ? 'is-danger' : ''}`}
                        onClick={() => setF({ ...f, rotate_keys: !f.rotate_keys })}
                      >
                        {f.rotate_keys ? '将在保存时重新生成密钥（旧订阅会失效）' : '重新生成 REALITY 密钥'}
                      </button>
                    </div>
                  ) : (
                    <p className="text-[12px] text-ink-mut sm:col-span-2">公私钥在创建时自动生成，可在节点「参数」里查看公钥。</p>
                  )}
                </>
              )}
              {needsTLS(f.profile) && (
                <>
                  <Field label="最低 TLS" hint="1.3 更安全。只有老客户端连不上时才降到 1.2。">
                    <select className="input-field" value={f.min_version} onChange={e => setF({ ...f, min_version: e.target.value })}>
                      <option value="1.3">TLS 1.3</option>
                      <option value="1.2">TLS 1.2（兼容）</option>
                    </select>
                  </Field>
                  <Field label="ALPN" hint="逗号分隔。默认 h2,http/1.1。">
                    <input className="input-field font-mono" value={f.alpn} onChange={e => setF({ ...f, alpn: e.target.value })} />
                  </Field>
                  <label className="flex items-start gap-2 sm:col-span-2 text-[13px] text-ink-soft">
                    <input
                      type="checkbox"
                      className="mt-0.5"
                      checked={f.reject_unknown_sni !== false}
                      onChange={e => setF({ ...f, reject_unknown_sni: e.target.checked })}
                    />
                    <span>拒绝未知 SNI。客户端给的域名必须对得上证书，可挡住乱扫。</span>
                  </label>
                </>
              )}
              {f.profile === 'vless-xhttp-tls' && (
                <>
                  <Field label="XHTTP mode" hint="auto 即可。stream-up 适合大流量，老客户端可能不认。">
                    <select className="input-field" value={f.mode} onChange={e => setF({ ...f, mode: e.target.value })}>
                      <option value="auto">auto</option>
                      <option value="packet-up">packet-up</option>
                      <option value="stream-up">stream-up</option>
                      <option value="stream-one">stream-one</option>
                    </select>
                  </Field>
                  <Field label="Host" hint="可选。HTTP Host，一般留空。">
                    <input className="input-field" value={f.host} onChange={e => setF({ ...f, host: e.target.value })} />
                  </Field>
                </>
              )}
            </>
          )}
          {Number(f.port) === 443 && (
            <div className="notice sm:col-span-2">443 很容易被 Nginx / 其它面板占用。建议改成 8443 或其它空闲端口。</div>
          )}
          {missingCore && (
            <div className="notice sm:col-span-2">这台实例还没有 {meta.core}。创建后会自动从 GitHub 下载并拉起，第一次可能要等一会儿。实例需要能访问 GitHub。</div>
          )}
        </form>
      </Modal>

      <Modal open={!!paramInb} title={paramInb ? `${paramInb.name} 参数` : '参数'} onClose={() => setParamInb(null)} wide footer={
        <>
          <button type="button" className="btn-ghost" onClick={() => setParamInb(null)}>关闭</button>
          {paramInb && (
            <button type="button" className="btn-primary" onClick={() => copyParams(paramInb)}>
              <Icon name="copy" size={15} /> 复制全部
            </button>
          )}
        </>
      }>
        {paramInb && inboundParamRows(paramInb).map(([k, v]) => (
          <div key={k} className="param-row">
            <div className="kicker">{k}</div>
            <code className="text-[12px] break-all font-mono">{v}</code>
            <button type="button" className="btn-ghost h-8 px-2" onClick={async () => {
              try { await copyText(String(v)); toast(`已复制 ${k}`) }
              catch { toast('请手动选中复制', 'error') }
            }}><Icon name="copy" size={13} /></button>
          </div>
        ))}
        <p className="text-[12px] text-ink-mut mt-3">这些是服务端参数，不能直接导入客户端。点节点上的「复制」拿协议链接。</p>
      </Modal>

      <Modal open={!!shareText} title="节点链接" onClose={() => setShareText('') } footer={
        <>
          <button type="button" className="btn-ghost" onClick={() => setShareText('')}>关闭</button>
          <button type="button" className="btn-primary" onClick={async () => {
            try { await copyText(shareText); toast('已复制节点链接') }
            catch { toast('请手动选中复制', 'error') }
          }}>
            <Icon name="copy" size={15} /> 复制
          </button>
        </>
      }>
        <p className="text-[12px] text-ink-mut mb-2">粘贴到小火箭 / v2rayN / Nekobox 即可导入。</p>
        <code className="block text-[12px] break-all font-mono p-3" style={{ background: 'var(--color-fill)' }}>{shareText}</code>
      </Modal>
    </div>
  )
}
