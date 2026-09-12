import { useEffect, useState } from 'react'
import { api } from '../lib/api'
import { useToast, useDialog } from '../components/Layout'
import { Empty, Field, Icon, Modal, PageHead } from '../components/ui'

const emptyForm = { name: '', domains: '', cert_pem: '', key_pem: '' }

export default function Certs() {
  const toast = useToast()
  const dialog = useDialog()
  const [list, setList] = useState([])
  const [f, setF] = useState(emptyForm)
  const [formOpen, setFormOpen] = useState(false)
  const [busy, setBusy] = useState(false)
  const load = () => api.get('/certs').then(d => setList(d.certs || [])).catch(e => toast(e.message, 'error'))
  useEffect(() => { load() }, [])

  const openCreate = () => {
    setF(emptyForm)
    setFormOpen(true)
  }

  const create = async (e) => {
    e.preventDefault()
    setBusy(true)
    try {
      await api.post('/certs', f)
      setF(emptyForm)
      setFormOpen(false)
      toast('已保存')
      load()
    } catch (e) { toast(e.message, 'error') }
    finally { setBusy(false) }
  }
  const del = async (id) => {
    if (!(await dialog.confirm({ title: '删除证书', message: '使用此证书的 TLS 入站将无法握手。', danger: true }))) return
    try { await api.del(`/certs/${id}`); load() }
    catch (e) { toast(e.message, 'error') }
  }

  return (
    <div>
      <PageHead
        title="证书"
        desc="VLESS+XHTTP、Trojan、AnyTLS 需要 PEM。REALITY 不需要证书。"
        actions={
          <button type="button" className="btn-primary" onClick={openCreate}>
            <Icon name="plus" size={15} /> 上传证书
          </button>
        }
      />
      <div className="card overflow-hidden">
        {list.length === 0 ? (
          <Empty title="暂无证书" hint="把完整证书链和私钥贴进来。" action={
            <button type="button" className="btn-primary" onClick={openCreate}><Icon name="plus" size={15} /> 上传证书</button>
          } />
        ) : (
          <div className="table-wrap">
            <table className="data">
              <thead><tr><th>名称</th><th>域名</th><th></th></tr></thead>
              <tbody>
                {list.map(c => (
                  <tr key={c.id}>
                    <td className="font-medium">{c.name}</td>
                    <td className="text-ink-mut">{c.domains || '—'}</td>
                    <td className="text-right">
                      <button type="button" className="row-act is-danger" onClick={() => del(c.id)}>删除</button>
                    </td>
                  </tr>
                ))}
              </tbody>
            </table>
          </div>
        )}
      </div>
      <Modal open={formOpen} title="上传证书" onClose={() => setFormOpen(false)} size="lg" footer={
        <>
          <button type="button" className="btn-ghost" onClick={() => setFormOpen(false)}>取消</button>
          <button type="submit" form="cert-form" className="btn-primary" disabled={busy}>{busy ? '保存中…' : '保存'}</button>
        </>
      }>
        <form id="cert-form" onSubmit={create} className="space-y-3">
          <div className="grid grid-cols-1 sm:grid-cols-2 gap-3">
            <Field label="名称"><input className="input-field" placeholder="example.com" value={f.name} onChange={e => setF({ ...f, name: e.target.value })} required autoFocus /></Field>
            <Field label="域名" hint="可选，逗号分隔"><input className="input-field" placeholder="www.example.com" value={f.domains} onChange={e => setF({ ...f, domains: e.target.value })} /></Field>
          </div>
          <Field label="证书 PEM"><textarea className="input-field font-mono text-[12px]" placeholder="-----BEGIN CERTIFICATE-----" value={f.cert_pem} onChange={e => setF({ ...f, cert_pem: e.target.value })} required /></Field>
          <Field label="私钥 PEM"><textarea className="input-field font-mono text-[12px]" placeholder="-----BEGIN PRIVATE KEY-----" value={f.key_pem} onChange={e => setF({ ...f, key_pem: e.target.value })} required /></Field>
        </form>
      </Modal>
    </div>
  )
}
