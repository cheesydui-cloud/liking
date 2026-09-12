import { useEffect } from 'react'
import { useUser, useToast } from '../components/Layout'
import { Meter, PageHead, fmtDate } from '../components/ui'
import { SubPanel } from '../components/SubPanel'

export default function My() {
  const { user, sub, refreshUser } = useUser()
  const toast = useToast()
  useEffect(() => { refreshUser() }, [refreshUser])
  const used = (user?.used_up || 0) + (user?.used_down || 0)

  return (
    <div>
      <PageHead kicker="Membership" title="我的订阅" desc="把链接导入 Clash Meta、sing-box 或通用客户端，也可以扫码。" />
      <div className="grid md:grid-cols-3 gap-3 mb-5">
        <div className="card p-5">
          <div className="kicker">账号</div>
          <div className="font-display text-[28px] mt-2">{user?.username}</div>
          <div className="text-[13px] text-ink-mut mt-1">{user?.package_name || '未分配套餐'}</div>
        </div>
        <div className="card p-5">
          <div className="kicker">流量</div>
          <Meter className="mt-3" value={used} max={user?.traffic_cap || 0} />
        </div>
        <div className="card p-5">
          <div className="kicker">到期</div>
          <div className="font-display text-[22px] mt-2">{user?.expires_at ? fmtDate(user.expires_at) : '—'}</div>
          {user?.expires_at && user.expires_at * 1000 < Date.now() ? (
            <div className="text-[12px] mt-1" style={{ color: 'var(--color-danger)' }}>已到期，节点已从订阅摘掉</div>
          ) : null}
        </div>
      </div>
      {!user?.package_id && (
        <div className="notice mb-5">还没有套餐，订阅里不会有节点。请联系管理员绑定。</div>
      )}
      {sub && user?.sub_token ? (
        <div className="card p-6">
          <SubPanel token={user.sub_token} onCopied={(msg, kind) => toast(msg, kind)} />
        </div>
      ) : null}
    </div>
  )
}
