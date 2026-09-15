import { useEffect, useMemo, useState } from 'react'
import { api } from '../lib/api'
import { peekList, putList } from '../lib/listCache'
import { useToast, useDialog } from '../components/Layout'
import { formatPortRange } from '../lib/ports'
import { hopStatus, nodeStatus } from '../lib/status'
import { parseShareURI } from '../lib/share'
import {
  LAND_PROFILES, MAX_HOPS, inboundSettings, forwardKind, kindLabel, protoShort,
  formatSocks, hopText, hopProto, pathHops, landingText, usedPortsText, hopFromSaved,
} from '../lib/forwards'
import { Badge, Empty, Field, Icon, LineStatus, Modal, MoreMenu, PageHead, SearchInput, SkeletonRows } from '../components/ui'

function emptyHop() {
  return { kind: 'panel', inbound_id: '', sk5_host: '', sk5_port: '1080', sk5_user: '', sk5_pass: '', uri: '' }
}

const emptyForm = {
  kind: 'chain',
  server_id: '',
  name: '',
  profile: 'vless-reality-vision',
  port: '',
  cert_id: '',
  dest: 'www.microsoft.com:443',
  method: '2022-blake3-aes-256-gcm',
  transport: 'BOTH',
  dest_host: '',
  dest_port: '',
  network: 'tcp',
  hops: [emptyHop()],
  enabled: true,
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
  return inb.name || `:${inb.port}`
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

function formFromInbound(inb) {
  const st = inboundSettings(inb)
  return {
    kind: forwardKind(inb) || 'chain',
    server_id: inb.server_id,
    name: inb.name || '',
    profile: inb.profile === 'port-forward' ? 'vless-reality-vision' : inb.profile,
    port: inb.port,
    cert_id: inb.cert_id || '',
    dest: st.dest || 'www.microsoft.com:443',
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

export default function Forwards() {
  const toast = useToast()
  const dialog = useDialog()
  const [servers, setServers] = useState(() => peekList('servers') ?? [])
  const [list, setList] = useState(() => peekList('inbounds') ?? [])
  const [certs, setCerts] = useState(() => peekList('certs') ?? [])
  const [profiles, setProfiles] = useState(() => peekList('profiles') ?? [])
  const [ready, setReady] = useState(() => peekList('inbounds') !== undefined)
  const [f, setF] = useState(emptyForm)
  const [editId, setEditId] = useState(0)
  const [open, setOpen] = useState(false)
  const [busy, setBusy] = useState(false)
  const [q, setQ] = useState('')
  const [kindFilter, setKindFilter] = useState('')

  const load = async () => {
    try {
      const [a, b, c, d] = await Promise.all([
        api.get('/servers'), api.get('/inbounds'), api.get('/certs'), api.get('/profiles'),
      ])
      setServers(putList('servers', a.servers || []))
      setList(putList('inbounds', b.inbounds || []))
      setCerts(putList('certs', c.certs || []))
      setProfiles(putList('profiles', d.profiles || []))
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

  const forwards = useMemo(() => list.filter(x => forwardKind(x)), [list])
  const landings = useMemo(
    () => list.filter(x => x.line_kind === 'direct' && LAND_PROFILES.includes(x.profile) && Number(x.id) !== Number(editId)),
    [list, editId],
  )
  const protoList = profiles.length ? profiles : [{ id: f.profile, title: f.profile, desc: '' }]

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
    const sid = servers[0]?.id || ''
    setF({ ...emptyForm, server_id: sid, hops: [emptyHop()] })
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
    const existing = editId ? inboundSettings(byID.get(Number(editId))) : {}
    const settings = { ...existing }
    const profile = f.profile
    if (isReality(profile) && String(f.dest || '').trim()) settings.dest = f.dest.trim()
    if (profile === 'ss2022') settings.method = f.method
    if (profile === 'mieru') settings.transport = f.transport
    const hops = f.hops.length ? f.hops : [emptyHop()]
    const last = hops[hops.length - 1]
    const mids = hops.slice(0, -1).map(serializeHop)
    if (mids.length) settings.hops = mids
    else delete settings.hops
    const body = {
      server_id: Number(f.server_id),
      name: f.name,
      profile,
      port: Number(f.port) || 0,
      listen: '0.0.0.0',
      line_kind: 'chain',
      enabled: f.enabled !== false,
      settings,
      exit_inbound_id: 0,
      exit_uri: '',
    }
    if (f.cert_id) body.cert_id = Number(f.cert_id)
    if (last.kind === 'panel') body.exit_inbound_id = Number(last.inbound_id) || 0
    else if (last.kind === 'uri') body.exit_uri = String(last.uri || '').trim()
    else body.exit_uri = formatSocks(last.sk5_host, last.sk5_port, last.sk5_user, last.sk5_pass)
    return body
  }

  const save = async (e) => {
    e.preventDefault()
    if (!Number(f.server_id)) { toast('请选择入口实例', 'error'); return }
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
      for (let i = 0; i < hops.length; i++) {
        const h = hops[i]
        const label = i === hops.length - 1 ? '落地' : `第 ${i + 1} 跳`
        if (h.kind === 'panel') {
          const id = Number(h.inbound_id)
          if (!id) { toast(`请选择${label}节点`, 'error'); return }
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
    if (f.kind !== 'port' && needsTLS(f.profile) && !Number(f.cert_id)) {
      toast('该协议需要 TLS 证书', 'error')
      return
    }
    const wasEdit = !!editId
    setBusy(true)
    try {
      const d = wasEdit ? await api.put(`/inbounds/${editId}`, bodyFromForm()) : await api.post('/inbounds', bodyFromForm())
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
    if (!(await dialog.confirm({ title: '删除转发', message: '入口端口会立刻停掉。链式入口会从订阅里消失。', danger: true }))) return
    try {
      await api.del(`/inbounds/${inb.id}`)
      if (editId === inb.id) closeForm()
      load()
    } catch (err) { toast(err.message, 'error') }
  }

  const selectedServer = servers.find(s => Number(s.id) === Number(f.server_id))
  const entryProfile = f.kind === 'port' ? 'port-forward' : f.profile
  const hops = f.hops.length ? f.hops : [emptyHop()]
  const needsLanding = f.kind === 'chain' && hops.some(h => h.kind === 'panel')

  return (
    <div>
      <PageHead
        title="转发"
        actions={
          <button type="button" className="btn-primary" onClick={openCreate}>
            <Icon name="plus" size={15} /> 新建转发
          </button>
        }
      />
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

      <div className="card overflow-hidden">
        {!ready ? (
          <SkeletonRows />
        ) : forwards.length === 0 ? (
          <Empty title="还没有转发" hint="点右上角新建。链式按跳走；端口中转只把本机端口转到别人的 IP。" />
        ) : rows.length === 0 ? (
          <Empty title="没有匹配的转发" hint="换个关键词或类型。" />
        ) : (
          <>
            <div className="hidden md:block table-wrap">
              <table className="data">
                <thead><tr><th>路径</th><th>类型</th><th>入口</th><th></th></tr></thead>
                <tbody>
                  {rows.map(inb => {
                    const k = forwardKind(inb)
                    const n = k === 'chain' ? pathHops(inb).length : 0
                    return (
                      <tr key={inb.id} className={!inb.enabled ? 'opacity-50' : ''}>
                        <td className="min-w-0">
                          <PathHops inb={inb} byID={byID} serversByID={serversByID} />
                          <div className="text-[11px] text-ink-mut mt-0.5">{pathSub(inb, byID)}</div>
                        </td>
                        <td>
                          <Badge tone="muted">{kindLabel(k)}</Badge>
                          {n > 1 ? <span className="ml-1 text-[11px] text-ink-mut tabular-nums">{n} 跳</span> : null}
                          {!inb.enabled ? <Badge tone="muted" className="ml-1">停用</Badge> : null}
                        </td>
                        <td className="min-w-0">
                          <div className="truncate">{inb.server_name || '—'}</div>
                          <div className="text-[11px] text-ink-mut font-mono tabular-nums truncate">{inb.server_host || ''}{inb.port ? `:${inb.port}` : ''}</div>
                        </td>
                        <td className="whitespace-nowrap">
                          <div className="icon-row">
                            <button type="button" className="icon-btn" onClick={() => startEdit(inb)} aria-label="编辑转发" title="编辑">
                              <Icon name="pencil" size={14} />
                            </button>
                            <MoreMenu iconOnly items={[
                              { label: inb.enabled ? '停用' : '启用', onSelect: () => toggle(inb) },
                              { sep: true },
                              { label: '删除', danger: true, onSelect: () => del(inb) },
                            ]} />
                          </div>
                        </td>
                      </tr>
                    )
                  })}
                </tbody>
              </table>
            </div>
            <div className="md:hidden divide-y" style={{ borderColor: 'var(--color-line-soft)' }}>
              {rows.map(inb => {
                const k = forwardKind(inb)
                const n = k === 'chain' ? pathHops(inb).length : 0
                return (
                  <div key={inb.id} className={`px-3.5 py-3 ${!inb.enabled ? 'opacity-50' : ''}`}>
                    <div className="flex items-start justify-between gap-3">
                      <div className="min-w-0">
                        <PathHops inb={inb} byID={byID} serversByID={serversByID} />
                        <div className="text-[12px] text-ink-mut mt-0.5">
                          {kindLabel(k)}{n > 1 ? ` · ${n} 跳` : ''} · {pathSub(inb, byID)}
                          {!inb.enabled ? ' · 停用' : ''}
                        </div>
                        <div className="text-[12px] text-ink-mut mt-0.5 truncate">
                          {inb.server_name || '—'} {inb.server_host || ''}{inb.port ? `:${inb.port}` : ''}
                        </div>
                      </div>
                      <div className="icon-row shrink-0">
                        <button type="button" className="icon-btn" onClick={() => startEdit(inb)} aria-label="编辑转发" title="编辑">
                          <Icon name="pencil" size={14} />
                        </button>
                        <MoreMenu iconOnly items={[
                          { label: inb.enabled ? '停用' : '启用', onSelect: () => toggle(inb) },
                          { sep: true },
                          { label: '删除', danger: true, onSelect: () => del(inb) },
                        ]} />
                      </div>
                    </div>
                  </div>
                )
              })}
            </div>
          </>
        )}
      </div>

      <Modal
        open={open}
        title={editId ? '编辑转发' : '新建转发'}
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
              <option value="chain">链式转发</option>
              <option value="port">端口中转</option>
            </select>
          </Field>
          <Field label="入口实例">
            <select className="input-field" value={f.server_id} onChange={e => setF({ ...f, server_id: e.target.value })} required>
              <option value="">选择实例</option>
              {servers.map(s => <option key={s.id} value={s.id}>{s.name}{s.public_host ? ` · ${s.public_host}` : ''}</option>)}
            </select>
          </Field>
          {f.kind !== 'port' && (
            <Field label="入口协议" hint={editId ? '创建后不能改协议' : '用户连这个协议，再转到后面的跳。'}>
              <select className="input-field" value={f.profile} onChange={e => setF({ ...f, profile: e.target.value })} disabled={!!editId}>
                {protoList.map(p => <option key={p.id} value={p.id}>{p.title}</option>)}
              </select>
            </Field>
          )}
          <div className="grid sm:grid-cols-2 gap-3">
            <Field label="名称" hint="可留空">
              <input className="input-field" value={f.name} onChange={e => setF({ ...f, name: e.target.value })} placeholder={entryProfile ? `${entryProfile}-端口` : ''} />
            </Field>
            <Field label="监听端口" hint={usedPortsText(serversByID.get(Number(f.server_id)), list, editId)}>
              <input className="input-field font-mono tabular-nums" value={f.port} onChange={e => setF({ ...f, port: e.target.value })} placeholder={f.server_id ? `随机 ${formatPortRange(serversByID.get(Number(f.server_id)))}` : '随机'} inputMode="numeric" />
            </Field>
          </div>
          {f.kind === 'chain' && (
            <div className="space-y-3">
              {hops.map((h, i) => {
                const last = i === hops.length - 1
                const used = hopUsedIds(hops, i)
                const opts = landings.filter(x => !used.has(Number(x.id)) || Number(x.id) === Number(h.inbound_id))
                return (
                  <div
                    key={i}
                    className="space-y-3 pt-3"
                    style={i ? { borderTop: '1px solid var(--color-line-soft)' } : undefined}
                  >
                    <div className="flex items-center justify-between gap-2">
                      <div className="text-[12px] font-medium text-ink-soft">
                        {last ? `落地 · 第 ${i + 1} 跳` : `第 ${i + 1} 跳`}
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
          {f.kind !== 'port' && needsTLS(f.profile) && (
            <Field label="证书">
              <select className="input-field" value={f.cert_id} onChange={e => setF({ ...f, cert_id: e.target.value })}>
                <option value="">选择证书</option>
                {certs.map(c => <option key={c.id} value={c.id}>{c.name || c.domains || c.id}</option>)}
              </select>
            </Field>
          )}
          {f.kind !== 'port' && isReality(f.profile) && (
            <Field label="REALITY dest" hint="伪装目标。可留空用默认。">
              <input className="input-field" value={f.dest} onChange={e => setF({ ...f, dest: e.target.value })} />
            </Field>
          )}
          {f.kind !== 'port' && f.profile === 'ss2022' && (
            <Field label="加密">
              <select className="input-field" value={f.method} onChange={e => setF({ ...f, method: e.target.value })}>
                <option value="2022-blake3-aes-256-gcm">2022-blake3-aes-256-gcm</option>
                <option value="2022-blake3-aes-128-gcm">2022-blake3-aes-128-gcm</option>
              </select>
            </Field>
          )}
          {f.kind !== 'port' && f.profile === 'mieru' && (
            <Field label="传输">
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
        </form>
      </Modal>
    </div>
  )
}
