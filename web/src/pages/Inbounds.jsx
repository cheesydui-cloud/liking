import { useEffect, useState } from 'react'
import { api } from '../lib/api'
import { copyText } from '../lib/copy'
import { useToast, useDialog } from '../components/Layout'
import { Badge, Empty, Field, Icon, Modal, PageHead } from '../components/ui'

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

function inboundSettings(inb) {
  const st = inb?.settings
  if (!st) return {}
  if (typeof st === 'string') {
    try { return JSON.parse(st) || {} } catch { return {} }
  }
  return st
}

function inboundParamRows(inb) {
  const st = inboundSettings(inb)
  const short = Array.isArray(st.short_ids) ? st.short_ids.filter(Boolean).join(',') : (st.short_id || '')
  return [
    ['名称', inb.name],
    ['节点', [inb.server_name, inb.server_host].filter(Boolean).join(' ')],
    ['协议', inb.profile],
    ['端口', String(inb.port || '')],
    ['dest', st.dest],
    ['sni', st.sni],
    ['path', st.path],
    ['public_key', st.public_key],
    ['short_id', short],
    ['fingerprint', st.fingerprint],
    ['method', st.method],
    ['server_password', st.server_password],
  ].filter(([, v]) => v)
}

function inboundParamLines(inb) {
  return inboundParamRows(inb).map(([k, v]) => `${k} ${v}`).join('\n')
}

