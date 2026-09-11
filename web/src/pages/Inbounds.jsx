import { useEffect, useState } from 'react'
import { api } from '../lib/api'
import { useToast, useDialog } from '../components/Layout'
import { Badge, Empty, Field, Icon, PageHead } from '../components/ui'

const empty = {
  server_id: 0, name: '', profile: 'vless-reality-vision', port: 443, listen: '0.0.0.0',
  line_kind: 'direct', exit_inbound_id: 0, cert_id: 0,
  dest: 'www.cloudflare.com:443', sni: '', path: '', method: '2022-blake3-aes-128-gcm', transport: 'TCP',
}

export default function Inbounds() {
  const toast = useToast()
  const dialog = useDialog()
  const [list, setList] = useState([])
  const [servers, setServers] = useState([])
  const [certs, setCerts] = useState([])
  const [profiles, setProfiles] = useState([])
  const [f, setF] = useState(empty)
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

  const meta = profiles.find(p => p.id === f.profile)

  const submit = async (e) => {
    e.preventDefault()
    const settings = {}
    if (f.profile.startsWith('vless-reality')) {
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
      settings,
    }
    if (f.cert_id) body.cert_id = Number(f.cert_id)
    if (f.line_kind === 'chain' && f.exit_inbound_id) body.exit_inbound_id = Number(f.exit_inbound_id)
    setBusy(true)
    try {
      await api.post('/inbounds', body)
      setF({ ...empty, server_id: f.server_id })
      toast('已创建')
      load()
    } catch (e) { toast(e.message, 'error') }
    finally { setBusy(false) }
  }

  const del = async (id) => {
    if (!(await dialog.confirm({ title: '删除入站', message: '订阅里对应的节点会立刻消失。', danger: true }))) return
    try { await api.del(`/inbounds/${id}`); load() }
    catch (e) { toast(e.message, 'error') }
  }

  const toggle = async (inb) => {
    try {
      await api.put(`/inbounds/${inb.id}`, { enabled: !inb.enabled, name: inb.name, port: inb.port, settings: inb.settings, line_kind: inb.line_kind, exit_inbound_id: inb.exit_inbound_id, cert_id: inb.cert_id })
      load()
    } catch (e) { toast(e.message, 'error') }
  }

  const landings = list.filter(x => x.line_kind === 'direct' && ['vless-reality', 'vless-reality-vision', 'vless-xhttp-tls', 'trojan-tls', 'ss2022'].includes(x.profile))

  return (
    <div>
      <PageHead kicker="Lines" title="入站" desc="一条入站就是一条线路。链式转发的落地只能是 Xray 家族；Mieru / AnyTLS 不能当落地。" />
      <form onSubmit={submit} className="card p-5 mb-4">
        <div className="kicker mb-4">新建线路</div>
        <div className="grid grid-cols-1 md:grid-cols-3 gap-3">
          <Field label="服务器">
            <select className="input-field" value={f.server_id} onChange={e => setF({ ...f, server_id: e.target.value })} required>
              <option value="">选择</option>
              {servers.map(s => <option key={s.id} value={s.id}>{s.name}</option>)}
            </select>
          </Field>
          <Field label="协议">
            <select className="input-field" value={f.profile} onChange={e => setF({ ...f, profile: e.target.value })}>
              {(profiles.length ? profiles : [{ id: f.profile, title: f.profile }]).map(p => <option key={p.id} value={p.id}>{p.title}</option>)}
            </select>
          </Field>
          <Field label="名称">
            <input className="input-field" value={f.name} onChange={e => setF({ ...f, name: e.target.value })} required placeholder="HK-443" />
          </Field>
          <Field label="端口">
            <input className="input-field" type="number" value={f.port} onChange={e => setF({ ...f, port: e.target.value })} required />
          </Field>
          {meta?.need_tls && (
            <Field label="TLS 证书">
              <select className="input-field" value={f.cert_id} onChange={e => setF({ ...f, cert_id: e.target.value })}>
                <option value="">选择证书</option>
                {certs.map(c => <option key={c.id} value={c.id}>{c.name}</option>)}
              </select>
            </Field>
          )}
          {f.profile.startsWith('vless-reality') && (
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
            <select className="input-field" value={f.line_kind} onChange={e => setF({ ...f, line_kind: e.target.value })}>
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
        <div className="mt-4">
          <button className="btn-primary" disabled={busy}><Icon name="plus" size={16} /> 创建入站</button>
        </div>
      </form>
      <div className="card overflow-hidden">
        {list.length === 0 ? (
          <Empty title="暂无入站" hint="选一台在线服务器，挑一种协议。443 若已被占用请换端口。" />
        ) : (
          <div className="table-wrap">
            <table className="data">
              <thead><tr><th>名称</th><th>服务器</th><th>协议</th><th>端口</th><th>线路</th><th>内核</th><th></th></tr></thead>
              <tbody>
                {list.map(inb => (
                  <tr key={inb.id} className={!inb.enabled ? 'opacity-50' : ''}>
                    <td className="font-medium">{inb.name}</td>
                    <td>{inb.server_name}</td>
                    <td><Badge tone="gold">{inb.profile}</Badge></td>
                    <td className="tabular-nums">{inb.port}</td>
                    <td>{inb.line_kind === 'chain' ? '链式' : '直出'}</td>
                    <td className="text-ink-mut">{inb.core}</td>
                    <td className="whitespace-nowrap">
                      <div className="flex gap-3 justify-end">
                        <button type="button" className="linkish" onClick={() => toggle(inb)}>{inb.enabled ? '停用' : '启用'}</button>
                        <button type="button" className="linkish" style={{ color: 'var(--color-danger)' }} onClick={() => del(inb.id)}>删除</button>
                      </div>
                    </td>
                  </tr>
                ))}
              </tbody>
            </table>
          </div>
        )}
      </div>
    </div>
  )
}
