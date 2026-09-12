import { useEffect, useState } from 'react'
import { Link } from 'react-router-dom'
import { api } from '../lib/api'
import { Badge, DayBars, Empty, PageHead, SkeletonRows, fmtAgo, fmtBps, fmtBytes } from '../components/ui'

function alertItemsOf(d) {
  if (Array.isArray(d.alert_items) && d.alert_items.length) return d.alert_items
  return (d.alerts || []).map(text => ({ text, to: '/nodes', kind: 'warn' }))
}

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
    { label: '服务器', value: d.servers, to: '/nodes', hint: '机器总数' },
    { label: '在线', value: d.online, to: '/nodes', hint: 'Agent 心跳' },
    { label: '节点', value: d.inbounds, to: '/nodes', hint: '入站线路' },
    { label: '用户', value: d.members ?? d.users, to: '/users', hint: '不含管理员' },
    { label: '计费', value: fmtBytes(d.used_bytes || 0), to: '/traffic', hint: '含双向和倍率' },
    { label: '今日', value: fmtBytes(d.today_bytes || 0), to: '/traffic', hint: '节点原始' },
  ]
  const alerts = alertItemsOf(d)
  const steps = [
    { n: 1, t: '添加服务器', to: '/nodes', done: (d.servers || 0) > 0 },
    { n: 2, t: '安装 Agent', hint: '复制命令，在机器上以 root 执行', done: (d.online || 0) > 0 },
    { n: 3, t: '等 Agent 变绿', done: (d.online || 0) > 0 },
    { n: 4, t: '增加节点', to: '/nodes', done: (d.inbounds || 0) > 0 },
    { n: 5, t: '建套餐并勾节点', to: '/packages', done: (d.packages || 0) > 0 },
    { n: 6, t: '开用户，复制订阅', to: '/users', done: (d.members || 0) > 0 },
  ]
  const next = steps.find(s => !s.done)
  const showSetup = (d.servers || 0) === 0

  return (
    <div>
      <PageHead title="总览" desc="先加服务器，在机器上挂节点，再把套餐绑给用户。已用流量按套餐方向和节点倍率计。" />
      <div className="grid grid-cols-2 md:grid-cols-3 gap-3 mb-5">
        {cards.map(c => (
          <Link key={c.label} to={c.to} className="card p-4 hover:border-[var(--color-accent)] transition-colors">
            <div className="text-[12px] text-ink-mut">{c.label}</div>
            <div className="text-[22px] font-semibold leading-none mt-2 tabular-nums tracking-tight">{c.value}</div>
            <div className="text-[12px] text-ink-mut mt-2">{c.hint}</div>
          </Link>
        ))}
      </div>
      {alerts.length > 0 && (
        <div className="space-y-2 mb-4">
          {alerts.map((a, i) => (
            <Link
              key={i}
              to={a.to || '/nodes'}
              className={`notice block ${a.kind === 'danger' ? 'notice-danger' : a.kind === 'warn' ? 'notice-warn' : ''}`}
            >
              {a.text}
            </Link>
          ))}
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
      {showSetup ? (
        <div className="card p-4">
          <div className="text-[14px] font-semibold mb-1">开始使用</div>
          <p className="text-[13px] text-ink-mut mb-2">按顺序做完就能给用户发订阅。</p>
          {steps.map(s => (
            <div key={s.n} className={`setup-step ${s.done ? 'is-done' : (next && next.n === s.n ? 'is-now' : '')}`}>
              <div className="setup-n">{s.done ? '✓' : s.n}</div>
              <div className="min-w-0 flex-1">
                <div className="text-[14px] font-medium">{s.t}</div>
                {s.hint ? <div className="text-[12px] text-ink-mut mt-0.5">{s.hint}</div> : null}
              </div>
              {!s.done && s.to && next && next.n === s.n ? (
                <Link to={s.to} className="btn-primary h-8 shrink-0">去做</Link>
              ) : null}
            </div>
          ))}
        </div>
      ) : (
      <div className="card overflow-hidden">
        <div className="px-4 py-3 flex items-center justify-between border-b" style={{ borderColor: 'var(--color-line-soft)' }}>
          <div className="text-[14px] font-semibold">服务器</div>
          <Link to="/nodes" className="row-act">管理</Link>
        </div>
        {(d.server_list || []).length === 0 ? (
          <Empty title="还没有服务器" hint="添加一台服务器，复制一键安装命令，在机器上以 root 执行。" action={<Link to="/nodes" className="btn-primary">去添加</Link>} />
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
                      {s.needs_reinstall ? <Badge tone="warn" className="ml-2">需重装</Badge>
                        : s.needs_upgrade ? <Badge tone="warn" className="ml-2">可升级</Badge> : null}
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
      )}
      {d.online === 0 && (d.servers || 0) > 0 && (
        <div className="mt-4 text-[13px] text-ink-mut flex items-center gap-2">
          <Badge tone="warn">提示</Badge>
          服务器离线时，请确认 Agent 已安装，且能访问面板的 8899 端口。
        </div>
      )}
    </div>
  )
}
