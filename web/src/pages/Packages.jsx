import { useEffect, useMemo, useState } from 'react'
import { api } from '../lib/api'
import { useToast, useDialog } from '../components/Layout'
import { Badge, Empty, Field, Icon, Modal, MoreMenu, PageHead, SearchInput, StatusWord } from '../components/ui'

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

function trafficLabel(bytes) {
  if (!bytes) return '不限'
  const gb = bytes / 1024 / 1024 / 1024
  const n = Math.round(gb * 1000) / 1000
  return `${n} GB`
}

function Metric({ label, value }) {
  return (
    <div className="metric">
      <span className="metric-k">{label}</span>
      <span className="metric-v">{value}</span>
    </div>
  )
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
      toast('请勾选实例上的节点', 'error')
      return
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
        <div className="machine-grid">
          {list.map(p => {
            const n = users.filter(u => u.role !== 'admin' && u.package_id === p.id).length
            const meta = packageNodes(p, ins, servers)
            const nodeLine = meta.all ? '未选节点' : `${meta.count} 个节点`
            const nodeTip = meta.names.length ? meta.names.join('、') : (meta.servers.join(' · ') || undefined)
            return (
              <div key={p.id} className="machine is-pkg">
                <div className="machine-head">
                  <div className="min-w-0 flex-1">
                    <div className="machine-title">
                      <span className="machine-name truncate">{p.name}</span>
                    </div>
                    {meta.servers.length ? (
                      <div className="machine-host">
                        <span className="machine-host-addr" title={nodeTip}>{meta.servers.join(' · ')}</span>
                      </div>
                    ) : null}
                  </div>
                  <div className="machine-toolbar">
                    <button type="button" className="icon-btn" onClick={() => startEdit(p)} aria-label="编辑套餐" title="编辑">
                      <Icon name="pencil" size={14} />
                    </button>
                    <MoreMenu iconOnly items={[
                      { label: '编辑', onSelect: () => startEdit(p) },
                      { label: '删除', danger: true, onSelect: () => del(p.id) },
                    ]} />
                  </div>
                </div>
                <div className="machine-metrics">
                  <Metric label="流量" value={trafficLabel(p.traffic_bytes)} />
                  <Metric label="计费" value={p.direction === 'twoway' ? '双向' : '单向'} />
                  <Metric label="节点" value={meta.all ? '0' : String(meta.count)} />
                  <Metric label="用户" value={String(n)} />
                </div>
                <div className="machine-foot">
                  <span className="text-[11px] font-mono text-ink-mut truncate" title={nodeTip}>{nodeLine}</span>
                  <button type="button" className="row-act" onClick={() => startEdit(p)}>编辑</button>
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
          <div className="grid grid-cols-1 sm:grid-cols-3 gap-3">
            <Field label="名称">
              <input className="input-field" value={f.name} onChange={e => setF({ ...f, name: e.target.value })} required autoFocus />
            </Field>
            <Field label="流量 GB" hint="留空 = 不限">
              <input className="input-field font-mono" type="number" min="0" step="0.1" value={f.gb} onChange={e => setF({ ...f, gb: e.target.value })} />
            </Field>
            <div>
              <div className="text-[12px] font-medium text-ink-soft mb-1.5">计费</div>
              <div className="seg" role="group" aria-label="计费">
                <button type="button" className={`seg-item${f.direction === 'oneway' ? ' is-on' : ''}`} onClick={() => setF({ ...f, direction: 'oneway' })}>单向</button>
                <button type="button" className={`seg-item${f.direction === 'twoway' ? ' is-on' : ''}`} onClick={() => setF({ ...f, direction: 'twoway' })}>双向</button>
              </div>
            </div>
          </div>
          <div>
            <div className="pkg-section-head">
              <div>
                <div className="text-[12px] font-medium text-ink-soft">节点</div>
                <div className="text-[12px] text-ink-mut mt-0.5">
                  {f.inbound_ids.length ? `已选 ${f.inbound_ids.length} / ${pickableIns.length}` : '请勾选节点，不选则套餐里没有节点'}
                </div>
              </div>
              <div className="flex gap-2 shrink-0">
                {filtered.length > 0 && (
                  <button type="button" className="row-act" onClick={() => setF({ ...f, inbound_ids: filtered.map(x => Number(x.id)) })}>全选</button>
                )}
                {f.inbound_ids.length > 0 && (
                  <button type="button" className="row-act" onClick={() => setF({ ...f, inbound_ids: [] })}>清空</button>
                )}
              </div>
            </div>
            {pickableIns.length > 6 && (
              <div className="mb-2">
                <SearchInput value={q} onChange={e => setQ(e.target.value)} placeholder="搜索节点 / 协议 / 端口" />
              </div>
            )}
            {pickableIns.length === 0 ? (
              <div className="text-[13px] text-ink-mut py-3">还没有节点。先到「节点」增加节点。</div>
            ) : groups.length === 0 ? (
              <div className="text-[13px] text-ink-mut py-3">没有匹配的节点。</div>
            ) : (
              <div className="pkg-pick-list">
                {groups.map(g => {
                  const ids = g.nodes.map(x => Number(x.id))
                  const picked = ids.filter(id => f.inbound_ids.includes(id)).length
                  const allOn = picked === ids.length && ids.length > 0
                  return (
                    <div key={g.server.id} className="pkg-group">
                      <button
                        type="button"
                        className="pkg-group-head"
                        onClick={() => toggleServerNodes(ids)}
                        aria-pressed={allOn}
                      >
                        <div className="min-w-0 flex items-center gap-2">
                          <span className="pkg-server truncate">{g.server.name}</span>
                          <StatusWord online={!!g.server.online} />
                          <span className="text-[12px] text-ink-mut font-mono truncate">{g.server.public_host || ''}</span>
                        </div>
                        <span className="text-[12px] text-ink-mut tabular-nums shrink-0">
                          {picked}/{ids.length}
                        </span>
                      </button>
                      {g.nodes.map(n => {
                        const on = f.inbound_ids.includes(Number(n.id))
                        return (
                          <div key={n.id} className={`pkg-node${on ? ' is-on' : ''}`}>
                            <button
                              type="button"
                              className="pkg-node-hit"
                              onClick={() => toggleNode(n.id)}
                              aria-pressed={on}
                            >
                              <span className={`node-check${on ? ' is-on' : ''}`} aria-hidden>{on ? '✓' : ''}</span>
                              <span className="min-w-0 flex items-center gap-2">
                                <span className={`pkg-node-name truncate${n.enabled === false ? ' text-ink-mut' : ''}`}>{n.name}</span>
                                {n.line_kind === 'chain' ? <Badge tone="muted">链式</Badge> : null}
                                {n.enabled === false ? <Badge tone="muted">停用</Badge> : null}
                              </span>
                              <span className="pkg-node-meta">
                                <span>{protoShort(n.profile)}</span>
                                <span className="pkg-node-port">:{n.port}</span>
                              </span>
                            </button>
                            {on ? (
                              <label className="pkg-mult-wrap">
                                倍率
                                <input
                                  className="input-field pkg-mult tabular-nums"
                                  type="number"
                                  min="0.1"
                                  step="0.1"
                                  aria-label={`${n.name} 倍率`}
                                  value={f.multipliers?.[n.id] ?? 1}
                                  onChange={e => setF(prev => ({ ...prev, multipliers: { ...prev.multipliers, [n.id]: e.target.value } }))}
                                />
                              </label>
                            ) : <span />}
                          </div>
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
