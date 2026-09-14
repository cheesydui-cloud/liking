import { useEffect, useMemo, useState } from 'react'
import { api } from '../lib/api'
import { copyText } from '../lib/copy'
import { useToast, useDialog } from '../components/Layout'
import { Badge, DayBars, Empty, Field, FilterTabs, Icon, Meter, Modal, MoreMenu, PageHead, SearchInput, billedBytes, fmtBytes, fmtDateShort } from '../components/ui'
import { SubPanel } from '../components/SubPanel'

function randPassword() {
  const a = 'ABCDEFGHJKLMNPQRSTUVWXYZabcdefghijkmnopqrstuvwxyz23456789'
  const b = new Uint8Array(10)
  crypto.getRandomValues(b)
  return [...b].map(x => a[x % a.length]).join('')
}

function trafficCap(u, pkgs) {
  if (u.traffic_limit != null) return Number(u.traffic_limit)
  const p = pkgs.find(x => x.id === u.package_id)
  return p?.traffic_bytes || 0
}

function ymd(ts) {
  if (!ts) return ''
  const d = new Date(ts * 1000)
  if (Number.isNaN(d.getTime())) return ''
  const z = n => String(n).padStart(2, '0')
  return `${d.getFullYear()}-${z(d.getMonth() + 1)}-${z(d.getDate())}`
}

function ymdToUnix(s) {
  if (!s) return 0
  const d = new Date(`${s}T23:59:59`)
  const n = d.getTime()
  return Number.isNaN(n) ? 0 : Math.floor(n / 1000)
}

function gbFromBytes(n) {
  if (n == null || n === '') return ''
  const gb = Number(n) / (1024 ** 3)
  if (!Number.isFinite(gb) || gb < 0) return ''
  if (gb === 0) return '0'
  return String(Math.round(gb * 1000) / 1000)
}

function bytesFromGB(s) {
  if (s == null || String(s).trim() === '') return null
  const n = Number(s)
  if (!Number.isFinite(n) || n < 0) return null
  return Math.round(n * 1024 * 1024 * 1024)
}

const emptyForm = { username: '', password: '', remark: '', package_id: '', days: 30, expires: '', traffic_gb: '', enabled: true, traffic_reset_day: 0 }

function UserFlags({ u }) {
  if (u.role === 'admin') return null
  if (u.expires_at && u.expires_at * 1000 < Date.now()) return <Badge tone="danger">到期</Badge>
  if (u.traffic_cap > 0 && billedBytes(u) >= u.traffic_cap) return <Badge tone="danger">超量</Badge>
  if (u.quota_ratio >= 80) return <Badge tone="warn">{u.quota_ratio}%</Badge>
  if (u.enabled === false) return <Badge tone="muted">停用</Badge>
  return null
}

function cardDate(ts) {
  if (!ts) return '不限期'
  const d = new Date(Number(ts) * 1000)
  if (Number.isNaN(d.getTime())) return '—'
  const z = n => String(n).padStart(2, '0')
  return `${d.getFullYear()}-${z(d.getMonth() + 1)}-${z(d.getDate())}`
}

function formatUserCard(u, pkgs = []) {
  const origin = typeof window !== 'undefined' ? window.location.origin : ''
  const lines = [`网址：${origin}`, `账号：${u.username || ''}`]
  if (u.password) lines.push(`密码：${u.password}`)
  lines.push(`到期：${cardDate(u.expires_at)}`)
  if (u.package_name) lines.push(`套餐：${u.package_name}`)
  else if (u.role !== 'admin') lines.push('套餐：未绑定')
  if (u.role !== 'admin') {
    const cap = u.traffic_cap || trafficCap(u, pkgs)
    if (cap > 0) lines.push(`流量：${fmtBytes(billedBytes(u))} / ${fmtBytes(cap)}`)
    else lines.push('流量：不限')
  }
  if (u.sub_token) lines.push(`订阅：${origin}/api/sub/${u.sub_token}`)
  if (u.remark) lines.push(`备注：${u.remark}`)
  if (u.enabled === false) lines.push('状态：停用')
  return lines.join('\n')
}

