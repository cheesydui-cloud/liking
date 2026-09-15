import { useEffect, useMemo, useState } from 'react'
import { api } from '../lib/api'
import { peekList, putList } from '../lib/listCache'
import { useToast, useDialog } from '../components/Layout'
import { formatPortRange } from '../lib/ports'
import { hopStatus, isDirectNode, nodeStatus } from '../lib/status'
import { parseShareURI } from '../lib/share'
import {
  LAND_PROFILES, inboundSettings, forwardKind, kindLabel, protoShort,
  hopProto, pathHops, landingText, usedPortsText,
} from '../lib/forwards'
import { Empty, Field, FilterTabs, Icon, LineStatus, Modal, MoreMenu, PageHead, SearchInput, SkeletonRows } from '../components/ui'

function landingProto(inb, byID) {
  if (forwardKind(inb) === 'port') {
    const st = inboundSettings(inb)
    return st.network === 'tcp,udp' ? 'TCP+UDP' : 'TCP'
  }
  const hops = pathHops(inb)
  return hopProto(hops[hops.length - 1], byID)
}

function cloneEntrySettings(inb) {
  const st = inboundSettings(inb)
  const out = {}
  if (st.dest) out.dest = st.dest
  if (st.method) out.method = st.method
  if (st.transport) out.transport = st.transport
  if (st.sni) out.sni = st.sni
  if (st.host) out.host = st.host
  if (st.alpn) out.alpn = st.alpn
  if (st.fingerprint) out.fingerprint = st.fingerprint
  if (st.min_version) out.min_version = st.min_version
  if (st.mode) out.mode = st.mode
  if (st.xver != null && st.xver !== '') out.xver = st.xver
  return out
}

function cardTone(inb, entrySrv) {
  if (inb.enabled === false) return 'is-off'
  const st = nodeStatus(inb, entrySrv, { skipLanding: true })
  if (st === '故障') return 'is-fault'
  if (st === '正常') return 'is-live'
  return 'is-off'
}

function Metric({ label, value, plain }) {
  return (
    <div className="metric">
      <span className="metric-k">{label}</span>
      <span className={`metric-v${plain ? ' is-plain' : ''}`}>{value}</span>
    </div>
  )
}

const emptyForm = {
  kind: 'chain',
  entry_id: '',
  server_id: '',
  name: '',
  port: '',
  exit_mode: 'panel',
  exit_inbound_id: '',
  exit_uri: '',
  dest_host: '',
  dest_port: '',
  network: 'tcp',
}

