import { useEffect, useMemo, useState } from 'react'
import { api } from '../lib/api'
import { useToast, useDialog } from '../components/Layout'
import { Badge, Empty, Field, FilterTabs, Icon, Modal, MoreMenu, PageHead, SearchInput } from '../components/ui'

const LAND_PROFILES = ['vless-reality', 'vless-reality-vision', 'vless-xhttp-tls', 'trojan-tls', 'ss2022']

const emptyForm = {
  kind: 'panel',
  server_id: '',
  name: '',
  profile: 'vless-reality-vision',
  port: '',
  cert_id: '',
  exit_inbound_id: '',
  dest: 'www.microsoft.com:443',
  method: '2022-blake3-aes-256-gcm',
  transport: 'BOTH',
  dest_host: '',
  dest_port: '',
  network: 'tcp',
  sk5_host: '',
  sk5_port: '1080',
  sk5_user: '',
  sk5_pass: '',
  enabled: true,
}

function protoShort(profile) {
  switch (profile) {
    case 'vless-reality-vision': return 'Vision'
    case 'vless-reality': return 'REALITY'
    case 'vless-xhttp-tls': return 'XHTTP'
    case 'trojan-tls': return 'Trojan'
    case 'ss2022': return 'SS2022'
    case 'anytls': return 'AnyTLS'
    case 'mieru': return 'Mieru'
    case 'port-forward': return '中转'
    default: return profile || ''
  }
}

function needsTLS(profile) {
  return profile === 'vless-xhttp-tls' || profile === 'trojan-tls' || profile === 'anytls'
}

function isReality(profile) {
  return String(profile).startsWith('vless-reality')
}

function inboundSettings(inb) {
  const st = inb?.settings
  if (!st) return {}
  if (typeof st === 'string') {
    try { return JSON.parse(st) || {} } catch { return {} }
  }
  return st
}

function forwardKind(inb) {
  if (!inb) return ''
  if (inb.profile === 'port-forward') return 'port'
  if (inb.exit_uri) return 'socks'
  if (inb.line_kind === 'chain') return 'panel'
  return ''
}

function kindLabel(k) {
  if (k === 'panel') return '链式'
  if (k === 'socks') return 'SK5'
  if (k === 'port') return '端口'
  return ''
}

function parseSocks(uri) {
  const out = { host: '', port: '1080', user: '', pass: '' }
  if (!uri) return out
  try {
    const u = new URL(uri)
    out.host = u.hostname || ''
    out.port = u.port || '1080'
    out.user = decodeURIComponent(u.username || '')
    out.pass = decodeURIComponent(u.password || '')
  } catch { /* ignore */ }
  return out
}

function formatSocks(host, port, user, pass) {
  host = String(host || '').trim()
  if (!host) return ''
  const p = Number(port) || 1080
  const hp = host.includes(':') && !host.startsWith('[') ? `[${host}]:${p}` : `${host}:${p}`
  const u = String(user || '').trim()
  const pw = String(pass || '')
  if (u || pw) return `socks5://${encodeURIComponent(u)}:${encodeURIComponent(pw)}@${hp}`
  return `socks5://${hp}`
}

function socksHostPort(uri) {
  const s = parseSocks(uri)
  if (!s.host) return 'SK5'
  return `${s.host}:${s.port || 1080}`
}

function landingText(inb, byID) {
  const k = forwardKind(inb)
  if (k === 'port') {
    const st = inboundSettings(inb)
    return `${st.dest_host || '?'}:${st.dest_port || '?'}`
  }
  if (k === 'socks') return socksHostPort(inb.exit_uri)
  const land = byID.get(Number(inb.exit_inbound_id))
  return land ? land.name : '落地已删除'
}

function entryText(inb) {
  if (forwardKind(inb) === 'port') return `:${inb.port}`
  return inb.name || `:${inb.port}`
}

function pathSub(inb, byID) {
  const k = forwardKind(inb)
  if (k === 'port') {
    const st = inboundSettings(inb)
    return st.network === 'tcp,udp' ? 'TCP + UDP' : 'TCP'
  }
  if (k === 'socks') return `${protoShort(inb.profile)} → SK5`
  const land = byID.get(Number(inb.exit_inbound_id))
  return `${protoShort(inb.profile)} → ${land ? protoShort(land.profile) : '—'}`
}

function usedPortsText(serverId, list, excludeId = 0) {
  const ports = list
    .filter(x => Number(x.server_id) === Number(serverId) && Number(x.id) !== Number(excludeId))
    .map(x => x.port)
  const used = ports.length ? `已用 ${[...new Set(ports)].sort((a, b) => a - b).join('、')}` : '这台服务器还没有节点'
  return `不填则随机。${used}`
}

