import { useEffect, useMemo, useState } from 'react'
import { api } from '../lib/api'
import { copyText } from '../lib/copy'
import { useToast, useDialog } from '../components/Layout'
import { Badge, Empty, Field, FilterTabs, Icon, Meter, Modal, MoreMenu, PageHead, SearchInput, StatusWord, fmtAgo, fmtBps, fmtBytes, fmtDateShort, machineTone } from '../components/ui'

const DEST_PRESETS = [
  'www.microsoft.com:443',
  'www.apple.com:443',
  'dl.google.com:443',
  'www.cloudflare.com:443',
  'www.samsung.com:443',
]

const FINGERPRINTS = ['chrome', 'firefox', 'safari', 'ios', 'android', 'edge', 'qq', 'random', 'randomized']

const emptyLine = {
  server_id: 0, name: '', profile: 'vless-reality-vision', port: '', listen: '0.0.0.0',
  line_kind: 'direct', exit_inbound_id: 0, cert_id: 0, enabled: true,
  dest: 'www.microsoft.com:443', sni: '', path: '', host: '', mode: 'auto',
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

function usedPortsText(serverId, list, excludeId = 0) {
  const ports = list
    .filter(x => Number(x.server_id) === Number(serverId) && Number(x.id) !== Number(excludeId))
    .map(x => x.port)
  const used = ports.length ? `已用 ${[...new Set(ports)].sort((a, b) => a - b).join('、')}` : '这台服务器还没有节点'
  return `不填则随机，避开已用端口。${used}`
}

function serverHasCore(s, core) {
  if (!s?.cores) return true
  const have = String(s.cores).split(',').map(x => x.trim().toLowerCase().replace('sing-box', 'singbox')).filter(Boolean)
  if (!have.length) return true
  const want = String(core || 'xray').toLowerCase().replace('sing-box', 'singbox')
  return have.includes(want)
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

function gbFromLimit(n) {
  if (!n) return ''
  const gb = Number(n) / (1024 ** 3)
  if (!Number.isFinite(gb) || gb <= 0) return ''
  return String(Math.round(gb * 1000) / 1000)
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
    default: return profile || ''
  }
}

function Metric({ label, value }) {
  return (
    <div className="metric">
      <span className="metric-k">{label}</span>
      <span className="metric-v">{value}</span>
    </div>
  )
}

function machineMeta(s) {
  const cores = String(s.cores || '').split(',').map(x => x.trim()).filter(Boolean)
  const bits = [
    s.agent_ver ? `Agent ${s.agent_ver}` : null,
    `心跳 ${fmtAgo(s.last_seen)}`,
    [s.os, s.arch].filter(Boolean).join('/') || null,
    s.disk_total ? `磁盘 ${fmtBytes(s.disk_free)}` : null,
    s.conns != null && s.conns !== '' ? `连接 ${s.conns}` : null,
    cores.length ? cores.join(', ') : null,
  ].filter(Boolean)
  return bits.join(' / ')
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
    line_kind: inb.line_kind || 'direct',
    exit_inbound_id: inb.exit_inbound_id || 0,
    cert_id: inb.cert_id || 0,
    enabled: inb.enabled !== false,
    dest: st.dest || 'www.microsoft.com:443',
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
  const [servers, setServers] = useState([])
  const [list, setList] = useState([])
  const [certs, setCerts] = useState([])
  const [profiles, setProfiles] = useState([])
  const [name, setName] = useState('')
  const [host, setHost] = useState('')
  const [cmd, setCmd] = useState('')
  const [nodeOpen, setNodeOpen] = useState(false)
  const [f, setF] = useState(emptyLine)
  const [editId, setEditId] = useState(0)
  const [lineOpen, setLineOpen] = useState(false)
  const [busy, setBusy] = useState(false)
  const [paramInb, setParamInb] = useState(null)
  const [shareText, setShareText] = useState('')
  const [showAdv, setShowAdv] = useState(false)
  const [cfOpen, setCfOpen] = useState(false)
  const [cfServer, setCfServer] = useState(null)
  const [cfDomains, setCfDomains] = useState([])
  const [cfLoading, setCfLoading] = useState(false)
  const [cfErr, setCfErr] = useState('')
  const [cfPick, setCfPick] = useState('')
  const [cfFilter, setCfFilter] = useState('')
  const [q, setQ] = useState('')
  const [statusFilter, setStatusFilter] = useState('')

  const load = async () => {
    try {
      const [a, b, c, d] = await Promise.all([
        api.get('/servers'), api.get('/inbounds'), api.get('/certs'), api.get('/profiles'),
      ])
      setServers(a.servers || [])
      setList(b.inbounds || [])
      setCerts(c.certs || [])
      setProfiles(d.profiles || [])
    } catch (e) { toast(e.message, 'error') }
  }
  useEffect(() => {
    load()
    const t = setInterval(() => {
      api.get('/servers').then(a => setServers(a.servers || [])).catch(() => {})
    }, 5000)
    return () => clearInterval(t)
  }, [])

  const meta = profiles.find(p => p.id === f.profile)
  const selectedServer = servers.find(s => Number(s.id) === Number(f.server_id))
  const missingCore = selectedServer && meta && !serverHasCore(selectedServer, meta.core)
  const landings = list.filter(x => x.line_kind === 'direct' && ['vless-reality', 'vless-reality-vision', 'vless-xhttp-tls', 'trojan-tls', 'ss2022'].includes(x.profile))
  const protoList = profiles.length ? profiles : [{ id: f.profile, title: f.profile, desc: '' }]
  const destHostName = String(f.dest || '').split(':')[0].trim().toLowerCase()
  const destIsSelf = isReality(f.profile) && destHostName && [selectedServer?.public_host, selectedServer?.connect_ip]
    .filter(Boolean)
    .some(h => String(h).split(':')[0].trim().toLowerCase() === destHostName)

  const openCreateNode = () => {
    setName('')
    setHost('')
    setNodeOpen(true)
  }

  const createNode = async (e) => {
    e.preventDefault()
    setBusy(true)
    try {
      const d = await api.post('/servers', { name, public_host: host })
      setName(''); setHost('')
      setNodeOpen(false)
      setCmd(d.install || '')
      toast('已添加服务器')
      load()
    } catch (e) { toast(e.message, 'error') }
    finally { setBusy(false) }
  }

  const showInstall = async (id) => {
    try {
      const d = await api.get(`/servers/${id}/install`)
      setCmd(d.command)
    } catch (e) { toast(e.message, 'error') }
  }

  const sync = async (id) => {
    try { await api.post(`/servers/${id}/sync`); toast('已下发'); load() }
    catch (e) { toast(e.message, 'error'); load() }
  }

  const delNode = async (id) => {
    if (!(await dialog.confirm({ title: '删除服务器', message: '这台机器上的节点也会一并删除，且无法恢复。', danger: true, okText: '删除' }))) return
    try { await api.del(`/servers/${id}`); load(); toast('已删除') }
    catch (e) { toast(e.message, 'error') }
  }

  const saveHost = async (s) => {
    const v = await dialog.prompt({ title: '公开地址', message: '客户端连接用的 IP 或域名。也可点「从 CF 同步」拉取托管域名。', defaultValue: s.public_host || '', okText: '保存' })
    if (v == null) return
    try { await api.put(`/servers/${s.id}`, { name: s.name, public_host: v }); load() }
    catch (e) { toast(e.message, 'error') }
  }

  const openCF = async (s) => {
    setCfServer(s)
    setCfOpen(true)
    setCfDomains([])
    setCfErr('')
    setCfPick('')
    setCfFilter('')
    setCfLoading(true)
    try {
      const d = await api.get(s?.id ? `/servers/${s.id}/cf-domains` : '/cf-domains')
      const names = d.domains || []
      setCfDomains(names)
      const current = s?.public_host || host
      const matched = names.find(x => x.matched)
      const cur = names.find(x => x.name === current)
      setCfPick((matched || cur || names[0] || {}).name || '')
    } catch (e) {
      setCfErr(e.message || '拉取失败')
    } finally {
      setCfLoading(false)
    }
  }

  const applyCF = async (picked) => {
    const name = String(typeof picked === 'string' ? picked : (cfPick || '')).trim()
    if (!name) {
      toast('请选择一个域名', 'error')
      return
    }
    if (!cfServer?.id) {
      setHost(name)
      setCfOpen(false)
      toast('已填入 ' + name)
      return
    }
    setBusy(true)
    try {
      await api.put(`/servers/${cfServer.id}`, { public_host: name })
      toast('公开地址已设为 ' + name)
      setCfOpen(false)
      load()
    } catch (e) { toast(e.message, 'error') }
    finally { setBusy(false) }
  }

  const saveLimit = async (s) => {
    const v = await dialog.prompt({
      title: `${s.name} 流量上限`,
      message: '单位 GB。留空表示不限。已用流量按这台机器上的线路累计。',
      defaultValue: gbFromLimit(s.traffic_limit),
      okText: '保存',
    })
    if (v == null) return
    const n = String(v).trim()
    let bytes = 0
    if (n !== '') {
      const gb = Number(n)
      if (!Number.isFinite(gb) || gb < 0) {
        toast('上限无效', 'error')
        return
      }
      bytes = Math.round(gb * 1024 * 1024 * 1024)
    }
    try {
      await api.put(`/servers/${s.id}`, { traffic_limit: bytes })
      load()
    } catch (e) { toast(e.message, 'error') }
  }

  const upgradeAgent = async (s) => {
    if (s.needs_reinstall) {
      toast('该 Agent 版本太旧，不支持远程升级。请用安装命令重装一次', 'error')
      showInstall(s.id)
      return
    }
    if (!(await dialog.confirm({
      title: '升级 Agent',
      message: `将 ${s.name} 的 Agent 升到面板版本。机器必须在线，大约几十秒。升级后会自动重启 Agent。`,
      okText: '升级',
    }))) return
    setBusy(true)
    try {
      const d = await api.post(`/servers/${s.id}/upgrade-agent`)
      toast(`已升级到 ${d.version || '当前版本'}，Agent 正在重启`)
      setTimeout(load, 2500)
    } catch (e) {
      toast(e.message, 'error')
      if (e.code === 'agent_too_old') showInstall(s.id)
    } finally { setBusy(false) }
  }

  const uninstallAgent = async (s) => {
    if (s.needs_reinstall) {
      toast('该 Agent 版本太旧，不支持远程卸载。请用安装命令重装或在机器上手动停服务', 'error')
      return
    }
    if (!(await dialog.confirm({
      title: '卸载 Agent',
      message: `停掉 ${s.name} 上的内核和 Agent。若与面板同机，不会删除面板程序和数据库。离线机器无法远程卸载。`,
      danger: true,
      okText: '卸载',
    }))) return
    setBusy(true)
    try {
      await api.post(`/servers/${s.id}/uninstall-agent`, { confirm: true })
      toast('已卸载 Agent')
      load()
    } catch (e) { toast(e.message, 'error') }
    finally { setBusy(false) }
  }

  const rotateToken = async (s) => {
    if (!(await dialog.confirm({
      title: '轮换令牌',
      message: `当前安装命令立刻失效。需要把新命令再跑一遍才能连上。`,
      danger: true,
      okText: '轮换',
    }))) return
    try {
      const d = await api.post(`/servers/${s.id}/rotate-token`)
      if (d.command) setCmd(d.command)
      toast('令牌已更换')
      load()
    } catch (e) { toast(e.message, 'error') }
  }

  const openCreateLine = (serverId) => {
    const sid = Number(serverId)
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
      line_kind: f.line_kind,
      enabled: f.enabled !== false,
      settings,
    }
    if (f.cert_id) body.cert_id = Number(f.cert_id)
    if (f.line_kind === 'chain' && f.exit_inbound_id) body.exit_inbound_id = Number(f.exit_inbound_id)
    return body
  }

  const submitLine = async (e) => {
    e.preventDefault()
    const raw = String(f.port ?? '').trim()
    if (raw !== '') {
      const port = Number(raw)
      if (!Number.isInteger(port) || port < 1 || port > 65535) {
        toast('端口范围 1–65535，或不填则随机', 'error')
        return
      }
    } else if (editId) {
      toast('编辑时需要填写端口', 'error')
      return
    }
    const wasEdit = !!editId
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
    try { await api.del(`/inbounds/${id}`); if (editId === id) closeLine(); load() }
    catch (e) { toast(e.message, 'error') }
  }

  const toggle = async (inb) => {
    try {
      const d = await api.put(`/inbounds/${inb.id}`, {
        enabled: !inb.enabled, name: inb.name, port: inb.port, listen: inb.listen,
        settings: inb.settings, line_kind: inb.line_kind, exit_inbound_id: inb.exit_inbound_id, cert_id: inb.cert_id,
      })
      if (d.apply_error) toast(d.apply_error, 'error')
      load()
    } catch (e) { toast(e.message, 'error') }
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
      try {
        await copyText(d.uri)
        toast('已复制节点链接')
      } catch {
        setShareText(d.uri)
        toast('浏览器不允许自动复制，请手动选中链接', 'error')
      }
    } catch (e) {
      toast(e.message, 'error')
    }
  }

  const linesOf = (id) => list.filter(x => Number(x.server_id) === Number(id))
  const online = servers.filter(s => s.online).length
  const visibleServers = useMemo(() => {
    const needle = q.trim().toLowerCase()
    return servers.filter(s => {
      if (statusFilter === 'online' && !s.online) return false
      if (statusFilter === 'offline' && s.online) return false
      if (statusFilter === 'upgrade' && !(s.needs_upgrade || s.needs_reinstall)) return false
      if (!needle) return true
      const names = linesOf(s.id).map(x => x.name).join(' ')
      const hay = [s.name, s.public_host, s.agent_ver, names]
      return hay.some(x => String(x || '').toLowerCase().includes(needle))
    })
  }, [servers, list, q, statusFilter])

  return (
    <div>
      <PageHead
        title="服务器管理"
        desc="一台机器一个 Agent，下面挂节点。"
        actions={
          <button type="button" className="btn-primary" onClick={openCreateNode}>
            <Icon name="plus" size={15} /> 添加服务器
          </button>
        }
      />
      {servers.length > 0 && (
        <div className="flex flex-col sm:flex-row sm:items-center gap-3 mb-4">
          <SearchInput value={q} onChange={e => setQ(e.target.value)} placeholder="搜索服务器 / 节点 / 地址" />
          <FilterTabs
            value={statusFilter}
            onChange={setStatusFilter}
            items={[['','全部'],['online','在线'],['offline','离线'],['upgrade','可升级']]}
          />
          <div className="text-[12px] text-ink-mut font-mono sm:ml-auto whitespace-nowrap">
            {online} 在线 / {servers.length - online} 离线 / {list.length} 节点
          </div>
        </div>
      )}
      {servers.length === 0 ? (
        <div className="card overflow-hidden">
          <Empty title="还没有服务器" hint="先起一个名字，添加后把安装命令拿到机器上以 root 执行，再增加节点。" action={
            <button type="button" className="btn-primary" onClick={openCreateNode}><Icon name="plus" size={15} /> 添加服务器</button>
          } />
        </div>
      ) : (
        <div>
          {visibleServers.length === 0 ? (
            <div className="card overflow-hidden">
              <Empty title="没有匹配的服务器" hint="换个关键词或筛选。" />
            </div>
          ) : null}
          {visibleServers.map(s => {
            const lines = linesOf(s.id)
            const used = (s.used_up || 0) + (s.used_down || 0)
            const load = s.online && s.load_milli ? (Number(s.load_milli) / 1000).toFixed(2) : '—'
            const mem = s.mem_avail ? fmtBytes(s.mem_avail) : '—'
            return (
              <div key={s.id} className={`machine ${machineTone(s)}`}>
                <div className="machine-head">
                  <div className="min-w-0 flex-1">
                    <div className="machine-title">
                      <span className="machine-name truncate">{s.name}</span>
                      <StatusWord online={s.online} fault={!!s.last_error} />
                      {s.needs_reinstall ? <Badge tone="warn">需重装</Badge>
                        : s.needs_upgrade ? <Badge tone="warn">可升级</Badge> : null}
                      {s.over_quota ? <Badge tone="danger">流量已满</Badge> : null}
                    </div>
                    <div className="machine-host">
                      <span className="machine-host-addr">{s.public_host || '未填公开地址'}</span>
                      <button type="button" className="icon-btn" onClick={() => saveHost(s)} aria-label="改公开地址" title="改公开地址">
                        <Icon name="pencil" size={13} />
                      </button>
                    </div>
                  </div>
                  <div className="machine-toolbar">
                    <button type="button" className="btn-ghost h-8" onClick={() => openCreateLine(s.id)}>
                      <Icon name="plus" size={14} /> 增加节点
                    </button>
                    <MoreMenu iconOnly items={[
                      { label: '同步', hint: '把配置下发到这台机器', onSelect: () => sync(s.id) },
                      { label: '安装命令', onSelect: () => showInstall(s.id) },
                      { label: '从 CF 同步', onSelect: () => openCF(s) },
                      { label: '改公开地址', onSelect: () => saveHost(s) },
                      { label: '流量上限', onSelect: () => saveLimit(s) },
                      { sep: true },
                      {
                        label: '一键升级',
                        hint: s.needs_reinstall ? '版本太旧，将打开安装命令' : (s.online ? '升到面板版本并重启' : '需在线'),
                        disabled: !s.online || busy,
                        onSelect: () => upgradeAgent(s),
                      },
                      { label: '轮换令牌', onSelect: () => rotateToken(s) },
                      { sep: true },
                      { label: '一键卸载', danger: true, disabled: !s.online || busy, hint: s.needs_reinstall ? '版本太旧，无法远程卸载' : '同机不删面板', onSelect: () => uninstallAgent(s) },
                      { label: '删除服务器', danger: true, onSelect: () => delNode(s.id) },
                    ]} />
                  </div>
                </div>
                <div className="machine-metrics">
                  <Metric label="上行" value={s.online ? fmtBps(s.net_up_bps) : '—'} />
                  <Metric label="下行" value={s.online ? fmtBps(s.net_down_bps) : '—'} />
                  <Metric label="负载" value={load} />
                  <Metric label="内存" value={mem} />
                </div>
                <div className="machine-meter">
                  <Meter value={used} max={Number(s.traffic_limit) || 0} />
                </div>
                {s.last_error ? <div className="machine-fault">{s.last_error}</div> : null}
                <div className="machine-meta">{machineMeta(s)}</div>
                <div className="machine-nodes">
                {lines.length === 0 ? (
                  <div className="machine-nodes-empty">还没有节点。选协议即可，名称和端口都可以留空。</div>
                ) : (
                  <>
                  <div className="hidden md:block table-wrap">
                    <table className="data">
                      <thead><tr><th>名称</th><th>协议</th><th>端口</th><th>流量</th><th></th></tr></thead>
                      <tbody>
                        {lines.map(inb => {
                          const dead = !serverHasCore(s, inb.core)
                          return (
                            <tr key={inb.id} className={!inb.enabled ? 'opacity-50' : ''}>
                              <td className="font-medium">
                                {inb.name}
                                {inb.line_kind === 'chain' ? <span className="text-ink-mut font-normal"> 链式</span> : null}
                                {dead ? <span className="text-ink-mut font-normal"> 未安装</span> : null}
                                {!inb.enabled ? <span className="text-ink-mut font-normal"> 停用</span> : null}
                              </td>
                              <td className="text-ink-mut" title={inb.profile}>{protoShort(inb.profile)}</td>
                              <td className="tabular-nums font-mono text-[12px]">{inb.port}</td>
                              <td className="tabular-nums font-mono text-[12px] whitespace-nowrap">{fmtBytes((inb.used_up || 0) + (inb.used_down || 0))}</td>
                              <td className="whitespace-nowrap">
                                <div className="icon-row">
                                  <button type="button" className="icon-btn" onClick={() => copyShare(inb)} aria-label="复制节点链接" title="复制">
                                    <Icon name="copy" size={14} />
                                  </button>
                                  <button type="button" className="icon-btn" onClick={() => startEdit(inb)} aria-label="编辑节点" title="编辑">
                                    <Icon name="pencil" size={14} />
                                  </button>
                                  <MoreMenu iconOnly items={[
                                    { label: '参数', onSelect: () => setParamInb(inb) },
                                    { label: inb.enabled ? '停用' : '启用', onSelect: () => toggle(inb) },
                                    { sep: true },
                                    { label: '删除', danger: true, onSelect: () => delLine(inb.id) },
                                  ]} />
                                </div>
                              </td>
                            </tr>
                          )
                        })}
                      </tbody>
                    </table>
                  </div>
                  <div className="md:hidden divide-y" style={{ borderColor: 'var(--color-line-soft)' }}>
                    {lines.map(inb => {
                      const dead = !serverHasCore(s, inb.core)
                      return (
                        <div key={inb.id} className={`px-3.5 py-3 ${!inb.enabled ? 'opacity-50' : ''}`}>
                          <div className="flex items-start justify-between gap-3">
                            <div className="min-w-0">
                              <div className="font-medium truncate">{inb.name}</div>
                              <div className="text-[12px] text-ink-mut mt-0.5">
                                {protoShort(inb.profile)} / {inb.port}
                                {inb.line_kind === 'chain' ? ' / 链式' : ''}
                                {dead ? ' / 未安装' : ''}
                              </div>
                              <div className="text-[12px] text-ink-mut tabular-nums font-mono mt-0.5">{fmtBytes((inb.used_up || 0) + (inb.used_down || 0))}</div>
                            </div>
                            <div className="icon-row shrink-0">
                              <button type="button" className="icon-btn" onClick={() => copyShare(inb)} aria-label="复制节点链接" title="复制">
                                <Icon name="copy" size={14} />
                              </button>
                              <MoreMenu iconOnly items={[
                                { label: '编辑', onSelect: () => startEdit(inb) },
                                { label: '参数', onSelect: () => setParamInb(inb) },
                                { label: inb.enabled ? '停用' : '启用', onSelect: () => toggle(inb) },
                                { sep: true },
                                { label: '删除', danger: true, onSelect: () => delLine(inb.id) },
                              ]} />
                            </div>
                          </div>
                        </div>
                      )
                    })}
                  </div>
                  </>
                )}
                </div>
              </div>
            )
          })}
        </div>
      )}

      <Modal open={nodeOpen} title="添加服务器" onClose={() => setNodeOpen(false)} footer={
        <>
          <button type="button" className="btn-ghost" onClick={() => setNodeOpen(false)}>取消</button>
          <button type="submit" form="node-form" className="btn-primary" disabled={busy}>{busy ? '添加中…' : '添加'}</button>
        </>
      }>
        <form id="node-form" onSubmit={createNode} className="space-y-3">
          <Field label="名称">
            <input className="input-field" value={name} onChange={e => setName(e.target.value)} required placeholder="香港-01" autoFocus />
          </Field>
          <Field label="公开地址" hint="客户端连接用的 IP 或域名。托管在 Cloudflare 的可直接拉取。">
            <input className="input-field" value={host} onChange={e => setHost(e.target.value)} placeholder="IP 或域名" />
          </Field>
          <button type="button" className="row-act" onClick={() => openCF(null)}>从 CF 同步</button>
        </form>
      </Modal>

      <Modal open={!!cmd} title="一键安装 Agent" onClose={() => setCmd('')} wide footer={
        <>
          <button type="button" className="btn-ghost" onClick={() => setCmd('')}>关闭</button>
          <button type="button" className="btn-primary" onClick={async () => {
            try { await copyText(cmd); toast('已复制') }
            catch { toast('浏览器不允许自动复制，请手动选中命令', 'error') }
          }}>
            <Icon name="copy" size={15} /> 复制
          </button>
        </>
      }>
        <p className="text-[13px] text-ink-mut mb-3">在服务器上以 root 执行。明文 http 会自动带 --insecure。</p>
        <pre className="text-[12px] font-mono bg-raised p-3 overflow-x-auto whitespace-pre-wrap">{cmd}</pre>
      </Modal>

      <Modal open={lineOpen} title={editId ? '编辑节点' : '增加节点'} onClose={closeLine} size="xl" footer={
        <>
          <button type="button" className="btn-ghost" onClick={closeLine}>取消</button>
          <button type="submit" form="line-form" className="btn-primary" disabled={busy || destIsSelf}>{busy ? '保存中…' : (editId ? '保存' : '创建')}</button>
        </>
      }>
        <form id="line-form" onSubmit={submitLine} className="grid grid-cols-1 sm:grid-cols-2 gap-3">
          <Field label="服务器">
            <select className="input-field" value={f.server_id} disabled>
              {servers.map(s => <option key={s.id} value={s.id}>{s.name}{s.cores ? ` / ${s.cores}` : ''}</option>)}
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
          <Field label="端口" hint={f.server_id ? usedPortsText(f.server_id, list, editId) : '不填则随机，避开已用端口'}>
            <input className="input-field" type="number" min="1" max="65535" value={f.port} onChange={e => setF({ ...f, port: e.target.value })} placeholder="" />
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
                  <input className="input-field" value={f.dest} onChange={e => setF({ ...f, dest: e.target.value })} placeholder="www.microsoft.com:443" />
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
                <input className="input-field" value={f.sni} onChange={e => setF({ ...f, sni: e.target.value })} placeholder={destHostName || 'www.microsoft.com'} />
              </Field>
            </>
          )}
          {needsTLS(f.profile) && (
            <Field label="SNI" hint="客户端校验的域名。留空则用服务器公开地址。">
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
          <Field label="线路">
            <select className="input-field" value={f.line_kind} onChange={e => setF({ ...f, line_kind: e.target.value })} disabled={!!editId}>
              <option value="direct">直出</option>
              <option value="chain">链式（本机入口 → 另一台落地）</option>
            </select>
          </Field>
          {f.line_kind === 'chain' && (
            <Field label="落地线路" hint="不可选 Mieru / AnyTLS">
              <select className="input-field" value={f.exit_inbound_id} onChange={e => setF({ ...f, exit_inbound_id: e.target.value })}>
                <option value="">选择落地</option>
                {landings.map(x => <option key={x.id} value={x.id}>{x.server_name} / {x.name}</option>)}
              </select>
            </Field>
          )}
          {f.profile === 'anytls' && (
            <div className="notice sm:col-span-2">AnyTLS 走 sing-box，需要证书。不能当链式落地。</div>
          )}
          {f.profile === 'mieru' && (
            <div className="notice sm:col-span-2">Mieru 只当入口。链式转发时请把它放在入口机，落地用 VLESS / Trojan / SS2022。</div>
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
            <div className="notice sm:col-span-2">这台服务器还没有 {meta.core}。创建后会自动从 GitHub 下载并拉起，第一次可能要等一会儿。服务器需要能访问 GitHub。</div>
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

      <Modal open={!!shareText} title="节点链接" onClose={() => setShareText('')} footer={
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

      <Modal open={cfOpen} title="从 Cloudflare 同步域名" onClose={() => setCfOpen(false)} wide footer={
        <>
          <button type="button" className="btn-ghost" onClick={() => setCfOpen(false)}>取消</button>
          <button type="button" className="btn-primary" disabled={busy || cfLoading || !cfPick} onClick={() => applyCF()}>
            {busy ? '保存中…' : (cfServer?.id ? '设为公开地址' : '填入')}
          </button>
        </>
      }>
        <p className="text-[12.5px] text-ink-mut leading-relaxed mb-3">
          列出 Token 下已托管的域名。指向这台机器 IP 的会排在前面。橙云代理的记录 REALITY 可能连不上，TLS 节点一般没问题。
        </p>
        {cfLoading ? (
          <div className="text-[13px] text-ink-mut py-6 text-center">正在从 Cloudflare 拉取…</div>
        ) : cfErr ? (
          <div>
            <div className="notice">{cfErr}</div>
            {String(cfErr).includes('Token') ? (
              <a href="/settings?tab=certs" className="row-act mt-3 inline-block">去设置保存 Token</a>
            ) : null}
          </div>
        ) : (
          <>
            {cfDomains.length > 6 ? (
              <div className="mb-3">
                <SearchInput value={cfFilter} onChange={e => setCfFilter(e.target.value)} placeholder="筛选域名" />
              </div>
            ) : null}
            {(() => {
              const q = cfFilter.trim().toLowerCase()
              const shown = !q ? cfDomains : cfDomains.filter(d =>
                d.name.includes(q) || (d.zone || '').includes(q) || (d.content || '').includes(q)
              )
              if (!shown.length) {
                return <Empty title="没有可拉取的域名" hint="先把域名 NS 指到 Cloudflare，并确认 Token 绑了这个区。" />
              }
              const selected = cfDomains.find(d => d.name === cfPick)
              return (
                <>
                  <div className="space-y-1.5 max-h-[50vh] overflow-y-auto pr-0.5">
                    {shown.map(d => {
                      const on = cfPick === d.name
                      return (
                        <button
                          type="button"
                          key={d.name}
                          className={`node-pick ${on ? 'is-on' : ''}`}
                          aria-pressed={on}
                          onClick={() => setCfPick(d.name)}
                          onDoubleClick={() => applyCF(d.name)}
                        >
                          <span className={`node-check ${on ? 'is-on' : ''}`}>{on ? '✓' : ''}</span>
                          <span className="min-w-0 flex-1 text-left">
                            <span className="flex items-center gap-1.5 flex-wrap">
                              <span className="text-[13px] font-medium font-mono">{d.name}</span>
                              {d.matched ? <Badge tone="ok">指向本机</Badge> : null}
                              {d.proxied ? <Badge tone="muted">已代理</Badge> : null}
                            </span>
                            <span className="block text-[12px] text-ink-mut mt-0.5 font-mono truncate">
                              {[d.type, d.content].filter(Boolean).join(' · ') || d.zone || 'Zone'}
                            </span>
                          </span>
                        </button>
                      )
                    })}
                  </div>
                  {selected?.proxied ? (
                    <p className="text-[12px] mt-3" style={{ color: 'var(--color-danger)' }}>
                      这条开了 Cloudflare 代理（橙云）。REALITY 需要 DNS only；TLS 节点可以走橙云。
                    </p>
                  ) : null}
                </>
              )
            })()}
          </>
        )}
      </Modal>
    </div>
  )
}
