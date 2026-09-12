import { useEffect, useState } from 'react'
import { api } from '../lib/api'
import { copyText } from '../lib/copy'
import { useToast, useDialog } from '../components/Layout'
import { Badge, Empty, Field, Icon, Modal, PageHead, fmtAgo } from '../components/ui'
import { ShareModal } from '../components/ShareModal'

const SUGGESTED_PORTS = [8443, 8444, 2053, 2083, 2087, 2096, 8880, 9443, 10443, 11443]

const emptyLine = {
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
  return ports.length ? `已用 ${[...new Set(ports)].sort((a, b) => a - b).join('、')}` : '该节点还没有线路'
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

export default function Nodes() {
  const toast = useToast()
  const dialog = useDialog()
  const [servers, setServers] = useState([])
  const [list, setList] = useState([])
  const [certs, setCerts] = useState([])
  const [profiles, setProfiles] = useState([])
  const [name, setName] = useState('')
  const [host, setHost] = useState('')
  const [cmd, setCmd] = useState('')
  const [nodeOpen, setNodeOpen] = useState(false)
  const [f, setF] = useState(emptyLine)
  const [editId, setEditId] = useState(0)
  const [lineOpen, setLineOpen] = useState(false)
  const [busy, setBusy] = useState(false)
  const [paramInb, setParamInb] = useState(null)
  const [share, setShare] = useState(null)

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

  const meta = profiles.find(p => p.id === f.profile)
  const selectedServer = servers.find(s => Number(s.id) === Number(f.server_id))
  const missingCore = selectedServer && meta && !serverHasCore(selectedServer, meta.core)
  const landings = list.filter(x => x.line_kind === 'direct' && ['vless-reality', 'vless-reality-vision', 'vless-xhttp-tls', 'trojan-tls', 'ss2022'].includes(x.profile))

  const openCreateNode = () => {
    setName('')
    setHost('')
    setNodeOpen(true)
  }

  const createNode = async (e) => {
    e.preventDefault()
    setBusy(true)
    try {
      const d = await api.post('/servers', { name, public_host: host })
      setName(''); setHost('')
      setNodeOpen(false)
      setCmd(d.install || '')
      toast('已添加节点')
      load()
    } catch (e) { toast(e.message, 'error') }
    finally { setBusy(false) }
  }

  const showInstall = async (id) => {
    try {
      const d = await api.get(`/servers/${id}/install`)
      setCmd(d.command)
    } catch (e) { toast(e.message, 'error') }
  }

  const sync = async (id) => {
    try { await api.post(`/servers/${id}/sync`); toast('已下发'); load() }
    catch (e) { toast(e.message, 'error'); load() }
  }

  const delNode = async (id) => {
    if (!(await dialog.confirm({ title: '删除节点', message: '这台机器上的线路也会一并删除，且无法恢复。', danger: true, okText: '删除' }))) return
    try { await api.del(`/servers/${id}`); load(); toast('已删除') }
    catch (e) { toast(e.message, 'error') }
  }

  const saveHost = async (s) => {
    const v = await dialog.prompt({ title: '公开地址', message: '客户端连接用的 IP 或域名。', defaultValue: s.public_host || '', okText: '保存' })
    if (v == null) return
    try { await api.put(`/servers/${s.id}`, { name: s.name, public_host: v }); load() }
    catch (e) { toast(e.message, 'error') }
  }

  const openCreateLine = (serverId) => {
    const sid = Number(serverId)
    setEditId(0)
    setF({ ...emptyLine, server_id: sid, port: nextPort(sid, list) })
    setLineOpen(true)
  }

  const startEdit = (inb) => {
    setEditId(inb.id)
    setF(formFromInbound(inb))
    setLineOpen(true)
  }

  const closeLine = () => {
    setLineOpen(false)
    setEditId(0)
    setF(emptyLine)
  }

  const bodyFromForm = () => {
    const settings = { ...(f.settings || {}) }
    if (String(f.profile).startsWith('vless-reality')) {
      settings.dest = f.dest
      const hostName = (f.dest || '').split(':')[0]
      if (hostName) settings.server_names = [hostName]
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

  const submitLine = async (e) => {
    e.preventDefault()
    const port = Number(f.port)
    if (!Number.isInteger(port) || port < 1 || port > 65535) {
      toast('端口范围 1–65535', 'error')
      return
    }
    const wasEdit = !!editId
    setBusy(true)
    try {
      const d = wasEdit ? await api.put(`/inbounds/${editId}`, bodyFromForm()) : await api.post('/inbounds', bodyFromForm())
      if (d.apply_error) toast(d.apply_error, 'error')
      else toast(wasEdit ? '已保存' : '已添加线路')
      closeLine()
      load()
    } catch (e) { toast(e.message, 'error') }
    finally { setBusy(false) }
  }

  const delLine = async (id) => {
    if (!(await dialog.confirm({ title: '删除线路', message: '订阅里对应的节点会立刻消失。', danger: true }))) return
    try { await api.del(`/inbounds/${id}`); if (editId === id) closeLine(); load() }
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
      setShare(await api.get(`/inbounds/${inb.id}/share`))
    } catch (e) {
      toast(e.message, 'error')
    }
  }

  const linesOf = (id) => list.filter(x => Number(x.server_id) === Number(id))
  const online = servers.filter(s => s.online).length

  return (
    <div>
      <PageHead
        title="节点管理"
        desc="一台机器一个 Agent，下面挂线路。端口可自定义；被占用的口只会停那一条，不会拖垮其它线路。"
        actions={
          <button type="button" className="btn-primary" onClick={openCreateNode}>
            <Icon name="plus" size={15} /> 添加节点
          </button>
        }
      />
      {servers.length > 0 && (
        <div className="text-[12px] text-ink-mut mb-3">
          {online} 在线 · {servers.length - online} 离线 · {list.length} 条线路
        </div>
      )}
      {servers.length === 0 ? (
        <div className="card overflow-hidden">
          <Empty title="还没有节点" hint="先起一个名字，添加后把安装命令拿到机器上以 root 执行，再在节点上添加线路。" action={
            <button type="button" className="btn-primary" onClick={openCreateNode}><Icon name="plus" size={15} /> 添加节点</button>
          } />
        </div>
      ) : (
        <div className="space-y-3">
          {servers.map(s => {
            const lines = linesOf(s.id)
            const cores = String(s.cores || '').split(',').map(x => x.trim()).filter(Boolean)
            return (
              <div key={s.id} className="card overflow-hidden">
                <div className="p-4 flex flex-col gap-2.5 sm:flex-row sm:items-start sm:justify-between">
                  <div className="min-w-0">
                    <div className="flex items-center gap-2 flex-wrap">
                      <span className="text-[15px] font-semibold truncate">{s.name}</span>
                      <span className={`dot ${s.online ? 'dot-on' : 'dot-off'}`} />
                      {s.online ? <Badge tone="ok">在线</Badge> : <Badge tone="muted">离线</Badge>}
                    </div>
                    <div className="text-[12px] font-mono text-ink-mut mt-1 truncate">
                      {s.public_host || '未填公开地址'}
                      <button type="button" className="row-act ml-2 font-sans" onClick={() => saveHost(s)}>改</button>
                    </div>
                    <div className="flex flex-wrap gap-1.5 mt-2">
                      {cores.length ? cores.map(c => <span key={c} className="chip">{c}</span>) : <span className="chip">未上报内核</span>}
                    </div>
                    <div className="text-[12px] text-ink-mut mt-1.5">
                      Agent {s.agent_ver || '—'} · 心跳 {fmtAgo(s.last_seen)}
                      {s.os ? ` · ${[s.os, s.arch].filter(Boolean).join('/')}` : ''}
                    </div>
                    {s.last_error ? (
                      <div className="text-[12px] mt-1" style={{ color: 'var(--color-danger)' }}>{s.last_error}</div>
                    ) : null}
                  </div>
                  <div className="flex flex-wrap gap-2.5 shrink-0">
                    <button type="button" className="row-act" onClick={() => showInstall(s.id)}>安装命令</button>
                    <button type="button" className="row-act" onClick={() => sync(s.id)}>同步</button>
                    <button type="button" className="row-act is-danger" onClick={() => delNode(s.id)}>删除节点</button>
                  </div>
                </div>
                <div className="px-4 py-2 flex items-center justify-between border-t" style={{ borderColor: 'var(--color-line-soft)' }}>
                  <span className="text-[12px] font-medium text-ink-mut">线路 · {lines.length}</span>
                  <button type="button" className="row-act" onClick={() => openCreateLine(s.id)}>
                    <span className="inline-flex items-center gap-1"><Icon name="plus" size={12} /> 添加线路</span>
                  </button>
                </div>
                {lines.length === 0 ? (
                  <div className="px-4 pb-4 text-[13px] text-ink-mut">这台节点还没有线路。选协议、填端口即可。</div>
                ) : (
                  <div className="table-wrap">
                    <table className="data">
                      <thead><tr><th>名称</th><th>协议</th><th>端口</th><th>类型</th><th>内核</th><th></th></tr></thead>
                      <tbody>
                        {lines.map(inb => {
                          const dead = !serverHasCore(s, inb.core)
                          return (
                            <tr key={inb.id} className={!inb.enabled ? 'opacity-50' : ''}>
                              <td className="font-medium">{inb.name}</td>
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
                                  <button type="button" className="row-act is-danger" onClick={() => delLine(inb.id)}>删除</button>
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
            )
          })}
        </div>
      )}

      <Modal open={nodeOpen} title="添加节点" onClose={() => setNodeOpen(false)} footer={
        <>
          <button type="button" className="btn-ghost" onClick={() => setNodeOpen(false)}>取消</button>
          <button type="submit" form="node-form" className="btn-primary" disabled={busy}>{busy ? '添加中…' : '添加'}</button>
        </>
      }>
        <form id="node-form" onSubmit={createNode} className="space-y-3">
          <Field label="名称">
            <input className="input-field" value={name} onChange={e => setName(e.target.value)} required placeholder="香港-01" autoFocus />
          </Field>
          <Field label="公开地址" hint="客户端连接用的 IP 或域名，可稍后填写">
            <input className="input-field" value={host} onChange={e => setHost(e.target.value)} placeholder="IP 或域名" />
          </Field>
        </form>
      </Modal>

      <Modal open={!!cmd} title="一键安装 Agent" onClose={() => setCmd('')} wide footer={
        <>
          <button type="button" className="btn-ghost" onClick={() => setCmd('')}>关闭</button>
          <button type="button" className="btn-primary" onClick={async () => {
            try { await copyText(cmd); toast('已复制') }
            catch { toast('浏览器不允许自动复制，请手动选中命令', 'error') }
          }}>
            <Icon name="copy" size={15} /> 复制
          </button>
        </>
      }>
        <p className="text-[13px] text-ink-mut mb-3">在节点上以 root 执行。明文 http 会自动带 --insecure。</p>
        <pre className="text-[12px] font-mono bg-raised p-3 rounded-lg overflow-x-auto whitespace-pre-wrap">{cmd}</pre>
      </Modal>

      <Modal open={lineOpen} title={editId ? '编辑线路' : '添加线路'} onClose={closeLine} size="lg" footer={
        <>
          <button type="button" className="btn-ghost" onClick={closeLine}>取消</button>
          <button type="submit" form="line-form" className="btn-primary" disabled={busy}>{busy ? '保存中…' : (editId ? '保存' : '创建')}</button>
        </>
      }>
        <form id="line-form" onSubmit={submitLine} className="grid grid-cols-1 sm:grid-cols-2 gap-3">
          <Field label="节点">
            <select className="input-field" value={f.server_id} disabled>
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
            <Field label="落地线路" hint="不可选 Mieru / AnyTLS">
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
        <p className="text-[12px] text-ink-mut mt-3">这些是服务端参数，不能直接导入客户端。点线路上的「复制」拿订阅链接。</p>
      </Modal>

      <ShareModal share={share} onClose={() => setShare(null)} onToast={toast} />
    </div>
  )
}
