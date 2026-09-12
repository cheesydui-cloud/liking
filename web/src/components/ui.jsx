import { useEffect, useRef, useState } from 'react'
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
  }
  return <svg {...common}>{p[name] || p.spark}</svg>
}

export function BrandMark({ size = 28, className = '' }) {
  return (
    <span className={`brand-mark ${className}`} style={{ width: size, height: size, fontSize: Math.round(size * 0.46) }} aria-hidden>
      L
    </span>
  )
}

export function fmtBytes(n) {
  if (!n) return '0 B'
  const u = ['B', 'KiB', 'MiB', 'GiB', 'TiB']
  let i = 0, x = Number(n)
  while (x >= 1024 && i < u.length - 1) { x /= 1024; i++ }
  return (i ? x.toFixed(1) : String(Math.round(x))) + ' ' + u[i]
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
  if (u.billed_bytes != null) return Number(u.billed_bytes) || 0
  return (Number(u.used_up) || 0) + (Number(u.used_down) || 0)
}

export function DayBars({ days = [], className = '' }) {
  const rows = Array.isArray(days) ? days : []
  const max = Math.max(1, ...rows.map(d => (Number(d.up) || 0) + (Number(d.down) || 0)))
  if (!rows.length) return null
  return (
    <div className={`traffic-bars ${className}`}>
      {rows.map(d => {
        const tot = (Number(d.up) || 0) + (Number(d.down) || 0)
        const pct = Math.max(tot ? 6 : 2, Math.round((tot / max) * 100))
        const label = String(d.day || '').slice(5)
        return (
          <div key={d.day} className="traffic-bar" title={`${d.day}  ↑${fmtBytes(d.up || 0)}  ↓${fmtBytes(d.down || 0)}`}>
            <div className="traffic-bar-track">
              <div className="traffic-bar-fill" style={{ height: `${pct}%` }} />
            </div>
            <div className="traffic-bar-label">{label}</div>
          </div>
        )
      })}
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
  return (
    <div className="mb-4">
      <div className="flex flex-col gap-3 sm:flex-row sm:items-start sm:justify-between">
        <div className="min-w-0">
          <h1 className="text-[20px] font-semibold tracking-tight leading-tight">{title}</h1>
          {desc && <p className="text-[13px] text-ink-mut mt-1 max-w-2xl leading-relaxed">{desc}</p>}
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
    <div className="py-12 px-6 text-center">
      <div className="mx-auto w-10 h-10 rounded-lg grid place-items-center mb-3 bg-raised text-ink-mut">
        <Icon name="spark" size={16} />
      </div>
      <div className="text-[15px] font-medium">{title}</div>
      {hint && <p className="text-[13px] text-ink-mut mt-1.5 max-w-md mx-auto">{hint}</p>}
      {action && <div className="mt-4">{action}</div>}
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
  const map = {
    gold: { color: 'var(--color-accent)', bg: 'var(--color-accent-soft)' },
    warn: { color: 'var(--color-warn)', bg: 'var(--color-warn-soft)' },
    ok: { color: 'var(--color-ok)', bg: 'var(--color-ok-soft)' },
    danger: { color: 'var(--color-danger)', bg: 'var(--color-danger-soft)' },
    muted: { color: 'var(--color-ink-mut)', bg: 'var(--color-raised)' },
  }
  const t = map[tone] || map.muted
  return (
    <span className={`inline-flex items-center gap-1 px-1.5 py-0.5 rounded-md text-[11px] font-medium ${className}`} style={{ color: t.color, background: t.bg }}>
      {children}
    </span>
  )
}

const modalStack = []

export function Modal({ open, title, onClose, children, footer, wide, size }) {
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
      <button type="button" className="absolute inset-0 bg-black/50" aria-label="关闭" onClick={onClose} />
      <div role="dialog" aria-modal="true" className={`relative card w-full ${max} p-5 m-0 sm:m-auto rounded-t-xl sm:rounded-xl max-h-[92dvh] overflow-y-auto`}>
        <div className="flex items-start justify-between gap-3 mb-4">
          <h2 className="text-[16px] font-semibold leading-tight">{title}</h2>
          <button type="button" className="btn-ghost h-8 w-8 px-0" onClick={onClose} aria-label="关闭"><Icon name="close" size={15} /></button>
        </div>
        <div>{children}</div>
        {footer && <div className="mt-5 flex justify-end gap-2">{footer}</div>}
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

export function MoreMenu({ label = '更多', items = [], disabled }) {
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
    document.addEventListener('mousedown', onDoc)
    document.addEventListener('keydown', onKey)
    window.addEventListener('scroll', onClose, true)
    window.addEventListener('resize', onClose)
    return () => {
      document.removeEventListener('mousedown', onDoc)
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
        className="row-act inline-flex items-center gap-1"
        disabled={disabled}
        aria-haspopup="menu"
        aria-expanded={open}
        onClick={() => setOpen(v => !v)}
      >
        {label}
        <Icon name="more" size={14} />
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

export function SearchInput({ value, onChange, placeholder = '搜索' }) {
  return (
    <div className="relative flex-1 min-w-[12rem]">
      <span className="absolute left-2.5 top-1/2 -translate-y-1/2 text-ink-mut"><Icon name="search" size={14} /></span>
      <input className="input-field pl-8" placeholder={placeholder} value={value} onChange={onChange} />
    </div>
  )
}
