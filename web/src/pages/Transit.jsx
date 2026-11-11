import { useEffect, useRef, useState } from 'react'
import { api } from '../lib/api'
import { copyText } from '../lib/copy'
import { protoShort } from '../lib/forwards'
import { landingSummary } from '../lib/share'
import { createOpLock } from '../lib/opLock'
import { asArray } from '../lib/safe'
import { useDialog, useToast, useUser } from '../components/Layout'
import { Badge, Empty, Field, Icon, Modal, MoreMenu, PageHead, SkeletonRows } from '../components/ui'

const emptyForm = {
  entry_inbound_id: 0,
  kind: 'chain',
  name: '',
  port: '',
  exit_uri: '',
  dest_host: '',
  dest_port: '',
  network: 'tcp',
}

function entryLabel(e) {
  if (!e) return ''
  const host = e.server_host || '入口'
  return `${protoShort(e.profile)} ${host}:${e.port}`
}

function cardTitle(row, packageName) {
  const name = String(row?.name || '').trim()
  const server = String(row?.server_name || '').trim()
  if (!name) return packageName
  if (server && (name === server || name === `${server} 链式` || name === `${server} 端口`)) return packageName
  return name
}

function hostPort(host, port) {
  const h = String(host || '').trim()
  const p = String(port ?? '').trim()
  if (!h || !p || p === '0') return ''
  return h.includes(':') ? `[${h}]:${p}` : `${h}:${p}`
}

function connectAddr(row) {
  return hostPort(row?.server_host, row?.port)
}

function landingText(row) {
  if (row.kind === 'port') {
    const net = row.network === 'tcp,udp' ? 'TCP+UDP' : 'TCP'
    const host = row.dest_host || '—'
    const port = row.dest_port || '—'
    return `${net}  ${host}:${port}`
  }
  return landingSummary(row.exit_uri) || '落地无法识别'
}

function probeDraftDirty(f, row) {
  if (!row) return true
  if ((f.kind === 'port' ? 'port' : 'chain') !== (row.kind === 'port' ? 'port' : 'chain')) return true
  if (f.kind === 'port') {
    const port = String(f.dest_port ?? '').trim()
    const saved = row.dest_port ? String(row.dest_port) : ''
    return String(f.dest_host || '').trim() !== String(row.dest_host || '').trim()
      || port !== saved
      || (f.network === 'tcp,udp' ? 'tcp,udp' : 'tcp') !== (row.network === 'tcp,udp' ? 'tcp,udp' : 'tcp')
  }
  return String(f.exit_uri || '').trim() !== String(row.exit_uri || '').trim()
}

