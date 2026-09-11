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
  }
  return <svg {...common}>{p[name] || p.spark}</svg>
}

export function fmtBytes(n) {
  if (!n) return '0 B'
  const u = ['B', 'KB', 'MB', 'GB', 'TB']
  let i = 0, x = Number(n)
  while (x >= 1024 && i < u.length - 1) { x /= 1024; i++ }
  return (i ? x.toFixed(1) : String(Math.round(x))) + ' ' + u[i]
}

export function fmtDate(ts) {
  if (!ts) return '—'
  try { return new Date(ts * 1000).toLocaleString() } catch { return '—' }
}

export function PageHead({ kicker, title, desc, actions }) {
  return (
    <div className="flex flex-col sm:flex-row sm:items-end gap-4 mb-7">
      <div className="flex-1 min-w-0">
        {kicker && <div className="kicker mb-2">{kicker}</div>}
        <h1 className="font-display text-[32px] sm:text-[36px] leading-none tracking-tight">{title}</h1>
        {desc && <p className="text-[13.5px] text-ink-mut mt-2 max-w-2xl">{desc}</p>}
      </div>
      {actions && <div className="flex flex-wrap gap-2">{actions}</div>}
    </div>
  )
}

export function Empty({ title, hint, action }) {
  return (
    <div className="py-14 px-6 text-center">
      <div className="mx-auto w-11 h-11 rounded-full grid place-items-center mb-3 border" style={{ borderColor: 'var(--color-line)', color: 'var(--color-gold)' }}>
        <Icon name="spark" />
      </div>
      <div className="font-display text-[22px]">{title}</div>
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

export function Badge({ tone = 'muted', children }) {
  const map = {
    gold: { color: 'var(--color-gold)', bg: 'var(--color-accent-soft)' },
    ok: { color: 'var(--color-ok)', bg: 'var(--color-ok-soft)' },
    danger: { color: 'var(--color-danger)', bg: 'var(--color-danger-soft)' },
    muted: { color: 'var(--color-ink-mut)', bg: 'var(--color-raised)' },
  }
  const t = map[tone] || map.muted
  return (
    <span className="inline-flex items-center gap-1 px-2 py-0.5 rounded-full text-[11px] font-semibold" style={{ color: t.color, background: t.bg }}>
      {children}
    </span>
  )
}

export function Modal({ open, title, onClose, children, footer, wide }) {
  if (!open) return null
  return (
    <div className="fixed inset-0 z-[80] flex items-end sm:items-center justify-center p-0 sm:p-6">
      <button type="button" className="absolute inset-0 bg-black/55 backdrop-blur-[2px]" aria-label="关闭" onClick={onClose} />
      <div className={`relative card w-full ${wide ? 'max-w-2xl' : 'max-w-md'} p-5 sm:p-6 m-0 sm:m-auto rounded-t-2xl sm:rounded-2xl`}>
        <div className="flex items-start justify-between gap-3 mb-4">
          <h2 className="font-display text-[24px] leading-tight">{title}</h2>
          <button type="button" className="btn-ghost h-9 w-9 px-0" onClick={onClose} aria-label="关闭"><Icon name="close" size={16} /></button>
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
      {Array.from({ length: rows }).map((_, i) => <div key={i} className="skeleton h-10" />)}
    </div>
  )
}
