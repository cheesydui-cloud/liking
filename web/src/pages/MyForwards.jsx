import { useEffect, useMemo, useRef, useState } from 'react'
import { Link } from 'react-router-dom'
import { useDialog, useToast } from '../components/Layout'
import { api } from '../lib/api'
import { copyText } from '../lib/copy'
import { cacheGen, peekList, putList } from '../lib/listCache'
import { createOpLock } from '../lib/opLock'
import { startPoll } from '../lib/poll'
import { asArray, isAbort } from '../lib/safe'
import { NodeCard } from '../components/LineCard'
import { Empty, PageHead, SkeletonRows } from '../components/ui'

export default function MyForwards() {
  const toast = useToast()
  const dialog = useDialog()
  const cached = peekList('me-nodes')
  const [payload, setPayload] = useState(() => cached ?? { nodes: [], starred: [] })
  const [ready, setReady] = useState(() => cached !== undefined)
  const [error, setError] = useState('')
  const [starBusy, setStarBusy] = useState(false)
  const [probes, setProbes] = useState({})
  const ops = useRef(createOpLock())

  useEffect(() => startPoll(async (signal) => {
    try {
      const d = await api.get('/me/nodes', signal)
      const g = cacheGen()
      setPayload(putList('me-nodes', {
        nodes: asArray(d.nodes),
        hidden: asArray(d.hidden),
        starred: asArray(d.starred),
        announce: d.announce || '',
      }, g))
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
  const starred = asArray(payload.starred)
  const starring = starred.length > 0
  const chains = useMemo(() => nodes.filter(n => n.line_kind === 'chain'), [nodes])
  const inSub = useMemo(
    () => chains.filter(n => !starring || n.starred || starred.includes(Number(n.id))),
    [chains, starring, starred],
  )
  const outSub = useMemo(
    () => (starring ? chains.filter(n => !n.starred && !starred.includes(Number(n.id))) : []),
    [chains, starring, starred],
  )

  const copyNode = async (n) => {
    if (!n?.uri) {
      toast('这个入口还没有分享链接', 'error')
      return
    }
    try {
      await copyText(n.uri)
      toast('已复制入口链接')
    } catch {
      toast('浏览器不允许自动复制，请到节点页复制', 'error')
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
      const d = await api.get('/me/nodes').catch(() => null)
      if (d) {
        setPayload(putList('me-nodes', {
          nodes: asArray(d.nodes),
          hidden: asArray(d.hidden),
          starred: asArray(d.starred),
          announce: d.announce || '',
        }, cacheGen()))
      }
    } finally {
      setStarBusy(false)
    }
  }

  const probeLatency = async (n) => {
    await ops.current.run(`probe-${n.id}`, async () => {
      setProbes(p => ({ ...p, [n.id]: { state: 'busy' } }))
      try {
        const d = await api.post(`/inbounds/${n.id}/probe`)
        if (d.ok) {
          const ms = Number(d.latency_ms) || 0
          setProbes(p => ({ ...p, [n.id]: { state: 'ok', ms, host: d.host, port: d.port } }))
          toast(`入口到 ${d.host}:${d.port}，${ms} ms`)
        } else {
          const msg = d.error || '探测失败'
          setProbes(p => ({ ...p, [n.id]: { state: 'err', error: msg } }))
          toast(msg, 'error')
        }
      } catch (e) {
        setProbes(p => ({ ...p, [n.id]: { state: 'err', error: e.message } }))
        toast(e.message, 'error')
      }
    })
  }

  const renderGrid = (list) => (
    <div className="machine-grid">
      {list.map(n => {
        const row = probes[n.id]
        const note = !row
          ? '从入口测到落地'
          : row.state === 'busy'
            ? '测量中…'
            : row.state === 'ok'
              ? `${row.ms} ms`
              : (row.error || '探测失败')
        return (
          <NodeCard
            key={n.id}
            n={n}
            starBusy={starBusy}
            onStar={toggleStar}
            onCopy={copyNode}
            extra={(
              <button type="button" className="row-act" disabled={row?.state === 'busy'} onClick={() => probeLatency(n)} title={note}>
                {row?.state === 'busy' ? '测量中…' : row?.state === 'ok' ? `${row.ms} ms` : '测延迟'}
              </button>
            )}
          />
        )
      })}
    </div>
  )

  return (
    <div>
      <PageHead
        title="中转"
        desc="会进订阅的链式入口。新建、改路径和端口中转在管理的中转里。"
        actions={<Link to="/forwards" className="btn-ghost">去管理里改</Link>}
      />
      {error ? <div className="alert-row is-warn mb-4">{error}</div> : null}
      {!ready ? (
        <div className="card overflow-hidden"><SkeletonRows /></div>
      ) : chains.length === 0 ? (
        <div className="card overflow-hidden">
          <Empty title="还没有链式入口" hint="到管理的中转里新建。端口中转不会出现在订阅里。" />
        </div>
      ) : (
        <div>
          <div className="text-[13px] text-ink-soft mb-3">
            {starring ? `星标 ${starred.length} 个进订阅，其余不进。` : '未标星，下面全部进订阅。'}
          </div>
          {inSub.length > 0 ? renderGrid(inSub) : (
            <div className="card overflow-hidden mb-4">
              <Empty title="没有进订阅的中转" hint="给要用的入口标星，或取消全部星标。" />
            </div>
          )}
          {outSub.length > 0 ? (
            <div className="mt-6">
              <div className="node-cluster-head">
                <span className="machine-name">未进订阅</span>
                <span className="node-cluster-count">{outSub.length} 个</span>
              </div>
              {renderGrid(outSub)}
            </div>
          ) : null}
          <div className="text-[12px] text-ink-mut mt-4">端口中转没有用户，在管理的中转里。</div>
        </div>
      )}
    </div>
  )
}
