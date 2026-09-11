import { useEffect, useState } from 'react'
import { api } from '../lib/api'
import { useToast, useDialog } from '../components/Layout'
import { Empty, Field, Icon, PageHead } from '../components/ui'

export default function Packages() {
  const toast = useToast()
  const dialog = useDialog()
  const [list, setList] = useState([])
  const [ins, setIns] = useState([])
  const [f, setF] = useState({ name: '', gb: 100, cycle_days: 30, direction: 'oneway', inbound_ids: [] })
  const [busy, setBusy] = useState(false)

  const load = async () => {
    try {
      const [a, b] = await Promise.all([api.get('/packages'), api.get('/inbounds')])
      setList(a.packages || [])
      setIns(b.inbounds || [])
    } catch (e) { toast(e.message, 'error') }
  }
  useEffect(() => { load() }, [])

  const toggleIn = (id) => {
    setF(prev => {
      const has = prev.inbound_ids.includes(id)
      return { ...prev, inbound_ids: has ? prev.inbound_ids.filter(x => x !== id) : [...prev.inbound_ids, id] }
    })
  }

  const create = async (e) => {
    e.preventDefault()
    setBusy(true)
    try {
      await api.post('/packages', {
        name: f.name,
        traffic_bytes: Math.round(Number(f.gb) * 1024 * 1024 * 1024),
        cycle_days: Number(f.cycle_days) || 30,
        direction: f.direction,
        inbound_ids: f.inbound_ids,
      })
      setF({ name: '', gb: 100, cycle_days: 30, direction: 'oneway', inbound_ids: [] })
      toast('已创建')
      load()
    } catch (e) { toast(e.message, 'error') }
    finally { setBusy(false) }
  }

  const del = async (id) => {
    if (!(await dialog.confirm({ title: '删除套餐', message: '绑定用户将失去线路。', danger: true }))) return
    try { await api.del(`/packages/${id}`); load() }
    catch (e) { toast(e.message, 'error') }
  }

  return (
    <div>
      <PageHead kicker="Plans" title="套餐" desc="流量按 (上行+下行)×计费方向×节点倍率。不选入站表示全部线路。" />
      <form onSubmit={create} className="card p-5 mb-4 space-y-4">
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
          <div className="text-[12px] font-medium text-ink-soft mb-2">包含入站</div>
          <div className="flex flex-wrap gap-2">
            {ins.length === 0 && <span className="text-[13px] text-ink-mut">还没有入站，创建后默认为全部。</span>}
            {ins.map(x => (
              <label key={x.id} className={`px-3 py-1.5 rounded-full border text-[13px] cursor-pointer ${f.inbound_ids.includes(x.id) ? 'border-[var(--color-gold)] bg-[var(--color-accent-soft)]' : ''}`}>
                <input type="checkbox" className="sr-only" checked={f.inbound_ids.includes(x.id)} onChange={() => toggleIn(x.id)} />
                {x.name}
              </label>
            ))}
          </div>
        </div>
        <button className="btn-primary" disabled={busy}><Icon name="plus" size={16} /> 创建套餐</button>
      </form>
      <div className="card overflow-hidden">
        {list.length === 0 ? (
          <Empty title="暂无套餐" hint="给用户准备一份流量与线路组合。" />
        ) : (
          <div className="table-wrap">
            <table className="data">
              <thead><tr><th>名称</th><th>流量</th><th>周期</th><th>计费</th><th>入站</th><th></th></tr></thead>
              <tbody>
                {list.map(p => (
                  <tr key={p.id}>
                    <td className="font-medium">{p.name}</td>
                    <td>{p.traffic_bytes ? (p.traffic_bytes / 1024 / 1024 / 1024).toFixed(0) + ' GB' : '不限'}</td>
                    <td>{p.cycle_days} 天</td>
                    <td>{p.direction === 'twoway' ? '双向' : '单向'}</td>
                    <td>{(p.inbound_ids || []).length || '全部'}</td>
                    <td className="text-right">
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
