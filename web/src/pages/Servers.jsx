import { useEffect, useMemo, useRef, useState } from 'react'
import { Link } from 'react-router-dom'
import { api } from '../lib/api'
import { cacheGen, peekList, putList } from '../lib/listCache'
import { startPoll } from '../lib/poll'
import { createOpLock } from '../lib/opLock'
import { asArray, isAbort } from '../lib/safe'
import { copyText } from '../lib/copy'
import { isDirectNode, serverStatus } from '../lib/status'
import { useToast, useDialog } from '../components/Layout'
import { Badge, Empty, Field, FilterTabs, Icon, LineStatus, Meter, Modal, MoreMenu, PageHead, SearchInput, SkeletonRows, fmtAgo, fmtBps, fmtBytes, machineTone } from '../components/ui'
import { CORE_OPTIONS, coreLabel, fmtExpires, fmtResetDay, isExpired, parseCores, ymd, ymdToUnix } from '../lib/display'
import { DEFAULT_PORT_MAX, DEFAULT_PORT_MIN, formatPortRange, parsePort } from '../lib/ports'

function gbFromLimit(n) {
  if (!n) return ''
  const gb = Number(n) / (1024 ** 3)
  if (!Number.isFinite(gb) || gb <= 0) return ''
  return String(Math.round(gb * 1000) / 1000)
}

function Metric({ label, value, tone }) {
  return (
    <div className={`metric${tone ? ` is-${tone}` : ''}`}>
      <span className="metric-k">{label}</span>
      <span className="metric-v">{value}</span>
    </div>
  )
}

function ServerDates({ s }) {
  const expired = isExpired(s.expires_at)
  return (
    <div className="machine-dates">
      <div>
        <span className="machine-date-k">到期</span>
        <span className={`machine-date-v${expired ? ' is-expired' : ''}`}>{fmtExpires(s.expires_at)}</span>
      </div>
      <div>
        <span className="machine-date-k">流量重置</span>
        <span className="machine-date-v">{fmtResetDay(s.traffic_reset_day)}</span>
      </div>
    </div>
  )
}

function ServerMeta({ s }) {
  const cores = parseCores(s.cores)
  return (
    <div className="machine-meta">
      {s.agent_ver ? <span className="machine-meta-item">Agent {s.agent_ver}</span> : null}
      <span className="machine-meta-item">心跳 {fmtAgo(s.last_seen)}</span>
      {cores.map(c => (
        <span key={c} className={`core-tag is-${c}`}>{coreLabel(c)}</span>
      ))}
    </div>
  )
}

const DEFAULT_GH_PROXY = 'https://gh-proxy.com/'

function loadGhProxyPref() {
  try {
    const j = JSON.parse(localStorage.getItem('lk-agent-gh-proxy') || '{}')
    return { on: !!j.on, url: String(j.url || DEFAULT_GH_PROXY) }
  } catch {
    return { on: false, url: DEFAULT_GH_PROXY }
  }
}

