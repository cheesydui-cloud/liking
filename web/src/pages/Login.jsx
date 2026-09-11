import { useEffect, useState } from 'react'
import { useNavigate } from 'react-router-dom'
import { api } from '../lib/api'
import { useUser } from '../components/Layout'
import { Icon } from '../components/ui'

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
    <div className="min-h-dvh grid lg:grid-cols-[1.05fr_0.95fr]">
      <div className="hidden lg:flex flex-col justify-between p-14 relative overflow-hidden">
        <div className="absolute inset-0 bg-[radial-gradient(900px_520px_at_18%_0%,color-mix(in_srgb,var(--color-gold)_26%,transparent),transparent_58%)]" />
        <div className="absolute -right-10 -bottom-16 font-display text-[420px] leading-none select-none pointer-events-none"
          style={{ color: 'color-mix(in srgb, var(--color-gold) 9%, transparent)' }}>L</div>
        <div className="relative">
          <div className="w-12 h-12 rounded-xl grid place-items-center font-display text-[26px] mb-10"
            style={{ color: 'var(--color-gold)', border: '1px solid color-mix(in srgb, var(--color-gold) 50%, transparent)', background: 'var(--color-accent-soft)' }}>L</div>
          <div className="kicker">Private Line Desk</div>
          <div className="font-display text-[72px] leading-[0.86] mt-5 tracking-tight">{panelName}</div>
        </div>
        <div className="relative max-w-md">
          <div className="gold-rule mb-7" />
          <p className="font-display text-[30px] leading-snug text-ink">一条线路，一个出口。<br />少一点花哨，多一点稳。</p>
          <p className="text-[13.5px] text-ink-mut mt-5 leading-relaxed">Xray 默认内核 · AnyTLS 走 sing-box · Mieru 走 mita。主控与节点反向纳管，配置下发失败会写在服务器页。</p>
        </div>
        <div className="relative kicker">Quiet luxury · v1</div>
      </div>
      <div className="grid place-items-center p-6 sm:p-12">
        <div className="card w-full max-w-[440px] p-8 sm:p-11" style={{ boxShadow: '0 30px 80px -40px color-mix(in srgb, var(--color-gold) 35%, transparent)' }}>
          <div className="lg:hidden mb-8">
            <div className="kicker">Sign in</div>
            <div className="font-display text-[40px] leading-none mt-1">{panelName}</div>
          </div>
          <div className="hidden lg:block mb-9">
            <div className="kicker">Welcome back</div>
            <h1 className="font-display text-[38px] leading-none mt-2">登录面板</h1>
            <p className="text-[13px] text-ink-mut mt-3">管理员进入控制台，用户查看自己的订阅。</p>
          </div>
          {error && (
            <div role="alert" className="mb-5 text-[13px] rounded-xl px-3 py-2.5" style={{ color: 'var(--color-danger)', background: 'var(--color-danger-soft)' }}>
              {error}
            </div>
          )}
          <form onSubmit={submit} className="flex flex-col gap-4">
            <label className="block">
              <span className="block text-[12px] text-ink-soft mb-1.5">用户名</span>
              <input className="input-field" value={username} onChange={e => setUsername(e.target.value)} required autoFocus autoComplete="username" />
            </label>
            <label className="block">
              <span className="block text-[12px] text-ink-soft mb-1.5">密码</span>
              <div className="relative">
                <input className="input-field pr-11" type={show ? 'text' : 'password'} value={password} onChange={e => setPassword(e.target.value)} required autoComplete="current-password" />
                <button type="button" className="absolute right-2 top-1/2 -translate-y-1/2 btn-ghost h-8 w-8 px-0 border-0" onClick={() => setShow(v => !v)} aria-label={show ? '隐藏密码' : '显示密码'}>
                  <Icon name={show ? 'eye-off' : 'eye'} size={16} />
                </button>
              </div>
            </label>
            <button className="btn-primary mt-2" disabled={loading}>{loading ? '登录中…' : '进入'}</button>
          </form>
        </div>
      </div>
    </div>
  )
}
