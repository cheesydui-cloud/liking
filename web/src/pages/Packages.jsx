import { useEffect, useMemo, useState } from 'react'
import { api } from '../lib/api'
import { useToast, useDialog } from '../components/Layout'
import { Badge, Empty, Field, Icon, Modal, MoreMenu, PageHead, SearchInput } from '../components/ui'

const emptyForm = { name: '', gb: '', direction: 'oneway', inbound_ids: [], multipliers: {} }

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

function selectedInboundIds(pkg, ins = []) {
  const iids = Array.isArray(pkg.inbound_ids) ? pkg.inbound_ids.map(Number).filter(Boolean) : []
  if (iids.length) return iids
  const sids = Array.isArray(pkg.server_ids) ? pkg.server_ids.map(Number).filter(Boolean) : []
  if (sids.length) return ins.filter(x => sids.includes(Number(x.server_id))).map(x => Number(x.id))
  return []
}

function packageNodes(p, ins, servers) {
  const iids = Array.isArray(p.inbound_ids) ? p.inbound_ids.map(Number).filter(Boolean) : []
  const sids = Array.isArray(p.server_ids) ? p.server_ids.map(Number).filter(Boolean) : []
  let nodes = []
  if (iids.length) nodes = iids.map(id => ins.find(x => Number(x.id) === id)).filter(Boolean)
  else if (sids.length) nodes = ins.filter(x => sids.includes(Number(x.server_id)))
  else return { all: true, count: 0, servers: [], names: [] }
  nodes = nodes.filter(n => n && n.profile !== 'port-forward' && n.user_facing !== false)

  const seen = new Set()
  const serverNames = []
  for (const n of nodes) {
    const sid = Number(n.server_id)
    if (seen.has(sid)) continue
    seen.add(sid)
    const s = servers.find(x => Number(x.id) === sid)
    const label = s?.name || n.server_name || ''
    if (label) serverNames.push(label)
  }
  return {
    all: false,
    count: nodes.length || iids.length || sids.length,
    servers: serverNames,
    names: nodes.map(n => n.name).filter(Boolean),
  }
}

function PackageNodesCell({ p, ins, servers }) {
  const meta = packageNodes(p, ins, servers)
  if (meta.all) return <span className="text-ink-mut">全部</span>
  const tip = meta.names.length ? meta.names.join('、') : undefined
  return (
    <div title={tip}>
      <span className="tabular-nums font-mono text-[12px]">{meta.count}</span>
      {meta.servers.length ? (
        <div className="text-[11px] text-ink-mut truncate mt-0.5">{meta.servers.join(' · ')}</div>
      ) : null}
    </div>
  )
}

