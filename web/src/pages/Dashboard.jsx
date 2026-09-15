import { useEffect, useState } from 'react'
import { Link } from 'react-router-dom'
import { api } from '../lib/api'
import { peekList, putList } from '../lib/listCache'
import { serverStatus } from '../lib/status'
import { Badge, DayBars, Empty, HourArea, Icon, LineStatus, PageHead, SkeletonRows, fmtAgo, fmtBps, fmtBytes, machineTone } from '../components/ui'

function alertItemsOf(d) {
  if (Array.isArray(d.alert_items) && d.alert_items.length) return d.alert_items
  return (d.alerts || []).map(text => ({ text, to: '/servers', kind: 'warn' }))
}

export default function Dashboard() {
  const [d, setD] = useState(() => peekList('dashboard') ?? null)
  const [err, setErr] = useState('')
  useEffect(() => {
    const pull = () => api.get('/dashboard').then(data => {
      putList('dashboard', data)
      if (Array.isArray(data.server_list)) putList('servers', data.server_list)
      setD(data)
    }).catch(e => setErr(e.message))
    pull()
    const t = setInterval(pull, 5000)
    return () => clearInterval(t)
  }, [])
  if (err) return <div className="alert-row is-fault">{err}</div>
  if (!d) return <div className="card"><SkeletonRows /></div>

  const alerts = alertItemsOf(d)
  const steps = [
    { n: 1, t: '添加实例', to: '/servers', done: (d.servers || 0) > 0 },
    { n: 2, t: '安装 Agent', hint: '复制命令，在机器上以 root 执行', done: (d.online || 0) > 0 },
    { n: 3, t: '等 Agent 在线', done: (d.online || 0) > 0 },
    { n: 4, t: '增加节点', to: '/nodes', done: (d.inbounds || 0) > 0 },
    { n: 5, t: '建套餐并勾节点', to: '/packages', done: (d.packages || 0) > 0 },
    { n: 6, t: '开用户，复制订阅', to: '/users', done: (d.members || 0) > 0 },
  ]
  const next = steps.find(s => !s.done)
  const showSetup = (d.members || 0) === 0
  const hours = d.hours || []
  const hasDayBars = (d.days || []).some(x => (x.up || 0) + (x.down || 0) > 0)
  const showServers = (d.servers || 0) > 0 || !showSetup

  return (
    <div>
      <PageHead title="总览" />
      <div className="dash-stats">
        <Link to="/traffic" className="dash-stat">
          <div className="dash-stat-k">计费</div>
          <div className="dash-stat-v">{fmtBytes(d.used_bytes || 0)}</div>
          <div className="dash-stat-h">用户已用</div>
        </Link>
        <Link to="/traffic" className="dash-stat">
          <div className="dash-stat-k">今日</div>
          <div className="dash-stat-v">{fmtBytes(d.today_bytes || 0)}</div>
          <div className="dash-stat-h">当天合计</div>
        </Link>
        <Link to="/traffic" className="dash-stat">
          <div className="dash-stat-k">本月</div>
          <div className="dash-stat-v">{fmtBytes(d.month_bytes || 0)}</div>
          <div className="dash-stat-h">1 日起算</div>
        </Link>
        <Link to="/users" className="dash-stat">
          <div className="dash-stat-k">用户</div>
          <div className="dash-stat-v">{d.members ?? d.users ?? 0}</div>
          <div className="dash-stat-h">不含管理员</div>
        </Link>
        <div className="dash-stat is-live">
          <div className="dash-stat-k">实时</div>
          <div className="dash-stat-live">
            <span>↑ {fmtBps(d.nic_up_bps || 0)}</span>
            <span>↓ {fmtBps(d.nic_down_bps || 0)}</span>
          </div>
          <div className="dash-stat-h">全部用户节点</div>
        </div>
      </div>
      {alerts.length > 0 && (
        <div className="mb-5">
          <div className="text-[14px] font-semibold mb-1">报警</div>
          <div className="flex flex-col gap-1.5">
            {alerts.map((a, i) => (
              <Link
                key={`${a.to || ''}-${a.text}-${i}`}
                to={a.to || '/servers'}
                className={`alert-row ${a.kind === 'danger' ? 'is-fault' : 'is-warn'}`}
              >
                {a.text}
              </Link>
            ))}
          </div>
        </div>
      )}
      {hours.length > 0 && (
        <div className="mt-5">
          <HourArea hours={hours} />
        </div>
      )}
      {hasDayBars && (
        <div className="mt-5">
          <div className="flex items-center justify-between mb-1">
            <div className="text-[14px] font-semibold">近 30 日</div>
            <Link to="/traffic" className="btn-ghost h-8">明细</Link>
          </div>
          <DayBars days={d.days} />
        </div>
      )}
      {showSetup ? (
        <div className="setup-list">
          <div className="text-[14px] font-semibold mb-1">开始使用</div>
          <p className="text-[13px] text-ink-mut mb-2">按顺序做完就能给用户发订阅。</p>
          {steps.map(s => (
            <div key={s.n} className={`setup-step ${s.done ? 'is-done' : (next && next.n === s.n ? 'is-now' : '')}`}>
              <div className="setup-n" aria-hidden="true">{s.done ? <Icon name="check" size={12} /> : s.n}</div>
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
      ) : null}
      {showServers ? (
      <div className="card overflow-hidden mt-5">
        <div className="panel-head">
          <div>实例</div>
          <Link to="/servers" className="btn-ghost h-8">管理</Link>
        </div>
        {(d.server_list || []).length === 0 ? (
          <Empty title="还没有实例" hint="添加一台实例，复制一键安装命令，在机器上以 root 执行。" action={<Link to="/servers" className="btn-primary">去添加</Link>} />
        ) : (
          <div className="table-wrap">
            <table className="data">
              <thead><tr><th>名称</th><th>地址</th><th>状态</th><th>上行</th><th>下行</th><th>已用</th><th>心跳</th></tr></thead>
              <tbody>
                {(d.server_list || []).map(s => (
                  <tr key={s.id} className={machineTone(s)}>
                    <td className="font-medium">{s.name}</td>
                    <td className="copy-text">{s.public_host || '—'}</td>
                    <td>
                      <LineStatus status={serverStatus(s)} />
                      {s.last_error ? <Badge tone="danger" className="ml-2">下发失败</Badge> : null}
                      {s.needs_reinstall ? <Badge tone="warn" className="ml-2">需重装</Badge>
                        : s.needs_upgrade ? <Badge tone="warn" className="ml-2">可升级</Badge> : null}
                      {s.over_quota ? <Badge tone="danger" className="ml-2">流量已满</Badge> : null}
                    </td>
                    <td className="tabular-nums text-[12px] whitespace-nowrap font-mono">{s.online ? fmtBps(s.net_up_bps) : '—'}</td>
                    <td className="tabular-nums text-[12px] whitespace-nowrap font-mono">{s.online ? fmtBps(s.net_down_bps) : '—'}</td>
                    <td className="tabular-nums text-[12px] whitespace-nowrap font-mono">
                      {fmtBytes((s.used_up || 0) + (s.used_down || 0))}
                      {s.traffic_limit ? ` / ${fmtBytes(s.traffic_limit)}` : ''}
                    </td>
                    <td className="text-[12px] text-ink-mut whitespace-nowrap font-mono">{fmtAgo(s.last_seen)}</td>
                  </tr>
                ))}
              </tbody>
            </table>
          </div>
        )}
      </div>
      ) : null}
      {d.online === 0 && (d.servers || 0) > 0 && (
        <div className="alert-row is-warn mt-4">
          实例离线时，请确认 Agent 已安装，且能访问面板的 8899 端口。
        </div>
      )}
    </div>
  )
}
