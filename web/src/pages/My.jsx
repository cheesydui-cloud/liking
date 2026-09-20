import { useEffect, useState } from 'react'
import { Link } from 'react-router-dom'
import { useUser, useToast, useDialog } from '../components/Layout'
import { api } from '../lib/api'
import { cacheGen, peekList, putList } from '../lib/listCache'
import { startPoll } from '../lib/poll'
import { asArray, isAbort } from '../lib/safe'
import { DayBars, Meter, PageHead, billedBytes, fmtBytes, fmtDateShort } from '../components/ui'
import { SubPanel } from '../components/SubPanel'

function sumDays(days) {
  return asArray(days).reduce((n, d) => n + (Number(d.up) || 0) + (Number(d.down) || 0), 0)
}

export default function My() {
  const { user, sub, refreshUser } = useUser()
  const toast = useToast()
  const dialog = useDialog()
  const [rotating, setRotating] = useState(false)
  const cached = peekList('me-nodes')
  const [traffic, setTraffic] = useState(null)
  const [payload, setPayload] = useState(() => cached ?? { nodes: [], hidden: [] })
  const [ready, setReady] = useState(() => cached !== undefined)
  const [error, setError] = useState('')
  const isAdmin = user?.role === 'admin'

  useEffect(() => { refreshUser() }, [refreshUser])
  useEffect(() => startPoll(async (signal) => {
    try {
      const [t, d] = await Promise.all([
        api.get('/me/traffic?days=14', signal),
        api.get('/me/nodes', signal),
      ])
      setTraffic(t)
      const g = cacheGen()
      const next = {
        nodes: asArray(d.nodes),
        hidden: asArray(d.hidden),
        starred: asArray(d.starred),
        announce: d.announce || '',
      }
      setPayload(putList('me-nodes', next, g))
      setError('')
    } catch (e) {
      if (isAbort(e)) return
      setError(e.message || '加载失败')
      setReady(true)
      throw e
    }
    setReady(true)
  }, 5000), [])

  const used = billedBytes(user)
  const cap = user?.traffic_cap || 0
  const ratio = cap > 0 ? Math.min(100, Math.round(used * 100 / cap)) : 0
  const nodes = asArray(payload.nodes)
  const period = sumDays(traffic?.days)
  const rawUsed = (Number(user?.used_up) || 0) + (Number(user?.used_down) || 0)

  return (
    <div>
      <PageHead
        title={isAdmin ? '订阅' : '我的订阅'}
        desc={isAdmin
          ? '包含全部节点。用管理员自己的身份，不占用用户额度。单条复制和常用节点在「节点」。'
          : '把链接导入 Clash Meta、sing-box 或通用客户端，也可以扫码。流量按 GiB（1024³ 字节）计。'}
      />
      <div className="stat-row">
        {isAdmin ? (
          <>
            <div>
              <div className="kicker">可用节点</div>
              <span className="stat-val">{ready ? nodes.length : '—'}</span>
              <div className="text-[12px] text-ink-mut mt-1">进订阅的节点</div>
            </div>
            <div>
              <div className="kicker">近 14 日（原始）</div>
              <span className="stat-val">{fmtBytes(period)}</span>
              <div className="text-[12px] text-ink-mut mt-1">不含其他用户</div>
            </div>
            <div>
              <div className="kicker">累计（计费）</div>
              <span className="stat-val">{fmtBytes(used)}</span>
              <div className="text-[12px] text-ink-mut mt-1">原始 {fmtBytes(rawUsed)} · 不占用用户额度</div>
            </div>
          </>
        ) : (
          <>
            <div>
              <div className="kicker">账号</div>
              <span className="stat-val">{user?.username}</span>
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
              <span className="stat-val">{user?.expires_at ? fmtDateShort(user.expires_at) : '—'}</span>
              {user?.expires_at && user.expires_at * 1000 < Date.now() ? (
                <div className="text-[12px] mt-1" style={{ color: 'var(--color-danger)' }}>已到期，节点已从订阅摘掉</div>
              ) : user?.expires_at && user.expires_at * 1000 < Date.now() + 7 * 86400 * 1000 ? (
                <div className="text-[12px] mt-1" style={{ color: 'var(--color-warn)' }}>即将到期</div>
              ) : null}
            </div>
          </>
        )}
      </div>
      {!isAdmin && !user?.package_id && (
        <div className="alert-row is-warn mb-4">还没有套餐，订阅里不会有节点。请联系管理员绑定。</div>
      )}
      {error ? <div className="alert-row is-warn mb-4">{error}</div> : null}

      {sub && user?.sub_token ? (
        <div className="card p-5 mb-4">
          <SubPanel
            token={user.sub_token}
            rotating={rotating}
            onCopied={(msg, kind) => toast(msg, kind)}
            onRotate={async () => {
              if (!(await dialog.confirm({ title: '重置订阅令牌', message: '旧订阅链接立刻失效。' }))) return
              setRotating(true)
              try {
                await api.post('/me/rotate-sub')
                await refreshUser()
                toast('订阅令牌已更换')
              } catch (e) { toast(e.message, 'error') }
              finally { setRotating(false) }
            }}
          />
          {isAdmin ? (
            <div className="text-[12px] text-ink-mut mt-3">
              星标的节点会进订阅；未标星则全部可用节点都进。
              <Link to="/my/nodes" className="linkish ml-1">去节点复制或标星</Link>
            </div>
          ) : null}
        </div>
      ) : null}

      {asArray(traffic?.days).length ? (
        <div className="mb-4">
          <div className="text-[14px] font-semibold mb-3">近 14 日（原始）</div>
          <DayBars days={asArray(traffic.days)} />
        </div>
      ) : null}
    </div>
  )
}
