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

export function toBase64(str) {
  const bytes = new TextEncoder().encode(str)
  let bin = ''
  for (let i = 0; i < bytes.length; i++) bin += String.fromCharCode(bytes[i])
  return btoa(bin)
}

export function shadowrocketQRPayload(token) {
  return `sub://${toBase64(subURL(token, 'uri'))}`
}

export function shadowrocketImportURL(token) {
  return `shadowrocket://add/sub://${toBase64(subURL(token, 'uri'))}`
}

function qrOpts() {
  const dark = document.documentElement.classList.contains('dark')
  return {
    width: 280,
    margin: 1,
    color: dark ? { dark: '#F4F4F5', light: '#18181B' } : { dark: '#242424', light: '#F6F6F4' },
  }
}

function QRBlock({ label, value }) {
  const [src, setSrc] = useState('')
  const [failed, setFailed] = useState(false)
  useEffect(() => {
    if (!value) { setSrc(''); setFailed(false); return }
    let live = true
    setFailed(false)
    QRCode.toDataURL(value, qrOpts()).then(url => {
      if (live) setSrc(url)
    }).catch(() => {
      if (!live) return
      setSrc('')
      setFailed(true)
    })
    return () => { live = false }
  }, [value])
  if (failed) {
    return (
      <div className="shrink-0 text-center w-[148px]">
        <div className="text-[12px]" style={{ color: 'var(--color-danger)' }}>二维码生成失败</div>
        <div className="text-[11px] text-ink-mut mt-2">{label}</div>
      </div>
    )
  }
  if (!src) return null
  return (
    <div className="shrink-0 text-center">
      <img src={src} alt={`${label}二维码`} className="w-[148px] h-[148px] border mx-auto" style={{ borderColor: 'var(--color-line)', borderRadius: 10 }} />
      <div className="text-[11px] text-ink-mut mt-2">{label}</div>
    </div>
  )
}

export function SubPanel({ token, onCopied, onRotate, rotating }) {
  const [downloading, setDownloading] = useState('')
  const auto = subURL(token)
  const uri = subURL(token, 'uri')
  const rocket = shadowrocketQRPayload(token)
  const rows = [
    ['自动识别', auto],
    ['Clash Meta', subURL(token, 'clash')],
    ['sing-box', subURL(token, 'singbox')],
    ['小火箭', rocket],
    ['v2rayN', uri],
  ]

  const copy = async (v, ok = '已复制') => {
    try {
      await copyText(v)
      onCopied?.(ok)
    } catch {
      onCopied?.('浏览器不允许自动复制，请手动选中链接', 'error')
    }
  }

  const download = async (fmt, filename) => {
    if (!token || downloading) return
    setDownloading(fmt)
    try {
      const res = await fetch(subURL(token, fmt), { credentials: 'same-origin' })
      if (!res.ok) throw new Error('下载失败')
      const blob = await res.blob()
      const a = document.createElement('a')
      a.href = URL.createObjectURL(blob)
      a.download = filename
      document.body.appendChild(a)
      a.click()
      a.remove()
      URL.revokeObjectURL(a.href)
      onCopied?.('已开始下载')
    } catch {
      onCopied?.('下载失败', 'error')
    } finally {
      setDownloading('')
    }
  }

  return (
    <div className="flex flex-col md:flex-row gap-5">
      <div className="flex flex-wrap gap-4 shrink-0 justify-center md:justify-start">
        <QRBlock label="扫码导入" value={auto} />
        <QRBlock label="小火箭" value={shadowrocketQRPayload(token)} />
        <QRBlock label="v2rayN" value={uri} />
      </div>
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
          <a className="btn-ghost h-8" href={shadowrocketImportURL(token)}>打开小火箭</a>
          <button type="button" className="btn-ghost h-8" onClick={() => copy(uri, '已复制 v2rayN 订阅')}>复制 v2rayN 订阅</button>
          <button type="button" className="btn-ghost h-8" disabled={!!downloading} onClick={() => download('clash', 'liking.yaml')}>
            {downloading === 'clash' ? '下载中…' : '下载 Clash'}
          </button>
          <button type="button" className="btn-ghost h-8" disabled={!!downloading} onClick={() => download('singbox', 'liking.json')}>
            {downloading === 'singbox' ? '下载中…' : '下载 sing-box'}
          </button>
          <button type="button" className="btn-ghost h-8" onClick={() => copy(auto, '已复制自动识别链接')}><Icon name="link" size={14} /> 复制自动识别</button>
          {onRotate ? (
            <button type="button" className="btn-ghost h-8" disabled={rotating} onClick={onRotate}>
              {rotating ? '重置中…' : '重置令牌'}
            </button>
          ) : null}
        </div>
      </div>
    </div>
  )
}