function formFromInbound(inb) {
  const st = inboundSettings(inb)
  const sk = parseSocks(inb.exit_uri)
  return {
    kind: forwardKind(inb) || 'panel',
    server_id: inb.server_id,
    name: inb.name || '',
    profile: inb.profile === 'port-forward' ? 'vless-reality-vision' : inb.profile,
    port: inb.port,
    cert_id: inb.cert_id || '',
    exit_inbound_id: inb.exit_inbound_id || '',
    dest: st.dest || 'www.microsoft.com:443',
    method: st.method || '2022-blake3-aes-256-gcm',
    transport: st.transport || 'BOTH',
    dest_host: st.dest_host || '',
    dest_port: st.dest_port || '',
    network: st.network || 'tcp',
    sk5_host: sk.host,
    sk5_port: sk.port,
    sk5_user: sk.user,
    sk5_pass: sk.pass,
    enabled: inb.enabled !== false,
  }
}

export default function Forwards() {
  const toast = useToast()
  const dialog = useDialog()
  const [servers, setServers] = useState([])
  const [list, setList] = useState([])
  const [certs, setCerts] = useState([])
  const [profiles, setProfiles] = useState([])
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
      setServers(a.servers || [])
      setList(b.inbounds || [])
      setCerts(c.certs || [])
      setProfiles(d.profiles || [])
    } catch (e) { toast(e.message, 'error') }
  }
  useEffect(() => { load() }, [])

  const byID = useMemo(() => {
    const m = new Map()
    for (const x of list) m.set(Number(x.id), x)
    return m
  }, [list])

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
      const hay = [inb.name, inb.server_name, inb.server_host, inb.port, land, kindLabel(k), protoShort(inb.profile)]
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
    setF({ ...emptyForm, server_id: sid })
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
    const settings = {}
    const profile = f.profile
    if (isReality(profile) && String(f.dest || '').trim()) settings.dest = f.dest.trim()
    if (profile === 'ss2022') settings.method = f.method
    if (profile === 'mieru') settings.transport = f.transport
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
    if (kind === 'panel') body.exit_inbound_id = Number(f.exit_inbound_id) || 0
    else body.exit_uri = formatSocks(f.sk5_host, f.sk5_port, f.sk5_user, f.sk5_pass)
    return body
  }

  const save = async (e) => {
    e.preventDefault()
    if (!Number(f.server_id)) { toast('请选择入口服务器', 'error'); return }
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
    if (f.kind === 'panel' && !Number(f.exit_inbound_id)) {
      toast('请选择落地节点', 'error')
      return
    }
    if (f.kind === 'socks') {
      if (!String(f.sk5_host || '').trim()) { toast('请填写 SK5 主机', 'error'); return }
      const p = Number(f.sk5_port)
      if (!Number.isInteger(p) || p < 1 || p > 65535) { toast('SK5 端口无效', 'error'); return }
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

  return (
    <div>
      <PageHead
        title="转发"
        desc="谁转发到谁。链式和 SK5 用户连入口协议；端口中转没有用户，只把本机端口转到目标。"
        actions={
          <button type="button" className="btn-primary" onClick={openCreate}>
            <Icon name="plus" size={15} /> 新建转发
          </button>
        }
      />
      <div className="flex flex-col sm:flex-row gap-2 mb-3">
        <SearchInput value={q} onChange={e => setQ(e.target.value)} placeholder="搜索入口 / 落地 / 服务器" />
        <FilterTabs
          value={kindFilter}
          onChange={setKindFilter}
          items={[['', '全部'], ['panel', '链式'], ['socks', 'SK5'], ['port', '端口']]}
        />
      </div>

      <div className="card overflow-hidden">
        {forwards.length === 0 ? (
          <Empty title="还没有转发" hint="把入口指到本面板落地、SK5，或把本机端口转到别人的 IP。" action={
            <button type="button" className="btn-primary" onClick={openCreate}><Icon name="plus" size={15} /> 新建转发</button>
          } />
        ) : rows.length === 0 ? (
          <Empty title="没有匹配的转发" hint="换个关键词或类型筛选。" />
        ) : (
          <>
            <div className="hidden md:block table-wrap">
              <table className="data">
                <thead><tr><th>路径</th><th>类型</th><th>入口</th><th></th></tr></thead>
                <tbody>
                  {rows.map(inb => {
                    const k = forwardKind(inb)
                    return (
                      <tr key={inb.id} className={!inb.enabled ? 'opacity-50' : ''}>
                        <td className="min-w-0">
                          <div className="font-medium truncate">{entryText(inb)} → {landingText(inb, byID)}</div>
                          <div className="text-[11px] text-ink-mut mt-0.5">{pathSub(inb, byID)}</div>
                        </td>
                        <td>
                          <Badge tone="muted">{kindLabel(k)}</Badge>
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
                return (
                  <div key={inb.id} className={`px-3.5 py-3 ${!inb.enabled ? 'opacity-50' : ''}`}>
                    <div className="flex items-start justify-between gap-3">
                      <div className="min-w-0">
                        <div className="font-medium truncate">{entryText(inb)} → {landingText(inb, byID)}</div>
                        <div className="text-[12px] text-ink-mut mt-0.5">
                          {kindLabel(k)} · {pathSub(inb, byID)}
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
          {editId ? (
            <div className="text-[13px] text-ink-mut">类型：{kindLabel(f.kind)}</div>
          ) : (
            <div>
              <div className="text-[12px] font-medium text-ink-soft mb-1.5">类型</div>
              <FilterTabs
                value={f.kind}
                onChange={kind => setF({ ...f, kind })}
                items={[['panel', '本面板节点'], ['socks', 'SK5'], ['port', 'IP+端口']]}
              />
            </div>
          )}
          <Field label="入口服务器">
            <select className="input-field" value={f.server_id} onChange={e => setF({ ...f, server_id: e.target.value })} required>
              <option value="">选择服务器</option>
              {servers.map(s => <option key={s.id} value={s.id}>{s.name}{s.public_host ? ` · ${s.public_host}` : ''}</option>)}
            </select>
          </Field>
          {f.kind !== 'port' && (
            <Field label="入口协议" hint={editId ? '创建后不能改协议' : '用户连这个协议，再转到落地。'}>
              <select className="input-field" value={f.profile} onChange={e => setF({ ...f, profile: e.target.value })} disabled={!!editId}>
                {protoList.map(p => <option key={p.id} value={p.id}>{p.title}</option>)}
              </select>
            </Field>
          )}
          <div className="grid sm:grid-cols-2 gap-3">
            <Field label="名称" hint="可留空">
              <input className="input-field" value={f.name} onChange={e => setF({ ...f, name: e.target.value })} placeholder={entryProfile ? `${entryProfile}-端口` : ''} />
            </Field>
            <Field label="监听端口" hint={usedPortsText(f.server_id, list, editId)}>
              <input className="input-field font-mono tabular-nums" value={f.port} onChange={e => setF({ ...f, port: e.target.value })} placeholder="随机" inputMode="numeric" />
            </Field>
          </div>
          {f.kind === 'panel' && (
            <Field label="落地节点" hint="只能选直出的 VLESS / Trojan / SS2022。">
              <select className="input-field" value={f.exit_inbound_id} onChange={e => setF({ ...f, exit_inbound_id: e.target.value })}>
                <option value="">选择落地</option>
                {landings.map(x => (
                  <option key={x.id} value={x.id}>{x.server_name} / {x.name} · {protoShort(x.profile)} :{x.port}</option>
                ))}
              </select>
            </Field>
          )}
          {f.kind === 'socks' && (
            <>
              <div className="grid sm:grid-cols-2 gap-3">
                <Field label="SK5 主机">
                  <input className="input-field font-mono" value={f.sk5_host} onChange={e => setF({ ...f, sk5_host: e.target.value })} placeholder="1.2.3.4" />
                </Field>
                <Field label="SK5 端口">
                  <input className="input-field font-mono tabular-nums" value={f.sk5_port} onChange={e => setF({ ...f, sk5_port: e.target.value })} inputMode="numeric" />
                </Field>
              </div>
              <div className="grid sm:grid-cols-2 gap-3">
                <Field label="用户" hint="可留空">
                  <input className="input-field" value={f.sk5_user} onChange={e => setF({ ...f, sk5_user: e.target.value })} autoComplete="off" />
                </Field>
                <Field label="密码" hint="可留空">
                  <input className="input-field" type="password" value={f.sk5_pass} onChange={e => setF({ ...f, sk5_pass: e.target.value })} autoComplete="new-password" />
                </Field>
              </div>
            </>
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
          {f.kind === 'panel' && landings.length === 0 ? (
            <div className="notice">还没有可落地的节点。先到「服务器管理」增加 VLESS / Trojan / SS2022 直出。</div>
          ) : null}
          {f.kind === 'panel' && selectedServer && !selectedServer.public_host ? (
            <div className="notice">入口机还没填公开地址。落地能建，用户连入口时需要地址。</div>
          ) : null}
        </form>
      </Modal>
    </div>
  )
}
