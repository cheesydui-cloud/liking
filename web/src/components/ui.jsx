import { useEffect, useId, useRef, useState } from 'react'
import { createPortal } from 'react-dom'

export function Icon({ name, size = 18, className = '' }) {
  const s = size
  const common = {
    width: s, height: s, viewBox: '0 0 24 24', fill: 'none',
    stroke: 'currentColor', strokeWidth: 1.7, strokeLinecap: 'round', strokeLinejoin: 'round',
    className, 'aria-hidden': true,
  }
  const p = {
    layout: <><rect x="3" y="3" width="7" height="9" rx="1.4" /><rect x="14" y="3" width="7" height="5" rx="1.4" /><rect x="14" y="12" width="7" height="9" rx="1.4" /><rect x="3" y="16" width="7" height="5" rx="1.4" /></>,
    servers: <><rect x="3" y="4" width="18" height="6" rx="1.4" /><rect x="3" y="14" width="18" height="6" rx="1.4" /><circle cx="7" cy="7" r="0.9" fill="currentColor" stroke="none" /><circle cx="7" cy="17" r="0.9" fill="currentColor" stroke="none" /></>,
    plugs: <><path d="M7 8v4a5 5 0 0 0 10 0V8" /><path d="M9 4v4M15 4v4M12 17v3" /></>,
    users: <><circle cx="9" cy="8" r="3" /><path d="M3.5 19a5.5 5.5 0 0 1 11 0" /><circle cx="17" cy="9" r="2.2" /><path d="M16.2 19a4.4 4.4 0 0 1 4.8-3.6" /></>,
    package: <><path d="M12 3 20 7.5v9L12 21 4 16.5v-9L12 3z" /><path d="M12 12 20 7.5M12 12v9M12 12 4 7.5" /></>,
    cert: <><rect x="5" y="3" width="14" height="18" rx="2" /><path d="M8 8h8M8 12h8M8 16h4" /></>,
    gear: <><circle cx="12" cy="12" r="3" /><path d="M12 3.5v2.2M12 18.3v2.2M4.9 6.5l1.6 1.6M17.5 16l1.6 1.6M3.5 12h2.2M18.3 12h2.2M4.9 17.5l1.6-1.6M17.5 8l1.6-1.6" /></>,
    key: <><circle cx="8" cy="14" r="3.2" /><path d="M11 14h9l-2 2.2 2 1.8" /></>,
    logout: <><path d="M10 5H6.5A1.5 1.5 0 0 0 5 6.5v11A1.5 1.5 0 0 0 6.5 19H10" /><path d="M14 8l5 4-5 4M9 12h10" /></>,
    sun: <><circle cx="12" cy="12" r="4" /><path d="M12 2.8v2M12 19.2v2M4.2 4.2l1.4 1.4M18.4 18.4l1.4 1.4M2.8 12h2M19.2 12h2M4.2 19.8l1.4-1.4M18.4 5.6l1.4-1.4" /></>,
    moon: <><path d="M18 14.5A7 7 0 1 1 11 5a6 6 0 0 0 7 9.5z" /></>,
    menu: <><path d="M4 7h16M4 12h16M4 17h16" /></>,
    close: <><path d="M6 6l12 12M18 6 6 18" /></>,
    copy: <><rect x="8" y="8" width="11" height="11" rx="1.6" /><path d="M5.5 15V5.8A1.8 1.8 0 0 1 7.3 4H15" /></>,
    plus: <><path d="M12 5v14M5 12h14" /></>,
    trash: <><path d="M5 7h14M9 7V5h6v2M8 7l.8 12h6.4L16 7" /></>,
    check: <><path d="M5 12.5 9.5 17 19 7.5" /></>,
    eye: <><path d="M2.8 12S6.4 6.5 12 6.5 21.2 12 21.2 12 17.6 17.5 12 17.5 2.8 12 2.8 12z" /><circle cx="12" cy="12" r="2.4" /></>,
    'eye-off': <><path d="M4 5l16 14M9.9 9.9A3 3 0 0 0 12 15a3 3 0 0 0 2.9-2.2M6.1 7.4C4 9 2.8 12 2.8 12S6.4 17.5 12 17.5c1.4 0 2.7-.3 3.8-.8M17.6 14.7C20 13 21.2 12 21.2 12S17.6 6.5 12 6.5c-.6 0-1.1 0-1.7.1" /></>,
    wifi: <><path d="M5 12.5a10 10 0 0 1 14 0M8.2 15.4a5.5 5.5 0 0 1 7.6 0" /><circle cx="12" cy="18.2" r="1" fill="currentColor" stroke="none" /></>,
    spark: <><path d="M12 3.5 13.6 9 19 10.5 13.6 12 12 17.5 10.4 12 5 10.5 10.4 9z" /></>,
    warning: <><path d="M12 4 21 19H3L12 4z" /><path d="M12 10v4M12 16.5v.5" /></>,
    search: <><circle cx="11" cy="11" r="6.2" /><path d="M16 16.5 20.5 21" /></>,
    calendar: <><rect x="4" y="5" width="16" height="15" rx="2" /><path d="M8 3.5V7M16 3.5V7M4 10h16" /></>,
    link: <><path d="M9 12a4 4 0 0 0 6 0l2-2a4 4 0 0 0-6-6l-1 1" /><path d="M15 12a4 4 0 0 0-6 0l-2 2a4 4 0 1 0 6 6l1-1" /></>,
    download: <><path d="M12 4v11" /><path d="M7 11l5 5 5-5" /><path d="M5 20h14" /></>,
    upload: <><path d="M12 20V9" /><path d="M7 13l5-5 5 5" /><path d="M5 4h14" /></>,
    bars: <><path d="M4 19V10M10 19V5M16 19v-7M22 19H2" /></>,
    more: <><circle cx="12" cy="5" r="1.15" fill="currentColor" stroke="none" /><circle cx="12" cy="12" r="1.15" fill="currentColor" stroke="none" /><circle cx="12" cy="19" r="1.15" fill="currentColor" stroke="none" /></>,
    pencil: <><path d="M4 20h4L19.2 8.8l-4-4L4 16v4z" /><path d="M13.2 6.8l4 4" /></>,
    forward: <><path d="M4 7h11" /><path d="M12 4l3 3-3 3" /><path d="M20 17H9" /><path d="M12 14l-3 3 3 3" /></>,
    star: <><path d="M12 3.8 14.2 9l5.8.6-4.4 3.9 1.3 5.7L12 16.6 6.9 19.2 8.2 13.5 3.8 9.6 9.6 9z" /></>,
    'star-on': <><path d="M12 3.8 14.2 9l5.8.6-4.4 3.9 1.3 5.7L12 16.6 6.9 19.2 8.2 13.5 3.8 9.6 9.6 9z" fill="currentColor" /></>,
  }
  return <svg {...common}>{p[name] || p.spark}</svg>
}

