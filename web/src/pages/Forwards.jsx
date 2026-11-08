import { useEffect, useMemo, useRef, useState } from 'react'
import { api } from '../lib/api'
import { cacheGen, peekList, putList } from '../lib/listCache'
import { startPoll } from '../lib/poll'
import { createOpLock } from '../lib/opLock'
import { asArray, isAbort } from '../lib/safe'
import { useToast, useDialog } from '../components/Layout'
import { formatPortRange } from '../lib/ports'
import { hopStatus, isDirectNode, nodeStatus } from '../lib/status'
import { parseShareURI } from '../lib/share'
import {
  LAND_PROFILES, MAX_HOPS, inboundSettings, forwardKind, kindLabel, protoShort,
  formatSocks, hopText, hopProto, pathHops, landingText, usedPortsText, hopFromSaved,
} from '../lib/forwards'
import { Empty, Field, Icon, LineStatus, Modal, MoreMenu, PageHead, SearchInput, SkeletonRows } from '../components/ui'

function emptyHop() {
  return { kind: 'panel', inbound_id: '', sk5_host: '', sk5_port: '1080', sk5_user: '', sk5_pass: '', uri: '' }
}

const emptyForm = {
  kind: 'chain',
  entry_mode: 'node',
  entry_id: '',
  server_id: '',
  name: '',
  profile: 'vless-reality-vision',
  port: '',
  cert_id: '',
  dest: 'www.cloudflare.com:443',
  method: '2022-blake3-aes-256-gcm',
  transport: 'BOTH',
  dest_host: '',
  dest_port: '',
  network: 'tcp',
  hops: [emptyHop()],
  enabled: true,
}

function cloneEntrySettings(inb) {
  const st = inboundSettings(inb)
  const out = {}
  for (const k of ['dest', 'method', 'transport', 'sni', 'host', 'alpn', 'fingerprint', 'min_version', 'mode', 'xver', 'server_names', 'path', 'spider_x', 'reject_unknown_sni']) {
    const v = st[k]
    if (v == null || v === '') continue
    if (Array.isArray(v) && v.length === 0) continue
    out[k] = v
  }
  return out
}

function needsTLS(profile) {
  return profile === 'vless-xhttp-tls' || profile === 'trojan-tls' || profile === 'anytls'
}

function isReality(profile) {
  return String(profile).startsWith('vless-reality')
}

function hopsFromInbound(inb) {
  const st = inboundSettings(inb)
  const hops = []
  const raw = Array.isArray(st.hops) ? st.hops : []
  for (const h of raw) hops.push(hopFromSaved(h))
  if (inb.exit_uri) hops.push(hopFromSaved({ kind: 'socks', uri: inb.exit_uri }))
  else hops.push({ ...emptyHop(), inbound_id: inb.exit_inbound_id || '' })
  return hops.length ? hops : [emptyHop()]
}

function serializeHop(h) {
  if (h.kind === 'uri') {
    return { kind: 'socks', uri: String(h.uri || '').trim() }
  }
  if (h.kind === 'socks') {
    return { kind: 'socks', uri: formatSocks(h.sk5_host, h.sk5_port, h.sk5_user, h.sk5_pass) }
  }
  return { kind: 'panel', inbound_id: Number(h.inbound_id) || 0 }
}

function entryText(inb) {
  if (forwardKind(inb) === 'port') return `:${inb.port}`
  return inb.server_name || inb.name || `:${inb.port}`
}

function pathLine(inb, byID) {
  if (forwardKind(inb) === 'port') return `${entryText(inb)} → ${landingText(inb, byID)}`
  const hops = pathHops(inb)
  return [entryText(inb), ...hops.map(h => hopText(h, byID))].join(' → ')
}

function PathHops({ inb, byID, serversByID }) {
  if (forwardKind(inb) === 'port') {
    return <div className="path-line">{pathLine(inb, byID)}</div>
  }
  const hops = pathHops(inb)
  const entrySrv = serversByID.get(Number(inb.server_id))
  const entrySt = nodeStatus(inb, entrySrv, { skipLanding: true })
  return (
    <div className="path-line">
      <span>{entryText(inb)}</span>
      {' '}
      <LineStatus status={entrySt} />
      {hops.map((h, i) => {
        const st = hopStatus(h, byID, serversByID)
        return (
          <span key={i}>
            <span className="path-arrow"> → </span>
            <span>{hopText(h, byID)}</span>
            {st ? <>{' '}<LineStatus status={st} /></> : null}
          </span>
        )
      })}
    </div>
  )
}

