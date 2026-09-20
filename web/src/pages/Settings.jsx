import { useEffect, useState } from 'react'
import { Navigate, useSearchParams } from 'react-router-dom'
import QRCode from 'qrcode'
import { api } from '../lib/api'
import { useToast, useDialog, useUser } from '../components/Layout'
import { Badge, Empty, Field, Icon, Modal, MoreMenu, PageHead, Tabs, fmtBytes, fmtDate, fmtDateShort } from '../components/ui'

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
  if (left < 30 * 86400 * 1000) return 'warn'
  return 'ok'
}

function expiryText(ts) {
  if (!ts) return '—'
  const left = Number(ts) * 1000 - Date.now()
  if (left < 0) return '已过期'
  return fmtDateShort(ts)
}

function TotpBox() {
  const toast = useToast()
  const { user, refreshUser } = useUser()
  const [secret, setSecret] = useState('')
  const [uri, setUri] = useState('')
  const [qr, setQr] = useState('')
  const [code, setCode] = useState('')
  const [pw, setPw] = useState('')
  const [busy, setBusy] = useState(false)

  useEffect(() => {
    if (!uri) { setQr(''); return }
    QRCode.toDataURL(uri, { width: 200, margin: 1 }).then(setQr).catch(() => setQr(''))
  }, [uri])

  const begin = async () => {
    if (!pw.trim()) { toast('请填写当前密码', 'error'); return }
    setBusy(true)
    try {
      const d = await api.post('/totp/begin', { password: pw })
      setSecret(d.secret || '')
      setUri(d.uri || '')
      setPw('')
    } catch (e) { toast(e.message, 'error') }
    finally { setBusy(false) }
  }

  const enable = async (e) => {
    e.preventDefault()
    setBusy(true)
    try {
      await api.post('/totp/enable', { code })
      toast('已开启两步验证')
      setSecret(''); setUri(''); setCode('')
      refreshUser()
    } catch (e) { toast(e.message, 'error') }
    finally { setBusy(false) }
  }

  const disable = async (e) => {
    e.preventDefault()
    setBusy(true)
    try {
      await api.post('/totp/disable', { password: pw, code })
      toast('已关闭两步验证')
      setPw(''); setCode('')
      refreshUser()
    } catch (e) { toast(e.message, 'error') }
    finally { setBusy(false) }
  }

  if (user?.totp_enabled) {
    return (
      <form onSubmit={disable} className="card p-5 max-w-md space-y-4 mt-4">
        <div>
          <div className="text-[15px] font-medium">两步验证</div>
          <p className="text-[12.5px] text-ink-mut mt-1">已开启。关闭需要当前密码和验证器里的 6 位码。</p>
        </div>
        <Field label="当前密码">
          <input className="input-field" type="password" value={pw} onChange={e => setPw(e.target.value)} required autoComplete="current-password" />
        </Field>
        <Field label="验证码">
          <input className="input-field font-mono" value={code} onChange={e => setCode(e.target.value)} required inputMode="numeric" autoComplete="one-time-code" />
        </Field>
        <button className="btn-danger" disabled={busy}>{busy ? '关闭中…' : '关闭两步验证'}</button>
      </form>
    )
  }

  return (
    <div className="card p-5 max-w-md space-y-4 mt-4">
      <div>
        <div className="text-[15px] font-medium">两步验证</div>
        <p className="text-[12.5px] text-ink-mut mt-1">用验证器扫码后填 6 位码。管理员建议开启。</p>
      </div>
      {!secret ? (
        <>
          <Field label="当前密码">
            <input className="input-field" type="password" value={pw} onChange={e => setPw(e.target.value)} autoComplete="current-password" />
          </Field>
          <button type="button" className="btn-primary" disabled={busy} onClick={begin}>{busy ? '生成中…' : '开始绑定'}</button>
        </>
      ) : (
        <form onSubmit={enable} className="space-y-3">
          {qr ? <img src={qr} alt="" width={160} height={160} /> : null}
          <code className="block text-[12px] font-mono break-all">{secret}</code>
          <Field label="验证码">
            <input className="input-field font-mono" value={code} onChange={e => setCode(e.target.value)} required inputMode="numeric" autoComplete="one-time-code" autoFocus />
          </Field>
          <button className="btn-primary" disabled={busy}>{busy ? '验证中…' : '开启'}</button>
        </form>
      )}
    </div>
  )
}

