import { useEffect, useState } from 'react'
import { api } from '../lib/api'
import { useToast, useUser } from '../components/Layout'
import { Field, PageHead } from '../components/ui'

export default function Settings() {
  const toast = useToast()
  const { refreshUser, version } = useUser()
  const [f, setF] = useState({ panel_name: '', panel_url: '' })
  const [busy, setBusy] = useState(false)
  useEffect(() => {
    api.get('/settings').then(d => setF({ panel_name: d.panel_name || '', panel_url: d.panel_url || '' })).catch(e => toast(e.message, 'error'))
  }, [])
  const save = async (e) => {
    e.preventDefault()
    setBusy(true)
    try {
      await api.put('/settings', f)
      toast('已保存')
      refreshUser()
    } catch (e) { toast(e.message, 'error') }
    finally { setBusy(false) }
  }
  return (
    <div>
      <PageHead title="设置" desc={`当前版本 ${version || '—'}`} />
      <form onSubmit={save} className="card p-5 max-w-xl space-y-4">
        <Field label="面板名称">
          <input className="input-field" value={f.panel_name} onChange={e => setF({ ...f, panel_name: e.target.value })} />
        </Field>
        <Field label="面板 URL" hint="安装命令与订阅用。留空则按当前访问地址自动生成。">
          <input className="input-field" value={f.panel_url} onChange={e => setF({ ...f, panel_url: e.target.value })} placeholder="https://panel.example.com" />
        </Field>
        <button className="btn-primary" disabled={busy}>保存</button>
      </form>
      <div className="mt-5 text-[13px] text-ink-mut max-w-xl leading-relaxed">
        节点按入站协议自行安装内核到 PATH：Xray（默认）、sing-box（AnyTLS，≥ 1.12）、mita（Mieru）。Agent 只在有对应入站时才拉起该内核。
      </div>
    </div>
  )
}
