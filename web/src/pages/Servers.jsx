import { useEffect, useState } from 'react'
import { api } from '../lib/api'
import { useToast, useDialog } from '../components/Layout'
import { Badge, Empty, Field, Icon, Modal, PageHead } from '../components/ui'

export default function Servers() {
  const toast = useToast()
  const dialog = useDialog()
  const [list, setList] = useState([])
  const [name, setName] = useState('')
  const [host, setHost] = useState('')
  const [cmd, setCmd] = useState('')
  const [busy, setBusy] = useState(false)

  const load = () => api.get('/servers').then(d => setList(d.servers || [])).catch(e => toast(e.message, 'error'))
  useEffect(() => { load() }, [])

  const create = async (e) => {
    e.preventDefault()
    setBusy(true)
    try {
      const d = await api.post('/servers', { name, public_host: host })
      setName(''); setHost('')
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
    try { await api.post(`/servers/${id}/sync`); toast('已下发') }
    catch (e) { toast(e.message, 'error') }
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

  return (
    <div>
      <PageHead kicker="Fleet" title="服务器" desc="每台机器一个 Agent。安装命令从面板下载二进制，不走 GitHub。" />
      <form onSubmit={create} className="card p-5 mb-4 grid grid-cols-1 md:grid-cols-[1fr_1fr_auto] gap-3 items-end">
        <Field label="名称">
          <input className="input-field" value={name} onChange={e => setName(e.target.value)} required placeholder="香港-01" />
        </Field>
        <Field label="公开地址" hint="可稍后填写">
          <input className="input-field" value={host} onChange={e => setHost(e.target.value)} placeholder="IP 或域名" />
        </Field>
        <button className="btn-primary" disabled={busy}><Icon name="plus" size={16} /> 添加</button>
      </form>
      <div className="card overflow-hidden">
        {list.length === 0 ? (
          <Empty title="暂无服务器" hint="先起一个名字，添加后再复制安装命令到节点上执行。" />
        ) : (
          <div className="table-wrap">
            <table className="data">
              <thead><tr><th>名称</th><th>地址</th><th>状态</th><th>版本</th><th></th></tr></thead>
              <tbody>
                {list.map(s => (
                  <tr key={s.id}>
                    <td className="font-medium">{s.name}</td>
                    <td className="font-mono text-[12px]">
                      {s.public_host || '—'}
                      <button type="button" className="linkish ml-2 text-[12px]" onClick={() => saveHost(s)}>改</button>
                    </td>
                    <td>
                      <span className={`dot ${s.online ? 'dot-on' : 'dot-off'}`} />
                      <span className="ml-2">{s.online ? '在线' : '离线'}</span>
                      {s.online ? <Badge tone="ok" className="ml-2">live</Badge> : null}
                    </td>
                    <td className="text-ink-mut text-[12px] font-mono">{s.agent_ver || '—'}</td>
                    <td className="whitespace-nowrap">
                      <div className="flex gap-3 justify-end">
                        <button type="button" className="linkish" onClick={() => showInstall(s.id)}>安装命令</button>
                        <button type="button" className="linkish" onClick={() => sync(s.id)}>同步</button>
                        <button type="button" className="linkish" style={{ color: 'var(--color-danger)' }} onClick={() => del(s.id)}>删除</button>
                      </div>
                    </td>
                  </tr>
                ))}
              </tbody>
            </table>
          </div>
        )}
      </div>
      <Modal open={!!cmd} title="一键安装 Agent" onClose={() => setCmd('')} wide footer={
        <>
          <button type="button" className="btn-ghost" onClick={() => setCmd('')}>关闭</button>
          <button type="button" className="btn-primary" onClick={() => navigator.clipboard.writeText(cmd).then(() => toast('已复制'))}>
            <Icon name="copy" size={15} /> 复制
          </button>
        </>
      }>
        <p className="text-[13px] text-ink-mut mb-3">在节点上以 root 执行。明文 http 会自动带 --insecure。</p>
        <pre className="text-[12px] font-mono bg-raised p-3 rounded-xl overflow-x-auto whitespace-pre-wrap">{cmd}</pre>
      </Modal>
    </div>
  )
}