function withGhProxyFlag(cmd, on, url) {
  if (!on || !cmd) return cmd
  const p = String(url || '').trim()
  if (!/^https?:\/\/[^ \t;|&`$<>\\]+$/i.test(p)) return cmd
  const norm = p.replace(/\/+$/, '') + '/'
  return `${cmd.replace(/\s+$/, '')} --gh-proxy ${norm}`
}

export default function Servers() {
  const toast = useToast()
  const dialog = useDialog()
  const [list, setList] = useState(() => peekList('servers') ?? [])
  const [ins, setIns] = useState(() => peekList('inbounds') ?? [])
  const [ready, setReady] = useState(() => peekList('servers') !== undefined)
  const [name, setName] = useState('')
  const [host, setHost] = useState('')
  const [cmd, setCmd] = useState('')
  const [cnInstall, setCnInstall] = useState(() => loadGhProxyPref().on)
  const [ghProxy, setGhProxy] = useState(() => loadGhProxyPref().url)
  const [formOpen, setFormOpen] = useState(false)
  const [busy, setBusy] = useState(false)
  const [busyId, setBusyId] = useState(0)
  const [pollErr, setPollErr] = useState('')
  const ops = useRef(createOpLock())
  const reloadTimer = useRef(0)
  const [cfOpen, setCfOpen] = useState(false)
  const [cfServer, setCfServer] = useState(null)
  const [cfDomains, setCfDomains] = useState([])
  const [cfLoading, setCfLoading] = useState(false)
  const [cfErr, setCfErr] = useState('')
  const [cfPick, setCfPick] = useState('')
  const [cfFilter, setCfFilter] = useState('')
  const [q, setQ] = useState('')
  const [statusFilter, setStatusFilter] = useState('')
  const [newPortMin, setNewPortMin] = useState(String(DEFAULT_PORT_MIN))
  const [newPortMax, setNewPortMax] = useState(String(DEFAULT_PORT_MAX))
  const [newExpires, setNewExpires] = useState('')
  const [newResetDay, setNewResetDay] = useState('0')
  const [editSrv, setEditSrv] = useState(null)
  const [edit, setEdit] = useState({ name: '', host: '', min: '', max: '', gb: '', expires: '', resetDay: '0' })
  const [coreSrv, setCoreSrv] = useState(null)
  const [corePick, setCorePick] = useState('xray')

  const load = async () => {
    try {
      const g = cacheGen()
      const [a, b] = await Promise.all([api.get('/servers'), api.get('/inbounds')])
      setList(putList('servers', asArray(a.servers), g))
      setIns(putList('inbounds', asArray(b.inbounds), g))
    } catch (e) { toast(e.message, 'error') }
    finally { setReady(true) }
  }
  useEffect(() => {
    load()
    return startPoll(async (signal) => {
      const g = cacheGen()
      try {
        const [a, b] = await Promise.all([api.get('/servers', signal), api.get('/inbounds', signal)])
        setList(putList('servers', asArray(a.servers), g))
        setIns(putList('inbounds', asArray(b.inbounds), g))
        setPollErr('')
      } catch (e) {
        if (isAbort(e)) return
        setPollErr('实时刷新失败，显示的是上次成功数据')
        throw e
      }
    }, 5000, { immediate: false })
  }, [])
  useEffect(() => () => { if (reloadTimer.current) clearTimeout(reloadTimer.current) }, [])

  const coreSrvId = coreSrv?.id
  useEffect(() => {
    if (!coreSrvId) return
    const next = list.find(x => Number(x.id) === Number(coreSrvId))
    if (!next) setCoreSrv(null)
    else setCoreSrv(next)
  }, [list, coreSrvId])

  useEffect(() => {
    try {
      localStorage.setItem('lk-agent-gh-proxy', JSON.stringify({ on: cnInstall, url: ghProxy }))
    } catch {}
  }, [cnInstall, ghProxy])

  const installCmd = useMemo(() => withGhProxyFlag(cmd, cnInstall, ghProxy), [cmd, cnInstall, ghProxy])

  const openCreate = () => {
    setName('')
    setHost('')
    setNewPortMin(String(DEFAULT_PORT_MIN))
    setNewPortMax(String(DEFAULT_PORT_MAX))
    setNewExpires('')
    setNewResetDay('0')
    setFormOpen(true)
  }

  const readPortRange = (minVal, maxVal) => {
    const min = parsePort(minVal)
    const max = parsePort(maxVal)
    if (min == null || max == null) {
      toast('端口区间 1–65535', 'error')
      return null
    }
    if (min > max) {
      toast('起始端口不能大于结束端口', 'error')
      return null
    }
    return { port_min: min, port_max: max }
  }

  const create = async (e) => {
    e.preventDefault()
    const range = readPortRange(newPortMin, newPortMax)
    if (!range) return
    setBusy(true)
    try {
      const d = await api.post('/servers', {
        name,
        public_host: host,
        ...range,
        expires_at: ymdToUnix(newExpires),
        traffic_reset_day: Number(newResetDay) || 0,
      })
      setName(''); setHost('')
      setFormOpen(false)
      setCmd(d.install || '')
      toast('已添加实例')
      load()
    } catch (e) { toast(e.message, 'error') }
    finally { setBusy(false) }
  }

  const openEdit = (s) => {
    setEditSrv(s)
    setEdit({
      name: s.name || '',
      host: s.public_host || '',
      min: String(s.port_min || DEFAULT_PORT_MIN),
      max: String(s.port_max || DEFAULT_PORT_MAX),
      gb: gbFromLimit(s.traffic_limit),
      expires: ymd(s.expires_at),
      resetDay: String(s.traffic_reset_day || 0),
    })
  }

  const saveEdit = async (e) => {
    e.preventDefault()
    if (!editSrv) return
    const range = readPortRange(edit.min, edit.max)
    if (!range) return
    const nm = edit.name.trim()
    if (!nm) { toast('请填写名称', 'error'); return }
    let bytes = 0
    const gbRaw = String(edit.gb || '').trim()
    if (gbRaw !== '') {
      const gb = Number(gbRaw)
      if (!Number.isFinite(gb) || gb < 0) { toast('流量上限无效', 'error'); return }
      bytes = Math.round(gb * 1024 * 1024 * 1024)
    }
    const reset = Number(edit.resetDay)
    if (!Number.isFinite(reset) || reset < 0 || reset > 31) { toast('重置日 0–31', 'error'); return }
    setBusy(true)
    try {
      await api.put(`/servers/${editSrv.id}`, {
        name: nm,
        public_host: edit.host,
        traffic_limit: bytes,
        expires_at: ymdToUnix(edit.expires),
        traffic_reset_day: reset,
        ...range,
      })
      setEditSrv(null)
      load()
    } catch (e) { toast(e.message, 'error') }
    finally { setBusy(false) }
  }

  const openCores = (s) => {
    const have = new Set(parseCores(s.cores))
    const next = CORE_OPTIONS.find(c => !have.has(c.id)) || CORE_OPTIONS[0]
    setCorePick(next.id)
    setCoreSrv(s)
  }

  const pushCore = async () => {
    if (!coreSrv) return
    setBusy(true)
    try {
      const d = await api.post(`/servers/${coreSrv.id}/push-core`, { core: corePick })
      toast(`已推送 ${coreLabel(corePick)}`)
      const cores = Array.isArray(d.cores) ? d.cores.join(',') : ''
      setCoreSrv(prev => prev ? { ...prev, cores: cores || prev.cores, can_push_cores: true } : prev)
      load()
    } catch (e) { toast(e.message, 'error') }
    finally { setBusy(false) }
  }

  const removeCore = async (core) => {
    if (!coreSrv) return
    if (!(await dialog.confirm({
      title: `卸载 ${coreLabel(core)}`,
      message: `从 ${coreSrv.name} 卸掉这个核心。正在用它的节点会连不上。`,
      danger: true,
      okText: '卸载',
    }))) return
    setBusy(true)
    try {
      const d = await api.post(`/servers/${coreSrv.id}/remove-core`, { core })
      toast(`已卸载 ${coreLabel(core)}`)
      const cores = Array.isArray(d.cores) ? d.cores.join(',') : ''
      setCoreSrv(prev => prev ? { ...prev, cores } : prev)
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
    await ops.current.run(`sync-${id}`, async () => {
      try { await api.post(`/servers/${id}/sync`); toast('已下发'); await load() }
      catch (e) { toast(e.message, 'error'); await load() }
    })
  }

  const del = async (id) => {
    if (!(await dialog.confirm({ title: '删除实例', message: '这台机器上的节点也会一并删除，且无法恢复。', danger: true, okText: '删除' }))) return
    await ops.current.run(`del-${id}`, async () => {
      try { await api.del(`/servers/${id}`); await load(); toast('已删除') }
      catch (e) { toast(e.message, 'error') }
    })
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
    setBusyId(s.id)
    try {
      const d = await api.post(`/servers/${s.id}/upgrade-agent`)
      toast(`已升级到 ${d.version || '当前版本'}，Agent 正在重启`)
      if (reloadTimer.current) clearTimeout(reloadTimer.current)
      reloadTimer.current = setTimeout(load, 2500)
    } catch (e) {
      toast(e.message, 'error')
      if (e.code === 'agent_too_old') showInstall(s.id)
    } finally { setBusyId(0) }
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
    setBusyId(s.id)
    try {
      await api.post(`/servers/${s.id}/uninstall-agent`, { confirm: true })
      toast('已卸载 Agent')
      load()
    } catch (e) { toast(e.message, 'error') }
    finally { setBusyId(0) }
  }

  const rotateToken = async (s) => {
    if (!(await dialog.confirm({
      title: '轮换令牌',
      message: '当前安装命令立刻失效。需要把新命令再跑一遍才能连上。',
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

  const nodesOf = (id) => ins.filter(x => Number(x.server_id) === Number(id) && isDirectNode(x))
  const online = list.filter(s => s.online).length
  const visible = useMemo(() => {
    const needle = q.trim().toLowerCase()
    return list.filter(s => {
      const st = serverStatus(s)
      if (statusFilter === 'online' && st !== '在线') return false
      if (statusFilter === 'offline' && st !== '离线' && st !== '故障') return false
      if (statusFilter === 'fresh' && st !== '未安装') return false
      if (statusFilter === 'upgrade' && !(s.needs_upgrade || s.needs_reinstall)) return false
      if (!needle) return true
      const names = nodesOf(s.id).map(x => x.name).join(' ')
      const hay = [s.name, s.public_host, s.agent_ver, names]
      return hay.some(x => String(x || '').toLowerCase().includes(needle))
    })
  }, [list, ins, q, statusFilter])

  return (
    <div>
      <PageHead
        title="实例"
        actions={
          <button type="button" className="btn-primary" onClick={openCreate}>
            <Icon name="plus" size={15} /> 添加实例
          </button>
        }
      />
      {pollErr ? <div className="alert-row is-warn mb-4">{pollErr}</div> : null}
      {list.length > 0 && (
        <div className="flex flex-col sm:flex-row sm:items-center gap-3 mb-4">
          <SearchInput value={q} onChange={e => setQ(e.target.value)} placeholder="搜索实例 / 地址" />
          <FilterTabs
            value={statusFilter}
            onChange={setStatusFilter}
            items={[['','全部'],['online','在线'],['offline','离线'],['fresh','未安装'],['upgrade','可升级']]}
          />
          <div className="text-[12px] text-ink-mut font-mono sm:ml-auto whitespace-nowrap">
            {online} 在线 / {list.length - online} 离线
          </div>
        </div>
      )}
      {!ready ? (
        <div className="card overflow-hidden"><SkeletonRows /></div>
      ) : list.length === 0 ? (
        <div className="card overflow-hidden">
          <Empty title="还没有实例" hint="先起一个名字，添加后把安装命令拿到机器上以 root 执行，再到节点页挂协议。" action={
            <button type="button" className="btn-primary" onClick={openCreate}><Icon name="plus" size={15} /> 添加实例</button>
          } />
        </div>
      ) : visible.length === 0 ? (
        <div className="card overflow-hidden">
          <Empty title="没有匹配的实例" hint="换个关键词或筛选。" />
        </div>
      ) : (
        <div className="machine-grid">
          {visible.map(s => {
            const n = nodesOf(s.id).length
            const used = (s.used_up || 0) + (s.used_down || 0)
            const loadAvg = s.online && s.load_milli ? (Number(s.load_milli) / 1000).toFixed(2) : '—'
            const st = serverStatus(s)
            const fresh = st === '未安装'
            const expired = isExpired(s.expires_at)
            const expSoon = !expired && s.expires_at && Number(s.expires_at) * 1000 < Date.now() + 7 * 86400 * 1000
            const quotaPct = s.traffic_limit ? Math.min(100, Math.floor(used * 100 / Number(s.traffic_limit))) : 0
            return (
              <div key={s.id} className={`machine ${machineTone(s)}`}>
                <div className="machine-head">
                  <div className="min-w-0 flex-1">
                    <div className="machine-title">
                      <span className="machine-name truncate">{s.name}</span>
                      <LineStatus status={st} />
                      {expired ? <Badge tone="danger">已到期</Badge> : expSoon ? <Badge tone="warn">即将到期</Badge> : null}
                      {s.needs_reinstall ? <Badge tone="warn">需重装</Badge>
                        : s.needs_upgrade ? <Badge tone="warn">可升级</Badge> : null}
                      {s.over_quota ? <Badge tone="danger">流量已满</Badge>
                        : quotaPct >= 80 ? <Badge tone="warn">{quotaPct}%</Badge> : null}
                    </div>
                    <div className="machine-host">
                      <span className="machine-host-addr">{s.public_host || '未填公开地址'}</span>
                      <button type="button" className="icon-btn" onClick={() => saveHost(s)} aria-label="改公开地址" title="改公开地址">
                        <Icon name="pencil" size={13} />
                      </button>
                    </div>
                  </div>
                  <div className="machine-toolbar">
                    <MoreMenu iconOnly items={[
                      { label: '同步', hint: '把配置下发到这台机器', onSelect: () => sync(s.id) },
                      { label: '安装命令', onSelect: () => showInstall(s.id) },
                      { label: '从 CF 同步', onSelect: () => openCF(s) },
                      { label: '改公开地址', onSelect: () => saveHost(s) },
                      { label: '编辑', hint: '名称、端口区间、到期、流量', onSelect: () => openEdit(s) },
                      { label: '推送核心', hint: s.can_push_cores ? '安装或卸载 Xray / sing-box / Mita' : (s.online ? '需先升级 Agent' : '需在线'), onSelect: () => openCores(s) },
                      { label: '流量上限', onSelect: () => saveLimit(s) },
                      { sep: true },
                      {
                        label: '一键升级',
                        hint: s.needs_reinstall ? '版本太旧，将打开安装命令' : (s.online ? '升到面板版本并重启' : '需在线'),
                        disabled: !s.online || busyId === s.id,
                        onSelect: () => upgradeAgent(s),
                      },
                      { label: '轮换令牌', onSelect: () => rotateToken(s) },
                      { sep: true },
                      { label: '一键卸载', danger: true, disabled: !s.online || busyId === s.id, hint: s.needs_reinstall ? '版本太旧，无法远程卸载' : '同机不删面板', onSelect: () => uninstallAgent(s) },
                      { label: '删除实例', danger: true, onSelect: () => del(s.id) },
                    ]} />
                  </div>
                </div>
                <ServerDates s={s} />
                {fresh ? (
                  <div className="machine-fresh">
                    <div className="text-[12px] text-ink-mut mb-2">还没装 Agent。复制命令到机器上以 root 执行。</div>
                    <button type="button" className="btn-primary h-8 w-full" onClick={() => showInstall(s.id)}>
                      <Icon name="copy" size={14} /> 复制安装命令
                    </button>
                  </div>
                ) : s.online ? (
                  <>
                    <div className="machine-metrics">
                      <Metric label="上行" value={fmtBps(s.net_up_bps)} tone="up" />
                      <Metric label="下行" value={fmtBps(s.net_down_bps)} tone="down" />
                      <Metric label="负载" value={loadAvg} />
                      <Metric label="已用流量" value={fmtBytes(used)} />
                    </div>
                    <div className="machine-meter">
                      <Meter value={used} max={Number(s.traffic_limit) || 0} />
                    </div>
                  </>
                ) : (
                  s.last_error ? <div className="machine-fault">{s.last_error}</div> : null
                )}
                {s.online && s.last_error ? <div className="machine-fault">{s.last_error}</div> : null}
                {!fresh ? <ServerMeta s={s} /> : null}
                <div className="machine-foot">
                  <button type="button" className="machine-ports" onClick={() => openEdit(s)} title="编辑实例">
                    端口 {formatPortRange(s)}
                  </button>
                  <Link to={`/nodes?server=${s.id}`} className="row-act">{n} 个节点</Link>
                </div>
              </div>
            )
          })}
        </div>
      )}

      <Modal open={formOpen} title="添加实例" onClose={() => setFormOpen(false)} footer={
        <>
          <button type="button" className="btn-ghost" onClick={() => setFormOpen(false)}>取消</button>
          <button type="submit" form="srv-form" className="btn-primary" disabled={busy}>{busy ? '添加中…' : '添加'}</button>
        </>
      }>
        <form id="srv-form" onSubmit={create} className="space-y-3">
          <Field label="名称">
            <input className="input-field" value={name} onChange={e => setName(e.target.value)} required placeholder="香港-01" autoFocus />
          </Field>
          <Field label="公开地址" hint="客户端连接用的 IP 或域名。托管在 Cloudflare 的可直接拉取。">
            <input className="input-field" value={host} onChange={e => setHost(e.target.value)} placeholder="IP 或域名" />
          </Field>
          <Field label="端口区间" hint="这台机器上新节点不填端口时，在此区间随机。已有节点不受影响。">
            <div className="grid grid-cols-[1fr_auto_1fr] items-center gap-2">
              <input className="input-field font-mono" inputMode="numeric" value={newPortMin} onChange={e => setNewPortMin(e.target.value)} placeholder={String(DEFAULT_PORT_MIN)} />
              <span className="text-[13px] text-ink-mut">–</span>
              <input className="input-field font-mono" inputMode="numeric" value={newPortMax} onChange={e => setNewPortMax(e.target.value)} placeholder={String(DEFAULT_PORT_MAX)} />
            </div>
          </Field>
          <div className="grid grid-cols-1 sm:grid-cols-2 gap-3">
            <Field label="到期时间" hint="这台机器的租期。留空不限。">
              <input className="input-field font-mono" type="date" value={newExpires} onChange={e => setNewExpires(e.target.value)} />
            </Field>
            <Field label="流量每月重置" hint="0 = 不提醒。">
              <input className="input-field font-mono" type="number" min="0" max="31" value={newResetDay} onChange={e => setNewResetDay(e.target.value)} />
            </Field>
          </div>
          <button type="button" className="row-act" onClick={() => openCF(null)}>从 CF 同步</button>
        </form>
      </Modal>

      <Modal open={!!editSrv} title="编辑实例" onClose={() => setEditSrv(null)} footer={
        <>
          <button type="button" className="btn-ghost" onClick={() => setEditSrv(null)}>取消</button>
          <button type="submit" form="srv-edit-form" className="btn-primary" disabled={busy}>{busy ? '保存中…' : '保存'}</button>
        </>
      }>
        <form id="srv-edit-form" onSubmit={saveEdit} className="space-y-3">
          <Field label="名称">
            <input className="input-field" value={edit.name} onChange={e => setEdit({ ...edit, name: e.target.value })} required autoFocus />
          </Field>
          <Field label="公开地址">
            <input className="input-field" value={edit.host} onChange={e => setEdit({ ...edit, host: e.target.value })} placeholder="IP 或域名" />
          </Field>
          <Field label="端口区间" hint="新节点不填端口时在此区间随机。手动填的端口不受限。">
            <div className="grid grid-cols-[1fr_auto_1fr] items-center gap-2">
              <input className="input-field font-mono" inputMode="numeric" value={edit.min} onChange={e => setEdit({ ...edit, min: e.target.value })} />
              <span className="text-[13px] text-ink-mut">–</span>
              <input className="input-field font-mono" inputMode="numeric" value={edit.max} onChange={e => setEdit({ ...edit, max: e.target.value })} />
            </div>
          </Field>
          <Field label="流量上限 GB" hint="留空 = 不限。已用流量按这台机器上的线路累计。">
            <input className="input-field font-mono" type="number" min="0" step="0.1" value={edit.gb} onChange={e => setEdit({ ...edit, gb: e.target.value })} />
          </Field>
          <div className="grid grid-cols-1 sm:grid-cols-2 gap-3">
            <Field label="到期时间">
              <input className="input-field font-mono" type="date" value={edit.expires} onChange={e => setEdit({ ...edit, expires: e.target.value })} />
            </Field>
            <Field label="流量每月重置日" hint="0 = 未设置">
              <input className="input-field font-mono" type="number" min="0" max="31" value={edit.resetDay} onChange={e => setEdit({ ...edit, resetDay: e.target.value })} />
            </Field>
          </div>
        </form>
      </Modal>

      <Modal open={!!coreSrv} title="推送核心" onClose={() => setCoreSrv(null)} footer={
        <button type="button" className="btn-ghost" onClick={() => setCoreSrv(null)}>关闭</button>
      }>
        {coreSrv ? (
          <div className="space-y-4">
            <p className="text-[13px] text-ink-mut">
              把内核装到 {coreSrv.name}。不用的可以卸掉。国内机器需先在安装命令里勾选 GitHub 镜像。
            </p>
            {!coreSrv.online ? (
              <div className="notice">Agent 不在线，无法推送或卸载。</div>
            ) : !coreSrv.can_push_cores ? (
              <div className="notice">该 Agent 还不支持推送核心。请先用菜单里的「一键升级」升到当前面板版本。</div>
            ) : (
              <>
                <Field label="选择核心">
                  <div className="flex gap-2">
                    <select className="input-field" value={corePick} onChange={e => setCorePick(e.target.value)}>
                      {CORE_OPTIONS.map(c => (
                        <option key={c.id} value={c.id}>{c.label}</option>
                      ))}
                    </select>
                    <button type="button" className="btn-primary shrink-0" disabled={busy} onClick={pushCore}>
                      {busy ? '推送中…' : '推送'}
                    </button>
                  </div>
                </Field>
                <div>
                  <div className="text-[12px] font-medium text-ink-soft mb-1">已安装</div>
                  {parseCores(coreSrv.cores).length === 0 ? (
                    <div className="text-[13px] text-ink-mut py-2">还没有探测到内核。</div>
                  ) : parseCores(coreSrv.cores).map(c => (
                    <div key={c} className="core-installed">
                      <span className={`core-tag is-${c}`}>{coreLabel(c)}</span>
                      <button type="button" className="row-act" style={{ color: 'var(--color-danger)' }} disabled={busy} onClick={() => removeCore(c)}>卸载</button>
                    </div>
                  ))}
                </div>
              </>
            )}
          </div>
        ) : null}
      </Modal>

      <Modal open={!!cmd} title="一键安装 Agent" onClose={() => setCmd('')} wide footer={
        <>
          <button type="button" className="btn-ghost" onClick={() => setCmd('')}>关闭</button>
          <button type="button" className="btn-primary" onClick={async () => {
            try { await copyText(installCmd); toast('已复制') }
            catch { toast('浏览器不允许自动复制，请手动选中命令', 'error') }
          }}>
            <Icon name="copy" size={15} /> 复制
          </button>
        </>
      }>
        <p className="text-[13px] text-ink-mut mb-3">在实例上以 root 执行。明文 http 会自动带 --insecure。</p>
        <pre className="text-[12px] font-mono bg-raised p-3 overflow-x-auto whitespace-pre-wrap">{installCmd}</pre>
        <label className="flex items-start gap-2 mt-3 text-[13px] text-ink-soft">
          <input type="checkbox" className="mt-0.5" checked={cnInstall} onChange={e => setCnInstall(e.target.checked)} />
          <span>
            <span className="font-medium text-ink">国内机器</span>
            <span className="block text-[12px] text-ink-mut mt-0.5">Agent 从本面板下载。勾选后第一次下发节点时，Xray / sing-box 走 GitHub 镜像。</span>
          </span>
        </label>
        {cnInstall ? (
          <div className="mt-3">
            <Field label="镜像地址" hint="国内可访问的 GitHub 前缀，一般不用改。">
              <input className="input-field font-mono" value={ghProxy} onChange={e => setGhProxy(e.target.value)} spellCheck={false} autoComplete="off" />
            </Field>
          </div>
        ) : null}
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
              const needle = cfFilter.trim().toLowerCase()
              const shown = !needle ? cfDomains : cfDomains.filter(d =>
                d.name.includes(needle) || (d.zone || '').includes(needle) || (d.content || '').includes(needle)
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