export function BrandMark({ size = 28, className = '' }) {
  const w = Math.max(2, Math.round(size * 0.11))
  return (
    <span className={`brand-mark ${className}`} style={{ width: size, height: size }} aria-hidden>
      <span style={{ width: w, height: '58%' }} />
      <span style={{ width: w, height: '100%' }} />
    </span>
  )
}

export function machineTone(s) {
  if (!s) return 'is-off'
  if (s.over_quota) return 'is-fault'
  if (s.online) return 'is-live'
  if (s.last_error) return 'is-fault'
  return 'is-off'
}

export function StatusWord({ online, fault }) {
  if (online) return <span className="status-word is-live">在线</span>
  if (fault) return <span className="status-word is-fault">故障</span>
  return <span className="status-word is-off">离线</span>
}

export function LineStatus({ status }) {
  if (!status) return null
  const cls = {
    '正常': 'is-live',
    '在线': 'is-live',
    '故障': 'is-fault',
    '未安装': 'is-warn',
    '停用': 'is-mute',
    '离线': 'is-off',
    '流量已满': 'is-fault',
  }[status] || 'is-off'
  return <span className={`status-word ${cls}`}>{status}</span>
}

export function FilterTabs({ value, onChange, items }) {
  return (
    <div className="filter-tabs" role="tablist">
      {items.map(it => {
        const id = Array.isArray(it) ? it[0] : it.id
        const lab = Array.isArray(it) ? it[1] : it.label
        const on = value === id
        return (
          <button
            key={id || 'all'}
            type="button"
            role="tab"
            aria-selected={on}
            className={`filter-tab${on ? ' is-on' : ''}`}
            onClick={() => onChange(id)}
          >
            {lab}
          </button>
        )
      })}
    </div>
  )
}

