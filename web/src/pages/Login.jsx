import { useEffect, useState } from 'react'
import { useNavigate } from 'react-router-dom'
import { api } from '../lib/api'
import { useUser } from '../components/Layout'
import { BrandMark, Icon } from '../components/ui'

export default function Login() {
  const [username, setUsername] = useState('')
  const [password, setPassword] = useState('')
  const [show, setShow] = useState(false)
  const [error, setError] = useState('')
  const [loading, setLoading] = useState(false)
  const [panelName, setPanelName] = useState('liking')
  const navigate = useNavigate()
  const { user, applySession } = useUser()

  useEffect(() => {
    if (user) navigate('/', { replace: true })
  }, [user, navigate])

  useEffect(() => {
    api.get('/branding').then(d => {
      if (d?.panel_name) {
        setPanelName(d.panel_name)
        document.title = d.panel_name
      }
    }).catch(() => {})
  }, [])

  const submit = async (e) => {
    e.preventDefault()
    setError('')
    setLoading(true)
    try {
      const data = await api.post('/login', { username, password })
      applySession(data)
      navigate('/', { replace: true })
    } catch (err) {
      setError(err.message || '登录失败')
    } finally {
      setLoading(false)
    }
  }

  return (
    <div className="min-h-dvh grid place-items-center p-5 relative overflow-hidden">
      <div className="pointer-events-none absolute inset-0"
        style={{ background: 'radial-gradient(720px 360px at 50% -10%, color-mix(in srgb, var(--color-accent) 16%, transparent), transparent 62%)' }} />
      <div className="relative card w-full max-w-[380px] p-6 sm:p-7">
        <div className="flex items-center gap-2.5 mb-6">
          <BrandMark size={32} />
          <div>
            <div className="text-[16px] font-semibold leading-tight">{panelName}</div>
            <div className="text-[12px] text-ink-mut mt-0.5">登录面板</div>
          </div>
        </div>
        {error && (
          <div role="alert" className="mb-4 text-[13px] rounded-lg px-3 py-2.5" style={{ color: 'var(--color-danger)', background: 'var(--color-danger-soft)' }}>
            {error}
          </div>
        )}
        <form onSubmit={submit} className="flex flex-col gap-3.5">
          <label className="block">
            <span className="block text-[12px] font-medium text-ink-soft mb-1.5">用户名</span>
            <input className="input-field h-10" value={username} onChange={e => setUsername(e.target.value)} required autoFocus autoComplete="username" />
          </label>
          <label className="block">
            <span className="block text-[12px] font-medium text-ink-soft mb-1.5">密码</span>
            <div className="relative">
              <input className="input-field h-10 pr-11" type={show ? 'text' : 'password'} value={password} onChange={e => setPassword(e.target.value)} required autoComplete="current-password" />
              <button type="button" className="absolute right-1.5 top-1/2 -translate-y-1/2 btn-ghost h-8 w-8 px-0 border-0" onClick={() => setShow(v => !v)} aria-label={show ? '隐藏密码' : '显示密码'}>
                <Icon name={show ? 'eye-off' : 'eye'} size={16} />
              </button>
            </div>
          </label>
          <button className="btn-primary h-10 mt-1" disabled={loading}>{loading ? '登录中…' : '登录'}</button>
        </form>
      </div>
    </div>
  )
}