export default function MyForwards() {
  const toast = useToast()
  const dialog = useDialog()
  const [servers, setServers] = useState(() => peekList('servers') ?? [])
  const [list, setList] = useState(() => peekList('inbounds') ?? [])
  const [ready, setReady] = useState(() => peekList('inbounds') !== undefined)
  const [f, setF] = useState(emptyForm)
  const [editId, setEditId] = useState(0)
  const [open, setOpen] = useState(false)
  const [busy, setBusy] = useState(false)
  const [q, setQ] = useState('')
  const [kindFilter, setKindFilter] = useState('')
  const [probe, setProbe] = useState({})
  const [probing, setProbing] = useState({})

  const load = async () => {
    try {
      const [a, b] = await Promise.all([api.get('/servers'), api.get('/inbounds')])
      setServers(putList('servers', a.servers || []))
      setList(putList('inbounds', b.inbounds || []))
    } catch (e) { toast(e.message, 'error') }
    finally { setReady(true) }
  }
  useEffect(() => {
    load()
    const t = setInterval(() => {
      api.get('/servers').then(a => setServers(putList('servers', a.servers || []))).catch(() => {})
    }, 5000)
    return () => clearInterval(t)
  }, [])

  const byID = useMemo(() => {
    const m = new Map()
    for (const x of list) m.set(Number(x.id), x)
    return m
  }, [list])
  const serversByID = useMemo(() => {
    const m = new Map()
    for (const s of servers) m.set(Number(s.id), s)
    return m
  }, [servers])

  const directs = useMemo(
    () => list.filter(x => isDirectNode(x) && Number(x.id) !== Number(editId)),
    [list, editId],
  )
  const landings = useMemo(
    () => list.filter(x => isDirectNode(x) && LAND_PROFILES.includes(x.profile) && Number(x.id) !== Number(editId) && Number(x.id) !== Number(f.entry_id)),
    [list, editId, f.entry_id],
  )
  const forwards = useMemo(() => list.filter(x => forwardKind(x)), [list])

  const rows = useMemo(() => {
    const needle = q.trim().toLowerCase()
    return forwards.filter(inb => {
      const k = forwardKind(inb)
      if (kindFilter && k !== kindFilter) return false
      if (!needle) return true
      const land = landingText(inb, byID)
      const hay = [inb.name, inb.server_name, inb.server_host, inb.port, land, kindLabel(k), protoShort(inb.profile)]
      return hay.some(x => String(x || '').toLowerCase().includes(needle))
    })
  }, [forwards, kindFilter, q, byID])

  const entryNode = directs.find(x => Number(x.id) === Number(f.entry_id)) || (editId ? byID.get(Number(editId)) : null)
  const selectedServer = f.kind === 'port'
    ? serversByID.get(Number(f.server_id))
    : serversByID.get(Number(entryNode?.server_id))
  const uriPreview = f.exit_mode === 'uri' ? parseShareURI(f.exit_uri) : null
  const exitLand = landings.find(x => Number(x.id) === Number(f.exit_inbound_id))
  const exitLabel = f.exit_mode === 'uri'
    ? (uriPreview?.ok ? uriPreview.label : '外部节点')
    : (exitLand?.name || '落地')

  const reset = () => {
    setEditId(0)
    setF(emptyForm)
  }
  const openCreate = () => {
    reset()
    const first = directs[0]
    setF({
      ...emptyForm,
      entry_id: first?.id || '',
      server_id: servers[0]?.id || '',
    })
    setOpen(true)
  }
  const startEdit = (inb) => {
    const k = forwardKind(inb)
    const st = inboundSettings(inb)
    setEditId(inb.id)
    setF({
      ...emptyForm,
      kind: k || 'chain',
      entry_id: '',
      server_id: inb.server_id,
      name: inb.name || '',
      port: inb.port,
      exit_mode: inb.exit_uri ? 'uri' : 'panel',
      exit_inbound_id: inb.exit_inbound_id || '',
      exit_uri: inb.exit_uri || '',
      dest_host: st.dest_host || '',
      dest_port: st.dest_port || '',
      network: st.network || 'tcp',
    })
    setOpen(true)
  }
  const closeForm = () => {
    setOpen(false)
    reset()
  }

  const bodyFromForm = () => {
    if (f.kind === 'port') {
      return {
        server_id: Number(f.server_id),
        name: f.name,
        profile: 'port-forward',
        port: Number(f.port) || 0,
        listen: '0.0.0.0',
        line_kind: 'direct',
        enabled: true,
        settings: {
          dest_host: String(f.dest_host || '').trim(),
          dest_port: Number(f.dest_port) || 0,
          network: f.network === 'tcp,udp' ? 'tcp,udp' : 'tcp',
        },
        exit_inbound_id: 0,
        exit_uri: '',
      }
    }
    const template = editId ? byID.get(Number(editId)) : entryNode
    if (!template) return null
    const settings = editId ? { ...inboundSettings(template) } : cloneEntrySettings(template)
    const body = {
      server_id: Number(template.server_id),
      name: String(f.name || '').trim() || `${entryNode?.name || template.name} → ${exitLabel}`,
      profile: template.profile,
      port: Number(f.port) || 0,
      listen: '0.0.0.0',
      line_kind: 'chain',
      enabled: editId ? template.enabled !== false : true,
      settings,
      exit_inbound_id: f.exit_mode === 'panel' ? Number(f.exit_inbound_id) || 0 : 0,
      exit_uri: f.exit_mode === 'uri' ? String(f.exit_uri || '').trim() : '',
    }
    const cert = template.cert_id
    if (cert) body.cert_id = Number(cert)
    return body
  }

  const save = async (e) => {
    e.preventDefault()
    if (f.kind === 'port') {
      if (!Number(f.server_id)) { toast('请选择实例', 'error'); return }
      if (!String(f.dest_host || '').trim()) { toast('请填写目标地址', 'error'); return }
      const p = Number(f.dest_port)
      if (!Number.isInteger(p) || p < 1 || p > 65535) { toast('目标端口无效', 'error'); return }
    } else {
      if (!editId && !Number(f.entry_id)) { toast('请选择入口节点', 'error'); return }
      if (f.exit_mode === 'panel') {
        if (!Number(f.exit_inbound_id)) { toast('请选择出口节点', 'error'); return }
        if (!editId && Number(f.exit_inbound_id) === Number(f.entry_id)) {
          toast('入口和出口不能是同一节点', 'error'); return
        }
      } else {
        const t = parseShareURI(f.exit_uri)
        if (!t.ok) { toast(t.error || '出口链接无效', 'error'); return }
      }
    }
    const raw = String(f.port ?? '').trim()
    if (raw !== '') {
      const port = Number(raw)
      if (!Number.isInteger(port) || port < 1 || port > 65535) {
        toast('端口范围 1–65535，或不填则随机', 'error')
        return
      }
    } else if (editId) {
      toast('编辑时需要填写端口', 'error')
      return
    }
    const body = bodyFromForm()
    if (!body) { toast('请选择入口节点', 'error'); return }
    const wasEdit = !!editId
    setBusy(true)
    try {
      const d = wasEdit ? await api.put(`/inbounds/${editId}`, body) : await api.post('/inbounds', body)
      if (d.apply_error) toast(d.apply_error, 'error')
      else toast(wasEdit ? '已保存' : '已创建')
      closeForm()
      load()
    } catch (err) { toast(err.message, 'error') }
    finally { setBusy(false) }
  }

  const toggle = async (inb) => {
    try {
      const d = await api.put(`/inbounds/${inb.id}`, {
        enabled: !inb.enabled,
        name: inb.name,
        port: inb.port,
        listen: inb.listen,
        settings: inb.settings,
        line_kind: inb.line_kind,
        exit_inbound_id: inb.exit_inbound_id || 0,
        exit_uri: inb.exit_uri || '',
        cert_id: inb.cert_id,
      })
      if (d.apply_error) toast(d.apply_error, 'error')
      load()
    } catch (err) { toast(err.message, 'error') }
  }

  const del = async (inb) => {
    if (!(await dialog.confirm({ title: '删除中转', message: '入口会立刻停掉。链式入口会从订阅里消失。原来的节点不受影响。', danger: true }))) return
    try {
      await api.del(`/inbounds/${inb.id}`)
      if (editId === inb.id) closeForm()
      load()
    } catch (err) { toast(err.message, 'error') }
  }

  const runProbe = async (inb) => {
    if (!inb?.id) return
    setProbing(p => ({ ...p, [inb.id]: true }))
    try {
      const d = await api.post(`/inbounds/${inb.id}/probe`)
      setProbe(p => ({
        ...p,
        [inb.id]: d.ok
          ? { text: `${d.latency_ms} ms`, ok: true }
          : { text: d.error || '失败', ok: false },
      }))
      if (!d.ok) toast(d.error || '探测失败', 'error')
    } catch (err) {
      setProbe(p => ({ ...p, [inb.id]: { text: err.message || '失败', ok: false } }))
      toast(err.message, 'error')
    } finally {
      setProbing(p => ({ ...p, [inb.id]: false }))
    }
  }

  const runFormProbe = async () => {
    let serverId = 0
    let host = ''
    let port = 0
    let uri = ''
    if (f.kind === 'port') {
      serverId = Number(f.server_id)
      host = String(f.dest_host || '').trim()
      port = Number(f.dest_port) || 0
    } else {
      const entry = editId ? byID.get(Number(editId)) : entryNode
      serverId = Number(entry?.server_id)
      if (f.exit_mode === 'uri') {
        uri = String(f.exit_uri || '').trim()
      } else {
        const land = byID.get(Number(f.exit_inbound_id))
        const srv = serversByID.get(Number(land?.server_id))
        host = String(land?.server_host || srv?.public_host || srv?.connect_ip || '').trim()
        port = Number(land?.port) || 0
        if (!host) { toast('落地节点没有公开地址', 'error'); return }
      }
    }
    if (!serverId) { toast('请先选入口', 'error'); return }
    setBusy(true)
    try {
      const body = uri ? { server_id: serverId, uri } : { server_id: serverId, host, port }
      const d = await api.post('/probe', body)
      if (d.ok) toast(`${d.latency_ms} ms`)
      else toast(d.error || '探测失败', 'error')
    } catch (err) { toast(err.message, 'error') }
    finally { setBusy(false) }
  }

  const groupedEntries = useMemo(() => {
    const groups = []
    for (const s of servers) {
      const nodes = directs.filter(n => Number(n.server_id) === Number(s.id))
      if (nodes.length) groups.push({ server: s, nodes })
    }
    return groups
  }, [servers, directs])

  return (
    <div>
      <PageHead
        title="中转"
        actions={
          <button type="button" className="btn-primary" onClick={openCreate}>
            <Icon name="plus" size={15} /> 增加中转
          </button>
        }
      />
      {forwards.length > 0 ? (
        <div className="flex flex-col sm:flex-row gap-2 mb-3">
          <SearchInput value={q} onChange={e => setQ(e.target.value)} placeholder="搜索入口 / 出口 / 实例" />
          <FilterTabs
            value={kindFilter}
            onChange={setKindFilter}
            items={[['','全部'],['chain','链式'],['port','端口']]}
          />
        </div>
      ) : null}

      {!ready ? (
        <div className="card overflow-hidden"><SkeletonRows /></div>
      ) : forwards.length === 0 ? (
        <div className="card overflow-hidden">
          <Empty title="还没有中转" hint="链式会新建一条入口，原来的节点不动。出口可以选面板节点，或粘贴 vless / ss / trojan 链接。" action={
            <button type="button" className="btn-primary" onClick={openCreate}>
              <Icon name="plus" size={15} /> 增加中转
            </button>
          } />
        </div>
      ) : rows.length === 0 ? (
        <div className="card overflow-hidden">
          <Empty title="没有匹配的中转" hint="换个关键词或类型。" />
        </div>
      ) : (
        <div className="machine-grid">
          {rows.map(inb => {
            const k = forwardKind(inb)
            const srv = serversByID.get(Number(inb.server_id))
            const st = nodeStatus(inb, srv, { skipLanding: true })
            const hops = pathHops(inb)
            const last = hops[hops.length - 1]
            const lastSt = k === 'chain' ? hopStatus(last, byID, serversByID) : ''
            const pr = probe[inb.id]
            return (
              <div key={inb.id} className={`machine ${cardTone(inb, srv)}`}>
                <div className="machine-head">
                  <div className="min-w-0 flex-1">
                    <div className="machine-title">
                      <span className="machine-name truncate">{inb.name}</span>
                      <LineStatus status={st} />
                    </div>
                  </div>
                  <div className="machine-toolbar">
                    <button type="button" className="icon-btn" onClick={() => startEdit(inb)} aria-label="编辑中转" title="编辑">
                      <Icon name="pencil" size={14} />
                    </button>
                    <MoreMenu iconOnly items={[
                      { label: '编辑', onSelect: () => startEdit(inb) },
                      { label: inb.enabled ? '停用' : '启用', onSelect: () => toggle(inb) },
                      { sep: true },
                      { label: '删除', danger: true, onSelect: () => del(inb) },
                    ]} />
                  </div>
                </div>
                <div className="machine-metrics">
                  <Metric label="类型" value={kindLabel(k)} plain />
                  <Metric label="延迟" value={pr ? pr.text : '—'} plain />
                  <Metric label="入口" value={inb.server_name || '—'} plain />
                  <Metric label="出口" value={landingText(inb, byID)} plain />
                </div>
                <div className="machine-foot">
                  <span className="machine-ports truncate">
                    {k === 'chain' ? (
                      <>
                        {protoShort(inb.profile)}
                        {lastSt ? <> · <LineStatus status={lastSt} /></> : null}
                        {hops.length > 1 ? ` · ${hops.length} 跳` : ''}
                      </>
                    ) : landingProto(inb, byID)}
                  </span>
                  <button type="button" className="row-act" disabled={!!probing[inb.id]} onClick={() => runProbe(inb)}>
                    {probing[inb.id] ? '探测中…' : '探测'}
                  </button>
                </div>
              </div>
            )
          })}
        </div>
      )}

      <Modal
        open={open}
        title={editId ? '编辑中转' : '增加中转'}
        onClose={closeForm}
        footer={(
          <>
            <button type="button" className="btn-ghost" onClick={closeForm}>取消</button>
            <button type="submit" form="myfwd-form" className="btn-primary" disabled={busy}>{busy ? '保存中…' : '确定'}</button>
          </>
        )}
      >
        <form id="myfwd-form" onSubmit={save} className="space-y-3">
          <div>
            <div className="text-[12px] font-medium text-ink-soft mb-1.5">类型</div>
            <div className="grid grid-cols-2 gap-2">
              {[
                { id: 'chain', title: '链式', desc: '用户连入口，再转到出口。' },
                { id: 'port', title: '端口', desc: '本机端口转到目标 IP。不进订阅。' },
              ].map(opt => (
                <button
                  key={opt.id}
                  type="button"
                  className={`node-pick ${f.kind === opt.id ? 'is-on' : ''}`}
                  disabled={!!editId}
                  aria-pressed={f.kind === opt.id}
                  onClick={() => setF({ ...f, kind: opt.id })}
                >
                  <span className="min-w-0 text-left">
                    <span className="text-[13px] font-medium">{opt.title}</span>
                    <span className="block text-[12px] text-ink-mut mt-0.5 leading-snug">{opt.desc}</span>
                  </span>
                </button>
              ))}
            </div>
          </div>

          {f.kind === 'chain' ? (
            <>
              <Field label="入口节点" hint={editId ? '入口创建后不能换。原来的节点还在。' : '用已有节点当模板，会新建一条入口，原来的不动。'}>
                {editId ? (
                  <input className="input-field" value={byID.get(Number(editId))?.name || ''} disabled />
                ) : (
                  <select className="input-field" value={f.entry_id} onChange={e => setF({ ...f, entry_id: e.target.value })} required>
                    <option value="">选择节点</option>
                    {groupedEntries.map(g => (
                      <optgroup key={g.server.id} label={g.server.name}>
                        {g.nodes.map(n => (
                          <option key={n.id} value={n.id}>{n.name} · {protoShort(n.profile)} :{n.port}</option>
                        ))}
                      </optgroup>
                    ))}
                  </select>
                )}
              </Field>
              {directs.length === 0 && !editId ? (
                <div className="notice">还没有可作入口的节点。先到管理页「节点」加一条直出。</div>
              ) : null}

              <div>
                <div className="text-[12px] font-medium text-ink-soft mb-1.5">出口</div>
                <div className="grid grid-cols-2 gap-2">
                  {[
                    { id: 'panel', title: '面板节点' },
                    { id: 'uri', title: '粘贴链接' },
                  ].map(opt => (
                    <button
                      key={opt.id}
                      type="button"
                      className={`node-pick ${f.exit_mode === opt.id ? 'is-on' : ''}`}
                      aria-pressed={f.exit_mode === opt.id}
                      onClick={() => setF({ ...f, exit_mode: opt.id })}
                    >
                      <span className="text-[13px] font-medium">{opt.title}</span>
                    </button>
                  ))}
                </div>
              </div>

              {f.exit_mode === 'panel' ? (
                <Field label="落地节点" hint="只能选直出的 VLESS / Trojan / SS2022 / SOCKS5。">
                  <select className="input-field" value={f.exit_inbound_id} onChange={e => setF({ ...f, exit_inbound_id: e.target.value })}>
                    <option value="">选择节点</option>
                    {landings.map(x => (
                      <option key={x.id} value={x.id}>{x.server_name} / {x.name} · {protoShort(x.profile)} :{x.port}</option>
                    ))}
                  </select>
                </Field>
              ) : (
                <Field label="节点链接" hint="vless://、ss://、trojan://，也可以 socks5://。">
                  <textarea
                    className="input-field font-mono"
                    rows={3}
                    value={f.exit_uri}
                    onChange={e => setF({ ...f, exit_uri: e.target.value })}
                    placeholder="vless://uuid@host:443?security=reality&…"
                    spellCheck={false}
                  />
                  {String(f.exit_uri || '').trim() ? (
                    <div className={`text-[12px] mt-1.5 ${uriPreview?.ok ? 'text-ink-soft' : ''}`} style={!uriPreview?.ok ? { color: 'var(--color-danger)' } : undefined}>
                      {uriPreview?.ok ? uriPreview.label : (uriPreview?.error || '无法识别')}
                    </div>
                  ) : null}
                </Field>
              )}
            </>
          ) : (
            <>
              <Field label="实例">
                <select className="input-field" value={f.server_id} onChange={e => setF({ ...f, server_id: e.target.value })} required>
                  <option value="">选择实例</option>
                  {servers.map(s => <option key={s.id} value={s.id}>{s.name}{s.public_host ? ` · ${s.public_host}` : ''}</option>)}
                </select>
              </Field>
              <div className="grid sm:grid-cols-2 gap-3">
                <Field label="目标地址">
                  <input className="input-field font-mono" value={f.dest_host} onChange={e => setF({ ...f, dest_host: e.target.value })} placeholder="IP 或域名" />
                </Field>
                <Field label="目标端口">
                  <input className="input-field font-mono tabular-nums" value={f.dest_port} onChange={e => setF({ ...f, dest_port: e.target.value })} inputMode="numeric" />
                </Field>
              </div>
              <Field label="网络">
                <select className="input-field" value={f.network} onChange={e => setF({ ...f, network: e.target.value })}>
                  <option value="tcp">仅 TCP</option>
                  <option value="tcp,udp">TCP + UDP</option>
                </select>
              </Field>
            </>
          )}

          <div className="grid sm:grid-cols-2 gap-3">
            <Field label="名称" hint="可留空">
              <input className="input-field" value={f.name} onChange={e => setF({ ...f, name: e.target.value })} placeholder={f.kind === 'chain' ? `${entryNode?.name || '入口'} → ${exitLabel}` : ''} />
            </Field>
            <Field label="监听端口" hint={selectedServer ? usedPortsText(selectedServer, list, editId) : '不填则随机'}>
              <input className="input-field font-mono tabular-nums" value={f.port} onChange={e => setF({ ...f, port: e.target.value })} placeholder={selectedServer ? `随机 ${formatPortRange(selectedServer)}` : '随机'} inputMode="numeric" />
            </Field>
          </div>

          <div className="flex justify-end">
            <button type="button" className="btn-ghost" disabled={busy} onClick={runFormProbe}>探测出口</button>
          </div>
        </form>
      </Modal>
    </div>
  )
}
