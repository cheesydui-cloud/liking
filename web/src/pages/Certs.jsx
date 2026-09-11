import { useEffect, useState } from 'react'
import { api } from '../lib/api'
import { useToast, useDialog } from '../components/Layout'
import { Empty, Field, Icon, PageHead } from '../components/ui'

export default function Certs() {
  const toast = useToast()
  const dialog = useDialog()
  const [list, setList] = useState([])
  const [f, setF] = useState({ name: '', domains: '', cert_pem: '', key_pem: '' })
  const [busy, setBusy] = useState(false)
  const load = () => api.get('/certs').then(d => setList(d.certs || [])).catch(e => toast(e.message, 'error'))
  useEffect(() => { load() }, [])

  const create = async (e) => {
    e.preventDefault()
    setBusy(true)
    try {
      await api.post('/certs', f)
      setF({ name: '', domains: '', cert_pem: '', key_pem: '' })
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
      <PageHead kicker="TLS" title="证书" desc="VLESS+XHTTP、Trojan、AnyTLS 需要 PEM。REALITY 不需要证书。" />
      <form onSubmit={create} className="card p-5 mb-4 space-y-3">
        <div className="grid grid-cols-1 md:grid-cols-2 gap-3">
          <Field label="名称"><input className="input-field" placeholder="example.com" value={f.name} onChange={e => setF({ ...f, name: e.target.value })} required /></Field>
          <Field label="域名" hint="可选，逗号分隔"><input className="input-field" placeholder="www.example.com" value={f.domains} onChange={e => setF({ ...f, domains: e.target.value })} /></Field>
        </div>
        <Field label="证书 PEM"><textarea className="input-field font-mono text-[12px]" placeholder="-----BEGIN CERTIFICATE-----" value={f.cert_pem} onChange={e => setF({ ...f, cert_pem: e.target.value })} required /></Field>
        <Field label="私钥 PEM"><textarea className="input-field font-mono text-[12px]" placeholder="-----BEGIN PRIVATE KEY-----" value={f.key_pem} onChange={e => setF({ ...f, key_pem: e.target.value })} required /></Field>
        <button className="btn-primary" disabled={busy}><Icon name="plus" size={16} /> 上传</button>
      </form>
      <div className="card overflow-hidden">
        {list.length === 0 ? (
          <Empty title="暂无证书" hint="把完整证书链和私钥贴进来。" />
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
                      <button type="button" className="linkish" style={{ color: 'var(--color-danger)' }} onClick={() => del(c.id)}>删除</button>
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
