import { useEffect, useMemo, useState } from 'react'
import { useToast } from '../components/Layout'
import { api } from '../lib/api'
import { copyText } from '../lib/copy'
import { cacheGen, peekList, putList } from '../lib/listCache'
import { startPoll } from '../lib/poll'
import { asArray, isAbort } from '../lib/safe'
import {
  Empty, FilterTabs, Icon, Modal, PageHead, SearchInput, SkeletonRows, StatusWord, fmtBytes,
} from '../components/ui'

function hiddenSummary(hidden) {
  const n = { '停用': 0, '实例已满': 0, '缺核心': 0 }
  for (const h of asArray(hidden)) {
    if (n[h.reason] != null) n[h.reason] += 1
  }
  const parts = Object.entries(n).filter(([, c]) => c).map(([k, c]) => `${k} ${c}`)
  return parts.length ? parts.join(' / ') : asArray(hidden).map(h => h.reason).filter(Boolean).join(' / ')
}

export default function MyNodes() {
  const toast = useToast()
  const cached = peekList('me-nodes')
  const [payload, setPayload] = useState(() => cached ?? { nodes: [], hidden: [], starred: [] })
  const [ready, setReady] = useState(() => cached !== undefined)
  const [error, setError] = useState('')
  const [q, setQ] = useState('')
  const [kind, setKind] = useState('')
  const [openHidden, setOpenHidden] = useState(false)
  const [shareText, setShareText] = useState('')
  const [starBusy, setStarBusy] = useState(false)

  const load = async () => {
    try {
      const d = await api.get('/me/nodes')
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
    }
    setReady(true)
  }
  useEffect(() => startPoll(async (signal) => {
    try {
      const d = await api.get('/me/nodes', signal)
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

  const nodes = asArray(payload.nodes)
  const hidden = asArray(payload.hidden)
  const starred = asArray(payload.starred)

  const filtered = useMemo(() => {
    const needle = q.trim().toLowerCase()
    return nodes.filter(n => {
      if (kind === 'direct' && n.line_kind === 'chain') return false
      if (kind === 'chain' && n.line_kind !== 'chain') return false
      if (!needle) return true
      const hay = [n.name, n.host, n.port, n.profile, n.server_name]
      return hay.some(x => String(x || '').toLowerCase().includes(needle))
    })
  }, [nodes, q, kind])

  const groups = useMemo(() => {
    const by = new Map()
    for (const n of filtered) {
      const sid = Number(n.server_id) || 0
      let g = by.get(sid)
      if (!g) {
        g = {
          id: sid,
          name: n.server_name || '未分组',
          online: !!n.server_online,
          nodes: [],
        }
        by.set(sid, g)
      }
      g.nodes.push(n)
    }
    return [...by.values()]
  }, [filtered])

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

  const copyAll = async () => {
    const uris = filtered.map(n => n.uri).filter(Boolean)
    if (!uris.length) {
      toast('没有可复制的链接', 'error')
      return
    }
    const text = uris.join('\n')
    try {
      await copyText(text)
      toast(`已复制 ${uris.length} 条链接`)
    } catch {
      setShareText(text)
      toast('浏览器不允许自动复制，请手动选中链接', 'error')
    }
  }

  const toggleStar = async (n) => {
    const id = Number(n.id)
    const on = starred.includes(id)
    const next = on ? starred.filter(x => Number(x) !== id) : [...starred, id]
    setPayload(prev => {
      const updated = {
        ...prev,
        starred: next,
        nodes: (prev.nodes || []).map(x => Number(x.id) === id ? { ...x, starred: !on } : x),
      }
      putList('me-nodes', updated, cacheGen())
      return updated
    })
    setStarBusy(true)
    try {
      await api.put('/me/starred', { inbound_ids: next })
    } catch (e) {
      toast(e.message, 'error')
      load()
    } finally {
      setStarBusy(false)
    }
  }

  const emptyHint = () => {
    if (q || kind) return '换个关键词或筛选。'
    if (hidden.length) return `有 ${hidden.length} 个节点未进订阅（${hiddenSummary(hidden)}）。`
    return '先到管理页加节点。标星后只把常用节点放进订阅；不标则全部进。'
  }

  return (
    <div>
      <PageHead
        title="节点"
        desc="按实例分组。复制是当前账号的链接。星标的会进 Clash / 订阅；一个都不标则全部可用节点都进。"
        actions={filtered.some(n => n.uri) ? (
          <button type="button" className="btn-ghost" onClick={copyAll}>复制全部链接</button>
        ) : null}
      />
      {error ? <div className="alert-row is-warn mb-4">{error}</div> : null}
      {nodes.length > 0 || q || kind ? (
        <div className="flex flex-col sm:flex-row gap-2 mb-3">
          <SearchInput value={q} onChange={e => setQ(e.target.value)} placeholder="搜索名称 / 实例 / 地址" />
          <FilterTabs
            value={kind}
            onChange={setKind}
            items={[['','全部'],['direct','直连'],['chain','中转']]}
          />
        </div>
      ) : null}

      {!ready ? (
        <div className="card overflow-hidden"><SkeletonRows /></div>
      ) : groups.length === 0 ? (
        <div className="card overflow-hidden">
          <Empty title={q || kind ? '没有匹配的节点' : '还没有可用节点'} hint={emptyHint()} />
        </div>
      ) : (
        <div>
          {groups.map(g => (
            <section key={g.id || g.name} className="node-cluster">
              <div className="node-cluster-head">
                <span className="machine-name truncate">{g.name}</span>
                <StatusWord online={g.online} />
                <span className="node-cluster-count">{g.nodes.length} 个</span>
              </div>
              <div className="card overflow-hidden mb-4">
                <div className="hidden md:block table-wrap">
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
                      {g.nodes.map(n => (
                        <tr key={n.id}>
                          <td className="font-medium">
                            {n.name}
                            {n.line_kind === 'chain' ? <span className="line-tag">中转</span> : null}
                          </td>
                          <td className="copy-text">{n.host ? `${n.host}:${n.port}` : '—'}</td>
                          <td className="text-[12px] text-ink-mut">{n.profile}</td>
                          <td className="tabular-nums text-[12px] font-mono whitespace-nowrap">{fmtBytes((n.period_up || 0) + (n.period_down || 0))}</td>
                          <td className="tabular-nums text-[12px] font-mono whitespace-nowrap">{fmtBytes((n.used_up || 0) + (n.used_down || 0))}</td>
                          <td className="text-right whitespace-nowrap w-px">
                            <div className="inline-flex items-center gap-1.5">
                              <button
                                type="button"
                                className={`icon-btn${n.starred ? ' is-on' : ''}`}
                                disabled={starBusy}
                                onClick={() => toggleStar(n)}
                                aria-label={n.starred ? '取消常用' : '标为常用'}
                                title={n.starred ? '取消常用' : '常用，进订阅'}
                              >
                                <Icon name={n.starred ? 'star-on' : 'star'} size={14} />
                              </button>
                              <button type="button" className="row-act" disabled={!n.uri} onClick={() => copyNode(n)}>复制</button>
                            </div>
                          </td>
                        </tr>
                      ))}
                    </tbody>
                  </table>
                </div>
                <div className="md:hidden divide-y" style={{ borderColor: 'var(--color-line-soft)' }}>
                  {g.nodes.map(n => (
                    <div key={n.id} className="px-3.5 py-3">
                      <div className="flex items-start justify-between gap-3">
                        <div className="min-w-0">
                          <div className="font-medium">
                            {n.name}
                            {n.line_kind === 'chain' ? <span className="line-tag">中转</span> : null}
                          </div>
                          <div className="copy-text mt-0.5">{n.host ? `${n.host}:${n.port}` : '—'}</div>
                          <div className="text-[12px] text-ink-mut mt-0.5">
                            {n.profile} · 近 14 日 {fmtBytes((n.period_up || 0) + (n.period_down || 0))} · 累计 {fmtBytes((n.used_up || 0) + (n.used_down || 0))}
                          </div>
                        </div>
                        <div className="inline-flex items-center gap-1.5 shrink-0">
                          <button
                            type="button"
                            className={`icon-btn${n.starred ? ' is-on' : ''}`}
                            disabled={starBusy}
                            onClick={() => toggleStar(n)}
                            aria-label={n.starred ? '取消常用' : '标为常用'}
                            title={n.starred ? '取消常用' : '常用，进订阅'}
                          >
                            <Icon name={n.starred ? 'star-on' : 'star'} size={14} />
                          </button>
                          <button type="button" className="row-act" disabled={!n.uri} onClick={() => copyNode(n)}>复制</button>
                        </div>
                      </div>
                    </div>
                  ))}
                </div>
              </div>
            </section>
          ))}
        </div>
      )}

      {ready && hidden.length > 0 ? (
        <div className="card p-4">
          <button type="button" className="row-act" onClick={() => setOpenHidden(v => !v)}>
            未进订阅：{hidden.length} 个（{hiddenSummary(hidden)}）
          </button>
          {openHidden ? (
            <ul className="mt-3 space-y-1.5 text-[13px] text-ink-soft">
              {hidden.map(h => (
                <li key={h.id}>
                  <span className="font-medium text-ink">{h.name}</span>
                  {h.server_name ? <span className="text-ink-mut"> · {h.server_name}</span> : null}
                  <span className="text-ink-mut"> · {h.reason}</span>
                </li>
              ))}
            </ul>
          ) : null}
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
