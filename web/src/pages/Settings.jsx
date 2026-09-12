import { useEffect, useState } from 'react'
import { useSearchParams } from 'react-router-dom'
import { api } from '../lib/api'
import { useToast, useDialog, useUser } from '../components/Layout'
import { Badge, Empty, Field, Icon, Modal, PageHead, Tabs, fmtDateShort } from '../components/ui'

const emptyIssue = { channel: 'acme-cf', name: '', domains: '', cert_pem: '', key_pem: '' }

const channels = [
  { id: 'acme-cf', title: 'Cloudflare', desc: 'Let\'s Encrypt，DNS 验证。域名不必指向本面板，支持 *.example.com。' },
  { id: 'selfsigned', title: '自签', desc: '面板立刻生成。客户端需跳过证书校验，适合测试。' },
  { id: 'upload', title: '上传', desc: '已有完整证书链和私钥 PEM。' },
]

function sourceLabel(src) {
  if (src === 'acme-cf') return 'Let\'s Encrypt'
  if (src === 'selfsigned') return '自签'
  return '上传'
}

function expiryTone(ts) {
  if (!ts) return 'muted'
  const left = Number(ts) * 1000 - Date.now()
  if (left < 0) return 'danger'
  if (left < 14 * 86400 * 1000) return 'danger'
  if (left < 30 * 86400 * 1000) return 'gold'
  return 'ok'
}

function expiryText(ts) {
  if (!ts) return '—'
  const left = Number(ts) * 1000 - Date.now()
  if (left < 0) return '已过期'
  return fmtDateShort(ts)
}

function AccountForm() {
  const toast = useToast()
  const { user, applySession, refreshUser } = useUser()
  const [username, setUsername] = useState(user?.username || '')
  const [oldP, setOld] = useState('')
  const [n, setN] = useState('')
  const [n2, setN2] = useState('')
  const [busy, setBusy] = useState(false)

  useEffect(() => { setUsername(user?.username || '') }, [user?.username])

  const submit = async (e) => {
    e.preventDefault()
    if (!oldP) { toast('请填写当前密码', 'error'); return }
    if (n && n.length < 6) { toast('新密码至少 6 位', 'error'); return }
    if (n && n !== n2) { toast('两次新密码不一致', 'error'); return }
    setBusy(true)
    try {
      const body = { username: username.trim(), old_password: oldP }
      if (n) body.new_password = n
      const data = await api.put('/me', body)
      if (data) applySession(data)
      else await refreshUser()
      toast('已保存')
      setOld(''); setN(''); setN2('')
    } catch (err) { toast(err.message, 'error') }
    finally { setBusy(false) }
  }

  return (
    <form onSubmit={submit} className="card p-5 max-w-md space-y-4">
      <Field label="用户名">
        <input className="input-field" value={username} onChange={e => setUsername(e.target.value)} required autoComplete="username" />
      </Field>
      <Field label="当前密码" hint="改用户名或密码都要填写。改完后当前会话仍然有效。">
        <input className="input-field" type="password" value={oldP} onChange={e => setOld(e.target.value)} required autoComplete="current-password" />
      </Field>
      <Field label="新密码" hint="留空表示不改密码。至少 6 位。">
        <input className="input-field" type="password" value={n} onChange={e => setN(e.target.value)} autoComplete="new-password" />
      </Field>
      <Field label="确认新密码">
        <input className="input-field" type="password" value={n2} onChange={e => setN2(e.target.value)} autoComplete="new-password" disabled={!n} />
      </Field>
      <button className="btn-primary" disabled={busy}>{busy ? '保存中…' : '保存'}</button>
    </form>
  )
}

const settingTabs = [
  { id: 'panel', label: '面板' },
  { id: 'certs', label: '证书' },
  { id: 'account', label: '账号' },
]

