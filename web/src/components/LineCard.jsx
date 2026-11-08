import { protoShort } from '../lib/forwards'
import { Icon, LineStatus, fmtBytes } from './ui'

export function periodBytes(n) {
  return (Number(n?.period_up) || 0) + (Number(n?.period_down) || 0)
}

export function lifeBytes(n) {
  return (Number(n?.used_up) || 0) + (Number(n?.used_down) || 0)
}

function Metric({ label, value, plain }) {
  return (
    <div className="metric">
      <span className="metric-k">{label}</span>
      <span className={`metric-v${plain ? ' is-plain' : ''}`}>{value}</span>
    </div>
  )
}

export function NodeCard({ n, starBusy, onStar, onCopy, extra }) {
  const online = !!n.server_online
  const addr = n.host ? `${n.host}:${n.port}` : '—'
  return (
    <div className={`machine is-static ${online ? 'is-live' : 'is-off'}`}>
      <div className="machine-head">
        <div className="min-w-0 flex-1">
          <div className="machine-title">
            <span className="machine-name truncate">{n.name}</span>
            {n.line_kind === 'chain' ? <span className="line-tag">中转</span> : null}
            <LineStatus status={online ? '在线' : '离线'} />
          </div>
          <div className="machine-host">
            <span className="machine-host-addr" title={addr}>{addr}</span>
          </div>
        </div>
        {onStar ? (
          <div className="machine-toolbar">
            <button
              type="button"
              className={`icon-btn${n.starred ? ' is-on' : ''}`}
              disabled={starBusy}
              onClick={() => onStar(n)}
              aria-label={n.starred ? '取消常用' : '标为常用'}
              title={n.starred ? '取消常用' : '常用，进订阅'}
            >
              <Icon name={n.starred ? 'star-on' : 'star'} size={14} />
            </button>
          </div>
        ) : null}
      </div>
      <div className="machine-metrics">
        <Metric label="协议" value={protoShort(n.profile) || '—'} plain />
        <Metric label="近 14 日" value={fmtBytes(periodBytes(n))} />
        <Metric label="上行" value={fmtBytes(n.period_up || 0)} />
        <Metric label="下行" value={fmtBytes(n.period_down || 0)} />
      </div>
      <div className="machine-foot">
        <span className="fwd-probe-note" title={`累计 ${fmtBytes(lifeBytes(n))}`}>累计 {fmtBytes(lifeBytes(n))}</span>
        {extra}
        {onCopy ? (
          <button type="button" className="row-act" disabled={!n.uri && !n.conf} onClick={() => onCopy(n)}>复制</button>
        ) : null}
      </div>
    </div>
  )
}
