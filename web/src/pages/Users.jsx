import { useEffect, useMemo, useState } from 'react'
import { api } from '../lib/api'
import { copyText } from '../lib/copy'
import { useToast, useDialog } from '../components/Layout'
import { Badge, Empty, Field, Icon, Meter, Modal, PageHead, SearchInput, fmtBytes, fmtDateShort } from '../components/ui'
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

const emptyForm = { username: '', password: '', remark: '', package_id: '', days: 30, expires: '', traffic_gb: '', enabled: true }

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
  const [q, setQ] = useState('')
  const [pkgFilter, setPkgFilter] = useState('')

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
        }
        if (!f.package_id) body.unbind_package = true
        else body.package_id = Number(f.package_id)
        if (f.password.trim()) body.password = f.password.trim()
        await api.put(`/users/${editUser.id}`, body)
        if (f.password.trim()) {
          try { await copyText(f.password.trim()); toast('已保存，新密码已复制') }
          catch { toast('已保存') }
        } else toast('已保存')
      } else {
        const body = { username, remark: f.remark, days: Number(f.days) || 0 }
        if (f.password) body.password = f.password
        if (f.package_id) body.package_id = Number(f.package_id)
        const d = await api.post('/users', body)
        const pw = d.password || f.password
        if (pw) {
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
    return list.filter(u => {
      if (pkgFilter === 'none' && u.package_id) return false
      if (pkgFilter && pkgFilter !== 'none' && String(u.package_id) !== pkgFilter) return false
      if (!needle) return true
      const hay = [u.username, u.remark, u.package_name, u.role === 'admin' ? '管理员' : '']
      return hay.some(x => String(x || '').toLowerCase().includes(needle))
    })
  }, [list, q, pkgFilter])

  const editing = !!editUser
  const selectedPkg = pkgs.find(p => Number(p.id) === Number(f.package_id))

  return (
    <div>
      <PageHead
        title="用户"
        desc="一人一套餐。点「编辑」改用户名、套餐、到期、流量和登录密码；到期或超量会从内核配置里摘掉客户端。"
        actions={
          <button type="button" className="btn-primary" onClick={openCreate}>
            <Icon name="plus" size={15} /> 新建用户
          </button>
        }
      />
      <div className="flex flex-col sm:flex-row gap-2 mb-3">
        <SearchInput value={q} onChange={e => setQ(e.target.value)} placeholder="搜索用户名 / 备注 / 套餐" />
        <select className="input-field sm:w-48" value={pkgFilter} onChange={e => setPkgFilter(e.target.value)}>
          <option value="">全部套餐</option>
          <option value="none">未绑定</option>
          {pkgs.map(p => <option key={p.id} value={p.id}>{p.name}</option>)}
        </select>
      </div>

      <div className="card overflow-hidden">
        {list.length === 0 ? (
          <Empty title="暂无用户" hint="先建套餐并勾选节点，再开账号。" action={
            <button type="button" className="btn-primary" onClick={openCreate}><Icon name="plus" size={15} /> 新建用户</button>
          } />
        ) : rows.length === 0 ? (
          <Empty title="没有匹配的用户" hint="换个关键词或套餐筛选。" />
        ) : (
          <div className="table-wrap">
            <table className="data">
              <thead><tr><th>用户</th><th>套餐</th><th>流量</th><th>到期</th><th>状态</th><th></th></tr></thead>
              <tbody>
                {rows.map(u => (
                  <tr key={u.id}>
                    <td>
                      <div className="font-medium">{u.username}</div>
                      <div className="text-[11px] text-ink-mut">{u.remark || (u.role === 'admin' ? '管理员' : '')}</div>
                    </td>
                    <td className="text-[13px]">{u.role === 'admin' ? '—' : (u.package_name || <span className="text-ink-mut">未绑定</span>)}</td>
                    <td className="min-w-[10rem]">
                      {u.role === 'admin' ? '—' : (
                        <Meter value={(u.used_up || 0) + (u.used_down || 0)} max={trafficCap(u, pkgs)} />
                      )}
                    </td>
                    <td className="text-[12px] whitespace-nowrap">{u.expires_at ? fmtDateShort(u.expires_at) : '—'}</td>
                    <td>
                      {u.expires_at && u.expires_at * 1000 < Date.now() ? <Badge tone="danger">到期</Badge>
                        : u.enabled ? <Badge tone="ok">启用</Badge> : <Badge tone="muted">停用</Badge>}
                    </td>
                    <td className="whitespace-nowrap">
                      {u.role !== 'admin' && (
                        <div className="flex gap-2.5 justify-end">
                          <button type="button" className="row-act" onClick={() => setSubUser(u)}>订阅</button>
                          <button type="button" className="row-act" onClick={() => openEdit(u)}>编辑</button>
                          <button type="button" className="row-act" onClick={() => act(() => api.put(`/users/${u.id}`, { enabled: !u.enabled }))}>{u.enabled ? '停用' : '启用'}</button>
                          <button type="button" className="row-act is-danger" onClick={() => remove(u)}>删除</button>
                        </div>
                      )}
                    </td>
                  </tr>
                ))}
              </tbody>
            </table>
          </div>
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
                const n = (p.server_ids || []).length
                const tag = n ? `${n} 台服务器` : ((p.inbound_ids || []).length ? '指定线路' : '全部节点')
                return <option key={p.id} value={p.id}>{p.name} · {tag}</option>
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
            <Field label="状态">
              <select className="input-field" value={f.enabled ? '1' : '0'} onChange={e => setF({ ...f, enabled: e.target.value === '1' })}>
                <option value="1">启用</option>
                <option value="0">停用</option>
              </select>
            </Field>
          )}
          {editing && (
            <div className="sm:col-span-2 rounded-md px-3 py-2.5" style={{ background: 'var(--color-fill)' }}>
              <div className="flex items-center justify-between gap-3">
                <div className="min-w-0 flex-1">
                  <div className="text-[12px] text-ink-mut mb-1">已用流量</div>
                  <Meter value={(editUser.used_up || 0) + (editUser.used_down || 0)} max={trafficCap({ ...editUser, traffic_limit: bytesFromGB(f.traffic_gb) ?? editUser.traffic_limit, package_id: f.package_id || editUser.package_id }, pkgs)} />
                </div>
                <button type="button" className="btn-ghost shrink-0 h-8" onClick={resetTraffic}>清零</button>
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
    </div>
  )
}
