import { useEffect, useState } from 'react'
import { Link } from 'react-router-dom'
import { useUser, useToast } from '../components/Layout'
import { api } from '../lib/api'
import { copyText } from '../lib/copy'
import { cacheGen, peekList, putList } from '../lib/listCache'
import { startPoll } from '../lib/poll'
import { asArray, isAbort } from '../lib/safe'
import { DayBars, Empty, Icon, Meter, Modal, PageHead, SkeletonRows, billedBytes, fmtBytes, fmtDate, remainingBytes } from '../components/ui'
import { SubPanel } from '../components/SubPanel'

function sumDays(days) {
  return asArray(days).reduce((n, d) => n + (Number(d.up) || 0) + (Number(d.down) || 0), 0)
}

function hiddenSummary(hidden) {
  const n = { '停用': 0, '实例已满': 0, '缺核心': 0 }
  for (const h of asArray(hidden)) {
    if (n[h.reason] != null) n[h.reason] += 1
  }
  const parts = Object.entries(n).filter(([, c]) => c).map(([k, c]) => `${k} ${c}`)
  return parts.join(' / ')
}

export default function My() {
  const { user, sub, refreshUser } = useUser()
  const toast = useToast()
  const cached = peekList('me-nodes')
  const [traffic, setTraffic] = useState(null)
  const [payload, setPayload] = useState(() => cached ?? { nodes: [], hidden: [] })
  const [ready, setReady] = useState(() => cached !== undefined)
  const [error, setError] = useState('')
  const [shareText, setShareText] = useState('')
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
  const hidden = asArray(payload.hidden)
  const period = sumDays(traffic?.days)
  const rawUsed = (Number(user?.used_up) || 0) + (Number(user?.used_down) || 0)

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
              <div className="text-[12px] text-ink-mut mt-1">{user?.package_name || '未分配套餐'}</div>
            </div>
            <div>
              <div className="kicker">流量{user?.direction === 'twoway' ? ' / 双向' : ''}</div>
              <Meter className="mt-2" value={used} max={user?.traffic_cap || 0} />
              {user?.traffic_cap > 0 ? (
                <div className="text-[12px] text-ink-mut mt-1.5">剩余 {fmtBytes(remainingBytes(user))}</div>
              ) : null}
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
          <SubPanel token={user.sub_token} onCopied={(msg, kind) => toast(msg, kind)} />
          {isAdmin ? (
            <div className="text-[12px] text-ink-mut mt-3">
              星标的节点会进订阅；未标星则全部可用节点都进。
              <Link to="/my/nodes" className="linkish ml-1">去节点复制或标星</Link>
            </div>
          ) : null}
        </div>
      ) : null}

      {isAdmin ? null : !ready ? (
        <div className="card overflow-hidden mb-4"><SkeletonRows /></div>
      ) : nodes.length > 0 ? (
        <div className="card overflow-hidden mb-4">
          <div className="panel-head">可用节点</div>
          <div className="table-wrap">
            <table className="data">
              <thead>
                <tr>
                  <th>名称</th>
                  <th>地址</th>
                  <th>协议</th>
                  <th>近 14 日（原始）</th>
                  <th>累计（原始）</th>
                  <th></th>
                </tr>
              </thead>
              <tbody>
                {nodes.map((n, i) => (
                  <tr key={n.id || i}>
                    <td className="font-medium">
                      {n.name}
                      {n.line_kind === 'chain' ? <span className="line-tag">中转</span> : null}
                    </td>
                    <td className="copy-text">{n.host}:{n.port}</td>
                    <td className="text-[12px] text-ink-mut">{n.profile}</td>
                    <td className="tabular-nums text-[12px] font-mono whitespace-nowrap">{fmtBytes((n.period_up || 0) + (n.period_down || 0))}</td>
                    <td className="tabular-nums text-[12px] font-mono whitespace-nowrap">{fmtBytes((n.used_up || 0) + (n.used_down || 0))}</td>
                    <td className="text-right whitespace-nowrap w-px">
                      <button type="button" className="row-act" disabled={!n.uri} onClick={() => copyNode(n)}>复制</button>
                    </td>
                  </tr>
                ))}
              </tbody>
            </table>
          </div>
        </div>
      ) : user?.package_id ? (
        <div className="card overflow-hidden mb-4">
          <Empty
            title="没有可用节点"
            hint={hidden.length
              ? `有 ${hidden.length} 个节点未进订阅${hiddenSummary(hidden) ? `（${hiddenSummary(hidden)}）` : ''}。`
              : '套餐里还没有能进订阅的节点。'}
          />
        </div>
      ) : null}

      {asArray(traffic?.days).length ? (
        <div className="mb-4">
          <div className="text-[14px] font-semibold mb-3">近 14 日（原始）</div>
          <DayBars days={asArray(traffic.days)} />
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