function pathSub(inb, byID) {
  const k = forwardKind(inb)
  if (k === 'port') {
    const st = inboundSettings(inb)
    return st.network === 'tcp,udp' ? 'TCP + UDP' : 'TCP'
  }
  const hops = pathHops(inb)
  return [protoShort(inb.profile), ...hops.map(h => hopProto(h, byID))].join(' → ')
}

function forwardStatus(inb, byID, serversByID) {
  const entrySrv = serversByID.get(Number(inb.server_id))
  const st = nodeStatus(inb, entrySrv, { byID })
  if (st !== '正常' || forwardKind(inb) !== 'chain') return st
  for (const h of pathHops(inb)) {
    const hs = hopStatus(h, byID, serversByID)
    if (hs && hs !== '正常') return hs
  }
  return st
}

function forwardCardTone(st) {
  if (st === '正常') return 'is-live'
  if (st === '故障' || st === '流量已满') return 'is-fault'
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

function latencyValue(row) {
  if (!row) return '—'
  if (row.state === 'busy') return '…'
  if (row.state === 'ok') return `${row.ms} ms`
  return '失败'
}

function formFromInbound(inb) {
  const st = inboundSettings(inb)
  return {
    kind: forwardKind(inb) || 'chain',
    entry_mode: 'new',
    entry_id: '',
    server_id: inb.server_id,
    name: inb.name || '',
    profile: inb.profile === 'port-forward' ? 'vless-reality-vision' : inb.profile,
    port: inb.port,
    cert_id: inb.cert_id || '',
    dest: st.dest || 'www.cloudflare.com:443',
    method: st.method || '2022-blake3-aes-256-gcm',
    transport: st.transport || 'BOTH',
    dest_host: st.dest_host || '',
    dest_port: st.dest_port || '',
    network: st.network || 'tcp',
    hops: hopsFromInbound(inb),
    enabled: inb.enabled !== false,
  }
}

function hopUsedIds(hops, except) {
  const s = new Set()
  hops.forEach((h, i) => {
    if (i === except || h.kind !== 'panel') return
    const id = Number(h.inbound_id)
    if (id) s.add(id)
  })
  return s
}

function ForwardsPanel() {
  const toast = useToast()
  const dialog = useDialog()
  const [servers, setServers] = useState(() => asArray(peekList('servers')))
  const [list, setList] = useState(() => asArray(peekList('inbounds')))
  const [certs, setCerts] = useState(() => asArray(peekList('certs')))
  const [profiles, setProfiles] = useState(() => asArray(peekList('profiles')))
  const [ready, setReady] = useState(() => peekList('inbounds') !== undefined)
  const [f, setF] = useState(emptyForm)
  const [editId, setEditId] = useState(0)
  const [open, setOpen] = useState(false)
  const [busy, setBusy] = useState(false)
  const [q, setQ] = useState('')
  const [kindFilter, setKindFilter] = useState('')
  const [pollErr, setPollErr] = useState('')
  const [probes, setProbes] = useState({})
  const ops = useRef(createOpLock())

  const load = async () => {
    try {
      const g = cacheGen()
      const [a, b, c, d] = await Promise.all([
        api.get('/servers'), api.get('/inbounds'), api.get('/certs'), api.get('/profiles'),
      ])
      setServers(putList('servers', asArray(a.servers), g))
      setList(putList('inbounds', asArray(b.inbounds), g))
      setCerts(putList('certs', asArray(c.certs), g))
      setProfiles(putList('profiles', asArray(d.profiles), g))
    } catch (e) { toast(e.message, 'error') }
    finally { setReady(true) }
  }
  useEffect(() => {
    load()
    return startPoll(async (signal) => {
      const g = cacheGen()
      try {
        const [a, b] = await Promise.all([
          api.get('/servers', signal),
          api.get('/inbounds', signal),
        ])
        setServers(putList('servers', asArray(a.servers), g))
        setList(putList('inbounds', asArray(b.inbounds), g))
        setPollErr('')
      } catch (e) {
        if (isAbort(e)) return
        setPollErr('实时刷新失败，显示的是上次成功数据')
        throw e
      }
    }, 5000, { immediate: false })
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

  const forwards = useMemo(() => list.filter(x => forwardKind(x)), [list])
  const directs = useMemo(() => list.filter(x => isDirectNode(x) && x.profile !== 'wireguard'), [list])
  const landings = useMemo(
    () => list.filter(x => isDirectNode(x) && LAND_PROFILES.includes(x.profile) && Number(x.id) !== Number(editId)),
    [list, editId],
  )
  const protoList = (profiles.length ? profiles : [{ id: f.profile, title: f.profile, desc: '' }]).filter(p => p.id !== 'wireguard' && p.id !== 'port-forward')

  const rows = useMemo(() => {
    const needle = q.trim().toLowerCase()
    return forwards.filter(inb => {
      const k = forwardKind(inb)
      if (kindFilter && k !== kindFilter) return false
      if (!needle) return true
      const land = landingText(inb, byID)
      const hay = [inb.name, inb.server_name, inb.server_host, inb.port, land, pathLine(inb, byID), kindLabel(k), protoShort(inb.profile)]
      return hay.some(x => String(x || '').toLowerCase().includes(needle))
    })
  }, [forwards, kindFilter, q, byID])

  const reset = () => {
    setEditId(0)
    setF(emptyForm)
  }
  const openCreate = () => {
    reset()
    const first = list.find(x => isDirectNode(x) && x.profile !== 'wireguard')
    const mode = first ? 'node' : 'new'
    setF({
      ...emptyForm,
      entry_mode: mode,
      entry_id: first?.id || '',
      server_id: mode === 'new' ? (servers[0]?.id || '') : '',
      hops: [emptyHop()],
    })
    setOpen(true)
  }
  const startEdit = (inb) => {
    setEditId(inb.id)
    setF(formFromInbound(inb))
    setOpen(true)
  }
  const closeForm = () => {
    setOpen(false)
    reset()
  }

  const setHop = (i, patch) => {
    const hops = f.hops.map((h, idx) => idx === i ? { ...h, ...patch } : h)
    setF({ ...f, hops })
  }
  const addHop = () => {
    if (f.hops.length >= MAX_HOPS) return
    const hops = f.hops.slice()
    hops.splice(Math.max(0, hops.length - 1), 0, emptyHop())
    setF({ ...f, hops })
  }
  const removeHop = (i) => {
    if (f.hops.length <= 1) return
    setF({ ...f, hops: f.hops.filter((_, idx) => idx !== i) })
  }

  const cloning = !editId && f.kind === 'chain' && f.entry_mode === 'node'
  const entryTemplate = cloning ? byID.get(Number(f.entry_id)) : null
  const templateId = entryTemplate ? Number(entryTemplate.id) : 0

  const suggestedName = () => {
    const entry = entryTemplate
      ? (entryTemplate.server_name || entryTemplate.name || '')
      : (serversByID.get(Number(f.server_id))?.name || '')
    const parts = [entry]
    for (const h of (f.hops.length ? f.hops : [])) {
      if (h.kind === 'panel') {
        const land = byID.get(Number(h.inbound_id))
        if (land?.name) parts.push(land.name)
        continue
      }
      const label = hopText(h, byID)
      if (label && label !== '—' && label !== '落地已删除') parts.push(label)
    }
    return parts.filter(Boolean).join(' → ')
  }

  const bodyFromForm = () => {
    const kind = f.kind
    if (kind === 'port') {
      return {
        server_id: Number(f.server_id),
        name: f.name,
        profile: 'port-forward',
        port: Number(f.port) || 0,
        listen: '0.0.0.0',
        line_kind: 'direct',
        enabled: f.enabled !== false,
        settings: {
          dest_host: String(f.dest_host || '').trim(),
          dest_port: Number(f.dest_port) || 0,
          network: f.network === 'tcp,udp' ? 'tcp,udp' : 'tcp',
        },
        exit_inbound_id: 0,
        exit_uri: '',
      }
    }
    const template = cloning ? byID.get(Number(f.entry_id)) : null
    if (cloning && !template) return null
    const existing = editId ? inboundSettings(byID.get(Number(editId))) : {}
    const settings = template ? cloneEntrySettings(template) : { ...existing }
    const profile = template ? template.profile : f.profile
    const serverId = template ? template.server_id : f.server_id
    const certId = template ? (template.cert_id || '') : f.cert_id
    if (!template) {
      if (isReality(profile) && String(f.dest || '').trim()) settings.dest = f.dest.trim()
      if (profile === 'ss2022') settings.method = f.method
      if (profile === 'mieru') settings.transport = f.transport
    }
    const hops = f.hops.length ? f.hops : [emptyHop()]
    const last = hops[hops.length - 1]
    const mids = hops.slice(0, -1).map(serializeHop)
    if (mids.length) settings.hops = mids
    else delete settings.hops
    let name = String(f.name || '').trim()
    if (!name && !editId) name = suggestedName()
    const body = {
      server_id: Number(serverId),
      name,
      profile,
      port: Number(f.port) || 0,
      listen: '0.0.0.0',
      line_kind: 'chain',
      enabled: f.enabled !== false,
      settings,
      exit_inbound_id: 0,
      exit_uri: '',
    }
    if (certId) body.cert_id = Number(certId)
    if (last.kind === 'panel') body.exit_inbound_id = Number(last.inbound_id) || 0
    else if (last.kind === 'uri') body.exit_uri = String(last.uri || '').trim()
    else body.exit_uri = formatSocks(last.sk5_host, last.sk5_port, last.sk5_user, last.sk5_pass)
    return body
  }

  const save = async (e) => {
    e.preventDefault()
    if (cloning) {
      if (!templateId) { toast('请选择要沿用的节点', 'error'); return }
    } else if (!Number(f.server_id)) {
      toast('请选择入口实例', 'error'); return
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
    if (f.kind === 'chain') {
      const hops = f.hops.length ? f.hops : []
      if (!hops.length) { toast('至少需要一跳落地', 'error'); return }
      if (hops.length > MAX_HOPS) { toast(`最多 ${MAX_HOPS} 跳`, 'error'); return }
      const seen = new Set()
      if (templateId) seen.add(templateId)
      for (let i = 0; i < hops.length; i++) {
        const h = hops[i]
        const label = i === hops.length - 1 ? '落地' : `第 ${i + 1} 跳`
        if (h.kind === 'panel') {
          const id = Number(h.inbound_id)
          if (!id) { toast(`请选择${label}节点`, 'error'); return }
          if (templateId && id === templateId) { toast('不能把被沿用的节点再当成这一跳', 'error'); return }
          if (seen.has(id)) { toast('路径里不能重复同一节点', 'error'); return }
          seen.add(id)
        } else if (h.kind === 'uri') {
          const t = parseShareURI(h.uri)
          if (!t.ok) { toast(t.error || `${label}链接无效`, 'error'); return }
        } else {
          if (!String(h.sk5_host || '').trim()) { toast(`请填写${label} SK5 主机`, 'error'); return }
          const p = Number(h.sk5_port)
          if (!Number.isInteger(p) || p < 1 || p > 65535) { toast(`${label} SK5 端口无效`, 'error'); return }
        }
      }
    }
    if (f.kind === 'port') {
      if (!String(f.dest_host || '').trim()) { toast('请填写目标地址', 'error'); return }
      const p = Number(f.dest_port)
      if (!Number.isInteger(p) || p < 1 || p > 65535) { toast('目标端口无效', 'error'); return }
    }
    const saveProfile = cloning ? (entryTemplate?.profile || '') : f.profile
    const saveCert = cloning ? (entryTemplate?.cert_id || 0) : f.cert_id
    if (f.kind !== 'port' && needsTLS(saveProfile) && !Number(saveCert)) {
      toast('该协议需要 TLS 证书', 'error')
      return
    }
    const wasEdit = !!editId
    if (wasEdit) {
      const prev = byID.get(Number(editId))
      if (prev && Number(f.port) !== Number(prev.port)) {
        if (!(await dialog.confirm({
          title: '修改端口',
          message: `入口端口将从 ${prev.port} 改为 ${f.port}，现有连接会断开。`,
          danger: true,
          okText: '改端口',
        }))) return
      }
    }
    const body = bodyFromForm()
    if (!body) { toast('请选择要沿用的节点', 'error'); return }
    setBusy(true)
    try {
      const d = wasEdit ? await api.put(`/inbounds/${editId}`, body) : await api.post('/inbounds', body)
      if (d.apply_error) toast(d.apply_error, 'error')
      else if (f.kind === 'chain') toast(wasEdit ? '已保存。到套餐里勾选这条入口，订阅才会带上。' : '已创建。到套餐里勾选这条入口，订阅才会带上。')
      else toast(wasEdit ? '已保存' : '已创建')
      closeForm()
      load()
    } catch (err) { toast(err.message, 'error') }
    finally { setBusy(false) }
  }

  const toggle = async (inb) => {
    const next = !inb.enabled
    if (!next) {
      if (!(await dialog.confirm({
        title: '停用中转',
        message: `将停掉「${inb.name || inb.port}」的入口端口，在线用户会立刻断开。`,
        danger: true,
        okText: '停用',
      }))) return
    }
    await ops.current.run(`tog-${inb.id}`, async () => {
      try {
        const d = await api.put(`/inbounds/${inb.id}`, {
          enabled: next,
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
        await load()
      } catch (err) { toast(err.message, 'error') }
    })
  }

  const probeLatency = async (inb) => {
    await ops.current.run(`probe-${inb.id}`, async () => {
      setProbes(p => ({ ...p, [inb.id]: { state: 'busy' } }))
      try {
        const d = await api.post(`/inbounds/${inb.id}/probe`)
        if (d.ok) {
          const ms = Number(d.latency_ms) || 0
          setProbes(p => ({ ...p, [inb.id]: { state: 'ok', ms, host: d.host, port: d.port } }))
          toast(`入口到 ${d.host}:${d.port}，${ms} ms`)
        } else {
          const error = d.error || '探测失败'
          setProbes(p => ({ ...p, [inb.id]: { state: 'err', error } }))
          toast(error, 'error')
        }
      } catch (e) {
        setProbes(p => ({ ...p, [inb.id]: { state: 'err', error: e.message } }))
        toast(e.message, 'error')
      }
    })
  }

  const del = async (inb) => {
    if (!(await dialog.confirm({ title: '删除中转', message: '入口会立刻停掉。链式入口会从订阅里消失。原来的节点不受影响。', danger: true }))) return
    await ops.current.run(`del-${inb.id}`, async () => {
      try {
        await api.del(`/inbounds/${inb.id}`)
        if (editId === inb.id) closeForm()
        await load()
      } catch (err) { toast(err.message, 'error') }
    })
  }

  const selectedServer = entryTemplate
    ? serversByID.get(Number(entryTemplate.server_id))
    : servers.find(s => Number(s.id) === Number(f.server_id))
  const entryCert = entryTemplate?.cert_id
    ? certs.find(c => Number(c.id) === Number(entryTemplate.cert_id))
    : null
  const hops = f.hops.length ? f.hops : [emptyHop()]
  const needsLanding = f.kind === 'chain' && hops.some(h => h.kind === 'panel')
  const showNewEntry = editId || f.kind === 'port' || f.entry_mode !== 'node'
  const groupedEntries = []
  if (!showNewEntry) {
    const seen = new Set()
    for (const s of servers) {
      const nodes = directs.filter(n => Number(n.server_id) === Number(s.id))
      if (!nodes.length) continue
      groupedEntries.push({ id: s.id, label: s.name, nodes })
      nodes.forEach(n => seen.add(Number(n.id)))
    }
    const rest = directs.filter(n => !seen.has(Number(n.id)))
    if (rest.length) groupedEntries.push({ id: 'rest', label: '其它', nodes: rest })
  }
  const nameHint = f.kind === 'chain' ? (suggestedName() || '可留空，按路径命名') : ''

  return (
    <div>
      <PageHead
        title="中转"
        desc="入口加一跳落地。需要时再加跳，最多 5 跳。保存后到套餐里勾选这条入口。"
        actions={
          <button type="button" className="btn-primary" onClick={openCreate}>
            <Icon name="plus" size={15} /> 新建中转
          </button>
        }
      />
      {pollErr ? <div className="alert-row is-warn mb-4">{pollErr}</div> : null}
      {forwards.length > 0 ? (
        <div className="flex flex-col sm:flex-row gap-2 mb-3">
          <SearchInput value={q} onChange={e => setQ(e.target.value)} placeholder="搜索入口 / 落地 / 实例" />
          <select className="input-field toolbar-select" value={kindFilter} onChange={e => setKindFilter(e.target.value)}>
            <option value="">全部类型</option>
            <option value="chain">链式</option>
            <option value="port">端口中转</option>
          </select>
        </div>
      ) : null}

      {!ready ? (
        <div className="card overflow-hidden"><SkeletonRows /></div>
      ) : forwards.length === 0 ? (
        <div className="card overflow-hidden">
          <Empty title="还没有中转" hint="默认入口加一跳落地。需要时再加跳。端口中转只把本机端口转到别人的 IP。" action={
            <button type="button" className="btn-primary" onClick={openCreate}>
              <Icon name="plus" size={15} /> 新建中转
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
            const n = k === 'chain' ? pathHops(inb).length : 0
            const st = forwardStatus(inb, byID, serversByID)
            const title = inb.name || (k === 'port' ? '端口中转' : '链式中转')
            const addr = `${inb.server_host || ''}${inb.port ? `:${inb.port}` : ''}`
            const row = probes[inb.id]
            const foot = !row
              ? (k === 'port' ? '从入口测到目标' : (n > 1 ? '从入口直连最后一跳' : '从入口测到落地'))
              : row.state === 'busy'
                ? '测量中…'
                : row.state === 'ok'
                  ? `${row.host}:${row.port}`
                  : (row.error || '探测失败')
            return (
              <div key={inb.id} className={`machine is-static ${forwardCardTone(st)}`}>
                <div className="machine-head">
                  <div className="min-w-0 flex-1">
                    <div className="machine-title">
                      <span className="machine-name truncate">{title}</span>
                      <LineStatus status={st} />
                    </div>
                    <div className="machine-host">
                      <span className="machine-host-addr">{inb.server_name || '—'}{addr ? ` · ${addr}` : ''}</span>
                    </div>
                  </div>
                  <div className="machine-toolbar">
                    <button type="button" className="icon-btn" onClick={() => startEdit(inb)} aria-label="编辑中转" title="编辑">
                      <Icon name="pencil" size={14} />
                    </button>
                    <MoreMenu iconOnly items={[
                      { label: '编辑', onSelect: () => startEdit(inb) },
                      { label: '测延迟', onSelect: () => probeLatency(inb) },
                      { label: inb.enabled ? '停用' : '启用', onSelect: () => toggle(inb) },
                      { sep: true },
                      { label: '删除', danger: true, onSelect: () => del(inb) },
                    ]} />
                  </div>
                </div>
                <div className="fwd-path">
                  <PathHops inb={inb} byID={byID} serversByID={serversByID} />
                  <div className="text-[11px] text-ink-mut mt-0.5">{pathSub(inb, byID)}{n > 1 ? ` · ${n} 跳` : ''}</div>
                </div>
                <div className="machine-metrics">
                  <Metric label="类型" value={kindLabel(k)} plain />
                  {k === 'port'
                    ? <Metric label="网络" value={pathSub(inb, byID)} plain />
                    : <Metric label="跳数" value={String(n || 1)} />}
                  <Metric label="延迟" value={latencyValue(row)} />
                  <Metric label="入口" value={inb.server_name || '—'} plain />
                </div>
                <div className="machine-foot">
                  <span className="fwd-probe-note" title={foot}>{foot}</span>
                  <button type="button" className="row-act" disabled={row?.state === 'busy'} onClick={() => probeLatency(inb)}>
                    {row?.state === 'busy' ? '测量中…' : '测延迟'}
                  </button>
                </div>
              </div>
            )
          })}
        </div>
      )}

      <Modal
        open={open}
        title={editId ? '编辑中转' : '新建中转'}
        onClose={closeForm}
        footer={(
          <>
            <button type="button" className="btn-ghost" onClick={closeForm}>取消</button>
            <button type="submit" form="fwd-form" className="btn-primary" disabled={busy}>{busy ? '保存中…' : '确定'}</button>
          </>
        )}
      >
        <form id="fwd-form" onSubmit={save} className="space-y-3">
          <Field label="类型" hint={editId ? '创建后不能改类型' : '链式：用户连入口，再按跳走。端口中转没有用户。'}>
            <select
              className="input-field"
              value={f.kind}
              disabled={!!editId}
              onChange={e => {
                const kind = e.target.value
                setF({ ...f, kind, hops: kind === 'chain' && (!f.hops || !f.hops.length) ? [emptyHop()] : f.hops })
              }}
            >
              <option value="chain">链式中转</option>
              <option value="port">端口中转</option>
            </select>
          </Field>
          {!editId && f.kind === 'chain' ? (
            <div>
              <div className="text-[12px] font-medium text-ink-soft mb-1.5">入口</div>
              <div className="grid grid-cols-2 gap-2">
                {[
                  { id: 'node', title: '沿用节点' },
                  { id: 'new', title: '新建入口' },
                ].map(opt => (
                  <button
                    key={opt.id}
                    type="button"
                    className={`node-pick ${f.entry_mode === opt.id ? 'is-on' : ''}`}
                    aria-pressed={f.entry_mode === opt.id}
                    onClick={() => setF({ ...f, entry_mode: opt.id })}
                  >
                    <span className="text-[13px] font-medium">{opt.title}</span>
                  </button>
                ))}
              </div>
            </div>
          ) : null}
          {showNewEntry ? (
            <Field label="入口实例">
              <select className="input-field" value={f.server_id} onChange={e => setF({ ...f, server_id: e.target.value })} required>
                <option value="">选择实例</option>
                {servers.map(s => <option key={s.id} value={s.id}>{s.name}{s.public_host ? ` · ${s.public_host}` : ''}</option>)}
              </select>
            </Field>
          ) : (
            <Field label="沿用节点" hint="会新建一条入口，协议和证书跟这个节点一样。原来的节点不动。">
              <select className="input-field" value={f.entry_id} onChange={e => setF({ ...f, entry_id: e.target.value })} required>
                <option value="">选择节点</option>
                {groupedEntries.map(g => (
                  <optgroup key={g.id} label={g.label}>
                    {g.nodes.map(n => (
                      <option key={n.id} value={n.id}>{n.name} · {protoShort(n.profile)} :{n.port}</option>
                    ))}
                  </optgroup>
                ))}
              </select>
            </Field>
          )}
          {f.kind !== 'port' && showNewEntry ? (
            <Field label="入口协议" hint={editId ? '创建后不能改协议' : '用户连这个协议，再转到后面的跳。'}>
              <select className="input-field" value={f.profile} onChange={e => setF({ ...f, profile: e.target.value })} disabled={!!editId}>
                {protoList.map(p => <option key={p.id} value={p.id}>{p.title}</option>)}
              </select>
            </Field>
          ) : null}
          {!showNewEntry && entryTemplate ? (
            <div className="text-[12px] text-ink-soft">
              {entryTemplate.server_name || '入口'} · {protoShort(entryTemplate.profile)}
              {entryTemplate.port ? ` :${entryTemplate.port}` : ''}
              {entryCert ? ` · 证书 ${entryCert.name || entryCert.domains || entryCert.id}` : (needsTLS(entryTemplate.profile) ? ' · 没有证书' : '')}
            </div>
          ) : null}
          {!showNewEntry && directs.length === 0 ? (
            <div className="notice">还没有可沿用的节点。可以改成在机器上新建入口。</div>
          ) : null}
          <div className="grid sm:grid-cols-2 gap-3">
            <Field label="名称" hint="可留空">
              <input className="input-field" value={f.name} onChange={e => setF({ ...f, name: e.target.value })} placeholder={nameHint} />
            </Field>
            <Field label="监听端口" hint={usedPortsText(selectedServer, list, editId)}>
              <input className="input-field font-mono tabular-nums" value={f.port} onChange={e => setF({ ...f, port: e.target.value })} placeholder={selectedServer ? `随机 ${formatPortRange(selectedServer)}` : '随机'} inputMode="numeric" />
            </Field>
          </div>
          {f.kind === 'chain' && (
            <div className="space-y-3">
              {hops.map((h, i) => {
                const last = i === hops.length - 1
                const used = hopUsedIds(hops, i)
                const opts = landings.filter(x => Number(x.id) !== templateId && (!used.has(Number(x.id)) || Number(x.id) === Number(h.inbound_id)))
                return (
                  <div
                    key={i}
                    className="space-y-3 pt-3"
                    style={i ? { borderTop: '1px solid var(--color-line-soft)' } : undefined}
                  >
                    <div className="flex items-center justify-between gap-2">
                      <div className="text-[12px] font-medium text-ink-soft">
                        {hops.length === 1 ? '落地' : (last ? `落地 · 第 ${i + 1} 跳` : `第 ${i + 1} 跳`)}
                      </div>
                      {hops.length > 1 ? (
                        <button type="button" className="btn-ghost px-2 text-[12px]" onClick={() => removeHop(i)}>去掉</button>
                      ) : null}
                    </div>
                    <Field label="这一跳">
                      <select className="input-field" value={h.kind} onChange={e => setHop(i, { kind: e.target.value })}>
                        <option value="panel">本面板节点</option>
                        <option value="socks">SK5</option>
                        <option value="uri">粘贴链接</option>
                      </select>
                    </Field>
                    {h.kind === 'panel' ? (
                      <Field label={last ? '落地节点' : '节点'} hint="只能选直出的 VLESS / Trojan / SS2022 / SOCKS5。">
                        <select className="input-field" value={h.inbound_id} onChange={e => setHop(i, { inbound_id: e.target.value })}>
                          <option value="">选择节点</option>
                          {opts.map(x => (
                            <option key={x.id} value={x.id}>{x.server_name} / {x.name} · {protoShort(x.profile)} :{x.port}</option>
                          ))}
                        </select>
                      </Field>
                    ) : h.kind === 'uri' ? (
                      <Field label={last ? '出口链接' : '节点链接'} hint="vless://、ss://、trojan://，也可以 socks5://。">
                        <textarea
                          className="input-field font-mono"
                          rows={3}
                          value={h.uri || ''}
                          onChange={e => setHop(i, { uri: e.target.value })}
                          placeholder="vless://uuid@host:443?security=reality&…"
                          spellCheck={false}
                        />
                        {String(h.uri || '').trim() ? (() => {
                          const t = parseShareURI(h.uri)
                          return (
                            <div className="text-[12px] mt-1.5" style={!t.ok ? { color: 'var(--color-danger)' } : undefined}>
                              {t.ok ? t.label : (t.error || '无法识别')}
                            </div>
                          )
                        })() : null}
                      </Field>
                    ) : (
                      <>
                        <div className="grid sm:grid-cols-2 gap-3">
                          <Field label="SK5 主机">
                            <input className="input-field font-mono" value={h.sk5_host} onChange={e => setHop(i, { sk5_host: e.target.value })} placeholder="1.2.3.4" />
                          </Field>
                          <Field label="SK5 端口">
                            <input className="input-field font-mono tabular-nums" value={h.sk5_port} onChange={e => setHop(i, { sk5_port: e.target.value })} inputMode="numeric" />
                          </Field>
                        </div>
                        <div className="grid sm:grid-cols-2 gap-3">
                          <Field label="用户" hint="可留空">
                            <input className="input-field" value={h.sk5_user} onChange={e => setHop(i, { sk5_user: e.target.value })} autoComplete="off" />
                          </Field>
                          <Field label="密码" hint="可留空">
                            <input className="input-field" type="password" value={h.sk5_pass} onChange={e => setHop(i, { sk5_pass: e.target.value })} autoComplete="new-password" />
                          </Field>
                        </div>
                      </>
                    )}
                  </div>
                )
              })}
              {hops.length < MAX_HOPS ? (
                <button type="button" className="btn-ghost" onClick={addHop}>
                  <Icon name="plus" size={15} /> 增加一跳
                </button>
              ) : (
                <div className="text-[11.5px] text-ink-mut">最多 {MAX_HOPS} 跳</div>
              )}
            </div>
          )}
          {f.kind === 'port' && (
            <>
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
          {f.kind !== 'port' && showNewEntry && needsTLS(f.profile) && (
            <Field label="证书">
              <select className="input-field" value={f.cert_id} onChange={e => setF({ ...f, cert_id: e.target.value })}>
                <option value="">选择证书</option>
                {certs.map(c => <option key={c.id} value={c.id}>{c.name || c.domains || c.id}</option>)}
              </select>
            </Field>
          )}
          {f.kind !== 'port' && showNewEntry && isReality(f.profile) && (
            <Field label="REALITY dest" hint="伪装目标。不要填苹果的网站。监听端口尽量用 443。可留空用默认。">
              <input className="input-field" value={f.dest} onChange={e => setF({ ...f, dest: e.target.value })} />
            </Field>
          )}
          {f.kind !== 'port' && showNewEntry && f.profile === 'ss2022' && (
            <Field label="加密" hint="256 更稳妥。改方法或密钥长度不对时会换成新密钥，旧订阅失效。">
              <select className="input-field" value={f.method} onChange={e => setF({ ...f, method: e.target.value })}>
                <option value="2022-blake3-aes-256-gcm">2022-blake3-aes-256-gcm</option>
                <option value="2022-blake3-aes-128-gcm">2022-blake3-aes-128-gcm</option>
              </select>
            </Field>
          )}
          {f.kind !== 'port' && showNewEntry && f.profile === 'mieru' && (
            <Field label="传输" hint="一台实例只能开一条。BOTH 同时开 TCP 和 UDP。">
              <select className="input-field" value={f.transport} onChange={e => setF({ ...f, transport: e.target.value })}>
                <option value="BOTH">BOTH（TCP + UDP）</option>
                <option value="TCP">仅 TCP</option>
                <option value="UDP">仅 UDP</option>
              </select>
            </Field>
          )}
          {needsLanding && landings.length === 0 ? (
            <div className="notice">还没有可落地的节点。先到「节点」增加 VLESS / Trojan / SS2022 / SOCKS5 直出。</div>
          ) : null}
          {f.kind === 'chain' && selectedServer && !selectedServer.public_host ? (
            <div className="notice">入口机还没填公开地址。落地能建，用户连入口时需要地址。</div>
          ) : null}
          {f.kind === 'chain' ? (
            <div className="notice">保存后到套餐里勾选这条入口，订阅才会带上。不会自动勾选。</div>
          ) : null}
        </form>
      </Modal>
    </div>
  )
}

export default function Forwards() {
  return <ForwardsPanel />
}