export function fmtBytes(n) {
  const x = Number(n)
  if (!Number.isFinite(x) || x <= 0) return '0 B'
  const u = ['B', 'KiB', 'MiB', 'GiB', 'TiB', 'PiB']
  let i = 0, v = x
  while (v >= 1024 && i < u.length - 1) { v /= 1024; i++ }
  return (i ? v.toFixed(1) : String(Math.round(v))) + ' ' + u[i]
}

export function fmtBps(n) {
  const x = Math.max(0, Number(n) || 0)
  const u = ['B/s', 'KiB/s', 'MiB/s', 'GiB/s']
  let i = 0, v = x
  while (v >= 1024 && i < u.length - 1) { v /= 1024; i++ }
  const num = i === 0 ? String(Math.round(v)) : (v >= 10 ? v.toFixed(1) : v.toFixed(2))
  return `${num} ${u[i]}`
}

export function fmtDate(ts) {
  if (!ts) return '—'
  try { return new Date(ts * 1000).toLocaleString() } catch { return '—' }
}

export function fmtDateShort(ts) {
  if (!ts) return '—'
  try { return new Date(ts * 1000).toLocaleDateString() } catch { return '—' }
}

export function fmtAgo(ts) {
  if (!ts) return '从未'
  const s = Math.max(0, Math.floor(Date.now() / 1000 - Number(ts)))
  if (s < 45) return '刚刚'
  if (s < 3600) return `${Math.floor(s / 60)} 分钟前`
  if (s < 86400) return `${Math.floor(s / 3600)} 小时前`
  if (s < 86400 * 10) return `${Math.floor(s / 86400)} 天前`
  return fmtDateShort(ts)
}

export function billedBytes(u) {
  if (!u) return 0
  if (u.billed_bytes != null && u.billed_bytes !== '') return Number(u.billed_bytes) || 0
  const up = Number(u.used_up) || 0
  const down = Number(u.used_down) || 0
  return u.direction === 'oneway' ? down : up + down
}

export function remainingBytes(u) {
  const cap = Number(u?.traffic_cap) || 0
  if (cap <= 0) return null
  return Math.max(0, cap - billedBytes(u))
}

function hourLabel(hour) {
  const s = String(hour || '')
  const hh = s.length >= 13 ? s.slice(11, 13) : s.slice(-2)
  return hh
}

function hourTick(hour) {
  return `${hourLabel(hour)}:00`
}

function hourTitle(hour) {
  const s = String(hour || '')
  if (s.length >= 13) return `${s.slice(5, 10)} ${s.slice(11, 13)}:00`
  return s
}

function catmullRomPath(pts, minY, maxY) {
  if (!pts.length) return ''
  if (pts.length === 1) return `M ${pts[0].x} ${pts[0].y}`
  const clampY = (y) => {
    if (minY == null || maxY == null) return y
    return Math.min(maxY, Math.max(minY, y))
  }
  let d = `M ${pts[0].x} ${pts[0].y}`
  for (let i = 0; i < pts.length - 1; i++) {
    const p0 = pts[Math.max(0, i - 1)]
    const p1 = pts[i]
    const p2 = pts[i + 1]
    const p3 = pts[Math.min(pts.length - 1, i + 2)]
    const c1x = p1.x + (p2.x - p0.x) / 6
    const c1y = clampY(p1.y + (p2.y - p0.y) / 6)
    const c2x = p2.x - (p3.x - p1.x) / 6
    const c2y = clampY(p2.y - (p3.y - p1.y) / 6)
    d += ` C ${c1x} ${c1y}, ${c2x} ${c2y}, ${p2.x} ${p2.y}`
  }
  return d
}