export default function Settings({ accountOnly = false }) {
  const toast = useToast()
  const dialog = useDialog()
  const { refreshUser, version } = useUser()
  const [searchParams, setSearchParams] = useSearchParams()
  const requested = searchParams.get('tab')
  const tab = accountOnly ? 'account' : (settingTabs.some(t => t.id === requested) ? requested : 'panel')
  const [f, setF] = useState({ panel_name: '', panel_url: '', acme_email: '', cf_api_token: '' })
  const [tokenSet, setTokenSet] = useState(false)
  const [busy, setBusy] = useState(false)
  const [certs, setCerts] = useState([])
  const [issue, setIssue] = useState(emptyIssue)
  const [formOpen, setFormOpen] = useState(false)
  const [issueBusy, setIssueBusy] = useState(false)
  const [renewing, setRenewing] = useState(0)

  const loadSettings = () => api.get('/settings').then(d => {
    setF({
      panel_name: d.panel_name || '',
      panel_url: d.panel_url || '',
      acme_email: d.acme_email || '',
      cf_api_token: '',
    })
    setTokenSet(!!d.cf_api_token_set)
  }).catch(e => toast(e.message, 'error'))

  const loadCerts = () => api.get('/certs').then(d => setCerts(d.certs || [])).catch(e => toast(e.message, 'error'))

  useEffect(() => {
    if (accountOnly) return
    loadSettings()
    loadCerts()
  }, [accountOnly])

  const goTab = (id) => setSearchParams(id === 'panel' ? {} : { tab: id }, { replace: true })

  const savePanel = async (e) => {
    e.preventDefault()
    setBusy(true)
    try {
      await api.put('/settings', { panel_name: f.panel_name, panel_url: f.panel_url })
      toast('已保存')
      refreshUser()
      loadSettings()
    } catch (e) { toast(e.message, 'error') }
    finally { setBusy(false) }
  }

  const saveCF = async (e) => {
    e.preventDefault()
    setBusy(true)
    try {
      const body = { acme_email: f.acme_email }
      if (f.cf_api_token.trim()) body.cf_api_token = f.cf_api_token.trim()
      await api.put('/settings', body)
      toast('已保存')
      setF(x => ({ ...x, cf_api_token: '' }))
      loadSettings()
    } catch (e) { toast(e.message, 'error') }
    finally { setBusy(false) }
  }

  const openIssue = () => {
    setIssue(emptyIssue)
    setFormOpen(true)
  }

  const submitIssue = async (e) => {
    e.preventDefault()
    setIssueBusy(true)
    try {
      if (issue.channel === 'acme-cf') {
        await api.post('/certs/acme', { name: issue.name, domains: issue.domains })
        toast('已签发')
      } else if (issue.channel === 'selfsigned') {
        await api.post('/certs/selfsign', { name: issue.name, domains: issue.domains })
        toast('已生成')
      } else {
        await api.post('/certs', { name: issue.name, domains: issue.domains, cert_pem: issue.cert_pem, key_pem: issue.key_pem })
        toast('已保存')
      }
      setFormOpen(false)
      setIssue(emptyIssue)
      loadCerts()
    } catch (err) { toast(err.message, 'error') }
    finally { setIssueBusy(false) }
  }

  const del = async (id) => {
    if (!(await dialog.confirm({ title: '删除证书', message: '使用此证书的 TLS 入站将无法握手。', danger: true }))) return
    try { await api.del(`/certs/${id}`); loadCerts() }
    catch (e) { toast(e.message, 'error') }
  }

  const renew = async (id) => {
    setRenewing(id)
    try {
      await api.post(`/certs/${id}/renew`)
      toast('已续期')
      loadCerts()
    } catch (e) { toast(e.message, 'error') }
    finally { setRenewing(0) }
  }

  const issueHint = issue.channel === 'acme-cf'
    ? (issueBusy ? '正在向 Let\'s Encrypt 申请，大约 1–2 分钟…' : '申请')
    : (issueBusy ? '保存中…' : '保存')

  const desc = accountOnly
    ? '改用户名或登录密码。改完后当前会话仍然有效。'
    : `当前版本 ${version || '—'}。VLESS+XHTTP、Trojan、AnyTLS 需要证书；REALITY 不需要。`

  return (
    <div>
      <PageHead title="设置" desc={desc} />
      {!accountOnly && <Tabs value={tab} onChange={goTab} items={settingTabs} />}

      {tab === 'panel' && (
      <form onSubmit={savePanel} className="card p-5 max-w-3xl space-y-4">
        <div className="grid grid-cols-1 sm:grid-cols-2 gap-3">
          <Field label="面板名称">
            <input className="input-field" value={f.panel_name} onChange={e => setF({ ...f, panel_name: e.target.value })} />
          </Field>
          <Field label="面板 URL" hint="安装命令与订阅用。留空则按当前访问地址自动生成。">
            <input className="input-field" value={f.panel_url} onChange={e => setF({ ...f, panel_url: e.target.value })} placeholder="https://panel.example.com" />
          </Field>
        </div>
        <button className="btn-primary" disabled={busy}>{busy ? '保存中…' : '保存'}</button>
      </form>
      )}

      {tab === 'certs' && (
      <>
      <form onSubmit={saveCF} className="card p-5 max-w-3xl space-y-4">
        <div className="text-[15px] font-medium">Cloudflare / Let&apos;s Encrypt</div>
        <p className="text-[12.5px] text-ink-mut leading-relaxed">
          Token 权限：Zone · Zone · Read，Zone · DNS · Edit。同一 Token 也用来在服务器管理里拉取已托管域名。域名不必指向本面板，也不用开放 80 端口。
        </p>
        <div className="grid grid-cols-1 sm:grid-cols-2 gap-3">
          <Field label="ACME 邮箱">
            <input className="input-field" type="email" autoComplete="off" value={f.acme_email} onChange={e => setF({ ...f, acme_email: e.target.value })} placeholder="you@example.com" />
          </Field>
          <Field label="Cloudflare API Token" hint={tokenSet ? '已保存。留空则不改。' : '在 Cloudflare 控制台创建自定义 Token。'}>
            <input className="input-field font-mono" type="password" autoComplete="new-password" value={f.cf_api_token} onChange={e => setF({ ...f, cf_api_token: e.target.value })} placeholder={tokenSet ? '••••••••' : ''} />
          </Field>
        </div>
        <button className="btn-primary" disabled={busy}>{busy ? '保存中…' : '保存'}</button>
      </form>

      <div className="card overflow-hidden max-w-3xl mt-5">
        <div className="px-5 py-4 flex items-start justify-between gap-3">
          <div>
            <div className="text-[15px] font-medium">证书</div>
            <p className="text-[12.5px] text-ink-mut mt-1 leading-relaxed">Let&apos;s Encrypt 到期前约 30 天自动续期。自签和上传的不会自动续。</p>
          </div>
          <button type="button" className="btn-primary shrink-0" onClick={openIssue}>
            <Icon name="plus" size={15} /> 签发证书
          </button>
        </div>
        {certs.length === 0 ? (
          <Empty title="暂无证书" hint="用 Cloudflare 申请、自签，或上传 PEM。" action={
            <button type="button" className="btn-primary" onClick={openIssue}><Icon name="plus" size={15} /> 签发证书</button>
          } />
        ) : (
          <div className="table-wrap">
            <table className="data">
              <thead><tr><th>名称</th><th>来源</th><th>域名</th><th>到期</th><th></th></tr></thead>
              <tbody>
                {certs.map(c => (
                  <tr key={c.id}>
                    <td className="font-medium">
                      <div>{c.name}</div>
                      {c.last_error ? <div className="text-[11.5px] text-[var(--color-danger)] mt-0.5 leading-snug">{c.last_error}</div> : null}
                    </td>
                    <td><Badge tone={c.source === 'acme-cf' ? 'ok' : 'muted'}>{sourceLabel(c.source)}</Badge></td>
                    <td className="text-ink-mut">{c.domains || '—'}</td>
                    <td><Badge tone={expiryTone(c.expires_at)}>{expiryText(c.expires_at)}</Badge></td>
                    <td className="text-right whitespace-nowrap">
                      {c.source === 'acme-cf' && (
                        <button type="button" className="row-act" disabled={renewing === c.id} onClick={() => renew(c.id)}>
                          {renewing === c.id ? '续期中…' : '续期'}
                        </button>
                      )}
                      <button type="button" className="row-act is-danger" onClick={() => del(c.id)}>删除</button>
                    </td>
                  </tr>
                ))}
              </tbody>
            </table>
          </div>
        )}
      </div>

      <div className="mt-5 text-[13px] text-ink-mut max-w-3xl leading-relaxed">
        第一次下发对应线路时，节点会自行从 GitHub 安装内核到 PATH：Xray（默认）、sing-box（AnyTLS，≥ 1.12）、mita（Mieru）。Agent 只在有对应入站时才拉起该内核。访问不了 GitHub 时可在节点设置环境变量 LIKING_GITHUB_PROXY。
      </div>
      </>
      )}

      {tab === 'account' && <AccountForm />}

      <Modal open={formOpen} title="签发证书" onClose={() => !issueBusy && setFormOpen(false)} size="lg" footer={
        <>
          <button type="button" className="btn-ghost" disabled={issueBusy} onClick={() => setFormOpen(false)}>取消</button>
          <button type="submit" form="cert-issue-form" className="btn-primary" disabled={issueBusy}>{issueHint}</button>
        </>
      }>
        <form id="cert-issue-form" onSubmit={submitIssue} className="space-y-3">
          <div className="grid grid-cols-1 sm:grid-cols-3 gap-2">
            {channels.map(ch => (
              <button
                key={ch.id}
                type="button"
                onClick={() => setIssue({ ...issue, channel: ch.id })}
                className={`text-left rounded-lg border p-3 transition-colors ${issue.channel === ch.id ? 'bg-raised' : ''}`}
                style={{ borderColor: issue.channel === ch.id ? 'var(--color-ink)' : 'var(--color-line)' }}
              >
                <div className="text-[13px] font-medium">{ch.title}</div>
                <div className="text-[11.5px] text-ink-mut mt-1 leading-relaxed">{ch.desc}</div>
              </button>
            ))}
          </div>
          <div className="grid grid-cols-1 sm:grid-cols-2 gap-3">
            <Field label="名称" hint="可留空，默认用第一个域名">
              <input className="input-field" placeholder="example.com" value={issue.name} onChange={e => setIssue({ ...issue, name: e.target.value })} autoFocus />
            </Field>
            <Field label="域名" hint={issue.channel === 'acme-cf' ? '逗号分隔，可写 *.example.com' : '可选，逗号分隔'}>
              <input className="input-field" placeholder="example.com, *.example.com" value={issue.domains} onChange={e => setIssue({ ...issue, domains: e.target.value })} required={issue.channel !== 'upload'} />
            </Field>
          </div>
          {issue.channel === 'acme-cf' && (
            <p className="text-[12px] text-ink-mut leading-relaxed">使用上方已保存的 Token 和邮箱。申请期间会在 Cloudflare 写入 TXT 记录，完成后自动删掉。</p>
          )}
          {issue.channel === 'selfsigned' && (
            <p className="text-[12px] text-ink-mut leading-relaxed">系统不信任自签证书。客户端需要开启跳过证书校验，否则 TLS 握手会失败。</p>
          )}
          {issue.channel === 'upload' && (
            <>
              <Field label="证书 PEM"><textarea className="input-field font-mono text-[12px]" placeholder="-----BEGIN CERTIFICATE-----" value={issue.cert_pem} onChange={e => setIssue({ ...issue, cert_pem: e.target.value })} required /></Field>
              <Field label="私钥 PEM"><textarea className="input-field font-mono text-[12px]" placeholder="-----BEGIN PRIVATE KEY-----" value={issue.key_pem} onChange={e => setIssue({ ...issue, key_pem: e.target.value })} required /></Field>
            </>
          )}
        </form>
      </Modal>
    </div>
  )
}
