import { useEffect, useState } from 'react'
import { useUser, useToast } from '../components/Layout'
import { api } from '../lib/api'
import { DayBars, Meter, PageHead, billedBytes, fmtDate } from '../components/ui'
import { SubPanel } from '../components/SubPanel'

export default function My() {
  const { user, sub, refreshUser } = useUser()
  const toast = useToast()
  const [traffic, setTraffic] = useState(null)
  useEffect(() => { refreshUser() }, [refreshUser])
  useEffect(() => {
    api.get('/me/traffic?days=14').then(setTraffic).catch(() => {})
  }, [])
  const used = billedBytes(user)

  return (
    <div>
      <PageHead title="我的订阅" desc="把链接导入 Clash Meta、sing-box 或通用客户端，也可以扫码。" />
      <div className="grid md:grid-cols-3 gap-3 mb-4">
        <div className="card p-4">
          <div className="kicker">账号</div>
          <div className="text-[18px] font-semibold mt-1.5">{user?.username}</div>
          <div className="text-[13px] text-ink-mut mt-1">{user?.package_name || '未分配套餐'}</div>
        </div>
        <div className="card p-4">
          <div className="kicker">流量{user?.direction === 'twoway' ? ' · 双向' : ''}</div>
          <Meter className="mt-2.5" value={used} max={user?.traffic_cap || 0} />
        </div>
        <div className="card p-4">
          <div className="kicker">到期</div>
          <div className="text-[15px] font-semibold mt-1.5">{user?.expires_at ? fmtDate(user.expires_at) : '—'}</div>
          {user?.expires_at && user.expires_at * 1000 < Date.now() ? (
            <div className="text-[12px] mt-1" style={{ color: 'var(--color-danger)' }}>已到期，节点已从订阅摘掉</div>
          ) : null}
        </div>
      </div>
      {!user?.package_id && (
        <div className="notice mb-4">还没有套餐，订阅里不会有节点。请联系管理员绑定。</div>
      )}
      {traffic?.days?.length ? (
        <div className="card p-4 mb-4">
          <div className="text-[14px] font-semibold mb-3">近 14 日</div>
          <DayBars days={traffic.days} />
        </div>
      ) : null}
      {sub && user?.sub_token ? (
        <div className="card p-5">
          <SubPanel token={user.sub_token} onCopied={(msg, kind) => toast(msg, kind)} />
        </div>
      ) : null}
    </div>
  )
}
