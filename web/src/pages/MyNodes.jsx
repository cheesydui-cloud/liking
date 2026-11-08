import { useEffect, useMemo, useState } from 'react'
import { useDialog, useToast } from '../components/Layout'
import { api } from '../lib/api'
import { copyText } from '../lib/copy'
import { cacheGen, peekList, putList } from '../lib/listCache'
import { startPoll } from '../lib/poll'
import { asArray, isAbort } from '../lib/safe'
import { NodeCard, periodBytes } from '../components/LineCard'
import {
  Empty, FilterTabs, Icon, Modal, PageHead, SearchInput, SkeletonRows, StatusWord,
} from '../components/ui'

function groupByServer(list) {
  const sorted = [...list].sort((a, b) => periodBytes(b) - periodBytes(a) || String(a.name || '').localeCompare(String(b.name || ''), 'zh'))
  const by = new Map()
  for (const n of sorted) {
    const sid = Number(n.server_id) || 0
    let g = by.get(sid)
    if (!g) {
      g = { id: sid, name: n.server_name || '未分组', online: !!n.server_online, nodes: [] }
      by.set(sid, g)
    }
    g.nodes.push(n)
  }
  return [...by.values()]
}

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
  const dialog = useDialog()
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

  const starring = starred.length > 0
  const inSub = useMemo(
    () => filtered.filter(n => !starring || n.starred || starred.includes(Number(n.id))),
    [filtered, starring, starred],
  )
  const outSub = useMemo(
    () => (starring ? filtered.filter(n => !n.starred && !starred.includes(Number(n.id))) : []),
    [filtered, starring, starred],
  )
  const inGroups = useMemo(() => groupByServer(inSub), [inSub])
  const outGroups = useMemo(() => groupByServer(outSub), [outSub])

  const copyNode = async (n) => {
    if (n?.profile === 'wireguard' && n.conf) {
      setShareText(n.conf)
      try { await copyText(n.conf); toast('已复制 WireGuard 配置') }
      catch { toast('请从窗口里复制配置', 'error') }
      return
    }
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
    const uris = inSub.map(n => n.uri).filter(Boolean)
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
    if (!on && starred.length === 0) {
      const ok = await dialog.confirm({
        title: '只让标星的进订阅',
        message: '之后只有标星的节点进 Clash 和订阅，其余会消失。一个都不标时，全部可用节点都会进。',
        okText: '标星',
      })
      if (!ok) return
    }
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
        desc="复制的是当前账号的链接。星标后只把常用节点放进订阅；一个都不标则全部可用节点都进。"
        actions={inSub.some(n => n.uri) ? (
          <button type="button" className="btn-ghost" onClick={copyAll}>复制进订阅的链接</button>
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
      ) : nodes.length === 0 ? (
        <div className="card overflow-hidden">
          <Empty title="还没有可用节点" hint={emptyHint()} />
        </div>
      ) : filtered.length === 0 ? (
        <div className="card overflow-hidden">
          <Empty title="没有匹配的节点" hint="换个关键词或筛选。" />
        </div>
      ) : (
        <div>
          <div className="text-[13px] text-ink-soft mb-3">
            {starring ? `星标 ${starred.length} 个进订阅，其余不进。` : '未标星，下面全部进订阅。'}
          </div>
          {inGroups.map(g => (
            <section key={`in-${g.id}`} className="node-cluster">
              <div className="node-cluster-head">
                <span className="machine-name truncate">{g.name}</span>
                <StatusWord online={g.online} />
                <span className="node-cluster-count">{g.nodes.length} 个</span>
              </div>
              <div className="machine-grid">
                {g.nodes.map(n => (
                  <NodeCard key={n.id} n={n} starBusy={starBusy} onStar={toggleStar} onCopy={copyNode} />
                ))}
              </div>
            </section>
          ))}
          {outGroups.length > 0 ? (
            <div className="mt-6">
              <div className="node-cluster-head">
                <span className="machine-name">未进订阅</span>
                <span className="node-cluster-count">{outSub.length} 个</span>
              </div>
              {outGroups.map(g => (
                <section key={`out-${g.id}`} className="node-cluster">
                  <div className="node-cluster-head">
                    <span className="machine-name truncate">{g.name}</span>
                    <StatusWord online={g.online} />
                    <span className="node-cluster-count">{g.nodes.length} 个</span>
                  </div>
                  <div className="machine-grid">
                    {g.nodes.map(n => (
                      <NodeCard key={n.id} n={n} starBusy={starBusy} onStar={toggleStar} onCopy={copyNode} />
                    ))}
                  </div>
                </section>
              ))}
            </div>
          ) : null}
        </div>
      )}

      {ready && hidden.length > 0 ? (
        <div className="mt-6">
          <div className="node-cluster-head">
            <button type="button" className="row-act" onClick={() => setOpenHidden(v => !v)}>
              {openHidden ? '收起' : '不可用'}：{hidden.length} 个（{hiddenSummary(hidden)}）
            </button>
          </div>
          {openHidden ? (
            <div className="machine-grid">
              {hidden.map(h => (
                <div key={h.id} className="machine is-static is-off">
                  <div className="machine-head">
                    <div className="min-w-0 flex-1">
                      <div className="machine-title">
                        <span className="machine-name truncate">{h.name}</span>
                        <span className="status-word is-mute">{h.reason}</span>
                      </div>
                      <div className="machine-host">
                        <span className="machine-host-addr">{h.server_name || '—'}</span>
                      </div>
                    </div>
                  </div>
                </div>
              ))}
            </div>
          ) : null}
        </div>
      ) : null}

      <Modal open={!!shareText} title={String(shareText).includes('[Interface]') ? 'WireGuard 配置' : '节点链接'} onClose={() => setShareText('')} footer={
        <>
          <button type="button" className="btn-ghost" onClick={() => setShareText('')}>关闭</button>
          <button type="button" className="btn-primary" onClick={async () => {
            try { await copyText(shareText); toast(String(shareText).includes('[Interface]') ? '已复制 WireGuard 配置' : '已复制节点链接') }
            catch { toast('请手动选中复制', 'error') }
          }}>
            <Icon name="copy" size={15} /> 复制
          </button>
        </>
      }>
        <p className="text-[12px] text-ink-mut mb-2">{String(shareText).includes('[Interface]') ? '保存成 .conf 后用官方 WireGuard 或 OpenWrt 导入。Clash 和 sing-box 订阅里已经带上这一条。' : '粘贴到小火箭 / v2rayN / Nekobox 即可导入。'}</p>
        <code className={`block text-[12px] font-mono p-3 ${String(shareText).includes('[Interface]') ? 'whitespace-pre-wrap' : 'break-all'}`} style={{ background: 'var(--color-fill)' }}>{shareText}</code>
      </Modal>
    </div>
  )
}
