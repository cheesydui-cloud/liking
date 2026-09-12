import { useEffect } from 'react'
import { copyText } from '../lib/copy'
import { Icon, Modal } from './ui'
import { clashImportURL, singboxImportURL, subURL } from './SubPanel'

const NODE_URI_OK = new Set([
  'vless-reality', 'vless-reality-vision', 'vless-xhttp-tls', 'trojan-tls', 'ss2022',
])

export function ShareModal({ share, onClose, onToast }) {
  const token = share?.sub_token || ''
  const auto = token ? subURL(token) : ''
  const clash = token ? subURL(token, 'clash') : ''
  const nodeOK = share && NODE_URI_OK.has(share.profile)

  useEffect(() => {
    if (!auto) return
    let gone = false
    copyText(auto).then(() => {
      if (gone) return
      if (share.profile === 'mieru') onToast?.('已复制通用订阅。Mieru 请用 Clash Meta / Mihomo 添加该订阅')
      else onToast?.('已复制通用订阅链接')
    }).catch(() => {})
    return () => { gone = true }
  }, [auto, share?.profile])

  const copy = async (v, ok) => {
    try {
      await copyText(v)
      onToast?.(ok)
    } catch {
      onToast?.('浏览器不允许自动复制，请手动选中链接', 'error')
    }
  }

  return (
    <Modal open={!!share} title={share ? `导入客户端 · ${share.username}` : '导入客户端'} onClose={onClose} size="lg" footer={
      <button type="button" className="btn-ghost" onClick={onClose}>关闭</button>
    }>
      {share && (
        <div className="space-y-4">
          <p className="text-[12px] text-ink-mut">多数软件认的是下面这条 HTTP 订阅，不是节点参数。Clash / v2rayN / 小火箭 / sing-box 里选「添加订阅」粘贴即可。</p>
          <ShareRow kicker="通用订阅" value={auto} hint="按客户端自动识别格式" onCopy={() => copy(auto, '已复制通用订阅链接')} />
          <ShareRow kicker="Clash Meta" value={clash} hint="Mieru / AnyTLS 用这个" onCopy={() => copy(clash, '已复制 Clash 订阅')} />
          {nodeOK ? (
            <ShareRow kicker="节点分享" value={share.uri} hint="v2rayN / 小火箭 / Nekobox 从剪贴板导入单节点" onCopy={() => copy(share.uri, '已复制节点链接')} />
          ) : (
            <div>
              <div className="kicker mb-1">节点分享</div>
              <p className="text-[12px] text-ink-mut">
                {share.profile === 'mieru'
                  ? 'Mieru 没有通用单节点链接，v2rayN 也不支持该协议。请用 Clash Meta 订阅。'
                  : '该协议没有多数软件都认的单节点链接，请用上面的订阅。'}
              </p>
            </div>
          )}
          <div className="flex flex-wrap gap-2 pt-1">
            <a className="btn-ghost h-8" href={clashImportURL(token)}>打开 Clash</a>
            <a className="btn-ghost h-8" href={singboxImportURL(token)}>打开 sing-box</a>
          </div>
        </div>
      )}
    </Modal>
  )
}

function ShareRow({ kicker, value, hint, onCopy }) {
  return (
    <div>
      <div className="kicker mb-1">{kicker}</div>
      <div className="flex gap-2 items-start">
        <code className="text-[12px] break-all flex-1 font-mono">{value}</code>
        <button type="button" className="btn-ghost h-8 shrink-0" onClick={onCopy}>
          <Icon name="copy" size={14} /> 复制
        </button>
      </div>
      {hint ? <p className="text-[11px] text-ink-mut mt-1">{hint}</p> : null}
    </div>
  )
}
