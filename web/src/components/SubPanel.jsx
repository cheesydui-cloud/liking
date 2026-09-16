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
      color: dark ? { dark: '#F4F4F5', light: '#18181B' } : { dark: '#242424', light: '#F6F6F4' },
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
          <img src={qr} alt="订阅二维码" className="w-[148px] h-[148px] border mx-auto" style={{ borderColor: 'var(--color-line)', borderRadius: 10 }} />
          <div className="text-[11px] text-ink-mut mt-2">扫码导入</div>
        </div>
      )}
      <div className="flex-1 space-y-3 min-w-0">
        {rows.map(([k, v]) => (
          <div key={k}>
            <div className="kicker mb-1">{k}</div>
            <div className="flex gap-2 items-center">
              <code className="copy-text copy-text-wrap flex-1">{v}</code>
              <button type="button" className="icon-btn" onClick={() => copy(v)} aria-label={`复制${k}`} title="复制">
                <Icon name="copy" size={14} />
              </button>
            </div>
          </div>
        ))}
        <div className="flex flex-wrap gap-2 pt-1">
          <button type="button" className="btn-primary h-8" onClick={() => copy(subURL(token, 'clash'), '已复制 Clash 订阅')}>复制 Clash 订阅</button>
          <a className="btn-ghost h-8" href={clashImportURL(token)}>打开 Clash</a>
          <a className="btn-ghost h-8" href={singboxImportURL(token)}>打开 sing-box</a>
          <button type="button" className="btn-ghost h-8" onClick={() => copy(auto, '已复制自动识别链接')}><Icon name="link" size={14} /> 复制自动识别</button>
        </div>
      </div>
    </div>
  )
}
