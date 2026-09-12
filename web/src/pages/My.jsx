import { useEffect, useState } from 'react'
import { copyText } from '../lib/copy'
import { useUser, useToast } from '../components/Layout'
import { Icon, PageHead, fmtBytes, fmtDate } from '../components/ui'
import QRCode from 'qrcode'

export default function My() {
  const { user, sub, refreshUser } = useUser()
  const toast = useToast()
  const [qr, setQr] = useState('')
  useEffect(() => { refreshUser() }, [refreshUser])
  useEffect(() => {
    if (!sub?.auto) return
    const dark = document.documentElement.classList.contains('dark')
    QRCode.toDataURL(sub.auto, {
      width: 280, margin: 1,
      color: dark ? { dark: '#f4efe6', light: '#14120f' } : { dark: '#1c1910', light: '#fffaf1' },
    }).then(setQr).catch(() => {})
  }, [sub])

  const copy = async (t) => {
    try {
      await copyText(t)
      toast('已复制')
    } catch {
      toast('浏览器不允许自动复制，请手动选中链接', 'error')
    }
  }
  const used = (user?.used_up || 0) + (user?.used_down || 0)

  return (
    <div>
      <PageHead kicker="Membership" title="我的订阅" desc="把链接导入 Clash Meta、sing-box 或通用客户端。" />
      <div className="grid md:grid-cols-3 gap-3 mb-5">
        <div className="card p-5">
          <div className="kicker">账号</div>
          <div className="font-display text-[28px] mt-2">{user?.username}</div>
          <div className="text-[13px] text-ink-mut mt-1">{user?.package_name || '未分配套餐'}</div>
        </div>
        <div className="card p-5">
          <div className="kicker">已用</div>
          <div className="font-display text-[28px] mt-2 tabular-nums">{fmtBytes(used)}</div>
        </div>
        <div className="card p-5">
          <div className="kicker">到期</div>
          <div className="font-display text-[22px] mt-2">{user?.expires_at ? fmtDate(user.expires_at) : '—'}</div>
        </div>
      </div>
      {sub && (
        <div className="card p-6 flex flex-col md:flex-row gap-8">
          {qr && (
            <div className="shrink-0">
              <img src={qr} alt="订阅二维码" className="w-[180px] h-[180px] rounded-xl border" style={{ borderColor: 'var(--color-line)' }} />
              <div className="text-[11px] text-ink-mut text-center mt-2">自动识别</div>
            </div>
          )}
          <div className="flex-1 space-y-4 min-w-0">
            {[['Clash Meta', sub.clash], ['sing-box', sub.singbox], ['URI / 通用', sub.uri], ['自动识别', sub.auto]].map(([k, v]) => (
              <div key={k}>
                <div className="kicker mb-1">{k}</div>
                <div className="flex gap-2 items-center">
                  <code className="text-[12px] break-all flex-1 font-mono">{v}</code>
                  <button type="button" className="btn-ghost h-9" onClick={() => copy(v)}><Icon name="copy" size={14} /> 复制</button>
                </div>
              </div>
            ))}
          </div>
        </div>
      )}
    </div>
  )
}
