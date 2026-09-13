import { useEffect, useMemo, useState } from 'react'
import { Link } from 'react-router-dom'
import { api } from '../lib/api'
import { copyText } from '../lib/copy'
import { isDirectNode, serverStatus } from '../lib/status'
import { useToast, useDialog } from '../components/Layout'
import { Badge, Empty, Field, FilterTabs, Icon, LineStatus, Meter, Modal, MoreMenu, PageHead, SearchInput, fmtAgo, fmtBps, fmtBytes, machineTone } from '../components/ui'

function gbFromLimit(n) {
  if (!n) return ''
  const gb = Number(n) / (1024 ** 3)
  if (!Number.isFinite(gb) || gb <= 0) return ''
  return String(Math.round(gb * 1000) / 1000)
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

export default function Servers() {
  const toast = useToast()
  const dialog = useDialog()
  const [list, setList] = useState([])
  const [ins, setIns] = useState([])
  const [name, setName] = useState('')
  const [host, setHost] = useState('')
  const [cmd, setCmd] = useState('')
  const [formOpen, setFormOpen] = useState(false)
  const [busy, setBusy] = useState(false)
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
      const [a, b] = await Promise.all([api.get('/servers'), api.get('/inbounds')])
      setList(a.servers || [])
      setIns(b.inbounds || [])
    } catch (e) { toast(e.message, 'error') }
  }
  useEffect(() => {
    load()
    const t = setInterval(() => {
      api.get('/servers').then(a => setList(a.servers || [])).catch(() => {})
    }, 5000)
    return () => clearInterval(t)
  }, [])

  const openCreate = () => {
    setName('')
    setHost('')
    setFormOpen(true)
  }

  const create = async (e) => {
    e.preventDefault()
    setBusy(true)
    try {
      const d = await api.post('/servers', { name, public_host: host })
      setName(''); setHost('')
      setFormOpen(false)
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

  const del = async (id) => {
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
        title="服务器"
        desc="一台机器一个 Agent。装好之后到节点页挂协议。"
        actions={
          <button type="button" className="btn-primary" onClick={openCreate}>
            <Icon name="plus" size={15} /> 添加服务器
          </button>
        }
      />
      {list.length > 0 && (
        <div className="flex flex-col sm:flex-row sm:items-center gap-3 mb-4">
          <SearchInput value={q} onChange={e => setQ(e.target.value)} placeholder="搜索服务器 / 地址" />
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
      {list.length === 0 ? (
        <div className="card overflow-hidden">
          <Empty title="还没有服务器" hint="先起一个名字，添加后把安装命令拿到机器上以 root 执行，再到节点页挂协议。" action={
            <button type="button" className="btn-primary" onClick={openCreate}><Icon name="plus" size={15} /> 添加服务器</button>
          } />
        </div>
      ) : visible.length === 0 ? (
        <div className="card overflow-hidden">
          <Empty title="没有匹配的服务器" hint="换个关键词或筛选。" />
        </div>
      ) : (
        <div>
          {visible.map(s => {
            const n = nodesOf(s.id).length
            const used = (s.used_up || 0) + (s.used_down || 0)
            const loadAvg = s.online && s.load_milli ? (Number(s.load_milli) / 1000).toFixed(2) : '—'
            const mem = s.mem_avail ? fmtBytes(s.mem_avail) : '—'
            const st = serverStatus(s)
            const fresh = st === '未安装'
            return (
              <div key={s.id} className={`machine ${machineTone(s)}`}>
                <div className="machine-head">
                  <div className="min-w-0 flex-1">
                    <div className="machine-title">
                      <span className="machine-name truncate">{s.name}</span>
                      <LineStatus status={st} />
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
                    {fresh ? (
                      <button type="button" className="btn-primary h-8" onClick={() => showInstall(s.id)}>
                        <Icon name="copy" size={14} /> 复制安装命令
                      </button>
                    ) : null}
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
                      { label: '删除服务器', danger: true, onSelect: () => del(s.id) },
                    ]} />
                  </div>
                </div>
                {fresh ? (
                  <div className="text-[13px] text-ink-mut">还没装 Agent。复制命令到机器上以 root 执行，上线后再挂节点。</div>
                ) : s.online ? (
                  <>
                    <div className="machine-metrics">
                      <Metric label="上行" value={fmtBps(s.net_up_bps)} />
                      <Metric label="下行" value={fmtBps(s.net_down_bps)} />
                      <Metric label="负载" value={loadAvg} />
                      <Metric label="内存" value={mem} />
                    </div>
                    <div className="machine-meter">
                      <Meter value={used} max={Number(s.traffic_limit) || 0} />
                    </div>
                  </>
                ) : (
                  s.last_error ? <div className="machine-fault">{s.last_error}</div> : null
                )}
                {s.online && s.last_error ? <div className="machine-fault">{s.last_error}</div> : null}
                {!fresh ? <div className="machine-meta">{machineMeta(s)}</div> : null}
                <div className="pt-1">
                  <Link to={`/nodes?server=${s.id}`} className="row-act">{n} 个节点</Link>
                </div>
              </div>
            )
          })}
        </div>
      )}

      <Modal open={formOpen} title="添加服务器" onClose={() => setFormOpen(false)} footer={
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
