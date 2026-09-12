import { useEffect, useState } from 'react'
import { api } from '../lib/api'
import { useToast, useDialog } from '../components/Layout'
import { Badge, Empty, Field, Icon, PageHead } from '../components/ui'

const SUGGESTED_PORTS = [8443, 8444, 2053, 2083, 2087, 2096, 8880, 9443, 10443, 11443]

const empty = {
  server_id: 0, name: '', profile: 'vless-reality-vision', port: 8443, listen: '0.0.0.0',
  line_kind: 'direct', exit_inbound_id: 0, cert_id: 0, enabled: true,
  dest: 'www.cloudflare.com:443', sni: '', path: '', method: '2022-blake3-aes-128-gcm', transport: 'TCP',
}

function nextPort(serverId, list, excludeId = 0) {
  const used = new Set(
    list.filter(x => Number(x.server_id) === Number(serverId) && Number(x.id) !== Number(excludeId))
      .map(x => Number(x.port)),
  )
  for (const p of SUGGESTED_PORTS) {
    if (!used.has(p)) return p
  }
  for (let p = 10000; p < 60000; p++) {
    if (!used.has(p)) return p
  }
  return 8443
}

function usedPortsText(serverId, list, excludeId = 0) {
  const ports = list
    .filter(x => Number(x.server_id) === Number(serverId) && Number(x.id) !== Number(excludeId))
    .map(x => x.port)
  return ports.length ? `已用 ${[...new Set(ports)].sort((a, b) => a - b).join('、')}` : '该节点还没有入站'
}

function serverHasCore(s, core) {
  if (!s?.cores) return true
  const have = String(s.cores).split(',').map(x => x.trim().toLowerCase().replace('sing-box', 'singbox')).filter(Boolean)
  if (!have.length) return true
  const want = String(core || 'xray').toLowerCase().replace('sing-box', 'singbox')
  return have.includes(want)
}

function formFromInbound(inb) {
  const st = inb.settings || {}
  return {
    server_id: inb.server_id,
    name: inb.name || '',
    profile: inb.profile,
    port: inb.port,
    listen: inb.listen || '0.0.0.0',
    line_kind: inb.line_kind || 'direct',
    exit_inbound_id: inb.exit_inbound_id || 0,
    cert_id: inb.cert_id || 0,
    enabled: inb.enabled !== false,
    dest: st.dest || 'www.cloudflare.com:443',
    sni: st.sni || '',
    path: st.path || '',
    method: st.method || '2022-blake3-aes-128-gcm',
    transport: st.transport || 'TCP',
    settings: st,
  }
}

