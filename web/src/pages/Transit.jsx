import { useEffect, useState } from 'react'
import { api } from '../lib/api'
import { copyText } from '../lib/copy'
import { protoShort } from '../lib/forwards'
import { asArray } from '../lib/safe'
import { useDialog, useToast } from '../components/Layout'
import { Badge, Empty, Field, Icon, Modal, PageHead, SkeletonRows } from '../components/ui'

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
  const host = e.server_name || e.server_host || '入口'
  return `${host} · ${e.name || protoShort(e.profile)} · ${protoShort(e.profile)}:${e.port}`
}

export default function Transit() {
  const toast = useToast()
  const dialog = useDialog()
  const [entries, setEntries] = useState([])
  const [forwards, setForwards] = useState([])
  const [max, setMax] = useState(20)
  const [ready, setReady] = useState(false)
  const [error, setError] = useState('')
  const [open, setOpen] = useState(false)
  const [editId, setEditId] = useState(0)
  const [f, setF] = useState(emptyForm)
  const [busy, setBusy] = useState(false)

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
    if (!(await dialog.confirm({ title: '删除中转', message: `删除「${row.name || '这条中转'}」后，这个端口不再转发。`, danger: true, okText: '删除' }))) return
    try {
      await api.del(`/me/forwards/${row.id}`)
      toast('已删除')
      load()
    } catch (err) {
      toast(err.message, 'error')
    }
  }

  const copyUri = async (row) => {
    if (!row.uri) {
      toast(row.enabled ? '还没有分享链接' : '停用后没有分享链接', 'error')
      return
    }
    try {
      await copyText(row.uri)
      toast('已复制入口链接')
    } catch {
      toast('浏览器不允许自动复制', 'error')
    }
  }

  const atCap = forwards.length >= max
  const editing = forwards.find(x => Number(x.id) === Number(editId))

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
              {forwards.map(row => (
                <div key={row.id} className={`machine${row.enabled ? '' : ' is-off'}`}>
                  <div className="machine-head">
                    <div className="min-w-0 flex-1">
                      <div className="machine-title">
                        <span className="machine-name truncate">{row.name || '未命名'}</span>
                        <Badge tone="muted">{row.kind === 'port' ? '端口' : '链式'}</Badge>
                        {row.auth_hold ? <Badge tone="warn">已取消授权</Badge> : null}
                        {!row.enabled && !row.auth_hold ? <Badge tone="muted">停用</Badge> : null}
                      </div>
                      <div className="machine-host">
                        <span className="machine-host-addr">
                          {row.server_name || '入口'} · {protoShort(row.entry_profile || row.profile)} :{row.port}
                        </span>
                      </div>
                    </div>
                  </div>
                  <div className="px-3 pb-2 text-[12px] text-ink-soft break-all">
                    {row.kind === 'port'
                      ? `转到 ${row.dest_host || '—'}:${row.dest_port || '—'}${row.network === 'tcp,udp' ? ' · TCP + UDP' : ''}`
                      : (row.exit_uri || '还没填落地')}
                  </div>
                  <div className="machine-foot">
                    {row.kind === 'chain' ? (
                      <button type="button" className="row-act" onClick={() => copyUri(row)}>复制</button>
                    ) : <span />}
                    <button type="button" className="row-act" onClick={() => toggle(row)}>{row.enabled ? '停用' : '启用'}</button>
                    <button type="button" className="row-act" onClick={() => openEdit(row)}>编辑</button>
                    <button type="button" className="row-act" onClick={() => remove(row)}>删除</button>
                  </div>
                </div>
              ))}
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
          ) : (
            <Field label="落地" hint="分享链接，或 socks5://用户:密码@地址:端口">
              <textarea className="input-field font-mono min-h-[88px]" value={f.exit_uri} onChange={e => setF({ ...f, exit_uri: e.target.value })} placeholder="socks5://user:pass@203.0.113.9:1080" />
            </Field>
          )}
        </form>
      </Modal>
    </div>
  )
}
