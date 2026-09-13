import { useEffect, useState } from 'react'
import { Link } from 'react-router-dom'
import { api } from '../lib/api'
import { useToast } from '../components/Layout'
import { DayBars, Empty, FilterTabs, Icon, PageHead, SkeletonRows, fmtBps, fmtBytes } from '../components/ui'

const RANGES = [7, 14, 30]

export default function Traffic() {
  const toast = useToast()
  const [daysN, setDaysN] = useState(14)
  const [d, setD] = useState(null)
  const [err, setErr] = useState('')

  const load = (n) => {
    api.get(`/traffic?days=${n}`).then(setD).catch(e => {
      setErr(e.message)
      toast(e.message, 'error')
    })
  }
  useEffect(() => { load(daysN) }, [daysN])

  if (err && !d) return <div style={{ color: 'var(--color-danger)' }}>{err}</div>
  if (!d) return <div className="card"><SkeletonRows /></div>

  const today = (d.days || [])[d.days.length - 1] || { up: 0, down: 0 }
  const rangeRaw = (d.days || []).reduce((s, x) => s + (x.up || 0) + (x.down || 0), 0)

  return (
    <div>
      <PageHead
        title="流量"
        desc="日统计和排行是节点原始流量。用户已用按套餐单向 / 双向和节点倍率计，超量会从内核摘掉。1 GiB = 1024³ 字节。"
        actions={
          <div className="flex flex-wrap items-center gap-4">
            <FilterTabs
              value={daysN}
              onChange={setDaysN}
              items={RANGES.map(n => [n, `${n} 天`])}
            />
            <button type="button" className="btn-ghost h-8" onClick={() => api.download(`/traffic.csv?days=${daysN}`, 'liking-traffic.csv').catch(e => toast(e.message, 'error'))}>
              <Icon name="download" size={14} /> 导出 CSV
            </button>
          </div>
        }
      />
      <div className="stat-row">
        <div>
          <div className="kicker">计费合计</div>
          <span className="stat-val">{fmtBytes(d.billed_bytes || 0)}</span>
        </div>
        <div>
          <div className="kicker">原始累计</div>
          <span className="stat-val">{fmtBytes(d.raw_bytes || rangeRaw)}</span>
        </div>
        <div>
          <div className="kicker">区间原始</div>
          <span className="stat-val">{fmtBytes(rangeRaw)}</span>
        </div>
        <div>
          <div className="kicker">本月原始</div>
          <span className="stat-val">{fmtBytes(d.month_bytes || 0)}</span>
        </div>
        <div>
          <div className="kicker">实时网卡</div>
          <span className="stat-val">{fmtBps((d.nic_up_bps || 0) + (d.nic_down_bps || 0))}</span>
        </div>
      </div>
      <div className="mb-5">
        <div className="text-[14px] font-semibold mb-3">每日流量</div>
        {rangeRaw === 0 ? (
          <Empty title="还没有统计" hint="Agent 每 5 秒上报一次。Xray、AnyTLS、Mieru 都会计入。" />
        ) : (
          <DayBars days={d.days} />
        )}
      </div>
      <div className="grid lg:grid-cols-2 gap-4">
        <div className="card overflow-hidden">
          <div className="panel-head">
            <div>用户</div>
            <Link to="/users" className="btn-ghost h-8">管理</Link>
          </div>
          {(d.users || []).length === 0 ? (
            <div className="px-4 py-6 text-[13px] text-ink-mut">这个区间没有用户流量。</div>
          ) : (
            <div className="table-wrap">
              <table className="data">
                <thead><tr><th>用户</th><th>上行</th><th>下行</th><th>合计</th></tr></thead>
                <tbody>
                  {d.users.map(u => (
                    <tr key={u.id}>
                      <td className="font-medium">{u.name}</td>
                      <td className="tabular-nums text-[12px]">{fmtBytes(u.up)}</td>
                      <td className="tabular-nums text-[12px]">{fmtBytes(u.down)}</td>
                      <td className="tabular-nums text-[12px]">{fmtBytes((u.up || 0) + (u.down || 0))}</td>
                    </tr>
                  ))}
                </tbody>
              </table>
            </div>
          )}
        </div>
        <div className="card overflow-hidden">
          <div className="panel-head">
            <div>节点</div>
            <Link to="/nodes" className="btn-ghost h-8">管理</Link>
          </div>
          {(d.inbounds || []).length === 0 ? (
            <div className="px-4 py-6 text-[13px] text-ink-mut">这个区间没有节点流量。</div>
          ) : (
            <div className="table-wrap">
              <table className="data">
                <thead><tr><th>节点</th><th>上行</th><th>下行</th><th>合计</th></tr></thead>
                <tbody>
                  {d.inbounds.map(inb => (
                    <tr key={inb.id}>
                      <td className="font-medium">{inb.name || `节点 ${inb.id}`}</td>
                      <td className="tabular-nums text-[12px]">{fmtBytes(inb.up)}</td>
                      <td className="tabular-nums text-[12px]">{fmtBytes(inb.down)}</td>
                      <td className="tabular-nums text-[12px]">{fmtBytes((inb.up || 0) + (inb.down || 0))}</td>
                    </tr>
                  ))}
                </tbody>
              </table>
            </div>
          )}
        </div>
      </div>
      <div className="mt-4 text-[12px] text-ink-mut">
        1 GiB = 1024³ 字节。链式只计入站，不重复计落地。计费含双向和节点倍率，原始是内核累计，网卡是 Agent 实时。
      </div>
    </div>
  )
}
