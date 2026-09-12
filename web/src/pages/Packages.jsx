import { useEffect, useState } from 'react'
import { api } from '../lib/api'
import { useToast, useDialog } from '../components/Layout'
import { Badge, Empty, Field, Icon, Modal, PageHead } from '../components/ui'

const emptyForm = { name: '', gb: 100, cycle_days: 30, direction: 'oneway', server_ids: [] }

function selectedServerIds(pkg, ins = []) {
  if (Array.isArray(pkg.server_ids) && pkg.server_ids.length) return pkg.server_ids.map(Number)
  const iids = pkg.inbound_ids || []
  if (iids.length) {
    return [...new Set(ins.filter(x => iids.includes(x.id)).map(x => Number(x.server_id)))]
  }
  return []
}

function packageNodesText(p, servers) {
  const sids = Array.isArray(p.server_ids) ? p.server_ids.map(Number).filter(Boolean) : []
  if (sids.length) {
    const names = sids.map(id => servers.find(s => s.id === id)?.name).filter(Boolean)
    return names.length ? names.join('、') : `${sids.length} 台`
  }
  if ((p.inbound_ids || []).length) return `${p.inbound_ids.length} 条线路`
  return '全部节点'
}

export default function Packages() {
  const toast = useToast()
  const dialog = useDialog()
  const [list, setList] = useState([])
  const [servers, setServers] = useState([])
  const [ins, setIns] = useState([])
  const [users, setUsers] = useState([])
  const [f, setF] = useState(emptyForm)
  const [editId, setEditId] = useState(0)
  const [formOpen, setFormOpen] = useState(false)
  const [busy, setBusy] = useState(false)

  const load = async () => {
    try {
      const [a, b, c, d] = await Promise.all([api.get('/packages'), api.get('/servers'), api.get('/inbounds'), api.get('/users')])
      setList(a.packages || [])
      setServers(b.servers || [])
      setIns(c.inbounds || [])
      setUsers(d.users || [])
    } catch (e) { toast(e.message, 'error') }
  }
  useEffect(() => { load() }, [])

  const toggleServer = (id) => {
    const n = Number(id)
    setF(prev => {
      const has = prev.server_ids.includes(n)
      return { ...prev, server_ids: has ? prev.server_ids.filter(x => x !== n) : [...prev.server_ids, n] }
    })
  }

  const resetForm = () => {
    setEditId(0)
    setF(emptyForm)
  }

  const openCreate = () => {
    resetForm()
    setFormOpen(true)
  }

  const startEdit = (p) => {
    setEditId(p.id)
    setF({
      name: p.name || '',
      gb: p.traffic_bytes ? Math.round(p.traffic_bytes / 1024 / 1024 / 1024) : 0,
      cycle_days: p.cycle_days || 30,
      direction: p.direction || 'oneway',
      server_ids: selectedServerIds(p, ins),
    })
    setFormOpen(true)
  }

  const closeForm = () => {
    setFormOpen(false)
    resetForm()
  }

  const save = async (e) => {
    e.preventDefault()
    if (!f.server_ids.length) {
      const ok = await dialog.confirm({
        title: '包含全部节点？',
        message: '没有勾选节点时，绑定该套餐的用户可以使用所有节点（含以后新加的）。',
        okText: '全部节点',
      })
      if (!ok) return
    }
    setBusy(true)
    const body = {
      name: f.name,
      traffic_bytes: Math.round(Number(f.gb) * 1024 * 1024 * 1024),
      cycle_days: Number(f.cycle_days) || 30,
      direction: f.direction,
      server_ids: f.server_ids,
    }
    const wasEdit = !!editId
    try {
      if (wasEdit) await api.put(`/packages/${editId}`, body)
      else await api.post('/packages', body)
      closeForm()
      toast(wasEdit ? '已保存' : '已创建')
      load()
    } catch (e) { toast(e.message, 'error') }
    finally { setBusy(false) }
  }

  const del = async (id) => {
    if (!(await dialog.confirm({ title: '删除套餐', message: '绑定用户将失去线路。', danger: true }))) return
    try {
      await api.del(`/packages/${id}`)
      if (editId === id) closeForm()
      load()
    } catch (e) { toast(e.message, 'error') }
  }

  const linesOf = (serverId) => ins.filter(x => x.server_id === serverId)

  return (
    <div>
      <PageHead
        title="套餐"
        desc="勾选这个套餐能用的节点。不选表示全部节点；节点上后加的线路会自动进入套餐。"
        actions={
          <button type="button" className="btn-primary" onClick={openCreate}>
            <Icon name="plus" size={15} /> 新建套餐
          </button>
        }
      />
      {list.length === 0 ? (
        <div className="card overflow-hidden">
          <Empty title="暂无套餐" hint="勾选节点，再把套餐绑给用户。" action={
            <button type="button" className="btn-primary" onClick={openCreate}><Icon name="plus" size={15} /> 新建套餐</button>
          } />
        </div>
      ) : (
        <div className="grid md:grid-cols-2 xl:grid-cols-3 gap-3">
          {list.map(p => {
            const n = users.filter(u => u.role !== 'admin' && u.package_id === p.id).length
            const names = (p.server_ids || []).map(id => servers.find(s => s.id === id)?.name).filter(Boolean)
            const gb = p.traffic_bytes ? `${Math.round(p.traffic_bytes / 1024 / 1024 / 1024)} GB` : '不限'
            return (
              <div key={p.id} className="card plan-card">
                <div className="flex items-start justify-between gap-3">
                  <div className="min-w-0">
                    <div className="text-[15px] font-semibold truncate">{p.name}</div>
                    <div className="text-[12px] text-ink-mut mt-0.5">{n} 个用户</div>
                  </div>
                  <Badge tone="gold">{p.direction === 'twoway' ? '双向' : '单向'}</Badge>
                </div>
                <div className="grid grid-cols-2 gap-2 text-[13px]">
                  <div>
                    <div className="kicker">流量</div>
                    <div className="mt-0.5 font-medium">{gb}</div>
                  </div>
                  <div>
                    <div className="kicker">周期</div>
                    <div className="mt-0.5 font-medium">{p.cycle_days} 天</div>
                  </div>
                </div>
                <div>
                  <div className="kicker mb-1.5">关联节点</div>
                  <div className="plan-nodes">
                    {names.length ? names.map(n => <span key={n} className="chip">{n}</span>) : (
                      <span className="chip">{packageNodesText(p, servers)}</span>
                    )}
                  </div>
                </div>
                <div className="flex gap-3 mt-auto pt-1">
                  <button type="button" className="row-act" onClick={() => startEdit(p)}>编辑</button>
                  <button type="button" className="row-act is-danger" onClick={() => del(p.id)}>删除</button>
                </div>
              </div>
            )
          })}
        </div>
      )}
      <Modal open={formOpen} title={editId ? '编辑套餐' : '新建套餐'} onClose={closeForm} size="lg" footer={
        <>
          <button type="button" className="btn-ghost" onClick={closeForm}>取消</button>
          <button type="submit" form="pkg-form" className="btn-primary" disabled={busy}>{busy ? '保存中…' : (editId ? '保存' : '创建')}</button>
        </>
      }>
        <form id="pkg-form" onSubmit={save} className="space-y-4">
          <div className="grid grid-cols-1 sm:grid-cols-2 gap-3">
            <Field label="名称"><input className="input-field" placeholder="标准月付" value={f.name} onChange={e => setF({ ...f, name: e.target.value })} required autoFocus /></Field>
            <Field label="流量 GB" hint="0 = 不限"><input className="input-field" type="number" value={f.gb} onChange={e => setF({ ...f, gb: e.target.value })} /></Field>
            <Field label="周期天数"><input className="input-field" type="number" value={f.cycle_days} onChange={e => setF({ ...f, cycle_days: e.target.value })} /></Field>
            <Field label="计费">
              <select className="input-field" value={f.direction} onChange={e => setF({ ...f, direction: e.target.value })}>
                <option value="oneway">单向</option>
                <option value="twoway">双向</option>
              </select>
            </Field>
          </div>
          <div>
            <div className="flex items-end justify-between gap-3 mb-2">
              <div>
                <div className="text-[12px] font-medium text-ink-soft">包含节点</div>
                <div className="text-[11.5px] text-ink-mut mt-0.5">
                  {f.server_ids.length ? `已选 ${f.server_ids.length} 台` : '未勾选 = 全部节点'}
                </div>
              </div>
              {f.server_ids.length > 0 && (
                <button type="button" className="row-act" onClick={() => setF({ ...f, server_ids: [] })}>清空为全部</button>
              )}
            </div>
            {servers.length === 0 ? (
              <div className="text-[13px] text-ink-mut py-3">还没有节点。先到「节点管理」添加并安装 Agent。</div>
            ) : (
              <div className="grid grid-cols-1 gap-2">
                {servers.map(s => {
                  const on = f.server_ids.includes(s.id)
                  const lines = linesOf(s.id)
                  return (
                    <button
                      type="button"
                      key={s.id}
                      className={`node-pick ${on ? 'is-on' : ''}`}
                      onClick={() => toggleServer(s.id)}
                      aria-pressed={on}
                    >
                      <span className={`node-check ${on ? 'is-on' : ''}`} aria-hidden>{on ? '✓' : ''}</span>
                      <span className="min-w-0 flex-1 text-left">
                        <span className="flex items-center gap-2">
                          <span className="font-medium truncate">{s.name}</span>
                          <span className={`dot ${s.online ? 'dot-on' : 'dot-off'}`} />
                          <span className="text-[11px] text-ink-mut">{s.online ? '在线' : '离线'}</span>
                        </span>
                        <span className="block text-[12px] text-ink-mut font-mono truncate mt-0.5">{s.public_host || '未填公开地址'}</span>
                        <span className="block text-[12px] text-ink-soft mt-1 truncate">
                          {lines.length ? lines.map(x => `${x.name} :${x.port}`).join(' · ') : '暂无线路'}
                        </span>
                      </span>
                    </button>
                  )
                })}
              </div>
            )}
          </div>
        </form>
      </Modal>
    </div>
  )
}
