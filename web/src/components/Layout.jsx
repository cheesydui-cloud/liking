import { createContext, useCallback, useContext, useEffect, useState } from 'react'
import { NavLink, useNavigate } from 'react-router-dom'
import { api } from '../lib/api'
import { Icon, Modal } from './ui'

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
  const [toasts, setToasts] = useState([])
  const [dialog, setDialog] = useState(null)

  const refreshUser = useCallback(async () => {
    try {
      const data = await api.get('/me')
      setUser(data?.user ?? null)
      if (data?.panel_name) setPanelName(data.panel_name)
      if (data?.version) setVersion(data.version)
      setSub(data?.sub || null)
      return data
    } catch {
      setUser(null)
      return null
    }
  }, [])

  useEffect(() => {
    api.get('/branding').then(d => {
      if (d?.panel_name) setPanelName(d.panel_name)
    }).catch(() => {})
    refreshUser()
  }, [refreshUser])

  useEffect(() => {
    if (panelName) document.title = panelName
  }, [panelName])

  useEffect(() => {
    const h = () => setUser(null)
    window.addEventListener('lk-unauthorized', h)
    return () => window.removeEventListener('lk-unauthorized', h)
  }, [])

  const toast = useCallback((msg, type) => {
    const id = Date.now() + Math.random()
    setToasts(t => [...t.slice(-3), { id, msg, type: type || 'ok' }])
    setTimeout(() => setToasts(t => t.filter(x => x.id !== id)), type === 'error' ? 6400 : 3200)
  }, [])

  const applySession = useCallback((data) => {
    if (data?.user !== undefined) setUser(data.user)
    if (data?.panel_name) setPanelName(data.panel_name)
    if (data?.version) setVersion(data.version)
    setSub(data?.sub || null)
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
    <UserCtx.Provider value={{ user, setUser, panelName, version, sub, refreshUser, applySession }}>
      <ToastCtx.Provider value={toast}>
        <DialogCtx.Provider value={{ confirm, prompt }}>
          {children}
          <div className="fixed right-4 bottom-4 z-[90] flex flex-col gap-2" aria-live="polite">
            {toasts.map(t => (
              <div key={t.id} className="card px-3.5 py-2.5 text-[13px] min-w-[220px] shadow-lg"
                style={{ borderColor: t.type === 'error' ? 'color-mix(in srgb, var(--color-danger) 50%, var(--color-line))' : 'var(--color-line)' }}>
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
      className={({ isActive }) =>
        `group flex items-center gap-3 px-3 py-2.5 rounded-xl text-[13.5px] transition-colors duration-200 ${
          isActive ? 'text-ink' : 'text-ink-mut hover:text-ink hover:bg-raised'
        }`
      }>
      {({ isActive }) => (
        <>
          <span className={`w-[3px] h-4 rounded-full ${isActive ? 'bg-gold' : 'bg-transparent'}`} />
          <span className={isActive ? 'text-gold' : ''}><Icon name={icon} size={17} /></span>
          <span className={isActive ? 'font-semibold' : ''}>{children}</span>
        </>
      )}
    </NavLink>
  )
}

export function Layout({ children }) {
  const { user, panelName, version, setUser } = useUser()
  const navigate = useNavigate()
  const [open, setOpen] = useState(false)
  const isAdmin = user?.role === 'admin'
  const [dark, setDark] = useState(() => document.documentElement.classList.contains('dark'))

  const logout = async () => {
    try { await api.post('/logout') } catch {}
    setUser(null)
    navigate('/login', { replace: true })
  }
  const toggleTheme = () => {
    const next = !dark
    setDark(next)
    document.documentElement.classList.toggle('dark', next)
    localStorage.setItem('lk-theme', next ? 'dark' : 'light')
  }

  const nav = isAdmin ? [
    { to: '/', end: true, icon: 'layout', label: '总览' },
    { to: '/servers', icon: 'servers', label: '服务器' },
    { to: '/inbounds', icon: 'plugs', label: '入站' },
    { to: '/users', icon: 'users', label: '用户' },
    { to: '/packages', icon: 'package', label: '套餐' },
    { to: '/certs', icon: 'cert', label: '证书' },
    { to: '/settings', icon: 'gear', label: '设置' },
  ] : [
    { to: '/my', icon: 'spark', label: '我的订阅' },
  ]

  return (
    <div className="flex h-screen">
      {open && <div className="fixed inset-0 bg-black/50 z-30 lg:hidden" onClick={() => setOpen(false)} />}
      <aside className={`fixed lg:static z-40 h-full w-[248px] flex flex-col border-r bg-surface/90 backdrop-blur-xl ${open ? 'translate-x-0' : '-translate-x-full lg:translate-x-0'} transition-transform duration-200`}
        style={{ borderColor: 'var(--color-line)' }}>
        <div className="px-6 pt-7 pb-5 flex items-start gap-3">
          <div className="mt-0.5 w-9 h-9 rounded-lg grid place-items-center font-display text-[18px] leading-none shrink-0"
            style={{ color: 'var(--color-gold)', border: '1px solid color-mix(in srgb, var(--color-gold) 55%, transparent)', background: 'var(--color-accent-soft)' }}>L</div>
          <div className="min-w-0">
            <div className="font-display text-[28px] leading-none tracking-tight truncate">{panelName || 'liking'}</div>
            <div className="kicker mt-2">{isAdmin ? 'Control' : 'Member'}{version ? ` · v${version}` : ''}</div>
          </div>
        </div>
        <div className="gold-rule mx-6 mb-3" />
        <nav className="flex-1 px-3 space-y-0.5" onClick={() => setOpen(false)}>
          {nav.map(n => <SideLink key={n.to} to={n.to} end={n.end} icon={n.icon}>{n.label}</SideLink>)}
        </nav>
        <div className="p-4 border-t" style={{ borderColor: 'var(--color-line)' }}>
          <div className="text-[13px] font-semibold truncate">{user?.username}</div>
          <div className="text-[11px] text-ink-mut mt-0.5">{isAdmin ? '管理员' : '用户'}</div>
          <div className="flex gap-2 mt-3">
            <NavLink to={isAdmin ? '/password' : '/my/password'} className="btn-ghost flex-1 h-9">密码</NavLink>
            <button type="button" onClick={logout} className="btn-ghost flex-1 h-9" aria-label="退出">
              <Icon name="logout" size={15} /> 退出
            </button>
          </div>
        </div>
      </aside>
      <main className="flex-1 min-w-0 flex flex-col">
        <div className="h-14 px-4 sm:px-8 flex items-center gap-3">
          <button type="button" className="lg:hidden btn-ghost h-10 w-10 px-0" onClick={() => setOpen(true)} aria-label="打开菜单">
            <Icon name="menu" />
          </button>
          <div className="flex-1" />
          <button type="button" className="btn-ghost h-10 w-10 px-0" onClick={toggleTheme} aria-label={dark ? '切换浅色' : '切换深色'}>
            <Icon name={dark ? 'sun' : 'moon'} size={16} />
          </button>
        </div>
        <div className="flex-1 overflow-y-auto px-4 sm:px-8 pb-10">
          <div className="max-w-[1180px] mx-auto">{children}</div>
        </div>
      </main>
    </div>
  )
}
