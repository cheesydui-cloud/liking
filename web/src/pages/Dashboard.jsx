import { useEffect, useState } from 'react'
import { Link } from 'react-router-dom'
import { api } from '../lib/api'
import { Badge, DayBars, Empty, PageHead, SkeletonRows, fmtAgo, fmtBps, fmtBytes } from '../components/ui'

export default function Dashboard() {
  const [d, setD] = useState(null)
  const [err, setErr] = useState('')
  useEffect(() => {
    const pull = () => api.get('/dashboard').then(setD).catch(e => setErr(e.message))
    pull()
    const t = setInterval(pull, 5000)
    return () => clearInterval(t)
  }, [])
  if (err) return <div style={{ color: 'var(--color-danger)' }}>{err}</div>
  if (!d) return <div className="card"><SkeletonRows /></div>

  const cards = [
    { label: '节点', value: d.servers, to: '/nodes', hint: '机器总数' },
    { label: '在线', value: d.online, to: '/nodes', hint: 'Agent 心跳' },
    { label: '线路', value: d.inbounds, to: '/nodes', hint: '已配置线路' },
    { label: '用户', value: d.members ?? d.users, to: '/users', hint: '不含管理员' },
    { label: '套餐', value: d.packages || 0, to: '/packages', hint: '可绑定套餐' },
    { label: '已用流量', value: fmtBytes(d.used_bytes || 0), to: '/traffic', hint: '计费合计' },
    { label: '今日', value: fmtBytes(d.today_bytes || 0), to: '/traffic', hint: '节点原始' },
  ]

  return (
    <div>
      <PageHead title="总览" desc="先加节点，在机器上挂线路，再把套餐绑给用户。已用流量按套餐方向和节点倍率计。" />
      <div className="grid grid-cols-2 lg:grid-cols-4 gap-3 mb-5">
        {cards.map(c => (
          <Link key={c.label} to={c.to} className="card p-4 hover:border-[var(--color-accent)] transition-colors">
            <div className="text-[12px] text-ink-mut">{c.label}</div>
            <div className="text-[22px] font-semibold leading-none mt-2 tabular-nums tracking-tight">{c.value}</div>
            <div className="text-[12px] text-ink-mut mt-2">{c.hint}</div>
          </Link>
        ))}
      </div>
      {(d.alerts || []).length > 0 && (
        <div className="notice mb-4 space-y-1">
          {(d.alerts || []).map((a, i) => <div key={i}>{a}</div>)}
        </div>
      )}
      {(d.days || []).some(x => (x.up || 0) + (x.down || 0) > 0) && (
        <div className="card p-4 mb-4">
          <div className="flex items-center justify-between mb-3">
            <div className="text-[14px] font-semibold">近 14 日</div>
            <Link to="/traffic" className="row-act">明细</Link>
          </div>
          <DayBars days={d.days} />
        </div>
      )}
      <div className="card overflow-hidden">
        <div className="px-4 py-3 flex items-center justify-between border-b" style={{ borderColor: 'var(--color-line-soft)' }}>
          <div className="text-[14px] font-semibold">节点</div>
          <Link to="/nodes" className="row-act">管理</Link>
        </div>
        {(d.server_list || []).length === 0 ? (
          <Empty title="还没有节点" hint="添加一台节点，复制一键安装命令，在机器上以 root 执行。" action={<Link to="/nodes" className="btn-primary">去添加</Link>} />
        ) : (
          <div className="table-wrap">
            <table className="data">
              <thead><tr><th>名称</th><th>地址</th><th>状态</th><th>上行</th><th>下行</th><th>已用</th><th>心跳</th></tr></thead>
              <tbody>
                {(d.server_list || []).map(s => (
                  <tr key={s.id}>
                    <td className="font-medium">{s.name}</td>
                    <td className="font-mono text-[12px] text-ink-soft">{s.public_host || '—'}</td>
                    <td>
                      <span className={`dot ${s.online ? 'dot-on' : 'dot-off'}`} />
                      <span className="ml-2">{s.online ? '在线' : '离线'}</span>
                      {s.last_error ? <Badge tone="danger" className="ml-2">下发失败</Badge> : null}
                      {s.needs_upgrade ? <Badge tone="gold" className="ml-2">可升级</Badge> : null}
                      {s.over_quota ? <Badge tone="danger" className="ml-2">流量已满</Badge> : null}
                    </td>
                    <td className="tabular-nums text-[12px] whitespace-nowrap">{s.online ? fmtBps(s.net_up_bps) : '—'}</td>
                    <td className="tabular-nums text-[12px] whitespace-nowrap">{s.online ? fmtBps(s.net_down_bps) : '—'}</td>
                    <td className="tabular-nums text-[12px] whitespace-nowrap">
                      {fmtBytes((s.used_up || 0) + (s.used_down || 0))}
                      {s.traffic_limit ? ` / ${fmtBytes(s.traffic_limit)}` : ''}
                    </td>
                    <td className="text-[12px] text-ink-mut whitespace-nowrap">{fmtAgo(s.last_seen)}</td>
                  </tr>
                ))}
              </tbody>
            </table>
          </div>
        )}
      </div>
      {d.online === 0 && (d.servers || 0) > 0 && (
        <div className="mt-4 text-[13px] text-ink-mut flex items-center gap-2">
          <Badge tone="gold">提示</Badge>
          节点离线时，请确认 Agent 已安装，且能访问面板的 8899 端口。
        </div>
      )}
    </div>
  )
}