function AccountForm() {
  const toast = useToast()
  const { user, applySession, refreshUser } = useUser()
  const isAdmin = user?.role === 'admin'
  const [username, setUsername] = useState(user?.username || '')
  const [oldP, setOld] = useState('')
  const [n, setN] = useState('')
  const [n2, setN2] = useState('')
  const [busy, setBusy] = useState(false)

  useEffect(() => { setUsername(user?.username || '') }, [user?.username])

  const submit = async (e) => {
    e.preventDefault()
    if (!oldP) { toast('请填写当前密码', 'error'); return }
    if (!isAdmin && !n) { toast('请填写新密码', 'error'); return }
    if (n && n.length < 6) { toast('新密码至少 6 位', 'error'); return }
    if (n && n !== n2) { toast('两次新密码不一致', 'error'); return }
    setBusy(true)
    try {
      const body = { old_password: oldP }
      if (isAdmin) body.username = username.trim()
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
    <div className="space-y-5">
      <form onSubmit={submit} className="card p-5 max-w-md space-y-4">
        {isAdmin ? (
          <Field label="用户名">
            <input className="input-field" value={username} onChange={e => setUsername(e.target.value)} required autoComplete="username" />
          </Field>
        ) : (
          <Field label="用户名">
            <div className="text-[14px] font-medium py-1">{user?.username}</div>
          </Field>
        )}
        <Field label="当前密码" hint={isAdmin ? '改用户名或密码都要填写。改完后当前会话仍然有效。' : '改密码需要填写当前密码。改完后当前会话仍然有效。'}>
          <input className="input-field" type="password" value={oldP} onChange={e => setOld(e.target.value)} required autoComplete="current-password" />
        </Field>
        <Field label="新密码" hint={isAdmin ? '留空表示不改密码。至少 6 位。' : '至少 6 位。'}>
          <input className="input-field" type="password" value={n} onChange={e => setN(e.target.value)} autoComplete="new-password" required={!isAdmin} />
        </Field>
        <Field label="确认新密码">
          <input className="input-field" type="password" value={n2} onChange={e => setN2(e.target.value)} autoComplete="new-password" disabled={!n} required={!isAdmin} />
        </Field>
        <button className="btn-primary" disabled={busy}>{busy ? '保存中…' : '保存'}</button>
      </form>
      <TotpBox />
    </div>
  )
}

function AuditPanel() {
  const toast = useToast()
  const [rows, setRows] = useState(null)
  const [sessions, setSessions] = useState([])
  useEffect(() => {
    api.get('/audit').then(d => setRows(d.audit || [])).catch(e => toast(e.message, 'error'))
    api.get('/sessions').then(d => setSessions(d.sessions || [])).catch(() => {})
  }, [])
  return (
    <div className="max-w-3xl space-y-5">
      <div className="card overflow-hidden">
        <div className="px-5 py-4">
          <div className="text-[15px] font-medium">操作记录</div>
          <p className="text-[12.5px] text-ink-mut mt-1">最近 200 条。只读。</p>
        </div>
        {!rows ? (
          <div className="px-5 pb-5 text-[13px] text-ink-mut">加载中…</div>
        ) : rows.length === 0 ? (
          <Empty title="还没有记录" />
        ) : (
          <div className="table-wrap">
            <table className="data">
              <thead><tr><th>时间</th><th>动作</th><th>详情</th></tr></thead>
              <tbody>
                {rows.map(a => (
                  <tr key={a.id}>
                    <td className="text-[12px] whitespace-nowrap">{fmtDate(a.at)}</td>
                    <td className="font-mono text-[12px]">{a.action}</td>
                    <td className="text-[12px] text-ink-mut">{a.detail}</td>
                  </tr>
                ))}
              </tbody>
            </table>
          </div>
        )}
      </div>
      {sessions.length > 0 && (
        <div className="card overflow-hidden">
          <div className="px-5 py-4">
            <div className="text-[15px] font-medium">在线会话</div>
            <p className="text-[12.5px] text-ink-mut mt-1">只读。每个账号当前有效登录数。</p>
          </div>
          <div className="table-wrap">
            <table className="data">
              <thead><tr><th>用户</th><th>会话</th></tr></thead>
              <tbody>
                {sessions.map((s, i) => (
                  <tr key={i}>
                    <td>{s.username || s.user_id}</td>
                    <td className="tabular-nums">{s.count}</td>
                  </tr>
                ))}
              </tbody>
            </table>
          </div>
        </div>
      )}
    </div>
  )
}

function backupLine(s) {
  if (!s) return '正在统计…'
  return `${s.servers} 台实例 · ${s.inbounds} 个节点 · ${s.users} 个用户 · ${s.packages} 个套餐 · ${s.certs} 张证书`
}

function BackupStat({ n, label }) {
  return (
    <div className="px-3 py-2.5" style={{ background: 'var(--color-fill)', borderRadius: 2 }}>
      <div className="text-[16px] font-semibold tabular-nums leading-none">{n ?? '—'}</div>
      <div className="text-[11.5px] text-ink-mut mt-1.5">{label}</div>
    </div>
  )
}

function BackupPanel() {
  const toast = useToast()
  const [live, setLive] = useState(null)
  const [busy, setBusy] = useState(false)
  const [file, setFile] = useState(null)
  const [preview, setPreview] = useState(null)
  const [previewErr, setPreviewErr] = useState('')
  const [reading, setReading] = useState(false)
  const [password, setPassword] = useState('')
  const [filePass, setFilePass] = useState('')
  const [encPass, setEncPass] = useState('')
  const [confirmPass, setConfirmPass] = useState('')
  const [ack, setAck] = useState(false)
  const [restoring, setRestoring] = useState(false)
  const [over, setOver] = useState(false)
  const [sched, setSched] = useState({ backup_hour: '3', backup_keep: 7, backup_password: '', backup_password_set: false })
  const [schedBusy, setSchedBusy] = useState(false)

  const loadLive = () => api.get('/backup/summary').then(setLive).catch(e => toast(e.message, 'error'))
  const loadSched = () => api.get('/settings').then(d => setSched({
    backup_hour: d.backup_hour || '3',
    backup_keep: d.backup_keep || 7,
    backup_password: '',
    backup_password_set: !!d.backup_password_set,
  })).catch(() => {})
  useEffect(() => { loadLive(); loadSched() }, [])

  const download = async () => {
    const confirm = confirmPass.trim()
    if (!confirm) { toast('请填写当前密码', 'error'); return }
    setBusy(true)
    try {
      const pw = encPass.trim()
      await api.downloadPost('/backup', { password: confirm, encrypt_password: pw }, pw ? 'liking-backup.lkb1' : 'liking-backup.lkbak')
      toast('已开始下载')
    } catch (e) { toast(e.message, 'error') }
    finally { setBusy(false) }
  }

  const saveSched = async (e) => {
    e.preventDefault()
    setSchedBusy(true)
    try {
      const body = { backup_hour: String(sched.backup_hour || '3'), backup_keep: Number(sched.backup_keep) || 7 }
      if (sched.backup_password !== '') body.backup_password = sched.backup_password
      await api.put('/settings', body)
      toast('已保存定时备份')
      setSched(x => ({ ...x, backup_password: '' }))
      loadSched()
    } catch (err) { toast(err.message, 'error') }
    finally { setSchedBusy(false) }
  }

  const previewFile = async (f, pass) => {
    const fd = new FormData()
    fd.append('file', f)
    if (pass) fd.append('file_password', pass)
    return api.postForm('/backup/preview', fd)
  }

  const pick = async (f) => {
    setOver(false)
    setFile(f || null)
    setPreview(null)
    setPreviewErr('')
    setAck(false)
    setPassword('')
    setFilePass('')
    if (!f) return
    setReading(true)
    try {
      setPreview(await previewFile(f, ''))
    } catch (e) {
      setPreviewErr(e.message)
    } finally {
      setReading(false)
    }
  }

  const retryPreview = async () => {
    if (!file) return
    setReading(true)
    setPreviewErr('')
    try {
      setPreview(await previewFile(file, filePass))
    } catch (e) {
      setPreviewErr(e.message)
    } finally {
      setReading(false)
    }
  }

  const restore = async () => {
    if (!file || !preview || !password || !ack) return
    setRestoring(true)
    try {
      const fd = new FormData()
      fd.append('file', file)
      fd.append('password', password)
      if (filePass) fd.append('file_password', filePass)
      await api.postForm('/backup/restore', fd)
      toast('已恢复，请用备份里的管理员账号重新登录')
      window.dispatchEvent(new CustomEvent('lk-unauthorized'))
    } catch (e) { toast(e.message, 'error') }
    finally { setRestoring(false) }
  }

  const steps = [
    { n: '1', t: '新机器安装', d: '装好面板，打开设置 → 备份' },
    { n: '2', t: '上传并恢复', d: '覆盖新机数据，会话会退出' },
    { n: '3', t: '核对面板 URL', d: '域名没变可跳过' },
    { n: '4', t: '重装 Agent', d: '令牌没变，用原来的安装命令' },
  ]

  return (
    <div className="max-w-3xl space-y-5">
      <div className="card p-5 space-y-4">
        <div>
          <div className="text-[15px] font-medium">下载备份</div>
          <p className="text-[12.5px] text-ink-mut mt-1 leading-relaxed">
            整份快照：用户和密码哈希、套餐、节点、证书私钥、Cloudflare Token、面板设置和 Agent 令牌。只保存在你自己的电脑上，不要发到聊天软件。
          </p>
        </div>
        <div className="grid grid-cols-2 sm:grid-cols-4 gap-2">
          <BackupStat n={live?.servers} label="实例" />
          <BackupStat n={live?.inbounds} label="节点" />
          <BackupStat n={live?.users} label="用户" />
          <BackupStat n={live?.certs} label="证书" />
        </div>
        <Field label="当前密码" hint="下载备份需要确认身份">
          <input className="input-field max-w-md" type="password" autoComplete="current-password" value={confirmPass} onChange={e => setConfirmPass(e.target.value)} />
        </Field>
        <Field label="加密密码" hint="选填。填写后下载 .lkb1，恢复时要同一密码。">
          <input className="input-field max-w-md" type="password" autoComplete="new-password" value={encPass} onChange={e => setEncPass(e.target.value)} placeholder="留空则不加密" />
        </Field>
        <div className="flex flex-wrap items-center gap-2">
          <button type="button" className="btn-primary" disabled={busy} onClick={download}>
            <Icon name="download" size={15} /> {busy ? '正在打包…' : (encPass.trim() ? '下载加密备份' : '下载备份')}
          </button>
          <span className="text-[12px] text-ink-mut">{encPass.trim() ? '文件名 liking-backup-日期.lkb1' : '文件名 liking-backup-日期.lkbak'}</span>
        </div>
      </div>

      <form onSubmit={saveSched} className="card p-5 space-y-4">
        <div>
          <div className="text-[15px] font-medium">定时备份</div>
          <p className="text-[12.5px] text-ink-mut mt-1 leading-relaxed">
            按面板时区每天写一份到数据目录 backups/，只保留最近 N 份。填 off 关闭。升级面板前安装脚本也会另存一份。
          </p>
        </div>
        <div className="grid grid-cols-1 sm:grid-cols-3 gap-3">
          <Field label="小时" hint="0–23，或 off">
            <input className="input-field" value={sched.backup_hour} onChange={e => setSched({ ...sched, backup_hour: e.target.value })} placeholder="3" />
          </Field>
          <Field label="保留份数">
            <input className="input-field" type="number" min="1" max="30" value={sched.backup_keep} onChange={e => setSched({ ...sched, backup_keep: e.target.value })} />
          </Field>
          <Field label="定时加密密码" hint={sched.backup_password_set ? '已保存。留空不改。' : '可选'}>
            <input className="input-field" type="password" autoComplete="new-password" value={sched.backup_password} onChange={e => setSched({ ...sched, backup_password: e.target.value })} placeholder={sched.backup_password_set ? '••••••••' : ''} />
          </Field>
        </div>
        <button className="btn-primary" disabled={schedBusy}>{schedBusy ? '保存中…' : '保存'}</button>
      </form>

      <div className="card p-5 space-y-4">
        <div>
          <div className="text-[15px] font-medium">恢复 / 迁到新机器</div>
          <p className="text-[12.5px] text-ink-mut mt-1 leading-relaxed">
            只换数据，不换程序。会覆盖本机全部面板数据，且无法撤销。恢复后用备份里的管理员账号登录，新装时打印的密码作废。
          </p>
        </div>
        <div className="grid grid-cols-1 sm:grid-cols-2 gap-2">
          {steps.map(s => (
            <div key={s.n} className="px-3 py-2.5 flex gap-2.5" style={{ background: 'var(--color-fill)', borderRadius: 2 }}>
              <div className="text-[12px] font-medium tabular-nums w-4 shrink-0 pt-px">{s.n}</div>
              <div>
                <div className="text-[13px] font-medium">{s.t}</div>
                <div className="text-[12px] text-ink-mut mt-0.5 leading-relaxed">{s.d}</div>
              </div>
            </div>
          ))}
        </div>

        <label
          className={`dropzone${over ? ' is-over' : ''}`}
          onDragOver={e => { e.preventDefault(); if (!over) setOver(true) }}
          onDragLeave={e => { if (!e.currentTarget.contains(e.relatedTarget)) setOver(false) }}
          onDrop={e => {
            e.preventDefault()
            const f = e.dataTransfer.files && e.dataTransfer.files[0]
            if (f) pick(f)
            else setOver(false)
          }}
        >
          <input
            type="file"
            className="sr-only"
            accept=".lkbak,.lkb1,.gz,.json,application/gzip,application/json,application/octet-stream"
            onChange={e => pick(e.target.files && e.target.files[0])}
          />
          <Icon name="upload" size={18} className="mx-auto mb-2 text-ink-mut" />
          <div className="text-[13px] font-medium">{file ? file.name : '选择备份文件'}</div>
          <div className="text-[12px] text-ink-mut mt-1">
            {file ? fmtBytes(file.size) : '.lkbak / .lkb1，可点此或拖到此处'}
          </div>
        </label>
        {file && (
          <button type="button" className="btn-ghost" onClick={() => pick(null)}>换一个文件</button>
        )}

        {reading ? (
          <div className="text-[13px] text-ink-mut">正在读取备份…</div>
        ) : previewErr ? (
          <div className="space-y-3">
            <div className="notice" style={{ color: 'var(--color-danger)' }}>{previewErr}</div>
            <Field label="备份文件密码" hint="加密备份（.lkb1）需要填写。">
              <input className="input-field" type="password" autoComplete="off" value={filePass} onChange={e => setFilePass(e.target.value)} />
            </Field>
            <button type="button" className="btn-primary" disabled={!filePass} onClick={retryPreview}>用密码打开</button>
          </div>
        ) : preview ? (
          <div className="px-3 py-2.5 space-y-1.5" style={{ background: 'var(--color-fill)', borderRadius: 2 }}>
            <div className="text-[12px] text-ink-mut">当前　{backupLine(live)}</div>
            <div className="text-[13px] font-medium">备份　{backupLine(preview)}</div>
            <div className="text-[12px] text-ink-mut">
              {preview.created_at ? fmtDate(preview.created_at) : '时间未知'}
              {preview.app_version ? ` · 来自 ${preview.app_version}` : ''}
              {preview.has_cf_token ? ' · 含 Cloudflare Token' : ''}
            </div>
          </div>
        ) : null}

        {preview && (
          <div className="space-y-3">
            <Field label="当前管理员密码" hint="确认是你本人。恢复后要改用备份里的密码登录。">
              <input className="input-field" type="password" autoComplete="current-password" value={password} onChange={e => setPassword(e.target.value)} />
            </Field>
            <label className="flex items-start gap-2.5 text-[13px] leading-relaxed cursor-pointer">
              <input type="checkbox" className="mt-1" checked={ack} onChange={e => setAck(e.target.checked)} />
              <span>覆盖本机现有用户、节点、证书和设置，且无法撤销。</span>
            </label>
            <button
              type="button"
              className="btn-danger"
              disabled={restoring || !password || !ack}
              onClick={restore}
            >
              {restoring ? '恢复中…' : '恢复备份'}
            </button>
          </div>
        )}
      </div>
    </div>
  )
}

const settingTabs = [
  { id: 'panel', label: '面板' },
  { id: 'certs', label: '证书' },
  { id: 'backup', label: '备份' },
  { id: 'security', label: '安全' },
  { id: 'account', label: '账号' },
  { id: 'audit', label: '审计' },
]

const TZ_OPTIONS = ['Asia/Shanghai', 'Asia/Hong_Kong', 'Asia/Tokyo', 'Asia/Singapore', 'UTC', 'Europe/London', 'America/New_York']

export default function Settings({ accountOnly = false }) {
  const toast = useToast()
  const dialog = useDialog()
  const { refreshUser } = useUser()
  const [searchParams, setSearchParams] = useSearchParams()
  const requested = searchParams.get('tab')
  const tab = accountOnly ? 'account' : (settingTabs.some(t => t.id === requested) ? requested : 'panel')
  const [f, setF] = useState({
    panel_name: '', panel_url: '', acme_email: '', cf_api_token: '',
    announce: '', admin_cidrs: '', timezone: 'Asia/Shanghai', panel_tls_cert_id: '',
  })
  const [tokenSet, setTokenSet] = useState(false)
  const [tlsActive, setTlsActive] = useState(false)
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
      announce: d.announce || '',
      admin_cidrs: d.admin_cidrs || '',
      timezone: d.timezone || 'Asia/Shanghai',
      panel_tls_cert_id: d.panel_tls_cert_id || '',
    })
    setTokenSet(!!d.cf_api_token_set)
    setTlsActive(!!d.tls_active)
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
      const data = await api.put('/settings', {
        panel_name: f.panel_name,
        panel_url: f.panel_url,
        announce: f.announce,
        timezone: f.timezone,
        panel_tls_cert_id: f.panel_tls_cert_id,
      })
      const needTLS = !!f.panel_tls_cert_id && !data?.tls_active
      toast(needTLS ? '已保存。选择证书后请重启 liking-server 才会启用 HTTPS' : '已保存')
      refreshUser()
      loadSettings()
    } catch (e) { toast(e.message, 'error') }
    finally { setBusy(false) }
  }

  const saveSecurity = async (e) => {
    e.preventDefault()
    setBusy(true)
    try {
      await api.put('/settings', { admin_cidrs: f.admin_cidrs })
      toast('已保存')
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

  if (!accountOnly && requested === 'subscribe') return <Navigate to="/subscribe" replace />

  return (
    <div>
      <PageHead title="设置" />
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
          <Field label="时区" hint="流量按日、定时备份按此时区。默认 Asia/Shanghai。">
            <select className="input-field" value={TZ_OPTIONS.includes(f.timezone) ? f.timezone : f.timezone} onChange={e => setF({ ...f, timezone: e.target.value })}>
              {!TZ_OPTIONS.includes(f.timezone) && f.timezone ? <option value={f.timezone}>{f.timezone}</option> : null}
              {TZ_OPTIONS.map(z => <option key={z} value={z}>{z}</option>)}
            </select>
          </Field>
          <Field label="面板 HTTPS 证书" hint={tlsActive ? '当前已启用 HTTPS。改证书后需重启 liking-server。' : '选好后重启 liking-server 才会启用 HTTPS / WSS。'}>
            <select className="input-field" value={f.panel_tls_cert_id} onChange={e => setF({ ...f, panel_tls_cert_id: e.target.value })}>
              <option value="">不启用（明文 HTTP）</option>
              {certs.map(c => (
                <option key={c.id} value={String(c.id)}>{c.name} · {c.domains || '无域名'}</option>
              ))}
            </select>
          </Field>
        </div>
        <Field label="公告" hint="登录后所有账号顶部可见。留空则不显示。">
          <textarea className="input-field min-h-[88px]" value={f.announce} onChange={e => setF({ ...f, announce: e.target.value })} placeholder="维护通知、套餐说明…" />
        </Field>
        <button className="btn-primary" disabled={busy}>{busy ? '保存中…' : '保存'}</button>
      </form>
      )}

      {tab === 'certs' && (
      <>
      <form onSubmit={saveCF} className="card p-5 max-w-3xl space-y-4">
        <div className="text-[15px] font-medium">Cloudflare / Let&apos;s Encrypt</div>
        <p className="text-[12.5px] text-ink-mut leading-relaxed">
          Token 权限：Zone · Zone · Read，Zone · DNS · Edit。同一 Token 也用来在实例页拉取已托管域名。域名不必指向本面板，也不用开放 80 端口。
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
                    <td className="copy-text max-w-[16rem]">{c.domains || '—'}</td>
                    <td><Badge tone={expiryTone(c.expires_at)}>{expiryText(c.expires_at)}</Badge></td>
                    <td className="whitespace-nowrap">
                      <div className="icon-row">
                        <MoreMenu iconOnly items={[
                          c.source === 'acme-cf' ? {
                            label: renewing === c.id ? '续期中…' : '续期',
                            disabled: renewing === c.id,
                            onSelect: () => renew(c.id),
                          } : null,
                          { label: '删除', danger: true, onSelect: () => del(c.id) },
                        ]} />
                      </div>
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

      {tab === 'backup' && <BackupPanel />}

      {tab === 'security' && (
      <form onSubmit={saveSecurity} className="card p-5 max-w-3xl space-y-4">
        <div>
          <div className="text-[15px] font-medium">管理员 IP 白名单</div>
          <p className="text-[12.5px] text-ink-mut mt-1 leading-relaxed">
            逗号或换行分隔 CIDR / IP。留空不限制。写上之后，不在名单里的地址不能登录管理员，也不能调管理接口。先把自己的 IP 写进去。
          </p>
        </div>
        <Field label="CIDR">
          <textarea className="input-field font-mono text-[12px] min-h-[88px]" value={f.admin_cidrs} onChange={e => setF({ ...f, admin_cidrs: e.target.value })} placeholder="127.0.0.1&#10;10.0.0.0/8" />
        </Field>
        <button className="btn-primary" disabled={busy}>{busy ? '保存中…' : '保存'}</button>
      </form>
      )}

      {tab === 'account' && <AccountForm />}

      {tab === 'audit' && <AuditPanel />}

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
                className={`node-pick ${issue.channel === ch.id ? 'is-on' : ''}`}
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
