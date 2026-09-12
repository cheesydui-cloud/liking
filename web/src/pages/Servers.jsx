import { useEffect, useState } from 'react'
import { api } from '../lib/api'
import { copyText } from '../lib/copy'
import { useToast, useDialog } from '../components/Layout'
import { Badge, Empty, Field, Icon, Modal, PageHead, fmtAgo } from '../components/ui'

export default function Servers() {
  const toast = useToast()
  const dialog = useDialog()
  const [list, setList] = useState([])
  const [ins, setIns] = useState([])
  const [name, setName] = useState('')
  const [host, setHost] = useState('')
  const [cmd, setCmd] = useState('')
  const [formOpen, setFormOpen] = useState(false)
  const [busy, setBusy] = useState(false)

  const load = async () => {
    try {
      const [a, b] = await Promise.all([api.get('/servers'), api.get('/inbounds')])
      setList(a.servers || [])
      setIns(b.inbounds || [])
    } catch (e) { toast(e.message, 'error') }
  }
  useEffect(() => { load() }, [])

  const openCreate = () => {
    setName('')
    setHost('')
    setFormOpen(true)
  }

  const create = async (e) => {
    e.preventDefault()
    setBusy(true)
    try {
      const d = await api.post('/servers', { name, public_host: host })
      setName(''); setHost('')
      setFormOpen(false)
      setCmd(d.install || '')
      toast('已创建')
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

  const del = async (id) => {
    if (!(await dialog.confirm({ title: '删除服务器', message: '入站也会一并删除，且无法恢复。', danger: true, okText: '删除' }))) return
    try { await api.del(`/servers/${id}`); load(); toast('已删除') }
    catch (e) { toast(e.message, 'error') }
  }

  const saveHost = async (s) => {
    const v = await dialog.prompt({ title: '公开地址', message: '客户端连接用的 IP 或域名。', defaultValue: s.public_host || '', okText: '保存' })
    if (v == null) return
    try { await api.put(`/servers/${s.id}`, { name: s.name, public_host: v }); load() }
    catch (e) { toast(e.message, 'error') }
  }

  const linesOf = (id) => ins.filter(x => x.server_id === id)
  const online = list.filter(s => s.online).length

  return (
    <div>
      <PageHead
        title="服务器"
        desc="每台机器一个 Agent。安装命令从面板下载二进制，不走 GitHub。"
        actions={
          <button type="button" className="btn-primary" onClick={openCreate}>
            <Icon name="plus" size={15} /> 添加服务器
          </button>
        }
      />
      {list.length > 0 && (
        <div className="text-[12px] text-ink-mut mb-3">{online} 在线 · {list.length - online} 离线</div>
      )}
      {list.length === 0 ? (
        <div className="card overflow-hidden">
          <Empty title="暂无服务器" hint="先起一个名字，添加后再复制安装命令到节点上执行。" action={
            <button type="button" className="btn-primary" onClick={openCreate}><Icon name="plus" size={15} /> 添加服务器</button>
          } />
        </div>
      ) : (
        <div className="grid md:grid-cols-2 gap-3">
          {list.map(s => {
            const lines = linesOf(s.id)
            const cores = String(s.cores || '').split(',').map(x => x.trim()).filter(Boolean)
            return (
              <div key={s.id} className="card server-card">
                <div className="flex items-start justify-between gap-3">
                  <div className="min-w-0">
                    <div className="text-[15px] font-semibold truncate">{s.name}</div>
                    <div className="text-[12px] font-mono text-ink-mut mt-1 truncate">
                      {s.public_host || '未填公开地址'}
                      <button type="button" className="row-act ml-2 font-sans" onClick={() => saveHost(s)}>改</button>
                    </div>
                  </div>
                  <div className="shrink-0 flex items-center gap-2">
                    <span className={`dot ${s.online ? 'dot-on' : 'dot-off'}`} />
                    {s.online ? <Badge tone="ok">在线</Badge> : <Badge tone="muted">离线</Badge>}
                  </div>
                </div>
                {s.last_error ? (
                  <div className="text-[12px]" style={{ color: 'var(--color-danger)' }}>{s.last_error}</div>
                ) : null}
                <div className="flex flex-wrap gap-1.5">
                  {cores.length ? cores.map(c => <span key={c} className="chip">{c}</span>) : <span className="chip">未上报内核</span>}
                  <span className="chip">{lines.length ? `${lines.length} 条入站` : '暂无入站'}</span>
                </div>
                <div className="text-[12px] text-ink-mut">
                  Agent {s.agent_ver || '—'} · 心跳 {fmtAgo(s.last_seen)}
                  {s.os ? ` · ${[s.os, s.arch].filter(Boolean).join('/')}` : ''}
                </div>
                {lines.length > 0 && (
                  <div className="text-[12px] text-ink-soft truncate">
                    {lines.map(x => `${x.name} :${x.port}`).join(' · ')}
                  </div>
                )}
                <div className="flex gap-3 mt-auto pt-1">
                  <button type="button" className="row-act" onClick={() => showInstall(s.id)}>安装命令</button>
                  <button type="button" className="row-act" onClick={() => sync(s.id)}>同步</button>
                  <button type="button" className="row-act is-danger" onClick={() => del(s.id)}>删除</button>
                </div>
              </div>
            )
          })}
        </div>
      )}
      <Modal open={formOpen} title="添加服务器" onClose={() => setFormOpen(false)} footer={
        <>
          <button type="button" className="btn-ghost" onClick={() => setFormOpen(false)}>取消</button>
          <button type="submit" form="srv-form" className="btn-primary" disabled={busy}>{busy ? '添加中…' : '添加'}</button>
        </>
      }>
        <form id="srv-form" onSubmit={create} className="space-y-3">
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
    </div>
  )
}
