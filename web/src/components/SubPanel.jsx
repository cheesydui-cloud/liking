import { useEffect, useState } from 'react'
import QRCode from 'qrcode'
import { copyText } from '../lib/copy'
import { FilterTabs } from './ui'
import { useUser } from './Layout'

const CLIENT_KEY = 'liking.sub.client'
const CLIENTS = [
  ['clash', 'Clash Meta'],
  ['singbox', 'sing-box'],
  ['rocket', '小火箭'],
  ['v2rayn', 'v2rayN'],
  ['auto', '自动识别'],
]

function loadClient() {
  try {
    const v = localStorage.getItem(CLIENT_KEY)
    if (CLIENTS.some(c => c[0] === v)) return v
  } catch { /* private mode */ }
  return 'clash'
}

export function panelBase(panelUrl) {
  const raw = String(panelUrl || '').trim().replace(/\/+$/, '')
  if (/^https?:\/\//i.test(raw)) return raw
  if (typeof window !== 'undefined') return window.location.origin
  return ''
}

export function subURL(token, fmt, base) {
  const origin = panelBase(base)
  const path = `${origin}/api/sub/${token}`
  if (fmt) return `${path}/${fmt}`
  return path
}

export function clashImportURL(token, base) {
  return `clash://install-config?url=${encodeURIComponent(subURL(token, 'clash', base))}`
}

export function singboxImportURL(token, base) {
  return `sing-box://import-remote-profile?url=${encodeURIComponent(subURL(token, 'singbox', base))}`
}

export function toBase64(str) {
  const bytes = new TextEncoder().encode(str)
  let bin = ''
  for (let i = 0; i < bytes.length; i++) bin += String.fromCharCode(bytes[i])
  return btoa(bin)
}

export function shadowrocketQRPayload(token, base) {
  return `sub://${toBase64(subURL(token, 'uri', base))}`
}

export function shadowrocketImportURL(token, base) {
  return `shadowrocket://add/sub://${toBase64(subURL(token, 'uri', base))}`
}

export function maskSubURL(url) {
  const s = String(url || '')
  if (!s) return ''
  if (s.startsWith('sub://')) return 'sub://••••'
  const masked = s.replace(/(\/api\/sub\/)[^/?#]+/, '$1••••')
  return masked === s ? '••••' : masked
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

function clientBundle(id, token, base) {
  const uri = subURL(token, 'uri', base)
  if (id === 'singbox') {
    return {
      label: 'sing-box',
      link: subURL(token, 'singbox', base),
      qr: subURL(token, 'singbox', base),
      open: singboxImportURL(token, base),
      openLabel: '打开 sing-box',
      download: ['singbox', 'liking.json', '下载 sing-box'],
      copied: '已复制 sing-box 订阅',
    }
  }
  if (id === 'rocket') {
    return {
      label: '小火箭',
      link: uri,
      qr: shadowrocketQRPayload(token, base),
      open: shadowrocketImportURL(token, base),
      openLabel: '打开小火箭',
      copied: '已复制小火箭订阅',
    }
  }
  if (id === 'v2rayn') {
    return {
      label: 'v2rayN',
      link: uri,
      qr: uri,
      copied: '已复制 v2rayN 订阅',
    }
  }
  if (id === 'auto') {
    return {
      label: '自动识别',
      link: subURL(token, '', base),
      qr: subURL(token, '', base),
      copied: '已复制自动识别链接',
    }
  }
  return {
    label: 'Clash Meta',
    link: subURL(token, 'clash', base),
    qr: subURL(token, 'clash', base),
    open: clashImportURL(token, base),
    openLabel: '打开 Clash',
    download: ['clash', 'liking.yaml', '下载 Clash'],
    copied: '已复制 Clash 订阅',
  }
}

export function SubPanel({ token, onCopied, onRotate, rotating }) {
  const { panelUrl } = useUser() || {}
  const base = panelBase(panelUrl)
  const [client, setClient] = useState(loadClient)
  const [show, setShow] = useState(false)
  const [downloading, setDownloading] = useState('')
  const pack = clientBundle(client, token, base)

  const pick = (id) => {
    setClient(id)
    try { localStorage.setItem(CLIENT_KEY, id) } catch { /* private mode */ }
  }

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

  const shown = show ? pack.link : maskSubURL(pack.link)
  return (
    <div>
      <div className="flex flex-col sm:flex-row sm:items-center sm:justify-between gap-2 mb-4">
        <FilterTabs value={client} onChange={pick} items={CLIENTS} />
        {onRotate ? (
          <button type="button" className="btn-ghost h-8 shrink-0" disabled={rotating} onClick={onRotate}>
            {rotating ? '重置中…' : '重置令牌'}
          </button>
        ) : null}
      </div>
      <div className="flex flex-col sm:flex-row gap-4 min-w-0">
        <QRBlock label={pack.label} value={pack.qr} />
        <div className="min-w-0 flex-1">
          <div className="kicker mb-1">{pack.label}</div>
          <div className="sub-link-row flex gap-2 items-start">
            <code className="copy-text copy-text-wrap flex-1">{shown}</code>
            <button type="button" className="btn-ghost h-8 shrink-0" onClick={() => setShow(v => !v)}>{show ? '隐藏' : '显示'}</button>
            <button type="button" className="btn-primary h-8 shrink-0" onClick={() => copy(pack.link, pack.copied)}>复制</button>
          </div>
          <div className="text-[12px] text-ink-mut mt-2">复制拿到完整地址。显示只用来核对。</div>
          <div className="flex flex-wrap gap-2 mt-3">
            {pack.open ? <a className="btn-ghost h-8" href={pack.open}>{pack.openLabel}</a> : null}
            {pack.download ? (
              <button
                type="button"
                className="btn-ghost h-8"
                disabled={!!downloading}
                onClick={() => download(pack.download[0], pack.download[1])}
              >
                {downloading === pack.download[0] ? '下载中…' : pack.download[2]}
              </button>
            ) : null}
          </div>
        </div>
      </div>
    </div>
  )
}
