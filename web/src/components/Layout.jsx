import { createContext, useCallback, useContext, useEffect, useRef, useState } from 'react'
import { Link, NavLink, useLocation, useNavigate } from 'react-router-dom'
import { api } from '../lib/api'
import { cacheGen, clearLists, putList } from '../lib/listCache'
import { asArray } from '../lib/safe'
import { BrandMark, Icon, Modal } from './ui'

const UserCtx = createContext(null)
const ToastCtx = createContext(() => {})
const DialogCtx = createContext(null)
export function useUser() { return useContext(UserCtx) }
export function useToast() { return useContext(ToastCtx) }
export function useDialog() { return useContext(DialogCtx) }

export function UserProvider({ children }) {
  const [user, setUser] = useState(undefined)
  const [panelName, setPanelName] = useState('liking')
  const [version, setVersion] = useState('')
  const [sub, setSub] = useState(null)
  const [announce, setAnnounce] = useState('')
  const [toasts, setToasts] = useState([])
  const [dialog, setDialog] = useState(null)

  const refreshUser = useCallback(async () => {
    try {
      const data = await api.get('/me')
      setUser(data?.user ?? null)
      if (data?.panel_name) setPanelName(data.panel_name)
      if (data?.version) setVersion(data.version)
      setSub(data?.sub || null)
      if (data?.announce != null) setAnnounce(data.announce)
      return data
    } catch (e) {
      if (e?.status === 401) setUser(null)
      else setUser(prev => (prev === undefined ? null : prev))
      return null
    }
  }, [])

  useEffect(() => {
    api.get('/branding').then(d => {
      if (d?.panel_name) setPanelName(d.panel_name)
      if (d?.announce != null) setAnnounce(d.announce)
    }).catch(() => {})
    refreshUser()
  }, [refreshUser])

  useEffect(() => {
    if (panelName) document.title = panelName
  }, [panelName])

  useEffect(() => {
    const h = () => {
      clearLists()
      setUser(null)
    }
    window.addEventListener('lk-unauthorized', h)
    return () => window.removeEventListener('lk-unauthorized', h)
  }, [])

  const toastTimers = useRef([])
  const toast = useCallback((msg, type) => {
    const id = Date.now() + Math.random()
    setToasts(t => [...t.slice(-3), { id, msg, type: type || 'ok' }])
    const h = setTimeout(() => setToasts(t => t.filter(x => x.id !== id)), type === 'error' ? 6400 : 3200)
    toastTimers.current.push(h)
  }, [])
  useEffect(() => () => toastTimers.current.forEach(clearTimeout), [])

  const applySession = useCallback((data) => {
    if (data?.user !== undefined) setUser(data.user)
    if (data?.panel_name) setPanelName(data.panel_name)
    if (data?.version) setVersion(data.version)
    setSub(data?.sub || null)
    if (data?.announce != null) setAnnounce(data.announce)
  }, [])

  const confirm = useCallback((opts) => new Promise(resolve => {
    setDialog({ kind: 'confirm', ...opts, resolve })
  }), [])
  const prompt = useCallback((opts) => new Promise(resolve => {
    setDialog({ kind: 'prompt', value: opts?.defaultValue || '', ...opts, resolve })
  }), [])
  const closeDialog = (val) => {
    dialog?.resolve(val)
    setDialog(null)
  }

  return (
    <UserCtx.Provider value={{ user, setUser, panelName, version, sub, announce, refreshUser, applySession }}>
      <ToastCtx.Provider value={toast}>
        <DialogCtx.Provider value={{ confirm, prompt }}>
          {children}
          <div className="toast-stack fixed right-4 bottom-4 z-[90] flex flex-col gap-2" aria-live="polite" aria-relevant="additions">
            {toasts.map(t => (
              <div key={t.id} className="card px-3.5 py-2.5 text-[13px] min-w-[220px]"
                style={{
                  borderLeft: `3px solid ${t.type === 'error' ? 'var(--color-danger)' : 'var(--color-accent)'}`,
                }}>
                <span style={{ color: t.type === 'error' ? 'var(--color-danger)' : 'var(--color-ink)' }}>{t.msg}</span>
              </div>
            ))}
          </div>
          <Modal
            open={!!dialog}
            title={dialog?.title || (dialog?.kind === 'prompt' ? '请输入' : '确认')}
            onClose={() => closeDialog(dialog?.kind === 'prompt' ? null : false)}
            footer={dialog ? (
              <>
                <button type="button" className="btn-ghost" onClick={() => closeDialog(dialog.kind === 'prompt' ? null : false)}>取消</button>
                <button type="button" className={dialog.danger ? 'btn-danger' : 'btn-primary'}
                  onClick={() => closeDialog(dialog.kind === 'prompt' ? dialog.value : true)}>
                  {dialog.okText || (dialog.danger ? '删除' : '确定')}
                </button>
              </>
            ) : null}
          >
            {dialog?.message && <p className="text-[14px] text-ink-soft">{dialog.message}</p>}
            {dialog?.kind === 'prompt' && (
              <input className="input-field mt-3" type={dialog.inputType || 'text'} autoFocus
                name="dialog-input"
                autoComplete="off"
                aria-label={dialog.title || '请输入'}
                value={dialog.value || ''}
                onChange={e => setDialog({ ...dialog, value: e.target.value })}
                onKeyDown={e => { if (e.key === 'Enter') closeDialog(dialog.value) }}
              />
            )}
          </Modal>
        </DialogCtx.Provider>
      </ToastCtx.Provider>
    </UserCtx.Provider>
  )
}

