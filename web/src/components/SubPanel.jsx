import { useEffect, useState } from 'react'
import QRCode from 'qrcode'
import { copyText } from '../lib/copy'
import { Icon } from './ui'

export function subURL(token, fmt) {
  const base = `${window.location.origin}/api/sub/${token}`
  if (fmt) return `${base}/${fmt}`
  return base
}

export function clashImportURL(token) {
  return `clash://install-config?url=${encodeURIComponent(subURL(token, 'clash'))}`
}

export function singboxImportURL(token) {
  return `sing-box://import-remote-profile?url=${encodeURIComponent(subURL(token, 'singbox'))}`
}

export function SubPanel({ token, onCopied }) {
  const [qr, setQr] = useState('')
  const auto = subURL(token)
  const rows = [
    ['自动识别', auto],
    ['Clash Meta', subURL(token, 'clash')],
    ['sing-box', subURL(token, 'singbox')],
    ['URI / 通用', subURL(token, 'uri')],
  ]

  useEffect(() => {
    if (!token) return
    const dark = document.documentElement.classList.contains('dark')
    QRCode.toDataURL(auto, {
      width: 280, margin: 1,
      color: dark ? { dark: '#fafafa', light: '#111113' } : { dark: '#000000', light: '#ffffff' },
    }).then(setQr).catch(() => setQr(''))
  }, [token, auto])

  const copy = async (v, ok = '已复制') => {
    try {
      await copyText(v)
      onCopied?.(ok)
    } catch {
      onCopied?.('浏览器不允许自动复制，请手动选中链接', 'error')
    }
  }

  return (
    <div className="flex flex-col md:flex-row gap-5">
      {qr && (
        <div className="shrink-0 text-center">
          <img src={qr} alt="订阅二维码" className="w-[148px] h-[148px] border mx-auto" style={{ borderColor: 'var(--color-line)', borderRadius: 2 }} />
          <div className="text-[11px] text-ink-mut mt-2">扫码导入</div>
        </div>
      )}
      <div className="flex-1 space-y-3 min-w-0">
        {rows.map(([k, v]) => (
          <div key={k}>
            <div className="kicker mb-1">{k}</div>
            <div className="flex gap-2 items-center">
              <code className="text-[12px] break-all flex-1 font-mono">{v}</code>
              <button type="button" className="btn-ghost h-8 shrink-0" onClick={() => copy(v)}><Icon name="copy" size={14} /> 复制</button>
            </div>
          </div>
        ))}
        <div className="flex flex-wrap gap-2 pt-1">
          <button type="button" className="btn-primary h-8" onClick={() => copy(subURL(token, 'clash'), '已复制 Clash 订阅')}>复制 Clash 订阅</button>
          <a className="btn-ghost h-8" href={clashImportURL(token)}>打开 Clash</a>
          <a className="btn-ghost h-8" href={singboxImportURL(token)}>打开 sing-box</a>
          <button type="button" className="btn-ghost h-8" onClick={() => copy(auto, '已复制自动识别链接')}><Icon name="link" size={14} /> 复制自动识别</button>
        </div>
        <p className="text-[12px] text-ink-mut">HTTP 打开面板时浏览器可能禁止剪贴板，直接选中链接也能复制。</p>
      </div>
    </div>
  )
}