export default function Transit() {
  const toast = useToast()
  const dialog = useDialog()
  const { user } = useUser()
  const packageName = user?.package_name || '套餐'
  const [entries, setEntries] = useState([])
  const [forwards, setForwards] = useState([])
  const [max, setMax] = useState(20)
  const [ready, setReady] = useState(false)
  const [error, setError] = useState('')
  const [open, setOpen] = useState(false)
  const [editId, setEditId] = useState(0)
  const [f, setF] = useState(emptyForm)
  const [busy, setBusy] = useState(false)
  const [probes, setProbes] = useState({})
  const ops = useRef(createOpLock())

  const load = async () => {
    try {
      const d = await api.get('/me/forwards')
      setEntries(asArray(d.entries))
      setForwards(asArray(d.forwards))
      setMax(Number(d.max) || 20)
      setError('')
    } catch (e) {
      setError(e.message || '加载失败')
      toast(e.message, 'error')
    } finally {
      setReady(true)
    }
  }

  useEffect(() => { load() }, [])

  const openCreate = () => {
    const first = entries[0]
    setEditId(0)
    setF({ ...emptyForm, entry_inbound_id: first ? Number(first.id) : 0 })
    setOpen(true)
  }

  const openEdit = (row) => {
    setEditId(row.id)
    setF({
      entry_inbound_id: Number(row.entry_inbound_id) || 0,
      kind: row.kind === 'port' ? 'port' : 'chain',
      name: row.name || '',
      port: row.port ? String(row.port) : '',
      exit_uri: row.exit_uri || '',
      dest_host: row.dest_host || '',
      dest_port: row.dest_port ? String(row.dest_port) : '',
      network: row.network === 'tcp,udp' ? 'tcp,udp' : 'tcp',
    })
    setOpen(true)
  }

  const save = async (e) => {
    e.preventDefault()
    const kind = f.kind === 'port' ? 'port' : 'chain'
    if (!editId && !Number(f.entry_inbound_id)) {
      toast('请选择中转入口', 'error')
      return
    }
    const port = String(f.port).trim() === '' ? 0 : Number(f.port)
    if (String(f.port).trim() !== '' && (!Number.isInteger(port) || port < 1 || port > 65535)) {
      toast('端口范围 1–65535，留空则自动分配', 'error')
      return
    }
    const body = {
      entry_inbound_id: Number(f.entry_inbound_id) || 0,
      kind,
      name: f.name.trim(),
      port,
      network: f.network === 'tcp,udp' ? 'tcp,udp' : 'tcp',
    }
    if (kind === 'chain') {
      body.exit_uri = f.exit_uri.trim()
      if (!body.exit_uri && !editId) {
        toast('请填写落地', 'error')
        return
      }
    } else {
      body.dest_host = f.dest_host.trim()
      body.dest_port = String(f.dest_port).trim() === '' ? 0 : Number(f.dest_port)
      if (!body.dest_host) {
        toast('请填写目标地址', 'error')
        return
      }
      if (!Number.isInteger(body.dest_port) || body.dest_port < 1 || body.dest_port > 65535) {
        toast('目标端口无效', 'error')
        return
      }
    }
    setBusy(true)
    try {
      if (editId) await api.put(`/me/forwards/${editId}`, body)
      else await api.post('/me/forwards', body)
      setOpen(false)
      toast(editId ? '已保存' : '已创建')
      load()
    } catch (err) {
      toast(err.message, 'error')
    } finally {
      setBusy(false)
    }
  }

  const toggle = async (row) => {
    if (!row.enabled && row.auth_hold) {
      toast('这个入口已取消授权', 'error')
      return
    }
    try {
      await api.put(`/me/forwards/${row.id}`, { enabled: !row.enabled })
      toast(row.enabled ? '已停用' : '已启用')
      load()
    } catch (err) {
      toast(err.message, 'error')
    }
  }

  const remove = async (row) => {
    if (!(await dialog.confirm({ title: '删除中转', message: `删除「${cardTitle(row, packageName)}」后，这个端口不再转发。`, danger: true, okText: '删除' }))) return
    try {
      await api.del(`/me/forwards/${row.id}`)
      toast('已删除')
      load()
    } catch (err) {
      toast(err.message, 'error')
    }
  }

  const probeLatency = async (row) => {
    await ops.current.run(`probe-${row.id}`, async () => {
      setProbes(p => ({ ...p, [row.id]: { state: 'busy' } }))
      try {
        const d = await api.post(`/me/forwards/${row.id}/probe`)
        if (d.ok) {
          const ms = Number(d.latency_ms) || 0
          setProbes(p => ({ ...p, [row.id]: { state: 'ok', ms, host: d.host, port: d.port } }))
          toast(`入口到 ${d.host}:${d.port}，${ms} ms`)
        } else {
          const msg = d.error || '探测失败'
          setProbes(p => ({ ...p, [row.id]: { state: 'err', error: msg } }))
          toast(msg, 'error')
        }
      } catch (e) {
        setProbes(p => ({ ...p, [row.id]: { state: 'err', error: e.message } }))
        toast(e.message, 'error')
      }
    })
  }

  const copyAddr = async (row) => {
    const addr = connectAddr(row)
    if (!addr) {
      toast('还没有连接地址', 'error')
      return
    }
    try {
      await copyText(addr)
      toast('已复制连接地址')
    } catch {
      toast('浏览器不允许自动复制', 'error')
    }
  }

  const atCap = forwards.length >= max
  const editing = forwards.find(x => Number(x.id) === Number(editId))
  const probed = editing ? probes[editing.id] : null
  const probeLabel = probed?.state === 'busy' ? '测量中…' : probed?.state === 'ok' ? `${probed.ms} ms` : '测试'
  const probeNote = !probed
    ? (f.kind === 'port' ? '从入口测到目标' : '从入口测到落地')
    : probed.state === 'busy'
      ? '测量中…'
      : probed.state === 'ok'
        ? `${probed.host}:${probed.port}`
        : (probed.error || '探测失败')
  const runProbe = () => {
    if (!editing || probeDraftDirty(f, editing)) {
      toast('先保存再测试', 'error')
      return
    }
    probeLatency(editing)
  }
  const probeButton = (
    <button type="button" className="row-act shrink-0" disabled={probed?.state === 'busy'} onClick={runProbe} title={probeNote}>{probeLabel}</button>
  )
  const connectShown = hostPort(
    editing?.server_host || entries.find(en => Number(en.id) === Number(f.entry_inbound_id))?.server_host,
    f.port,
  )

  return (
    <div>
      <PageHead
        title="中转"
        desc="用套餐授权的机器做入口，落地和目标由你填。这些转发只在这个账号里，流量也算在这个账号上。"
        actions={
          <button type="button" className="btn-primary" onClick={openCreate} disabled={!entries.length || atCap}>
            <Icon name="plus" size={15} /> 新建
          </button>
        }
      />
      {error ? <div className="alert-row is-warn mb-4">{error}</div> : null}
      {!ready ? (
        <div className="card overflow-hidden"><SkeletonRows /></div>
      ) : entries.length === 0 && forwards.length === 0 ? (
        <div className="card overflow-hidden">
          <Empty title="还没有中转入口" hint="套餐里勾了「中转」之后，这里才能新建链式和端口转发。" />
        </div>
      ) : (
        <div>
          {entries.length ? (
            <div className="text-[13px] text-ink-soft mb-3">
              可用入口 {entries.length} 个{atCap ? `，已到 ${max} 条上限` : `，还可以建 ${Math.max(0, max - forwards.length)} 条`}
            </div>
          ) : (
            <div className="alert-row is-warn mb-4">入口授权已取消。已有的中转已停，重新授权后会自动恢复。</div>
          )}
          {forwards.length === 0 ? (
            <div className="card overflow-hidden">
              <Empty title="还没有中转" hint="新建一条链式，或把一个端口转到你自己的地址。" action={
                <button type="button" className="btn-primary" onClick={openCreate}><Icon name="plus" size={15} /> 新建</button>
              } />
            </div>
          ) : (
            <div className="machine-grid">
              {forwards.map(row => {
                return (
                <div key={row.id} className={`machine${row.enabled ? '' : ' is-off'}`}>
                  <div className="machine-head">
                    <div className="min-w-0 flex-1">
                      <div className="machine-title">
                        <span className="machine-name truncate">{cardTitle(row, packageName)}</span>
                        <Badge tone="muted">{row.kind === 'port' ? '端口' : '链式'}</Badge>
                        {row.auth_hold ? <Badge tone="warn">已取消授权</Badge> : null}
                        {!row.enabled && !row.auth_hold ? <Badge tone="muted">停用</Badge> : null}
                      </div>
                      <div className="machine-host">
                        {row.kind === 'port' ? (
                          <span className="machine-host-addr truncate" title={connectAddr(row) || '还没有入口地址'}>
                            {connectAddr(row) || '还没有入口地址'}
                          </span>
                        ) : (
                          <span className="machine-host-addr truncate" title={`${packageName} · ${protoShort(row.entry_profile || row.profile)} :${row.port}`}>
                            {packageName} · {protoShort(row.entry_profile || row.profile)} :{row.port}
                          </span>
                        )}
                      </div>
                    </div>
                  <div className="machine-toolbar">
                    <MoreMenu
                      colorDots
                      label="更多操作"
                      items={[
                        { label: '编辑', onSelect: () => openEdit(row) },
                        { label: row.enabled ? '停用' : '启用', onSelect: () => toggle(row) },
                        { sep: true },
                        { label: '删除', danger: true, onSelect: () => remove(row) },
                      ]}
                    />
                  </div>
                </div>
                <div className="px-3 pb-2 text-[12px] text-ink-soft truncate" title={row.kind === 'port' ? `转到 ${landingText(row)}` : landingText(row)}>
                    {row.kind === 'port' ? `转到 ${landingText(row)}` : landingText(row)}
                  </div>
                  {row.kind === 'port' ? (
                    <div className="machine-foot">
                      <button type="button" className="row-act ml-auto" disabled={!connectAddr(row)} onClick={() => copyAddr(row)}>复制地址</button>
                    </div>
                  ) : null}
                </div>
                )
              })}
            </div>
          )}
        </div>
      )}
      <Modal open={open} title={editId ? '编辑中转' : '新建中转'} onClose={() => setOpen(false)} footer={
        <>
          <button type="button" className="btn-ghost" onClick={() => setOpen(false)}>取消</button>
          <button type="submit" form="transit-form" className="btn-primary" disabled={busy}>{busy ? '保存中…' : (editId ? '保存' : '创建')}</button>
        </>
      }>
        <form id="transit-form" onSubmit={save} className="space-y-4">
          {!editId && entries.length > 1 ? (
            <Field label="入口">
              <select className="input-field" value={f.entry_inbound_id} onChange={e => setF({ ...f, entry_inbound_id: Number(e.target.value) })}>
                {entries.map(en => (
                  <option key={en.id} value={en.id}>{entryLabel(en)}</option>
                ))}
              </select>
            </Field>
          ) : (
            <div className="text-[13px] text-ink-soft">
              入口 {entryLabel(entries.find(en => Number(en.id) === Number(f.entry_inbound_id)) || entries[0] || { name: editing?.entry_name, server_name: editing?.server_name, profile: editing?.profile, port: editing?.port })}
            </div>
          )}
          {!editId ? (
            <div>
              <div className="text-[12px] font-medium text-ink-soft mb-1.5">类型</div>
              <div className="seg" role="group" aria-label="类型">
                <button type="button" className={`seg-item${f.kind === 'chain' ? ' is-on' : ''}`} onClick={() => setF({ ...f, kind: 'chain' })}>链式</button>
                <button type="button" className={`seg-item${f.kind === 'port' ? ' is-on' : ''}`} onClick={() => setF({ ...f, kind: 'port' })}>端口</button>
              </div>
              <div className="text-[12px] text-ink-mut mt-1.5">
                {f.kind === 'port' ? '把这台机器上的一个新端口转到你填的地址。不进订阅。' : '在这台机器上新开一个入口，落到你填的链接。会进你的订阅。'}
              </div>
            </div>
          ) : null}
          <div className="grid grid-cols-1 sm:grid-cols-2 gap-3">
            <Field label="名称" hint="可留空">
              <input className="input-field" value={f.name} onChange={e => setF({ ...f, name: e.target.value })} />
            </Field>
            <Field label="监听端口" hint="留空则自动分配">
              <input className="input-field font-mono" inputMode="numeric" value={f.port} onChange={e => setF({ ...f, port: e.target.value })} />
            </Field>
          </div>
          {f.kind === 'port' ? (
            <div>
              <div className="grid grid-cols-1 sm:grid-cols-3 gap-3">
                <Field label="目标地址">
                  <input className="input-field font-mono" value={f.dest_host} onChange={e => setF({ ...f, dest_host: e.target.value })} placeholder="IP 或域名" />
                </Field>
                <Field label="目标端口">
                  <input className="input-field font-mono" inputMode="numeric" value={f.dest_port} onChange={e => setF({ ...f, dest_port: e.target.value })} />
                </Field>
                <Field label="协议">
                  <select className="input-field" value={f.network} onChange={e => setF({ ...f, network: e.target.value })}>
                    <option value="tcp">仅 TCP</option>
                    <option value="tcp,udp">TCP + UDP</option>
                  </select>
                </Field>
              </div>
              <div className="mt-1 flex items-center justify-between gap-3">
                <span className="min-w-0 text-[11.5px] text-ink-mut">
                  {connectShown ? `别人连接 ${connectShown}` : '端口留空时，保存后才有连接地址'}
                </span>
                {probeButton}
              </div>
            </div>
          ) : (
            <Field as="div" label="落地">
              <textarea className="input-field font-mono min-h-[88px]" value={f.exit_uri} onChange={e => setF({ ...f, exit_uri: e.target.value })} placeholder="ss:// 或 vless:// 或 socks5://user:pass@地址:端口" />
              <div className="mt-1 flex items-center justify-between gap-3">
                <span className="min-w-0 text-[11.5px] text-ink-mut">直接粘贴 vless://、ss://、trojan:// 或 socks5://，编码过的链接会自动识别</span>
                {probeButton}
              </div>
            </Field>
          )}
        </form>
      </Modal>
    </div>
  )
}