function SideLink({ to, end, icon, children }) {
  return (
    <NavLink to={to} end={end}
      className={({ isActive }) => `sidebar-link${isActive ? ' is-active' : ''}`}>
      <Icon name={icon} size={20} />
      <span>{children}</span>
    </NavLink>
  )
}

const PAGE_TITLES = {
  '/': '总览',
  '/nodes': '节点',
  '/servers': '实例',
  '/users': '用户',
  '/packages': '套餐',
  '/forwards': '中转',
  '/traffic': '流量',
  '/settings': '设置',
  '/my': '订阅',
  '/my/nodes': '节点',
  '/my/forwards': '中转',
  '/my/settings': '设置',
}

function pageTitleOf(path) {
  if (PAGE_TITLES[path]) return PAGE_TITLES[path]
  const hit = Object.keys(PAGE_TITLES).sort((a, b) => b.length - a.length).find(p => p !== '/' && path.startsWith(p))
  return hit ? PAGE_TITLES[hit] : ''
}

export function Layout({ children }) {
  const { user, panelName, version, announce, setUser } = useUser()
  const navigate = useNavigate()
  const loc = useLocation()
  const [open, setOpen] = useState(false)
  const isAdmin = user?.role === 'admin'
  const userView = loc.pathname === '/my' || loc.pathname.startsWith('/my/')
  const [dark, setDark] = useState(() => document.documentElement.classList.contains('dark'))
  const mainRef = useRef(null)
  const scrollRef = useRef(null)
  const pageTitle = pageTitleOf(loc.pathname)
  const announceText = (announce || '').trim()
  const announceLong = announceText.length > 80

  useEffect(() => {
    setOpen(false)
    // Layout stays mounted across routes; the content pane keeps its scrollTop otherwise.
    if (scrollRef.current) scrollRef.current.scrollTop = 0
    window.scrollTo(0, 0)
    mainRef.current?.focus({ preventScroll: true })
  }, [loc.pathname])

  useEffect(() => {
    document.documentElement.classList.add('page-aliyun')
    const meta = document.querySelector('meta[name="theme-color"]')
    if (meta) meta.setAttribute('content', dark ? '#141414' : '#F7F8FA')
  }, [dark])

  useEffect(() => {
    if (!open) return
    const prev = document.body.style.overflow
    document.body.style.overflow = 'hidden'
    return () => { document.body.style.overflow = prev }
  }, [open])

  useEffect(() => {
    if (!isAdmin) return
    const g = cacheGen()
    const ac = new AbortController()
    api.get('/servers', ac.signal).then(a => putList('servers', asArray(a.servers), g)).catch(() => {})
    api.get('/inbounds', ac.signal).then(a => putList('inbounds', asArray(a.inbounds), g)).catch(() => {})
    api.get('/me/nodes', ac.signal).then(d => putList('me-nodes', {
      nodes: asArray(d.nodes),
      hidden: asArray(d.hidden),
      starred: asArray(d.starred),
      announce: d.announce || '',
    }, g)).catch(() => {})
    return () => ac.abort()
  }, [isAdmin])

  const logout = async () => {
    try { await api.post('/logout') } catch {}
    clearLists()
    setUser(null)
    navigate('/login', { replace: true })
  }
  const toggleTheme = () => {
    const next = !dark
    setDark(next)
    document.documentElement.classList.toggle('dark', next)
    localStorage.setItem('lk-theme', next ? 'dark' : 'light')
    const meta = document.querySelector('meta[name="theme-color"]')
    if (meta) meta.setAttribute('content', next ? '#141414' : '#F7F8FA')
  }

  const groups = isAdmin && !userView ? [
    { items: [{ to: '/', end: true, icon: 'layout', label: '总览' }] },
    {
      label: '服务器',
      items: [
        { to: '/servers', icon: 'servers', label: '实例' },
        { to: '/nodes', icon: 'plugs', label: '节点' },
      ],
    },
    {
      label: '业务',
      items: [
        { to: '/packages', icon: 'package', label: '套餐' },
        { to: '/users', icon: 'users', label: '用户' },
        { to: '/forwards', icon: 'forward', label: '中转' },
        { to: '/traffic', icon: 'bars', label: '流量' },
      ],
    },
    {
      label: '系统',
      items: [
        { to: '/settings', icon: 'gear', label: '设置' },
      ],
    },
  ] : [
    { items: [
      { to: '/my', end: true, icon: 'spark', label: isAdmin ? '订阅' : '我的订阅' },
      ...(isAdmin ? [
        { to: '/my/nodes', icon: 'plugs', label: '节点' },
        { to: '/my/forwards', icon: 'forward', label: '中转' },
      ] : []),
    ] },
    ...(isAdmin ? [] : [{ label: '账号', items: [{ to: '/my/settings', icon: 'gear', label: '设置' }] }]),
  ]

  return (
    <div className="flex h-dvh max-h-dvh overflow-hidden">
      <a href="#main" className="skip-link">跳到内容</a>
      {open && (
        <button type="button" className="fixed inset-0 modal-scrim z-30 lg:hidden" aria-label="关闭菜单" onClick={() => setOpen(false)} />
      )}
      <aside
        className={`sidebar-pane fixed lg:static z-40 h-full w-[200px] flex flex-col ${open ? 'translate-x-0' : '-translate-x-full lg:translate-x-0'}`}
        style={{ transition: 'transform var(--duration-med) ease', overscrollBehavior: 'contain' }}
        aria-label="主导航"
      >
        <div className="px-4 pt-4 pb-3 flex items-end gap-2.5">
          <BrandMark size={28} />
          <div className="min-w-0">
            <div className="sidebar-brand truncate">{panelName || 'liking'}</div>
          </div>
        </div>
        <nav className="flex-1 px-2.5 overflow-y-auto">
          {groups.map((g, i) => (
            <div key={i} className={i ? 'mt-3.5' : ''}>
              {g.label && <div className="sidebar-group px-2.5 mb-1">{g.label}</div>}
              <div className="space-y-0.5">
                {g.items.map(n => (
                  <SideLink key={n.to} to={n.to} end={n.end} icon={n.icon}>{n.label}</SideLink>
                ))}
              </div>
            </div>
          ))}
        </nav>
        <div className="p-3 border-t" style={{ borderColor: 'var(--color-line)' }}>
          <div className="flex items-center gap-2 px-1">
            <span className="user-mark">
              {(user?.username || '?').slice(0, 1).toUpperCase()}
            </span>
            <div className="min-w-0 flex-1">
              <div className="sidebar-user truncate">{user?.username}</div>
              <div className="sidebar-role">{isAdmin ? '管理员' : '用户'}</div>
            </div>
          </div>
          <button type="button" onClick={logout} className="btn-ghost w-full h-9 mt-2.5 text-[14px]" aria-label="退出">
            <Icon name="logout" size={16} /> 退出
          </button>
          {isAdmin && version ? <div className="sidebar-ver mt-2 px-1">v{version}</div> : null}
        </div>
      </aside>
      <main id="main" ref={mainRef} tabIndex={-1} className="flex-1 min-w-0 flex flex-col bg-app outline-none">
        <div className="topbar h-12 px-3 sm:px-5 flex items-center gap-2 shrink-0">
          <button type="button" className="lg:hidden btn-ghost h-11 w-11 px-0" onClick={() => setOpen(true)} aria-label="打开菜单">
            <Icon name="menu" size={16} />
          </button>
          {pageTitle ? <div className="lg:hidden text-[14px] font-medium truncate">{pageTitle}</div> : null}
          {announceText ? (
            <div className="hidden sm:block flex-1 min-w-0 text-[13px] text-ink-soft truncate" title={announceText}>{announceText}</div>
          ) : <div className="flex-1" />}
          {isAdmin ? (
            <Link to={userView ? '/' : '/my'} className="btn-ghost h-9 shrink-0 px-2.5 text-[13px]">
              {userView ? '管理' : '用户页'}
            </Link>
          ) : null}
          <button type="button" className="btn-ghost h-9 w-9 px-0" onClick={toggleTheme} aria-label={dark ? '切换浅色' : '切换深色'}>
            <Icon name={dark ? 'sun' : 'moon'} size={15} />
          </button>
        </div>
        <div ref={scrollRef} className="scroll-pane flex-1 overflow-y-auto px-3 sm:px-5 py-4 sm:py-5">
          <div className="max-w-[1280px]">
            {announceText ? (
              <div className={`notice mb-4${announceLong ? '' : ' sm:hidden'}`}>{announceText}</div>
            ) : null}
            {children}
          </div>
        </div>
      </main>
    </div>
  )
}