function trafficLabel(bytes) {
  if (!bytes) return '不限'
  const gb = bytes / 1024 / 1024 / 1024
  const n = Math.round(gb * 1000) / 1000
  return `${n} GB`
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
  const [q, setQ] = useState('')

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

  const toggleNode = (id) => {
    const n = Number(id)
    setF(prev => {
      const has = prev.inbound_ids.includes(n)
      return { ...prev, inbound_ids: has ? prev.inbound_ids.filter(x => x !== n) : [...prev.inbound_ids, n] }
    })
  }

  const toggleServerNodes = (ids) => {
    setF(prev => {
      const set = new Set(prev.inbound_ids)
      const allOn = ids.length && ids.every(id => set.has(id))
      if (allOn) ids.forEach(id => set.delete(id))
      else ids.forEach(id => set.add(id))
      return { ...prev, inbound_ids: [...set] }
    })
  }

  const resetForm = () => {
    setEditId(0)
    setF(emptyForm)
    setQ('')
  }

  const openCreate = () => {
    resetForm()
    setFormOpen(true)
  }

  const startEdit = (p) => {
    setEditId(p.id)
    setQ('')
    setF({
      name: p.name || '',
      gb: p.traffic_bytes ? String(Math.round((p.traffic_bytes / 1024 / 1024 / 1024) * 1000) / 1000) : '',
      direction: p.direction || 'oneway',
      inbound_ids: selectedInboundIds(p, ins),
      multipliers: Object.fromEntries((selectedInboundIds(p, ins) || []).map((id, i) => [id, p.multipliers?.[i] ?? 1])),
    })
    setFormOpen(true)
  }

  const closeForm = () => {
    setFormOpen(false)
    resetForm()
  }

  const save = async (e) => {
    e.preventDefault()
    const name = f.name.trim()
    if (!name) { toast('请填写名称', 'error'); return }
    if (!f.inbound_ids.length) {
      const ok = await dialog.confirm({
        title: '包含全部节点？',
        message: '没有勾选节点时，绑定该套餐的用户可以使用所有节点（含以后新建的）。',
        okText: '全部节点',
      })
      if (!ok) return
    }
    const gbRaw = String(f.gb).trim()
    const gb = gbRaw === '' ? 0 : Number(gbRaw)
    if (!Number.isFinite(gb) || gb < 0) { toast('流量无效', 'error'); return }
    setBusy(true)
    const body = {
      name,
      traffic_bytes: Math.round(gb * 1024 * 1024 * 1024),
      cycle_days: 0,
      direction: f.direction,
      inbound_ids: f.inbound_ids,
      multipliers: f.inbound_ids.map(id => {
        const n = Number(f.multipliers?.[id])
        return Number.isFinite(n) && n > 0 ? n : 1
      }),
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

  const pickableIns = useMemo(
    () => ins.filter(x => x.profile !== 'port-forward' && x.user_facing !== false),
    [ins],
  )

  const filtered = useMemo(() => {
    const needle = q.trim().toLowerCase()
    if (!needle) return pickableIns
    return pickableIns.filter(x => {
      const hay = `${x.name || ''} ${x.profile || ''} ${x.protocol || ''} ${x.port || ''} ${x.server_name || ''} ${x.server_host || ''}`.toLowerCase()
      return hay.includes(needle)
    })
  }, [pickableIns, q])

  const groups = useMemo(() => {
    const by = new Map()
    for (const s of servers) by.set(Number(s.id), { server: s, nodes: [] })
    for (const n of filtered) {
      const sid = Number(n.server_id)
      let g = by.get(sid)
      if (!g) {
        g = {
          server: { id: sid, name: n.server_name || '未命名', public_host: n.server_host || '', online: !!n.server_online },
          nodes: [],
        }
        by.set(sid, g)
      }
      g.nodes.push(n)
    }
    return [...by.values()].filter(g => g.nodes.length)
  }, [servers, filtered])

  return (
    <div>
      <PageHead
        title="套餐"
        desc="勾选这个套餐能用的节点。不选表示全部节点。到期时间在用户上设置。"
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
        <div className="card overflow-hidden">
          <div className="hidden md:block table-wrap">
            <table className="data">
              <thead><tr><th>名称</th><th>流量</th><th>计费</th><th>节点</th><th>用户</th><th></th></tr></thead>
              <tbody>
                {list.map(p => {
                  const n = users.filter(u => u.role !== 'admin' && u.package_id === p.id).length
                  return (
                    <tr key={p.id}>
                      <td className="font-medium">{p.name}</td>
                      <td className="tabular-nums font-mono text-[12px]">{trafficLabel(p.traffic_bytes)}</td>
                      <td>{p.direction === 'twoway' ? '双向' : '单向'}</td>
                      <td className="max-w-[14rem]"><PackageNodesCell p={p} ins={ins} servers={servers} /></td>
                      <td className="tabular-nums font-mono text-[12px]">{n}</td>
                      <td className="whitespace-nowrap">
                        <div className="icon-row">
                          <button type="button" className="icon-btn" onClick={() => startEdit(p)} aria-label="编辑套餐" title="编辑">
                            <Icon name="pencil" size={14} />
                          </button>
                          <MoreMenu iconOnly items={[
                            { label: '删除', danger: true, onSelect: () => del(p.id) },
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
            {list.map(p => {
              const n = users.filter(u => u.role !== 'admin' && u.package_id === p.id).length
              const meta = packageNodes(p, ins, servers)
              const nodeLine = meta.all ? '全部节点' : `${meta.count} 节点${meta.servers.length ? ` · ${meta.servers.join(' · ')}` : ''}`
              return (
                <div key={p.id} className="px-3.5 py-3">
                  <div className="flex items-start justify-between gap-3">
                    <div className="min-w-0">
                      <div className="font-medium truncate">{p.name}</div>
                      <div className="text-[12px] text-ink-mut mt-0.5">
                        {trafficLabel(p.traffic_bytes)} / {p.direction === 'twoway' ? '双向' : '单向'} / {n} 用户
                      </div>
                      <div className="text-[12px] text-ink-mut mt-0.5 truncate" title={meta.names.join('、')}>{nodeLine}</div>
                    </div>
                    <div className="icon-row shrink-0">
                      <button type="button" className="icon-btn" onClick={() => startEdit(p)} aria-label="编辑套餐" title="编辑">
                        <Icon name="pencil" size={14} />
                      </button>
                      <MoreMenu iconOnly items={[
                        { label: '删除', danger: true, onSelect: () => del(p.id) },
                      ]} />
                    </div>
                  </div>
                </div>
              )
            })}
          </div>
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
            <Field label="名称">
              <input className="input-field" value={f.name} onChange={e => setF({ ...f, name: e.target.value })} required autoFocus />
            </Field>
            <Field label="流量 GB" hint="留空 = 不限">
              <input className="input-field" type="number" min="0" step="0.1" placeholder="" value={f.gb} onChange={e => setF({ ...f, gb: e.target.value })} />
            </Field>
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
                  {f.inbound_ids.length ? `已选 ${f.inbound_ids.length} / ${pickableIns.length}` : '未勾选 = 全部节点'}
                </div>
              </div>
              <div className="flex gap-3 shrink-0">
                {filtered.length > 0 && (
                  <button type="button" className="row-act" onClick={() => setF({ ...f, inbound_ids: filtered.map(x => Number(x.id)) })}>全选当前</button>
                )}
                {f.inbound_ids.length > 0 && (
                  <button type="button" className="row-act" onClick={() => setF({ ...f, inbound_ids: [] })}>清空为全部</button>
                )}
              </div>
            </div>
            {pickableIns.length > 6 && (
              <div className="mb-2">
                <SearchInput value={q} onChange={e => setQ(e.target.value)} placeholder="搜索节点 / 协议 / 端口" />
              </div>
            )}
            {pickableIns.length === 0 ? (
              <div className="text-[13px] text-ink-mut py-3">还没有节点。先到「服务器管理」增加节点。</div>
            ) : groups.length === 0 ? (
              <div className="text-[13px] text-ink-mut py-3">没有匹配的节点。</div>
            ) : (
              <div className="space-y-2 max-h-[min(52vh,28rem)] overflow-y-auto">
                {groups.map(g => {
                  const ids = g.nodes.map(x => Number(x.id))
                  const picked = ids.filter(id => f.inbound_ids.includes(id)).length
                  const allOn = picked === ids.length && ids.length > 0
                  return (
                    <div key={g.server.id} className="pkg-group">
                      <div className="pkg-group-head">
                        <div className="min-w-0 flex items-center gap-2">
                          <span className="text-[13px] font-medium truncate">{g.server.name}</span>
                          <span className={`dot ${g.server.online ? 'dot-on' : 'dot-off'}`} />
                          <span className="text-[12px] text-ink-mut truncate">{g.server.public_host || '未填公开地址'}</span>
                          {picked ? <span className="text-[11.5px] text-ink-mut tabular-nums shrink-0">{picked}/{ids.length}</span> : null}
                        </div>
                        <button type="button" className="row-act shrink-0" onClick={() => toggleServerNodes(ids)}>
                          {allOn ? '取消本机' : '全选本机'}
                        </button>
                      </div>
                      {g.nodes.map(n => {
                        const on = f.inbound_ids.includes(Number(n.id))
                        return (
                          <button
                            type="button"
                            key={n.id}
                            className={`pkg-node ${on ? 'is-on' : ''}`}
                            onClick={() => toggleNode(n.id)}
                            aria-pressed={on}
                          >
                            <span className={`node-check ${on ? 'is-on' : ''}`} aria-hidden>{on ? '✓' : ''}</span>
                            <span className="min-w-0 flex-1 text-left">
                              <span className="flex items-center gap-2 min-w-0">
                                <span className={`font-medium truncate ${n.enabled === false ? 'text-ink-mut' : ''}`}>{n.name}</span>
                                {n.line_kind === 'chain' ? <Badge tone="muted">链式</Badge> : null}
                                {n.enabled === false ? <Badge tone="muted">停用</Badge> : null}
                              </span>
                            </span>
                            <span className="shrink-0 text-right">
                              <span className="block text-[12px] text-ink-soft">{protoShort(n.profile)}</span>
                              <span className="block text-[12px] text-ink-mut font-mono tabular-nums">:{n.port}</span>
                            </span>
                            {on ? (
                              <label className="shrink-0 text-[11px] text-ink-mut ml-2" onClick={e => e.stopPropagation()}>
                                倍率
                                <input
                                  className="input-field h-7 w-14 ml-1 text-[12px] tabular-nums"
                                  type="number"
                                  min="0.1"
                                  step="0.1"
                                  value={f.multipliers?.[n.id] ?? 1}
                                  onChange={e => setF(prev => ({ ...prev, multipliers: { ...prev.multipliers, [n.id]: e.target.value } }))}
                                />
                              </label>
                            ) : null}
                          </button>
                        )
                      })}
                    </div>
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