function UserRowActs({ u, onEdit, onSub, onCard, onTraffic, onToggle, onRemove }) {
  if (u.role === 'admin') return null
  return (
    <div className="icon-row">
      <button type="button" className="icon-btn" onClick={() => onEdit(u)} aria-label="编辑用户" title="编辑">
        <Icon name="pencil" size={14} />
      </button>
      <button type="button" className="icon-btn" onClick={() => onSub(u)} aria-label="订阅" title="订阅">
        <Icon name="link" size={14} />
      </button>
      <button type="button" className="icon-btn" onClick={() => onCard(u)} aria-label="复制名片" title="复制名片">
        <Icon name="copy" size={14} />
      </button>
      <MoreMenu iconOnly items={[
        { label: '复制名片', onSelect: () => onCard(u) },
        { sep: true },
        { label: '流量', onSelect: () => onTraffic(u) },
        { label: u.enabled ? '停用' : '启用', onSelect: onToggle },
        { sep: true },
        { label: '删除', danger: true, onSelect: () => onRemove(u) },
      ]} />
    </div>
  )
}

export default function Users() {
  const toast = useToast()
  const dialog = useDialog()
  const [list, setList] = useState([])
  const [pkgs, setPkgs] = useState([])
  const [f, setF] = useState(emptyForm)
  const [busy, setBusy] = useState(false)
  const [formOpen, setFormOpen] = useState(false)
  const [editUser, setEditUser] = useState(null)
  const [subUser, setSubUser] = useState(null)
  const [trafficUser, setTrafficUser] = useState(null)
  const [trafficDetail, setTrafficDetail] = useState(null)
  const [q, setQ] = useState('')
  const [pkgFilter, setPkgFilter] = useState('')
  const [statusFilter, setStatusFilter] = useState('')
  const [bulkOpen, setBulkOpen] = useState(false)
  const [bulkText, setBulkText] = useState('')
  const [bulkBusy, setBulkBusy] = useState(false)
  const [bulkPkg, setBulkPkg] = useState('')
  const [bulkDays, setBulkDays] = useState(30)

  const load = async () => {
    try {
      const [a, b] = await Promise.all([api.get('/users'), api.get('/packages')])
      setList(a.users || [])
      setPkgs(b.packages || [])
    } catch (e) { toast(e.message, 'error') }
  }
  useEffect(() => { load() }, [])

  const closeForm = () => {
    setFormOpen(false)
    setEditUser(null)
    setF({ ...emptyForm, package_id: f.package_id, days: 30 })
  }

  const openCreate = () => {
    setEditUser(null)
    setF({ ...emptyForm, package_id: f.package_id, days: 30 })
    setFormOpen(true)
  }

  const openEdit = (u) => {
    setEditUser(u)
    setF({
      username: u.username || '',
      password: '',
      remark: u.remark || '',
      package_id: u.package_id || '',
      days: 30,
      expires: ymd(u.expires_at),
      traffic_gb: gbFromBytes(u.traffic_limit),
      enabled: u.enabled !== false,
      traffic_reset_day: u.traffic_reset_day || 0,
    })
    setFormOpen(true)
  }

  const bumpExpiry = (days) => {
    const today = new Date()
    today.setHours(0, 0, 0, 0)
    let d = f.expires ? new Date(`${f.expires}T00:00:00`) : new Date(today)
    if (Number.isNaN(d.getTime()) || d < today) d = new Date(today)
    d.setDate(d.getDate() + days)
    const z = n => String(n).padStart(2, '0')
    setF({ ...f, expires: `${d.getFullYear()}-${z(d.getMonth() + 1)}-${z(d.getDate())}` })
  }

  const save = async (e) => {
    e.preventDefault()
    const username = f.username.trim()
    if (!username) {
      toast('用户名不能为空', 'error')
      return
    }
    if (editUser) {
      const prev = Number(editUser.package_id || 0)
      const next = Number(f.package_id || 0)
      if (prev && next && prev !== next) {
        const from = editUser.package_name || '当前套餐'
        const to = pkgs.find(p => Number(p.id) === next)?.name || '新套餐'
        if (!(await dialog.confirm({ title: '更换套餐', message: `将从「${from}」换到「${to}」，已用流量会清零。` }))) return
      }
    }
    setBusy(true)
    try {
      if (editUser) {
        const body = {
          username,
          remark: f.remark,
          enabled: f.enabled !== false,
          expires_at: ymdToUnix(f.expires),
          traffic_limit: bytesFromGB(f.traffic_gb),
          traffic_reset_day: Number(f.traffic_reset_day) || 0,
        }
        if (!f.package_id) body.unbind_package = true
        else body.package_id = Number(f.package_id)
        if (f.password.trim()) body.password = f.password.trim()
        await api.put(`/users/${editUser.id}`, body)
        if (f.password.trim()) {
          const pkgName = pkgs.find(p => Number(p.id) === Number(f.package_id))?.name || ''
          const card = formatUserCard({
            ...editUser,
            username,
            remark: f.remark,
            expires_at: ymdToUnix(f.expires),
            package_name: pkgName,
            password: f.password.trim(),
            enabled: f.enabled !== false,
          }, pkgs)
          try { await copyText(card); toast('已保存，名片已复制') }
          catch { toast('已保存') }
        } else toast('已保存')
      } else {
        const body = { username, remark: f.remark, days: Number(f.days) || 0 }
        if (f.password) body.password = f.password
        if (f.package_id) body.package_id = Number(f.package_id)
        const d = await api.post('/users', body)
        const pw = d.password || f.password
        const created = d.user ? { ...d.user, password: pw || d.user.password || '' } : null
        if (created) {
          try { await copyText(formatUserCard(created, pkgs)); toast('已创建，名片已复制') }
          catch { toast(pw ? `已创建，密码 ${pw}` : '已创建') }
        } else if (pw) {
          try { await copyText(pw); toast('已创建，密码已复制') } catch { toast(`已创建，密码 ${pw}`) }
        } else toast('已创建')
        if (!body.package_id) toast('未绑定套餐，订阅里不会有节点', 'error')
        else if (d.user) setSubUser(d.user)
      }
      closeForm()
      load()
    } catch (e) { toast(e.message, 'error') }
    finally { setBusy(false) }
  }

  const act = async (fn) => {
    try { await fn(); load() } catch (e) { toast(e.message, 'error') }
  }

  const resetTraffic = async () => {
    if (!editUser) return
    if (!(await dialog.confirm({ title: '清零流量', message: `将清空 ${editUser.username} 已用流量。` }))) return
    try {
      await api.post(`/users/${editUser.id}/reset-traffic`)
      toast('已清零流量')
      const fresh = (await api.get('/users')).users?.find(x => x.id === editUser.id)
      if (fresh) setEditUser(fresh)
      load()
    } catch (e) { toast(e.message, 'error') }
  }

  const remove = async (u) => {
    if (!(await dialog.confirm({ title: '删除用户', message: `将删除 ${u.username} 及其订阅。`, danger: true }))) return
    await act(() => api.del(`/users/${u.id}`))
  }

  const openTraffic = async (u) => {
    setTrafficUser(u)
    setTrafficDetail(null)
    try {
      setTrafficDetail(await api.get(`/users/${u.id}/traffic?days=14`))
    } catch (e) { toast(e.message, 'error') }
  }

  const copyCard = async (u) => {
    try {
      await copyText(formatUserCard(u, pkgs))
      toast(u.password ? '已复制名片' : '已复制名片。此账号没有保存的密码，编辑用户并重设后才会出现在名片里。')
    } catch {
      toast('浏览器不允许自动复制', 'error')
    }
  }

  const rotate = async (u) => {
    if (!(await dialog.confirm({ title: '重置订阅令牌', message: '旧订阅链接立刻失效。' }))) return
    try {
      const d = await api.post(`/users/${u.id}/rotate-sub`)
      load()
      const next = { ...u, sub_token: d.sub_token }
      setSubUser(next)
      toast('订阅令牌已更换')
    } catch (e) { toast(e.message, 'error') }
  }

  const rows = useMemo(() => {
    const needle = q.trim().toLowerCase()
    const now = Date.now()
    return list.filter(u => {
      if (u.role === 'admin') return false
      if (pkgFilter === 'none' && u.package_id) return false
      if (pkgFilter && pkgFilter !== 'none' && String(u.package_id) !== pkgFilter) return false
      if (statusFilter === 'warn') {
        if (!(u.quota_ratio >= 80 && u.quota_ratio < 100)) return false
      }
      if (statusFilter === 'expired') {
        if (!(u.expires_at && u.expires_at * 1000 < now)) return false
      }
      if (!needle) return true
      const hay = [u.username, u.remark, u.package_name]
      return hay.some(x => String(x || '').toLowerCase().includes(needle))
    })
  }, [list, q, pkgFilter, statusFilter])

  const hasCustomers = list.some(u => u.role !== 'admin')
  const editing = !!editUser
  const selectedPkg = pkgs.find(p => Number(p.id) === Number(f.package_id))

  return (
    <div>
      <PageHead
        title="用户"
        actions={
          <div className="flex gap-2">
            <button type="button" className="btn-ghost" onClick={() => setBulkOpen(true)}>批量开户</button>
            <button type="button" className="btn-primary" onClick={openCreate}>
              <Icon name="plus" size={15} /> 新建用户
            </button>
          </div>
        }
      />
      <div className="flex flex-col sm:flex-row gap-2 mb-3">
        <SearchInput value={q} onChange={e => setQ(e.target.value)} placeholder="搜索用户名 / 备注 / 套餐" />
        <select className="input-field toolbar-select" value={pkgFilter} onChange={e => setPkgFilter(e.target.value)}>
          <option value="">全部套餐</option>
          <option value="none">未绑定</option>
          {pkgs.map(p => <option key={p.id} value={p.id}>{p.name}</option>)}
        </select>
        <FilterTabs
          value={statusFilter}
          onChange={setStatusFilter}
          items={[['','全部'],['warn','将满 80%'],['expired','已到期']]}
        />
      </div>

      <div className="card overflow-hidden">
        {!hasCustomers ? (
          <Empty title="暂无用户" hint="先建套餐并勾选节点，再开账号。" action={
            <button type="button" className="btn-primary" onClick={openCreate}><Icon name="plus" size={15} /> 新建用户</button>
          } />
        ) : rows.length === 0 ? (
          <Empty title="没有匹配的用户" hint="换个关键词或套餐筛选。" />
        ) : (
          <>
          <div className="hidden md:block table-wrap">
            <table className="data">
              <thead><tr><th>用户</th><th>套餐</th><th>流量</th><th>到期</th><th></th></tr></thead>
              <tbody>
                {rows.map(u => (
                  <tr key={u.id}>
                    <td>
                      <div className="flex items-center gap-2 flex-wrap min-w-0">
                        <span className="font-medium truncate">{u.username}</span>
                        <UserFlags u={u} />
                      </div>
                      <div className="text-[11px] text-ink-mut mt-0.5">{u.remark || (u.role === 'admin' ? '管理员' : '')}</div>
                    </td>
                    <td className="text-[13px]">{u.role === 'admin' ? '—' : (u.package_name || <span className="text-ink-mut">未绑定</span>)}</td>
                    <td className="min-w-[10rem]">
                      {u.role === 'admin' ? '—' : (
                        <div>
                          <Meter value={billedBytes(u)} max={u.traffic_cap || trafficCap(u, pkgs)} />
                          {u.direction === 'twoway' ? <div className="text-[11px] text-ink-mut mt-0.5">双向计费</div> : null}
                        </div>
                      )}
                    </td>
                    <td className="text-[12px] whitespace-nowrap font-mono tabular-nums">{u.expires_at ? fmtDateShort(u.expires_at) : '—'}</td>
                    <td className="whitespace-nowrap">
                      <UserRowActs
                        u={u}
                        onEdit={openEdit}
                        onSub={setSubUser}
                        onCard={copyCard}
                        onTraffic={openTraffic}
                        onToggle={() => act(() => api.put(`/users/${u.id}`, { enabled: !u.enabled }))}
                        onRemove={remove}
                      />
                    </td>
                  </tr>
                ))}
              </tbody>
            </table>
          </div>
          <div className="md:hidden divide-y" style={{ borderColor: 'var(--color-line-soft)' }}>
            {rows.map(u => (
              <div key={u.id} className="px-3.5 py-3">
                <div className="flex items-start justify-between gap-3">
                  <div className="min-w-0">
                    <div className="flex items-center gap-2 flex-wrap min-w-0">
                      <span className="font-medium truncate">{u.username}</span>
                      <UserFlags u={u} />
                    </div>
                    <div className="text-[12px] text-ink-mut mt-0.5">
                      {u.role === 'admin' ? '管理员' : (u.package_name || '未绑定')}
                      {u.expires_at ? ` / ${fmtDateShort(u.expires_at)}` : ''}
                    </div>
                  </div>
                  <UserRowActs
                    u={u}
                    onEdit={openEdit}
                    onSub={setSubUser}
                    onCard={copyCard}
                    onTraffic={openTraffic}
                    onToggle={() => act(() => api.put(`/users/${u.id}`, { enabled: !u.enabled }))}
                    onRemove={remove}
                  />
                </div>
                {u.role !== 'admin' ? (
                  <div className="mt-2">
                    <Meter value={billedBytes(u)} max={u.traffic_cap || trafficCap(u, pkgs)} />
                  </div>
                ) : null}
              </div>
            ))}
          </div>
          </>
        )}
      </div>
      <Modal open={formOpen} title={editing ? `编辑 ${editUser.username}` : '新建用户'} onClose={closeForm} size="lg" footer={
        <>
          <button type="button" className="btn-ghost" onClick={closeForm}>取消</button>
          <button type="submit" form="user-form" className="btn-primary" disabled={busy}>{busy ? '保存中…' : (editing ? '保存' : '创建')}</button>
        </>
      }>
        <form id="user-form" onSubmit={save} className="grid grid-cols-1 sm:grid-cols-2 gap-3">
          <Field label="用户名">
            <input className="input-field" placeholder="alice" value={f.username} onChange={e => setF({ ...f, username: e.target.value })} required autoFocus />
          </Field>
          <Field label="登录密码" hint={editing ? '留空不改；节点链接里的 UUID / 密钥不变' : '可留空随机'}>
            <div className="flex gap-2">
              <input className="input-field" placeholder={editing ? '不修改' : '随机'} value={f.password} onChange={e => setF({ ...f, password: e.target.value })} autoComplete="new-password" />
              <button type="button" className="btn-ghost shrink-0" onClick={() => setF({ ...f, password: randPassword() })}>随机</button>
            </div>
          </Field>
          <Field label="备注"><input className="input-field" placeholder="可选" value={f.remark} onChange={e => setF({ ...f, remark: e.target.value })} /></Field>
          <Field label="套餐" hint={editing && Number(f.package_id || 0) !== Number(editUser.package_id || 0) && f.package_id ? '更换套餐会清零已用流量' : undefined}>
            <select className="input-field" value={f.package_id} onChange={e => setF({ ...f, package_id: e.target.value })}>
              <option value="">不绑定</option>
              {pkgs.map(p => {
                const n = (p.inbound_ids || []).length
                const tag = n ? `${n} 个节点` : ((p.server_ids || []).length ? `${p.server_ids.length} 台实例` : '全部节点')
                return <option key={p.id} value={p.id}>{p.name} / {tag}</option>
              })}
            </select>
          </Field>
          {editing ? (
            <div>
              <Field label="到期" hint="留空表示不限期">
                <input className="input-field" type="date" value={f.expires} onChange={e => setF({ ...f, expires: e.target.value })} />
              </Field>
              <div className="flex gap-2 mt-1.5">
                <button type="button" className="row-act" onClick={() => bumpExpiry(30)}>+30 天</button>
                <button type="button" className="row-act" onClick={() => bumpExpiry(60)}>+60 天</button>
                <button type="button" className="row-act" onClick={() => bumpExpiry(90)}>+90 天</button>
              </div>
            </div>
          ) : (
            <Field label="天数" hint="从今天起算">
              <input className="input-field" type="number" min="0" value={f.days} onChange={e => setF({ ...f, days: e.target.value })} />
            </Field>
          )}
          {editing && (
            <Field label="流量上限 GB" hint={f.traffic_gb === '' ? (selectedPkg?.traffic_bytes ? `留空跟随套餐 ${fmtBytes(selectedPkg.traffic_bytes)}` : '留空跟随套餐，0 为不限') : (Number(f.traffic_gb) === 0 ? '0 = 不限流量' : '覆盖套餐额度')}>
              <input className="input-field" type="number" min="0" step="0.1" placeholder="跟随套餐" value={f.traffic_gb} onChange={e => setF({ ...f, traffic_gb: e.target.value })} />
            </Field>
          )}
          {editing && (
            <Field label="流量重置日" hint="0 跟随套餐 / 每月周期。1–31 表示每月这一天清零。">
              <input className="input-field" type="number" min="0" max="31" value={f.traffic_reset_day} onChange={e => setF({ ...f, traffic_reset_day: e.target.value })} />
            </Field>
          )}
          {editing && (
            <Field label="状态">
              <select className="input-field" value={f.enabled ? '1' : '0'} onChange={e => setF({ ...f, enabled: e.target.value === '1' })}>
                <option value="1">启用</option>
                <option value="0">停用</option>
              </select>
            </Field>
          )}
          {editing && (
            <div className="sm:col-span-2 px-3 py-2.5" style={{ background: 'var(--color-fill)' }}>
              <div className="flex items-center justify-between gap-3">
                <div className="min-w-0 flex-1">
                  <div className="text-[12px] text-ink-mut mb-1">已用流量{editUser.direction === 'twoway' || selectedPkg?.direction === 'twoway' ? '（双向）' : ''}</div>
                  <Meter value={billedBytes(editUser)} max={trafficCap({ ...editUser, traffic_limit: bytesFromGB(f.traffic_gb) ?? editUser.traffic_limit, package_id: f.package_id || editUser.package_id }, pkgs)} />
                </div>
                <button type="button" className="btn-danger shrink-0 h-8" onClick={resetTraffic}>清零</button>
              </div>
            </div>
          )}
        </form>
      </Modal>
      <Modal open={!!subUser} title={subUser ? `${subUser.username} 的订阅` : '订阅'} onClose={() => setSubUser(null)} wide footer={
        <>
          <button type="button" className="btn-ghost" onClick={() => subUser && rotate(subUser)}>重置令牌</button>
          <button type="button" className="btn-ghost" onClick={() => setSubUser(null)}>关闭</button>
        </>
      }>
        {subUser && <SubPanel token={subUser.sub_token} onCopied={(msg, kind) => toast(msg, kind)} />}
      </Modal>
      <Modal open={!!trafficUser} title={trafficUser ? `${trafficUser.username} 的流量` : '流量'} onClose={() => { setTrafficUser(null); setTrafficDetail(null) }} size="lg" footer={
        <button type="button" className="btn-ghost" onClick={() => { setTrafficUser(null); setTrafficDetail(null) }}>关闭</button>
      }>
        {trafficUser && (
          <div>
            <Meter className="mb-3" value={billedBytes(trafficUser)} max={trafficUser.traffic_cap || trafficCap(trafficUser, pkgs)} />
            {trafficUser.direction === 'twoway' ? <div className="text-[12px] text-ink-mut mb-3">套餐双向计费，进度条已按上下行之和 × 2。</div> : null}
            {trafficDetail ? (
              <>
                <div className="text-[13px] font-medium mb-2">近 14 日（原始）</div>
                <DayBars days={trafficDetail.days} />
                {(trafficDetail.inbounds || []).length > 0 && (
                  <div className="table-wrap mt-4">
                    <table className="data">
                      <thead><tr><th>节点</th><th>上行</th><th>下行</th></tr></thead>
                      <tbody>
                        {trafficDetail.inbounds.map(inb => (
                          <tr key={inb.id}>
                            <td>{inb.name}</td>
                            <td className="tabular-nums">{fmtBytes(inb.up)}</td>
                            <td className="tabular-nums">{fmtBytes(inb.down)}</td>
                          </tr>
                        ))}
                      </tbody>
                    </table>
                  </div>
                )}
              </>
            ) : <div className="text-[13px] text-ink-mut">加载中</div>}
          </div>
        )}
      </Modal>
      <Modal open={bulkOpen} title="批量开户" onClose={() => setBulkOpen(false)} size="lg" footer={
        <>
          <button type="button" className="btn-ghost" onClick={() => setBulkOpen(false)}>取消</button>
          <button type="button" className="btn-primary" disabled={bulkBusy} onClick={async () => {
            const lines = bulkText.split('\n').map(x => x.trim()).filter(Boolean)
            if (!lines.length) { toast('请填写用户名，一行一个', 'error'); return }
            setBulkBusy(true)
            try {
              const users = lines.map(line => {
                const [username, password] = line.split(/[\s,]+/).filter(Boolean)
                return { username, password: password || '', package_id: bulkPkg ? Number(bulkPkg) : undefined, days: Number(bulkDays) || 0 }
              })
              const d = await api.post('/users/bulk', { users })
              const pw = (d.users || []).filter(x => x.ok && x.password).map(x => `${x.username} ${x.password}`).join('\n')
              toast(`成功 ${d.ok}/${d.total}`)
              if (pw) {
                try { await copyText(pw); toast('随机密码已复制') } catch {}
              }
              setBulkOpen(false)
              setBulkText('')
              load()
            } catch (e) { toast(e.message, 'error') }
            finally { setBulkBusy(false) }
          }}>{bulkBusy ? '创建中…' : '创建'}</button>
        </>
      }>
        <div className="space-y-3">
          <Field label="用户名" hint="一行一个。可写成「用户名 密码」，密码留空则随机。">
            <textarea className="input-field font-mono text-[12px] min-h-40" value={bulkText} onChange={e => setBulkText(e.target.value)} placeholder={'alice\nbob secret12'} />
          </Field>
          <div className="grid grid-cols-1 sm:grid-cols-2 gap-3">
            <Field label="套餐">
              <select className="input-field" value={bulkPkg} onChange={e => setBulkPkg(e.target.value)}>
                <option value="">不绑定</option>
                {pkgs.map(p => <option key={p.id} value={p.id}>{p.name}</option>)}
              </select>
            </Field>
            <Field label="天数" hint="0 表示不限期">
              <input className="input-field" type="number" min="0" value={bulkDays} onChange={e => setBulkDays(e.target.value)} />
            </Field>
          </div>
        </div>
      </Modal>
    </div>
  )
}
