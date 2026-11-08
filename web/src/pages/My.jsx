import { useEffect, useState } from 'react'
import { Link } from 'react-router-dom'
import { useUser, useToast, useDialog } from '../components/Layout'
import { api } from '../lib/api'
import { cacheGen, peekList, putList } from '../lib/listCache'
import { startPoll } from '../lib/poll'
import { asArray, isAbort } from '../lib/safe'
import { DayBars, Meter, PageHead, billedBytes, fmtBytes, fmtDateShort } from '../components/ui'
import { SubPanel } from '../components/SubPanel'
import { NodeCard, periodBytes } from '../components/LineCard'
import { copyText } from '../lib/copy'

function sumDays(days) {
  return asArray(days).reduce((n, d) => n + (Number(d.up) || 0) + (Number(d.down) || 0), 0)
}

function sumDir(days) {
  return asArray(days).reduce((acc, d) => {
    acc.up += Number(d.up) || 0
    acc.down += Number(d.down) || 0
    return acc
  }, { up: 0, down: 0 })
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

  const used = traffic ? (Number(traffic.billed_bytes) || 0) : billedBytes(user)
  const cap = traffic ? (Number(traffic.traffic_cap) || 0) : (user?.traffic_cap || 0)
  const ratio = cap > 0 ? Math.min(100, Math.round(used * 100 / cap)) : 0
  const nodes = asArray(payload.nodes)
  const starred = asArray(payload.starred)
  const starring = starred.length > 0
  const inSub = nodes.filter(n => !starring || n.starred || starred.includes(Number(n.id)))
  const ranked = [...inSub].sort((a, b) => periodBytes(b) - periodBytes(a))
  const period = sumDays(traffic?.days)
  const dir = sumDir(traffic?.days)
  const rawUsed = (Number(user?.used_up) || 0) + (Number(user?.used_down) || 0)

  const copyNode = async (n) => {
    const text = n?.profile === 'wireguard' && n.conf ? n.conf : n?.uri
    if (!text) {
      toast('这个节点还没有分享链接', 'error')
      return
    }
    try {
      await copyText(text)
      toast(n.profile === 'wireguard' ? '已复制 WireGuard 配置' : '已复制节点链接')
    } catch {
      toast('浏览器不允许自动复制，请到节点页复制', 'error')
    }
  }

  return (
    <div>
      <PageHead
        title={isAdmin ? '订阅' : '我的订阅'}
        desc={isAdmin
          ? '用自己的身份导入。不占用用户额度。'
          : '把链接导入 Clash Meta、sing-box、小火箭或 v2rayN，也可以扫码。流量按 G（1024³ 字节）计。'}
      />
      <div className="stat-row">
        {isAdmin ? (
          <>
            <div>
              <div className="kicker">进订阅</div>
              <span className="stat-val">{ready ? inSub.length : '—'}</span>
              <div className="text-[12px] text-ink-mut mt-1">
                {starring ? `星标 ${starred.length} 个，其余不进` : '未标星，全部可用节点都进'}
              </div>
            </div>
            <div>
              <div className="kicker">近 14 日（原始）</div>
              <span className="stat-val">{fmtBytes(period)}</span>
              <div className="text-[12px] text-ink-mut mt-1">上行 {fmtBytes(dir.up)} · 下行 {fmtBytes(dir.down)}</div>
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
              if (!(await dialog.confirm({ title: '重置订阅令牌', message: '旧订阅链接立刻失效。已经导入的节点 UUID 不变。' }))) return
              setRotating(true)
              try {
                await api.post('/me/rotate-sub')
                await refreshUser()
                toast('订阅令牌已更换')
              } catch (e) { toast(e.message, 'error') }
              finally { setRotating(false) }
            }}
          />
        </div>
      ) : null}

      {isAdmin && ready && ranked.length > 0 ? (
        <div className="mb-4">
          <div className="node-cluster-head">
            <span className="machine-name">进订阅的节点</span>
            <span className="node-cluster-count">{ranked.length} 个</span>
            <Link to="/my/nodes" className="linkish ml-auto">去节点标星</Link>
          </div>
          <div className="machine-grid">
            {ranked.map(n => (
              <NodeCard key={n.id} n={n} onCopy={copyNode} />
            ))}
          </div>
        </div>
      ) : null}

      {asArray(traffic?.days).length ? (
        <div className="mb-4">
          <div className="text-[14px] font-semibold mb-3">近 14 日 · 绿上行 · 橙下行</div>
          <DayBars days={asArray(traffic.days)} />
        </div>
      ) : null}
    </div>
  )
}