export default function Inbounds() {
  const toast = useToast()
  const dialog = useDialog()
  const [list, setList] = useState([])
  const [servers, setServers] = useState([])
  const [certs, setCerts] = useState([])
  const [profiles, setProfiles] = useState([])
  const [f, setF] = useState(empty)
  const [editId, setEditId] = useState(0)
  const [busy, setBusy] = useState(false)

  const load = async () => {
    try {
      const [a, b, c, d] = await Promise.all([
        api.get('/inbounds'), api.get('/servers'), api.get('/certs'), api.get('/profiles'),
      ])
      setList(a.inbounds || [])
      setServers(b.servers || [])
      setCerts(c.certs || [])
      setProfiles(d.profiles || [])
    } catch (e) { toast(e.message, 'error') }
  }
  useEffect(() => { load() }, [])

  useEffect(() => {
    if (editId || Number(f.server_id) || servers.length !== 1) return
    const sid = servers[0].id
    setF(prev => ({ ...prev, server_id: sid, port: nextPort(sid, list) }))
  }, [servers, list, editId, f.server_id])

  const meta = profiles.find(p => p.id === f.profile)
  const selectedServer = servers.find(s => Number(s.id) === Number(f.server_id))
  const missingCore = selectedServer && meta && !serverHasCore(selectedServer, meta.core)

  const pickServer = (sid) => {
    const n = Number(sid) || 0
    const port = nextPort(n, list, editId)
    setF({ ...f, server_id: sid, port })
  }

  const resetForm = () => {
    setEditId(0)
    const sid = servers.length === 1 ? servers[0].id : 0
    setF({ ...empty, server_id: sid, port: sid ? nextPort(sid, list) : 8443 })
  }

  const startEdit = (inb) => {
    setEditId(inb.id)
    setF(formFromInbound(inb))
    window.scrollTo({ top: 0, behavior: 'smooth' })
  }

  const bodyFromForm = () => {
    const settings = { ...(f.settings || {}) }
    if (String(f.profile).startsWith('vless-reality')) {
      settings.dest = f.dest
      const host = (f.dest || '').split(':')[0]
      if (host) settings.server_names = [host]
    }
    if (f.sni) settings.sni = f.sni
    if (f.path) settings.path = f.path
    if (f.profile === 'ss2022') settings.method = f.method
    if (f.profile === 'mieru') settings.transport = f.transport
    const body = {
      server_id: Number(f.server_id),
      name: f.name,
      profile: f.profile,
      port: Number(f.port),
      listen: f.listen || '0.0.0.0',
      line_kind: f.line_kind,
      enabled: f.enabled !== false,
      settings,
    }
    if (f.cert_id) body.cert_id = Number(f.cert_id)
    if (f.line_kind === 'chain' && f.exit_inbound_id) body.exit_inbound_id = Number(f.exit_inbound_id)
    return body
  }

  const submit = async (e) => {
    e.preventDefault()
    const port = Number(f.port)
    if (!Number.isInteger(port) || port < 1 || port > 65535) {
      toast('端口范围 1–65535', 'error')
      return
    }
    if (missingCore) {
      toast(`节点未安装 ${meta.core}，换协议或先装内核`, 'error')
      return
    }
    const body = bodyFromForm()
    setBusy(true)
    try {
      const d = editId ? await api.put(`/inbounds/${editId}`, body) : await api.post('/inbounds', body)
      if (d.apply_error) toast(d.apply_error, 'error')
      else toast(editId ? '已保存' : '已创建')
      resetForm()
      load()
    } catch (e) { toast(e.message, 'error') }
    finally { setBusy(false) }
  }

  const del = async (id) => {
    if (!(await dialog.confirm({ title: '删除入站', message: '订阅里对应的节点会立刻消失。', danger: true }))) return
    try { await api.del(`/inbounds/${id}`); if (editId === id) resetForm(); load() }
    catch (e) { toast(e.message, 'error') }
  }

  const toggle = async (inb) => {
    try {
      const d = await api.put(`/inbounds/${inb.id}`, {
        enabled: !inb.enabled, name: inb.name, port: inb.port, listen: inb.listen,
        settings: inb.settings, line_kind: inb.line_kind, exit_inbound_id: inb.exit_inbound_id, cert_id: inb.cert_id,
      })
      if (d.apply_error) toast(d.apply_error, 'error')
      load()
    } catch (e) { toast(e.message, 'error') }
  }

  const landings = list.filter(x => x.line_kind === 'direct' && ['vless-reality', 'vless-reality-vision', 'vless-xhttp-tls', 'trojan-tls', 'ss2022'].includes(x.profile))

  return (
    <div>
      <PageHead kicker="Lines" title="入站" desc="一条入站就是一条线路。端口可自定义；被占用的端口会自动停用，不会拖垮其它线路。" />
      <form onSubmit={submit} className="card p-5 mb-4">
        <div className="kicker mb-4">{editId ? '编辑线路' : '新建线路'}</div>
        <div className="grid grid-cols-1 md:grid-cols-3 gap-3">
          <Field label="服务器">
            <select className="input-field" value={f.server_id} onChange={e => pickServer(e.target.value)} required disabled={!!editId}>
              <option value="">选择</option>
              {servers.map(s => <option key={s.id} value={s.id}>{s.name}{s.cores ? ` · ${s.cores}` : ''}</option>)}
            </select>
          </Field>
          <Field label="协议">
            <select className="input-field" value={f.profile} onChange={e => setF({ ...f, profile: e.target.value })} disabled={!!editId}>
              {(profiles.length ? profiles : [{ id: f.profile, title: f.profile }]).map(p => <option key={p.id} value={p.id}>{p.title}</option>)}
            </select>
          </Field>
          <Field label="名称">
            <input className="input-field" value={f.name} onChange={e => setF({ ...f, name: e.target.value })} required placeholder="HK-8443" />
          </Field>
          <Field label="端口" hint={f.server_id ? usedPortsText(f.server_id, list, editId) : '1–65535，不要用已被占用的口'}>
            <input className="input-field" type="number" min="1" max="65535" value={f.port} onChange={e => setF({ ...f, port: e.target.value })} required />
          </Field>
          <Field label="监听地址" hint="一般保持 0.0.0.0">
            <input className="input-field" value={f.listen} onChange={e => setF({ ...f, listen: e.target.value })} />
          </Field>
          {meta?.need_tls && (
            <Field label="TLS 证书">
              <select className="input-field" value={f.cert_id} onChange={e => setF({ ...f, cert_id: e.target.value })}>
                <option value="">选择证书</option>
                {certs.map(c => <option key={c.id} value={c.id}>{c.name}</option>)}
              </select>
            </Field>
          )}
          {String(f.profile).startsWith('vless-reality') && (
            <Field label="REALITY dest">
              <input className="input-field" value={f.dest} onChange={e => setF({ ...f, dest: e.target.value })} />
            </Field>
          )}
          {(f.profile === 'vless-xhttp-tls' || f.profile === 'trojan-tls' || f.profile === 'anytls') && (
            <Field label="SNI">
              <input className="input-field" value={f.sni} onChange={e => setF({ ...f, sni: e.target.value })} />
            </Field>
          )}
          {f.profile === 'vless-xhttp-tls' && (
            <Field label="Path" hint="可留空自动生成">
              <input className="input-field" value={f.path} onChange={e => setF({ ...f, path: e.target.value })} />
            </Field>
          )}
          {f.profile === 'ss2022' && (
            <Field label="方法">
              <select className="input-field" value={f.method} onChange={e => setF({ ...f, method: e.target.value })}>
                <option>2022-blake3-aes-128-gcm</option>
                <option>2022-blake3-aes-256-gcm</option>
              </select>
            </Field>
          )}
          {f.profile === 'mieru' && (
            <Field label="传输">
              <select className="input-field" value={f.transport} onChange={e => setF({ ...f, transport: e.target.value })}>
                <option>TCP</option>
                <option>UDP</option>
                <option>BOTH</option>
              </select>
            </Field>
          )}
          <Field label="线路">
            <select className="input-field" value={f.line_kind} onChange={e => setF({ ...f, line_kind: e.target.value })} disabled={!!editId}>
              <option value="direct">直出</option>
              <option value="chain">链式（本机入口 → 另一台落地）</option>
            </select>
          </Field>
          {f.line_kind === 'chain' && (
            <Field label="落地入站" hint="不可选 Mieru / AnyTLS">
              <select className="input-field" value={f.exit_inbound_id} onChange={e => setF({ ...f, exit_inbound_id: e.target.value })}>
                <option value="">选择落地</option>
                {landings.map(x => <option key={x.id} value={x.id}>{x.server_name} / {x.name}</option>)}
              </select>
            </Field>
          )}
        </div>
        {Number(f.port) === 443 && (
          <div className="notice mt-4">443 很容易被 Nginx / 其它面板占用。建议改成 8443 或其它空闲端口。</div>
        )}
        {missingCore && (
          <div className="notice mt-4">这台节点没有 {meta.core}，该协议下发后不会生效。请换 VLESS / SS2022，或先安装内核。</div>
        )}
        <div className="mt-4 flex gap-2">
          <button className="btn-primary" disabled={busy}>
            <Icon name={editId ? 'check' : 'plus'} size={16} /> {editId ? '保存入站' : '创建入站'}
          </button>
          {editId ? (
            <button type="button" className="btn-ghost" onClick={resetForm}>取消编辑</button>
          ) : null}
        </div>
      </form>
      <div className="card overflow-hidden">
        {list.length === 0 ? (
          <Empty title="暂无入站" hint="选一台在线服务器，填自定义端口，挑一种协议。" />
        ) : (
          <div className="table-wrap">
            <table className="data">
              <thead><tr><th>名称</th><th>服务器</th><th>协议</th><th>端口</th><th>线路</th><th>内核</th><th></th></tr></thead>
              <tbody>
                {list.map(inb => {
                  const srv = servers.find(s => s.id === inb.server_id)
                  const dead = srv && !serverHasCore(srv, inb.core)
                  return (
                    <tr key={inb.id} className={!inb.enabled ? 'opacity-50' : ''}>
                      <td className="font-medium">{inb.name}</td>
                      <td>{inb.server_name}</td>
                      <td><Badge tone="gold">{inb.profile}</Badge></td>
                      <td className="tabular-nums">{inb.port}</td>
                      <td>{inb.line_kind === 'chain' ? '链式' : '直出'}</td>
                      <td className="text-ink-mut">{inb.core}{dead ? ' · 未安装' : ''}{!inb.enabled ? ' · 停用' : ''}</td>
                      <td className="whitespace-nowrap">
                        <div className="flex gap-3 justify-end">
                          <button type="button" className="linkish" onClick={() => startEdit(inb)}>编辑</button>
                          <button type="button" className="linkish" onClick={() => toggle(inb)}>{inb.enabled ? '停用' : '启用'}</button>
                          <button type="button" className="linkish" style={{ color: 'var(--color-danger)' }} onClick={() => del(inb.id)}>删除</button>
                        </div>
                      </td>
                    </tr>
                  )
                })}
              </tbody>
            </table>
          </div>
        )}
      </div>
    </div>
  )
}