export function HourArea({ hours = [], className = '' }) {
  const rows = Array.isArray(hours) ? hours : []
  const wrapRef = useRef(null)
  const [w, setW] = useState(0)
  const [hi, setHi] = useState(-1)
  const gid = useId().replace(/:/g, '')
  useEffect(() => {
    const el = wrapRef.current
    if (!el) return
    const ro = new ResizeObserver(() => setW(el.clientWidth))
    ro.observe(el)
    setW(el.clientWidth)
    return () => ro.disconnect()
  }, [])
  if (!rows.length) return null
  const values = rows.map(d => (Number(d.up) || 0) + (Number(d.down) || 0))
  const max = Math.max(0, ...values)
  const W = Math.max(Math.floor(w), 1)
  const compact = W < 480
  const padL = compact ? 48 : 58
  const padR = 16
  const padT = 10
  const padB = 28
  const H = compact ? 220 : 280
  const plotW = Math.max(W - padL - padR, 1)
  const plotH = H - padT - padB
  const n = rows.length
  const pts = values.map((v, i) => ({
    x: padL + (n <= 1 ? plotW / 2 : (i / (n - 1)) * plotW),
    y: padT + (max <= 0 ? plotH : (1 - v / max) * plotH),
    v,
    row: rows[i],
  }))
  const line = catmullRomPath(pts, padT, padT + plotH)
  const last = pts[pts.length - 1]
  const area = line ? `${line} L ${last.x} ${padT + plotH} L ${pts[0].x} ${padT + plotH} Z` : ''
  const yTicks = max <= 0
    ? [{ p: 0, y: padT + plotH, label: '0' }]
    : [1, 0.75, 0.5, 0.25, 0].map(p => ({
      p,
      y: padT + (1 - p) * plotH,
      label: p === 0 ? '0' : fmtBytes(max * p),
    }))
  const onMove = (e) => {
    const rect = e.currentTarget.getBoundingClientRect()
    const x = e.clientX - rect.left
    let best = 0
    let bestD = Infinity
    for (let i = 0; i < pts.length; i++) {
      const d = Math.abs(pts[i].x - x)
      if (d < bestD) { bestD = d; best = i }
    }
    setHi(h => (h === best ? h : best))
  }
  const hover = hi >= 0 ? pts[hi] : null
  const hideX = (i) => {
    if (compact) return i % 4 !== 0 && i !== n - 1
    if (W < 880) return i % 2 === 1
    return false
  }

  return (
    <div className={`traffic-area-card ${className}`}>
      <div className="traffic-area-head">
        <i className="traffic-area-dot" aria-hidden="true" />
        24小时流量统计
      </div>
      <div
        className="traffic-area-body"
        ref={wrapRef}
        onPointerDown={onMove}
        onPointerMove={onMove}
        onPointerLeave={() => setHi(-1)}
      >
        {W > 1 ? (
          <svg width={W} height={H} className="traffic-area-svg" role="img" aria-label="24小时流量统计">
            <defs>
              <linearGradient id={`ta-${gid}`} x1="0" y1="0" x2="0" y2="1">
                <stop offset="0%" stopColor="var(--color-accent-fill, var(--color-accent))" stopOpacity="0.38" />
                <stop offset="100%" stopColor="var(--color-accent)" stopOpacity="0.02" />
              </linearGradient>
              <clipPath id={`tc-${gid}`}>
                <rect x={padL} y={padT} width={plotW} height={plotH} />
              </clipPath>
            </defs>
            {yTicks.map((t, i) => (
              <g key={i}>
                {t.p > 0 ? (
                  <line x1={padL} x2={W - padR} y1={t.y} y2={t.y} className="traffic-area-grid" />
                ) : (
                  <line x1={padL} x2={W - padR} y1={t.y} y2={t.y} className="traffic-area-base" />
                )}
                <text x={padL - 8} y={t.y + 3.5} textAnchor="end" className="traffic-area-ylab">{t.label}</text>
              </g>
            ))}
            <g clipPath={`url(#tc-${gid})`}>
              {area ? <path d={area} fill={`url(#ta-${gid})`} /> : null}
              {line ? <path d={line} fill="none" stroke="var(--color-accent)" strokeWidth="2.4" strokeLinejoin="round" strokeLinecap="round" /> : null}
            </g>
            {pts.map((p, i) => (
              <text
                key={p.row.hour || i}
                x={p.x}
                y={H - 8}
                textAnchor="middle"
                className={`traffic-area-xlab${hideX(i) ? ' is-hide' : ''}`}
              >
                {hourTick(p.row.hour)}
              </text>
            ))}
            {hover ? (
              <>
                <line x1={hover.x} x2={hover.x} y1={padT} y2={padT + plotH} className="traffic-area-cursor" />
                <circle cx={hover.x} cy={Math.min(Math.max(hover.y, padT), padT + plotH)} r="4.5" fill="var(--color-surface)" stroke="var(--color-accent)" strokeWidth="2" />
              </>
            ) : null}
          </svg>
        ) : <div style={{ height: H }} />}
        {hover ? (
          <div
            className="traffic-area-tip"
            style={{ left: Math.min(Math.max(hover.x, 72), W - 72), top: Math.max(Math.min(hover.y, padT + plotH) - 8, 28) }}
          >
            <div>时间: {hourTick(hover.row.hour)}</div>
            <div className="is-val">流量: {fmtBytes(hover.v)}</div>
          </div>
        ) : null}
      </div>
    </div>
  )
}

