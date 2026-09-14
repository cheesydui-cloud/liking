import { useEffect, useState } from 'react'
import { useUser, useToast } from '../components/Layout'
import { api } from '../lib/api'
import { copyText } from '../lib/copy'
import { DayBars, Icon, Meter, Modal, PageHead, billedBytes, fmtDate } from '../components/ui'
import { SubPanel } from '../components/SubPanel'

export default function My() {
  const { user, sub, refreshUser } = useUser()
  const toast = useToast()
  const [traffic, setTraffic] = useState(null)
  const [nodes, setNodes] = useState(null)
  const [shareText, setShareText] = useState('')
  const isAdmin = user?.role === 'admin'
  useEffect(() => { refreshUser() }, [refreshUser])
  useEffect(() => {
    api.get('/me/traffic?days=14').then(setTraffic).catch(() => {})
    api.get('/me/nodes').then(setNodes).catch(() => {})
  }, [])
  const used = billedBytes(user)
  const cap = user?.traffic_cap || 0
  const ratio = cap > 0 ? Math.min(100, Math.round(used * 100 / cap)) : 0

  const copyNode = async (n) => {
    if (!n?.uri) {
      toast('这个节点还没有分享链接', 'error')
      return
    }
    try {
      await copyText(n.uri)
      toast('已复制节点链接')
    } catch {
      setShareText(n.uri)
      toast('浏览器不允许自动复制，请手动选中链接', 'error')
    }
  }

  return (
    <div>
      <PageHead title="我的订阅" desc={isAdmin
        ? '包含全部节点。用管理员自己的身份，不占用用户额度。'
        : '把链接导入 Clash Meta、sing-box 或通用客户端，也可以扫码。流量按 GiB（1024³ 字节）计。'} />
      <div className="stat-row">
        <div>
          <div className="kicker">账号</div>
          <span className="stat-val">{user?.username}</span>
          <div className="text-[12px] text-ink-mut mt-1">{user?.package_name || (isAdmin ? '全部节点' : '未分配套餐')}</div>
        </div>
        <div>
          <div className="kicker">流量{user?.direction === 'twoway' ? ' / 双向' : ''}</div>
          <Meter className="mt-2" value={used} max={user?.traffic_cap || 0} />
          {ratio >= 80 ? (
            <div className="text-[12px] mt-1.5" style={{ color: ratio >= 100 ? 'var(--color-danger)' : 'var(--color-warn)' }}>
              {ratio >= 100 ? '已用尽，节点已从订阅摘掉' : `已用 ${ratio}%`}
            </div>
          ) : null}
        </div>
        <div>
          <div className="kicker">到期</div>
          <span className="stat-val">{user?.expires_at ? fmtDate(user.expires_at) : '—'}</span>
          {user?.expires_at && user.expires_at * 1000 < Date.now() ? (
            <div className="text-[12px] mt-1" style={{ color: 'var(--color-danger)' }}>已到期，节点已从订阅摘掉</div>
          ) : null}
        </div>
      </div>
      {!isAdmin && !user?.package_id && (
        <div className="alert-row is-warn mb-4">还没有套餐，订阅里不会有节点。请联系管理员绑定。</div>
      )}
      {(nodes?.nodes || []).length > 0 && (
        <div className="card overflow-hidden mb-4">
          <div className="panel-head">可用节点</div>
          <div className="table-wrap">
            <table className="data">
              <thead><tr><th>名称</th><th>地址</th><th>协议</th><th></th></tr></thead>
              <tbody>
                {nodes.nodes.map((n, i) => (
                  <tr key={n.id || i}>
                    <td className="font-medium">{n.name}</td>
                    <td className="copy-text">{n.host}:{n.port}</td>
                    <td className="text-[12px] text-ink-mut">{n.profile}</td>
                    <td className="text-right whitespace-nowrap w-px">
                      <button type="button" className="row-act" disabled={!n.uri} onClick={() => copyNode(n)}>复制</button>
                    </td>
                  </tr>
                ))}
              </tbody>
            </table>
          </div>
        </div>
      )}
      {traffic?.days?.length ? (
        <div className="mb-4">
          <div className="text-[14px] font-semibold mb-3">近 14 日</div>
          <DayBars days={traffic.days} />
        </div>
      ) : null}
      {sub && user?.sub_token ? (
        <div className="card p-5">
          <SubPanel token={user.sub_token} onCopied={(msg, kind) => toast(msg, kind)} />
        </div>
      ) : null}
      <Modal open={!!shareText} title="节点链接" onClose={() => setShareText('')} footer={
        <>
          <button type="button" className="btn-ghost" onClick={() => setShareText('')}>关闭</button>
          <button type="button" className="btn-primary" onClick={async () => {
            try { await copyText(shareText); toast('已复制节点链接') }
            catch { toast('请手动选中复制', 'error') }
          }}>
            <Icon name="copy" size={15} /> 复制
          </button>
        </>
      }>
        <p className="text-[12px] text-ink-mut mb-2">粘贴到小火箭 / v2rayN / Nekobox 即可导入。</p>
        <code className="block text-[12px] break-all font-mono p-3" style={{ background: 'var(--color-fill)' }}>{shareText}</code>
      </Modal>
    </div>
  )
}
