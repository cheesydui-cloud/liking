import { useEffect, useMemo, useState } from 'react'
import { api } from '../lib/api'
import { copyText } from '../lib/copy'
import { useToast, useDialog } from '../components/Layout'
import { Badge, Empty, Field, Icon, Meter, Modal, PageHead, SearchInput, fmtDateShort } from '../components/ui'
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

export default function Users() {
  const toast = useToast()
  const dialog = useDialog()
  const [list, setList] = useState([])
  const [pkgs, setPkgs] = useState([])
  const [f, setF] = useState({ username: '', password: '', remark: '', package_id: '', days: 30 })
  const [busy, setBusy] = useState(false)
  const [formOpen, setFormOpen] = useState(false)
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

  const openCreate = () => {
    setF({ username: '', password: '', remark: '', package_id: f.package_id, days: 30 })
    setFormOpen(true)
  }

  const create = async (e) => {
    e.preventDefault()
    setBusy(true)
    try {
      const body = { username: f.username, remark: f.remark, days: Number(f.days) || 0 }
      if (f.password) body.password = f.password
      if (f.package_id) body.package_id = Number(f.package_id)
      const d = await api.post('/users', body)
      const pw = d.password || f.password
      setFormOpen(false)
      setF({ username: '', password: '', remark: '', package_id: f.package_id, days: 30 })
      if (pw) {
        try { await copyText(pw); toast(`已创建，密码已复制`) } catch { toast(`已创建，密码 ${pw}`) }
      } else toast('已创建')
      if (!body.package_id) toast('未绑定套餐，订阅里不会有节点', 'error')
      else if (d.user) setSubUser(d.user)
      load()
    } catch (e) { toast(e.message, 'error') }
    finally { setBusy(false) }
  }

  const act = async (fn) => {
    try { await fn(); load() } catch (e) { toast(e.message, 'error') }
  }

  const resetPw = async (u) => {
    const pw = await dialog.prompt({ title: `重置 ${u.username} 的密码`, message: '留空则随机生成。至少 6 位。', inputType: 'text', okText: '保存' })
    if (pw == null) return
    const next = pw.trim() || randPassword()
    await act(() => api.post(`/users/${u.id}/password`, { password: next }))
    try { await copyText(next); toast('新密码已复制') } catch { toast(`新密码 ${next}`) }
  }

  const remove = async (u) => {
    if (!(await dialog.confirm({ title: '删除用户', message: `将删除 ${u.username} 及其订阅。`, danger: true }))) return
    await act(() => api.del(`/users/${u.id}`))
  }

  const bindPkg = async (u, val) => {
    try {
      const body = { remark: u.remark || '', enabled: u.enabled }
      if (!val) body.unbind_package = true
      else body.package_id = Number(val)
      await api.put(`/users/${u.id}`, body)
      toast(val ? '已绑定套餐' : '已解绑套餐')
      load()
    } catch (e) { toast(e.message, 'error') }
  }

  const extend = async (u, days) => {
    try {
      await api.put(`/users/${u.id}`, { remark: u.remark || '', enabled: u.enabled, extend_days: days })
      toast(`已续期 +${days} 天`)
      load()
    } catch (e) { toast(e.message, 'error') }
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
      if (u.role === 'admin') return !needle && !pkgFilter
      if (pkgFilter === 'none' && u.package_id) return false
      if (pkgFilter && pkgFilter !== 'none' && String(u.package_id) !== pkgFilter) return false
      if (!needle) return true
      return [u.username, u.remark, u.package_name].some(x => String(x || '').toLowerCase().includes(needle))
    })
  }, [list, q, pkgFilter])

  const members = list.filter(u => u.role !== 'admin')

  return (
    <div>
      <PageHead
        title="用户"
        desc="一人一套餐。创建后密码和订阅都能直接复制；到期或超量会从内核配置里摘掉客户端。"
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
        {members.length === 0 ? (
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
                    <td>
                      {u.role === 'admin' ? '—' : (
                        <select className="input-field h-8 text-[12px] min-w-[9rem]" value={u.package_id || ''} onChange={e => bindPkg(u, e.target.value)}>
                          <option value="">未绑定</option>
                          {pkgs.map(p => <option key={p.id} value={p.id}>{p.name}</option>)}
                        </select>
                      )}
                    </td>
                    <td className="min-w-[10rem]">
                      {u.role === 'admin' ? '—' : (
                        <Meter value={(u.used_up || 0) + (u.used_down || 0)} max={trafficCap(u, pkgs)} />
                      )}
                    </td>
                    <td className="text-[12px] whitespace-nowrap">
                      <div>{u.expires_at ? fmtDateShort(u.expires_at) : '—'}</div>
                      {u.role !== 'admin' && (
                        <div className="flex gap-2 mt-1">
                          <button type="button" className="row-act" onClick={() => extend(u, 30)}>+30</button>
                          <button type="button" className="row-act" onClick={() => extend(u, 60)}>+60</button>
                          <button type="button" className="row-act" onClick={() => extend(u, 90)}>+90</button>
                        </div>
                      )}
                    </td>
                    <td>
                      {u.expires_at && u.expires_at * 1000 < Date.now() ? <Badge tone="danger">到期</Badge>
                        : u.enabled ? <Badge tone="ok">启用</Badge> : <Badge tone="muted">停用</Badge>}
                    </td>
                    <td className="whitespace-nowrap">
                      {u.role !== 'admin' && (
                        <div className="flex gap-2.5 justify-end">
                          <button type="button" className="row-act" onClick={() => setSubUser(u)}>订阅</button>
                          <button type="button" className="row-act" onClick={() => act(() => api.post(`/users/${u.id}/reset-traffic`))}>清流量</button>
                          <button type="button" className="row-act" onClick={() => resetPw(u)}>改密</button>
                          <button type="button" className="row-act" onClick={() => act(() => api.put(`/users/${u.id}`, { remark: u.remark, enabled: !u.enabled }))}>{u.enabled ? '停用' : '启用'}</button>
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
      <Modal open={formOpen} title="新建用户" onClose={() => setFormOpen(false)} size="lg" footer={
        <>
          <button type="button" className="btn-ghost" onClick={() => setFormOpen(false)}>取消</button>
          <button type="submit" form="user-create" className="btn-primary" disabled={busy}>{busy ? '创建中…' : '创建'}</button>
        </>
      }>
        <form id="user-create" onSubmit={create} className="grid grid-cols-1 sm:grid-cols-2 gap-3">
          <Field label="用户名"><input className="input-field" placeholder="alice" value={f.username} onChange={e => setF({ ...f, username: e.target.value })} required autoFocus /></Field>
          <Field label="密码" hint="可留空随机">
            <div className="flex gap-2">
              <input className="input-field" placeholder="随机" value={f.password} onChange={e => setF({ ...f, password: e.target.value })} />
              <button type="button" className="btn-ghost shrink-0" onClick={() => setF({ ...f, password: randPassword() })}>随机</button>
            </div>
          </Field>
          <Field label="备注"><input className="input-field" placeholder="可选" value={f.remark} onChange={e => setF({ ...f, remark: e.target.value })} /></Field>
          <Field label="套餐">
            <select className="input-field" value={f.package_id} onChange={e => setF({ ...f, package_id: e.target.value })}>
              <option value="">不绑定</option>
              {pkgs.map(p => {
                const n = (p.server_ids || []).length
                const tag = n ? `${n} 节点` : ((p.inbound_ids || []).length ? '指定线路' : '全部节点')
                return <option key={p.id} value={p.id}>{p.name} · {tag}</option>
              })}
            </select>
          </Field>
          <Field label="天数"><input className="input-field" type="number" value={f.days} onChange={e => setF({ ...f, days: e.target.value })} /></Field>
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