export function DayBars({ days = [], className = '', legend = true, label = '' }) {
  const rows = Array.isArray(days) ? days : []
  const max = Math.max(1, ...rows.map(d => (Number(d.up) || 0) + (Number(d.down) || 0)))
  if (!rows.length) return null
  const isHour = rows.some(d => d.hour)
  const dense = rows.length > 16
  const aria = label || (isHour ? '近 24 小时流量' : '每日流量')
  return (
    <div className={className}>
      {legend ? (
        <div className="traffic-legend">
          <span><i className="is-up" aria-hidden="true" />上行</span>
          <span><i className="is-down" aria-hidden="true" />下行</span>
        </div>
      ) : null}
      <div className={`traffic-bars ${isHour ? 'is-hours' : 'is-days'} ${dense ? 'is-dense' : ''}`} role="img" aria-label={aria}>
        {rows.map((d, i) => {
          const up = Number(d.up) || 0
          const down = Number(d.down) || 0
          const tot = up + down
          const pct = Math.max(tot ? 6 : 2, Math.round((tot / max) * 100))
          const key = d.hour || d.day || i
          const text = d.hour ? hourLabel(d.hour) : String(d.day || '').slice(5)
          const title = d.hour ? hourTitle(d.hour) : d.day
          return (
            <div key={key} className="traffic-bar" title={`${title}  ↑${fmtBytes(up)}  ↓${fmtBytes(down)}`}>
              <div className="traffic-bar-track">
                <div className="traffic-bar-stack" style={{ height: `${pct}%` }}>
                  {tot === 0 ? (
                    <div className="traffic-bar-fill is-empty" />
                  ) : (
                    <>
                      {up > 0 ? <div className="traffic-bar-fill is-up" style={{ flex: Math.max(1, Math.round((up / tot) * 1000)) }} /> : null}
                      {down > 0 ? <div className="traffic-bar-fill is-down" style={{ flex: Math.max(1, Math.round((down / tot) * 1000)) }} /> : null}
                    </>
                  )}
                </div>
              </div>
              <div className="traffic-bar-label">{text}</div>
            </div>
          )
        })}
      </div>
    </div>
  )
}