function formFromInbound(inb) {
  const st = inboundSettings(inb)
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
  const [formOpen, setFormOpen] = useState(false)
  const [busy, setBusy] = useState(false)
  const [serverFilter, setServerFilter] = useState('')
  const [paramInb, setParamInb] = useState(null)
  const [shareText, setShareText] = useState('')

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
    if (!formOpen || editId || Number(f.server_id) || servers.length !== 1) return
    const sid = servers[0].id
    setF(prev => ({ ...prev, server_id: sid, port: nextPort(sid, list) }))
  }, [servers, list, editId, f.server_id, formOpen])

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

  const openCreate = () => {
    resetForm()
    setFormOpen(true)
  }

  const startEdit = (inb) => {
    setEditId(inb.id)
    setF(formFromInbound(inb))
    setFormOpen(true)
  }

  const closeForm = () => {
    setFormOpen(false)
    resetForm()
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
    const body = bodyFromForm()
    setBusy(true)
    try {
      const d = editId ? await api.put(`/inbounds/${editId}`, body) : await api.post('/inbounds', body)
      if (d.apply_error) toast(d.apply_error, 'error')
      else toast(editId ? '已保存' : '已创建')
      closeForm()
      load()
    } catch (e) { toast(e.message, 'error') }
    finally { setBusy(false) }
  }

  const del = async (id) => {
    if (!(await dialog.confirm({ title: '删除入站', message: '订阅里对应的节点会立刻消失。', danger: true }))) return
    try { await api.del(`/inbounds/${id}`); if (editId === id) closeForm(); load() }
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
  const shown = serverFilter ? list.filter(x => String(x.server_id) === String(serverFilter)) : list

  const copyParams = async (inb) => {
    try {
      await copyText(inboundParamLines(inb))
      toast('参数已复制')
    } catch {
      setParamInb(inb)
      toast('浏览器不允许自动复制，请手动选中', 'error')
    }
  }

  const copyShare = async (inb) => {
    try {
      const d = await api.get(`/inbounds/${inb.id}/share`)
      try {
        await copyText(d.uri)
        if (d.profile === 'mieru') toast('已复制。Mieru 请到用户页复制 Clash 订阅，单条链接多数软件不认')
        else toast(`已复制分享链接（${d.username}）`)
      } catch {
        setShareText(d.uri)
        toast('浏览器不允许自动复制，请手动选中链接', 'error')
      }
    } catch (e) {
      toast(e.message, 'error')
    }
  }

  return (
    <div>
      <PageHead
        title="入站"
        desc="一条入站就是一条线路。端口可自定义；被占用的端口会自动停用，不会拖垮其它线路。"
        actions={
          <button type="button" className="btn-primary" onClick={openCreate}>
            <Icon name="plus" size={15} /> 新建入站
          </button>
        }
      />
      {list.length > 0 && (
        <div className="flex flex-col sm:flex-row gap-2 mb-3">
          <select className="input-field sm:w-64" value={serverFilter} onChange={e => setServerFilter(e.target.value)}>
            <option value="">全部服务器 · {list.length} 条</option>
            {servers.map(s => {
              const n = list.filter(x => x.server_id === s.id).length
              return <option key={s.id} value={s.id}>{s.name} · {n} 条</option>
            })}
          </select>
        </div>
      )}
      <div className="card overflow-hidden">
        {list.length === 0 ? (
          <Empty title="暂无入站" hint="选一台在线服务器，填自定义端口，挑一种协议。" action={
            <button type="button" className="btn-primary" onClick={openCreate}><Icon name="plus" size={15} /> 新建入站</button>
          } />
        ) : shown.length === 0 ? (
          <Empty title="这台服务器还没有入站" hint="换一台，或新建入站。" />
        ) : (
          <div className="table-wrap">
            <table className="data">
              <thead><tr><th>名称</th><th>服务器</th><th>协议</th><th>端口</th><th>线路</th><th>内核</th><th></th></tr></thead>
              <tbody>
                {shown.map(inb => {
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
                        <div className="flex gap-2.5 justify-end">
                          <button type="button" className="row-act" onClick={() => setParamInb(inb)}>参数</button>
                          <button type="button" className="row-act" onClick={() => copyShare(inb)}>复制</button>
                          <button type="button" className="row-act" onClick={() => startEdit(inb)}>编辑</button>
                          <button type="button" className="row-act" onClick={() => toggle(inb)}>{inb.enabled ? '停用' : '启用'}</button>
                          <button type="button" className="row-act is-danger" onClick={() => del(inb.id)}>删除</button>
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
      <Modal open={formOpen} title={editId ? '编辑入站' : '新建入站'} onClose={closeForm} size="lg" footer={
        <>
          <button type="button" className="btn-ghost" onClick={closeForm}>取消</button>
          <button type="submit" form="inb-form" className="btn-primary" disabled={busy}>{busy ? '保存中…' : (editId ? '保存' : '创建')}</button>
        </>
      }>
        <form id="inb-form" onSubmit={submit} className="grid grid-cols-1 sm:grid-cols-2 gap-3">
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
            <input className="input-field" value={f.name} onChange={e => setF({ ...f, name: e.target.value })} required placeholder="HK-8443" autoFocus />
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
          {Number(f.port) === 443 && (
            <div className="notice sm:col-span-2">443 很容易被 Nginx / 其它面板占用。建议改成 8443 或其它空闲端口。</div>
          )}
          {missingCore && (
            <div className="notice sm:col-span-2">这台节点还没有 {meta.core}。创建后会自动从 GitHub 下载并拉起，第一次可能要等一会儿。节点需要能访问 GitHub。</div>
          )}
        </form>
      </Modal>
      <Modal open={!!paramInb} title={paramInb ? `${paramInb.name} 参数` : '参数'} onClose={() => setParamInb(null)} wide footer={
        <>
          <button type="button" className="btn-ghost" onClick={() => setParamInb(null)}>关闭</button>
          {paramInb && (
            <button type="button" className="btn-primary" onClick={() => copyParams(paramInb)}>
              <Icon name="copy" size={15} /> 复制全部
            </button>
          )}
        </>
      }>
        {paramInb && inboundParamRows(paramInb).map(([k, v]) => (
          <div key={k} className="param-row">
            <div className="kicker">{k}</div>
            <code className="text-[12px] break-all font-mono">{v}</code>
            <button type="button" className="btn-ghost h-8 px-2" onClick={async () => {
              try { await copyText(String(v)); toast(`已复制 ${k}`) }
              catch { toast('请手动选中复制', 'error') }
            }}><Icon name="copy" size={13} /></button>
          </div>
        ))}
        <p className="text-[12px] text-ink-mut mt-3">这些是服务端参数，不能直接导入客户端。分享链接请点线路上的「复制」；用户订阅在用户页。</p>
      </Modal>
      <Modal open={!!shareText} title="分享链接" onClose={() => setShareText('')} footer={
        <>
          <button type="button" className="btn-ghost" onClick={() => setShareText('')}>关闭</button>
          <button type="button" className="btn-primary" onClick={async () => {
            try { await copyText(shareText); toast('已复制分享链接') }
            catch { toast('请手动选中复制', 'error') }
          }}>
            <Icon name="copy" size={15} /> 复制
          </button>
        </>
      }>
        <p className="text-[12px] text-ink-mut mb-2">粘贴到 v2rayN / Nekobox / Shadowrocket 等即可导入。Mieru 请用用户页的 Clash 订阅。</p>
        <code className="block text-[12px] break-all font-mono p-3 rounded-md" style={{ background: 'var(--color-fill)' }}>{shareText}</code>
      </Modal>
    </div>
  )
}
