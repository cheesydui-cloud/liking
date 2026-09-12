import { useEffect, useState } from 'react'
import { api } from '../lib/api'
import { useToast, useDialog } from '../components/Layout'
import { Empty, Field, Icon, PageHead } from '../components/ui'

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
  const [f, setF] = useState(emptyForm)
  const [editId, setEditId] = useState(0)
  const [busy, setBusy] = useState(false)

  const load = async () => {
    try {
      const [a, b, c] = await Promise.all([api.get('/packages'), api.get('/servers'), api.get('/inbounds')])
      setList(a.packages || [])
      setServers(b.servers || [])
      setIns(c.inbounds || [])
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

  const startEdit = (p) => {
    setEditId(p.id)
    setF({
      name: p.name || '',
      gb: p.traffic_bytes ? Math.round(p.traffic_bytes / 1024 / 1024 / 1024) : 0,
      cycle_days: p.cycle_days || 30,
      direction: p.direction || 'oneway',
      server_ids: selectedServerIds(p, ins),
    })
    window.scrollTo({ top: 0, behavior: 'smooth' })
  }

  const save = async (e) => {
    e.preventDefault()
    setBusy(true)
    const body = {
      name: f.name,
      traffic_bytes: Math.round(Number(f.gb) * 1024 * 1024 * 1024),
      cycle_days: Number(f.cycle_days) || 30,
      direction: f.direction,
      server_ids: f.server_ids,
    }
    try {
      if (editId) await api.put(`/packages/${editId}`, body)
      else await api.post('/packages', body)
      resetForm()
      toast(editId ? '已保存' : '已创建')
      load()
    } catch (e) { toast(e.message, 'error') }
    finally { setBusy(false) }
  }

  const del = async (id) => {
    if (!(await dialog.confirm({ title: '删除套餐', message: '绑定用户将失去线路。', danger: true }))) return
    try {
      await api.del(`/packages/${id}`)
      if (editId === id) resetForm()
      load()
    } catch (e) { toast(e.message, 'error') }
  }

  const linesOf = (serverId) => ins.filter(x => x.server_id === serverId)

  return (
    <div>
      <PageHead kicker="Plans" title="套餐" desc="勾选这个套餐能用的节点。不选表示全部节点；节点上后加的线路会自动进入套餐。" />
      <form onSubmit={save} className="card p-5 mb-4 space-y-4">
        <div className="kicker">{editId ? '编辑套餐' : '新建套餐'}</div>
        <div className="grid grid-cols-1 md:grid-cols-4 gap-3">
          <Field label="名称"><input className="input-field" placeholder="标准月付" value={f.name} onChange={e => setF({ ...f, name: e.target.value })} required /></Field>
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
              <button type="button" className="linkish text-[12px]" onClick={() => setF({ ...f, server_ids: [] })}>清空为全部</button>
            )}
          </div>
          {servers.length === 0 ? (
            <div className="text-[13px] text-ink-mut py-3">还没有节点。先到「服务器」添加并安装 Agent。</div>
          ) : (
            <div className="grid grid-cols-1 md:grid-cols-2 gap-2">
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
                        {lines.length ? lines.map(x => `${x.name} :${x.port}`).join(' · ') : '暂无入站'}
                      </span>
                    </span>
                  </button>
                )
              })}
            </div>
          )}
        </div>
        <div className="flex gap-2">
          <button className="btn-primary" disabled={busy}>
            <Icon name={editId ? 'check' : 'plus'} size={16} /> {editId ? '保存套餐' : '创建套餐'}
          </button>
          {editId ? (
            <button type="button" className="btn-ghost" onClick={resetForm}>取消编辑</button>
          ) : null}
        </div>
      </form>
      <div className="card overflow-hidden">
        {list.length === 0 ? (
          <Empty title="暂无套餐" hint="勾选节点，再把套餐绑给用户。" />
        ) : (
          <div className="table-wrap">
            <table className="data">
              <thead><tr><th>名称</th><th>流量</th><th>周期</th><th>计费</th><th>节点</th><th></th></tr></thead>
              <tbody>
                {list.map(p => (
                  <tr key={p.id}>
                    <td className="font-medium">{p.name}</td>
                    <td>{p.traffic_bytes ? (p.traffic_bytes / 1024 / 1024 / 1024).toFixed(0) + ' GB' : '不限'}</td>
                    <td>{p.cycle_days} 天</td>
                    <td>{p.direction === 'twoway' ? '双向' : '单向'}</td>
                    <td className="text-[13px]">{packageNodesText(p, servers)}</td>
                    <td className="whitespace-nowrap text-right">
                      <button type="button" className="linkish mr-3" onClick={() => startEdit(p)}>编辑</button>
                      <button type="button" className="linkish" style={{ color: 'var(--color-danger)' }} onClick={() => del(p.id)}>删除</button>
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