export function Meter({ value = 0, max = 0, className = '', mark = 80 }) {
  const used = Number(value) || 0
  const cap = Number(max) || 0
  const unlimited = !cap
  const pct = unlimited ? 0 : Math.min(100, Math.round((used / cap) * 100))
  const tone = unlimited ? 'ok' : pct >= 100 ? 'danger' : pct >= mark ? 'warn' : 'ok'
  const color = { ok: 'var(--color-ok)', warn: 'var(--color-warn)', danger: 'var(--color-danger)' }[tone]
  return (
    <div className={className}>
      <div className="flex items-baseline justify-between gap-2 text-[12px] tabular-nums">
        <span>{fmtBytes(used)}{unlimited ? '' : ` / ${fmtBytes(cap)}`}</span>
        <span className="text-ink-mut">{unlimited ? '不限' : `${pct}%`}</span>
      </div>
      <div className="meter mt-1.5">
        <div className="meter-bar" style={{ width: unlimited ? '8%' : `${Math.max(pct, used ? 3 : 0)}%`, background: color, opacity: unlimited ? 0.35 : 1 }} />
        {!unlimited && mark > 0 && mark < 100 ? <div className="meter-mark" style={{ left: `${mark}%` }} title={`${mark}%`} /> : null}
      </div>
    </div>
  )
}

export function PageHead({ title, desc, actions }) {
  if (!desc && !actions) {
    return (
      <div className="mb-4 max-lg:hidden">
        <h1 className="page-title">{title}</h1>
      </div>
    )
  }
  return (
    <div className="mb-4">
      <div className="flex flex-col gap-3 sm:flex-row sm:items-start sm:justify-between">
        <div className="min-w-0">
          <h1 className="page-title">{title}</h1>
          {desc && <p className="page-desc max-lg:mt-0 mt-1.5 max-w-2xl leading-relaxed">{desc}</p>}
        </div>
        {actions && <div className="hidden sm:flex flex-wrap items-center gap-2 shrink-0">{actions}</div>}
      </div>
      {actions ? <div className="page-cta-bar">{actions}</div> : null}
    </div>
  )
}

export function Tabs({ value, onChange, items }) {
  return (
    <div className="lk-tabs" role="tablist">
      {items.map(it => (
        <button
          key={it.id}
          type="button"
          role="tab"
          aria-selected={value === it.id}
          className={`lk-tab${value === it.id ? ' is-active' : ''}`}
          onClick={() => onChange(it.id)}
        >
          {it.label}
        </button>
      ))}
    </div>
  )
}

export function Empty({ title, hint, action }) {
  return (
    <div className="empty-state">
      <div className="text-[14px] font-medium">{title}</div>
      {hint && <p className="text-[13px] text-ink-mut mt-1 max-w-xl">{hint}</p>}
      {action && <div className="mt-3">{action}</div>}
    </div>
  )
}

export function Field({ label, hint, children }) {
  return (
    <label className="block">
      <span className="block text-[12px] font-medium text-ink-soft mb-1.5">{label}</span>
      {children}
      {hint && <span className="block text-[11.5px] text-ink-mut mt-1">{hint}</span>}
    </label>
  )
}

export function Badge({ tone = 'muted', children, className = '' }) {
  return (
    <span className={`badge badge-${tone} ${className}`}>{children}</span>
  )
}

const modalStack = []

export function Modal({ open, title, onClose, children, footer, wide, size }) {
  const titleId = useId()
  useEffect(() => {
    if (!open) return
    const id = {}
    modalStack.push(id)
    const onKey = (e) => {
      if (e.key !== 'Escape') return
      if (modalStack[modalStack.length - 1] !== id) return
      e.preventDefault()
      onClose?.()
    }
    document.addEventListener('keydown', onKey)
    const prev = document.body.style.overflow
    document.body.style.overflow = 'hidden'
    return () => {
      const i = modalStack.lastIndexOf(id)
      if (i >= 0) modalStack.splice(i, 1)
      document.removeEventListener('keydown', onKey)
      document.body.style.overflow = prev
    }
  }, [open, onClose])

  if (!open) return null
  const max = size === 'xl' ? 'max-w-3xl' : (size === 'lg' || wide) ? 'max-w-2xl' : 'max-w-md'
  return (
    <div className="fixed inset-0 z-[80] flex items-end sm:items-center justify-center p-0 sm:p-6">
      <button type="button" className="absolute inset-0 modal-scrim" aria-label="关闭" onClick={onClose} />
      <div
        role="dialog"
        aria-modal="true"
        aria-labelledby={titleId}
        className={`modal-panel card w-full ${max}`}
      >
        <div className="modal-head">
          <h2 id={titleId} className="modal-title">{title}</h2>
          <button type="button" className="icon-btn" onClick={onClose} aria-label="关闭">
            <Icon name="close" size={15} />
          </button>
        </div>
        <div className="modal-body">{children}</div>
        {footer ? <div className="modal-foot">{footer}</div> : null}
      </div>
    </div>
  )
}

