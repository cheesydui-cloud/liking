import { useEffect, useRef, useState } from 'react'
import { Link } from 'react-router-dom'
import { api } from '../lib/api'
import { useToast } from '../components/Layout'
import { startPoll } from '../lib/poll'
import { asArray, isAbort } from '../lib/safe'
import { DayBars, Empty, FilterTabs, Icon, PageHead, SkeletonRows, fmtBps, fmtBytes } from '../components/ui'

const RANGES = [7, 14, 30]

export default function Traffic() {
  const toast = useToast()
  const [daysN, setDaysN] = useState(14)
  const [d, setD] = useState(null)
  const [err, setErr] = useState('')
  const [pollErr, setPollErr] = useState('')
  const seq = useRef(0)

  useEffect(() => {
    const n = ++seq.current
    setErr('')
    api.get(`/traffic?days=${daysN}`).then(data => {
      if (n !== seq.current) return
      setD(data)
    }).catch(e => {
      if (n !== seq.current || isAbort(e)) return
      setErr(e.message)
      toast(e.message, 'error')
    })
  }, [daysN])

  useEffect(() => startPoll(async (signal) => {
    const n = ++seq.current
    try {
      const data = await api.get(`/traffic?days=${daysN}`, signal)
      if (n !== seq.current) return
      setD(data)
      setPollErr('')
      setErr('')
    } catch (e) {
      if (isAbort(e)) return
      setPollErr('实时刷新失败，显示的是上次成功数据')
      throw e
    }
  }, 5000, { immediate: false }), [daysN])

  if (err && !d) return <div style={{ color: 'var(--color-danger)' }}>{err}</div>
  if (!d) return <div className="card"><SkeletonRows /></div>

  const days = asArray(d.days)
  const users = asArray(d.users)
  const inbounds = asArray(d.inbounds)
  const rangeRaw = days.reduce((s, x) => s + (x.up || 0) + (x.down || 0), 0)

  return (
    <div>
      <PageHead title="流量" />
      <div className="flex flex-col sm:flex-row sm:items-center gap-2 mb-4">
        <FilterTabs
          value={daysN}
          onChange={setDaysN}
          items={RANGES.map(n => [n, `${n} 天`])}
        />
        <button type="button" className="btn-ghost h-8 shrink-0 self-start sm:ml-auto" onClick={() => api.download(`/traffic.csv?days=${daysN}`, 'liking-traffic.csv').catch(e => toast(e.message, 'error'))}>
          <Icon name="download" size={14} /> 导出 CSV
        </button>
      </div>
      {pollErr ? <div className="alert-row is-warn mb-4">{pollErr}</div> : null}
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
          <DayBars days={days} />
        )}
      </div>
      <div className="grid lg:grid-cols-2 gap-4">
        <div className="card overflow-hidden">
          <div className="panel-head">
            <div>用户</div>
            <Link to="/users" className="btn-ghost h-8">管理</Link>
          </div>
          {users.length === 0 ? (
            <div className="px-4 py-6 text-[13px] text-ink-mut">这个区间没有用户流量。</div>
          ) : (
            <>
            <div className="hidden md:block table-wrap">
              <table className="data">
                <thead><tr><th>用户</th><th>上行</th><th>下行</th><th>合计</th></tr></thead>
                <tbody>
                  {users.map(u => (
                    <tr key={u.id}>
                      <td className="font-medium">{u.name}</td>
                      <td className="tabular-nums text-[12px] speed-up">{fmtBytes(u.up)}</td>
                      <td className="tabular-nums text-[12px] speed-down">{fmtBytes(u.down)}</td>
                      <td className="tabular-nums text-[12px]">{fmtBytes((u.up || 0) + (u.down || 0))}</td>
                    </tr>
                  ))}
                </tbody>
              </table>
            </div>
            <div className="md:hidden divide-y" style={{ borderColor: 'var(--color-line-soft)' }}>
              {users.map(u => (
                <div key={u.id} className="px-3.5 py-3">
                  <div className="font-medium">{u.name}</div>
                  <div className="text-[12px] text-ink-mut mt-0.5 font-mono">
                    <span className="speed-up">↑ {fmtBytes(u.up)}</span>
                    {' · '}
                    <span className="speed-down">↓ {fmtBytes(u.down)}</span>
                    {' · '}
                    {fmtBytes((u.up || 0) + (u.down || 0))}
                  </div>
                </div>
              ))}
            </div>
            </>
          )}
        </div>
        <div className="card overflow-hidden">
          <div className="panel-head">
            <div>节点</div>
            <Link to="/nodes" className="btn-ghost h-8">管理</Link>
          </div>
          {inbounds.length === 0 ? (
            <div className="px-4 py-6 text-[13px] text-ink-mut">这个区间没有节点流量。</div>
          ) : (
            <>
            <div className="hidden md:block table-wrap">
              <table className="data">
                <thead><tr><th>节点</th><th>上行</th><th>下行</th><th>合计</th></tr></thead>
                <tbody>
                  {inbounds.map(inb => (
                    <tr key={inb.id}>
                      <td className="font-medium">{inb.name || `节点 ${inb.id}`}</td>
                      <td className="tabular-nums text-[12px] speed-up">{fmtBytes(inb.up)}</td>
                      <td className="tabular-nums text-[12px] speed-down">{fmtBytes(inb.down)}</td>
                      <td className="tabular-nums text-[12px]">{fmtBytes((inb.up || 0) + (inb.down || 0))}</td>
                    </tr>
                  ))}
                </tbody>
              </table>
            </div>
            <div className="md:hidden divide-y" style={{ borderColor: 'var(--color-line-soft)' }}>
              {inbounds.map(inb => (
                <div key={inb.id} className="px-3.5 py-3">
                  <div className="font-medium">{inb.name || `节点 ${inb.id}`}</div>
                  <div className="text-[12px] text-ink-mut mt-0.5 font-mono">
                    <span className="speed-up">↑ {fmtBytes(inb.up)}</span>
                    {' · '}
                    <span className="speed-down">↓ {fmtBytes(inb.down)}</span>
                    {' · '}
                    {fmtBytes((inb.up || 0) + (inb.down || 0))}
                  </div>
                </div>
              ))}
            </div>
            </>
          )}
        </div>
      </div>
      <div className="mt-4 text-[12px] text-ink-mut">
        1 GiB = 1024³ 字节。链式只计入站，不重复计落地。计费：单向只计下行，双向计上下行；节点倍率在入账时乘。原始是内核累计。
      </div>
    </div>
  )
}
