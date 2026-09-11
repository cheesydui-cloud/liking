import { useEffect, useState } from 'react'
import { api } from '../lib/api'
import { useToast, useDialog } from '../components/Layout'
import { Badge, Empty, Field, Icon, PageHead, fmtBytes, fmtDate } from '../components/ui'

export default function Users() {
  const toast = useToast()
  const dialog = useDialog()
  const [list, setList] = useState([])
  const [pkgs, setPkgs] = useState([])
  const [f, setF] = useState({ username: '', password: '', remark: '', package_id: '', days: 30 })
  const [busy, setBusy] = useState(false)

  const load = async () => {
    try {
      const [a, b] = await Promise.all([api.get('/users'), api.get('/packages')])
      setList(a.users || [])
      setPkgs(b.packages || [])
    } catch (e) { toast(e.message, 'error') }
  }
  useEffect(() => { load() }, [])

  const create = async (e) => {
    e.preventDefault()
    setBusy(true)
    try {
      const body = { ...f, days: Number(f.days) || 0 }
      if (f.package_id) body.package_id = Number(f.package_id)
      else delete body.package_id
      await api.post('/users', body)
      setF({ username: '', password: '', remark: '', package_id: f.package_id, days: 30 })
      toast('已创建')
      load()
    } catch (e) { toast(e.message, 'error') }
    finally { setBusy(false) }
  }

  const act = async (fn) => {
    try { await fn(); load() } catch (e) { toast(e.message, 'error') }
  }

  const resetPw = async (u) => {
    const pw = await dialog.prompt({ title: `重置 ${u.username} 的密码`, message: '至少 6 位。', inputType: 'password', okText: '保存' })
    if (!pw) return
    await act(() => api.post(`/users/${u.id}/password`, { password: pw }))
    toast('密码已改')
  }

  const remove = async (u) => {
    if (!(await dialog.confirm({ title: '删除用户', message: `将删除 ${u.username} 及其订阅。`, danger: true }))) return
    await act(() => api.del(`/users/${u.id}`))
  }

  const copySub = (u) => {
    const url = `${window.location.origin}/api/sub/${u.sub_token}`
    navigator.clipboard.writeText(url).then(() => toast('已复制订阅链接'))
  }

  const rows = list.filter(u => u.role !== 'admin').concat(list.filter(u => u.role === 'admin'))

  return (
    <div>
      <PageHead kicker="People" title="用户" desc="一人一套餐。到期或超量会从内核配置里摘掉客户端。" />
      <form onSubmit={create} className="card p-5 mb-4 grid grid-cols-1 md:grid-cols-5 gap-3">
        <Field label="用户名"><input className="input-field" placeholder="alice" value={f.username} onChange={e => setF({ ...f, username: e.target.value })} required /></Field>
        <Field label="密码"><input className="input-field" placeholder="至少 6 位" value={f.password} onChange={e => setF({ ...f, password: e.target.value })} required /></Field>
        <Field label="套餐">
          <select className="input-field" value={f.package_id} onChange={e => setF({ ...f, package_id: e.target.value })}>
            <option value="">不绑定</option>
            {pkgs.map(p => <option key={p.id} value={p.id}>{p.name}</option>)}
          </select>
        </Field>
        <Field label="天数"><input className="input-field" type="number" value={f.days} onChange={e => setF({ ...f, days: e.target.value })} /></Field>
        <div className="flex items-end"><button className="btn-primary w-full" disabled={busy}><Icon name="plus" size={16} /> 创建</button></div>
      </form>
      <div className="card overflow-hidden">
        {rows.length === 0 ? (
          <Empty title="暂无用户" hint="先建套餐，再开账号。" />
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
                    <td>{u.package_name || '—'}</td>
                    <td className="text-[12px] tabular-nums">{fmtBytes((u.used_up || 0) + (u.used_down || 0))}</td>
                    <td className="text-[12px]">{u.expires_at ? fmtDate(u.expires_at) : '—'}</td>
                    <td>{u.enabled ? <Badge tone="ok">启用</Badge> : <Badge tone="muted">停用</Badge>}</td>
                    <td className="whitespace-nowrap">
                      {u.role !== 'admin' && (
                        <div className="flex gap-3 justify-end text-[12px]">
                          <button type="button" className="linkish" onClick={() => act(() => api.put(`/users/${u.id}`, { remark: u.remark, enabled: !u.enabled }))}>{u.enabled ? '停用' : '启用'}</button>
                          <button type="button" className="linkish" onClick={() => copySub(u)}>订阅</button>
                          <button type="button" className="linkish" onClick={() => act(() => api.post(`/users/${u.id}/reset-traffic`))}>清流量</button>
                          <button type="button" className="linkish" onClick={() => resetPw(u)}>改密</button>
                          <button type="button" className="linkish" style={{ color: 'var(--color-danger)' }} onClick={() => remove(u)}>删除</button>
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
    </div>
  )
}