export function SkeletonRows({ rows = 4 }) {
  return (
    <div className="p-4 space-y-3">
      {Array.from({ length: rows }).map((_, i) => <div key={i} className="skeleton h-9" />)}
    </div>
  )
}

export function MoreMenu({ label = '更多', items = [], disabled, iconOnly }) {
  const [open, setOpen] = useState(false)
  const [pos, setPos] = useState({ top: 0, left: 0 })
  const btnRef = useRef(null)
  const popRef = useRef(null)

  const place = () => {
    const r = btnRef.current?.getBoundingClientRect()
    if (!r) return
    const width = 200
    const margin = 8
    let left = r.right - width
    if (left < margin) left = margin
    if (left + width > window.innerWidth - margin) left = Math.max(margin, window.innerWidth - width - margin)
    const spaceBelow = window.innerHeight - r.bottom
    const openUp = spaceBelow < 240 && r.top > spaceBelow
    setPos(openUp
      ? { top: undefined, bottom: window.innerHeight - r.top + 4, left }
      : { top: r.bottom + 4, bottom: undefined, left })
  }

  useEffect(() => {
    if (!open) return
    place()
    const onDoc = (e) => {
      if (btnRef.current?.contains(e.target) || popRef.current?.contains(e.target)) return
      setOpen(false)
    }
    const onKey = (e) => { if (e.key === 'Escape') setOpen(false) }
    const onClose = () => setOpen(false)
    document.addEventListener('pointerdown', onDoc)
    document.addEventListener('keydown', onKey)
    window.addEventListener('scroll', onClose, true)
    window.addEventListener('resize', onClose)
    return () => {
      document.removeEventListener('pointerdown', onDoc)
      document.removeEventListener('keydown', onKey)
      window.removeEventListener('scroll', onClose, true)
      window.removeEventListener('resize', onClose)
    }
  }, [open])

  const vis = (items || []).filter(Boolean)
  if (!vis.length) return null
  return (
    <div className="more-menu">
      <button
        ref={btnRef}
        type="button"
        className={iconOnly ? 'icon-btn' : 'row-act'}
        disabled={disabled}
        aria-label={label}
        aria-haspopup="menu"
        aria-expanded={open}
        onClick={() => setOpen(v => !v)}
      >
        {iconOnly ? <Icon name="more" size={16} /> : <>{label}<Icon name="more" size={14} /></>}
      </button>
      {open ? createPortal(
        <div
          ref={popRef}
          className="more-menu-pop"
          role="menu"
          style={{ top: pos.top, bottom: pos.bottom, left: pos.left }}
        >
          {vis.map((it, i) => (
            it.sep ? (
              <div key={`sep-${i}`} className="more-menu-sep" />
            ) : (
              <button
                key={it.label || i}
                type="button"
                role="menuitem"
                className={`more-menu-item${it.danger ? ' is-danger' : ''}`}
                disabled={it.disabled}
                onClick={() => { setOpen(false); if (!it.disabled) it.onSelect?.() }}
              >
                <span>{it.label}</span>
                {it.hint ? <span className="more-menu-hint">{it.hint}</span> : null}
              </button>
            )
          ))}
        </div>,
        document.body,
      ) : null}
    </div>
  )
}

export function SearchInput({ value, onChange, placeholder = '搜索…' }) {
  return (
    <div className="search-field">
      <span className="search-field-icon" aria-hidden="true"><Icon name="search" size={14} /></span>
      <input
        className="input-field"
        type="search"
        name="q"
        autoComplete="off"
        spellCheck={false}
        aria-label={placeholder.replace(/…$/, '') || '搜索'}
        placeholder={placeholder.endsWith('…') ? placeholder : `${placeholder}…`}
        value={value}
        onChange={onChange}
      />
    </div>
  )
}
