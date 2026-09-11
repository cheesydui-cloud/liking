import { useState } from 'react'
import { api } from '../lib/api'
import { useToast } from '../components/Layout'
import { Field, PageHead } from '../components/ui'

export default function Password() {
  const toast = useToast()
  const [oldP, setOld] = useState('')
  const [n, setN] = useState('')
  const [busy, setBusy] = useState(false)
  const submit = async (e) => {
    e.preventDefault()
    setBusy(true)
    try {
      await api.post('/password', { old: oldP, new: n })
      toast('已修改')
      setOld(''); setN('')
    } catch (e) { toast(e.message, 'error') }
    finally { setBusy(false) }
  }
  return (
    <div>
      <PageHead kicker="Account" title="修改密码" desc="至少 6 位。改完后当前会话仍然有效。" />
      <form onSubmit={submit} className="card p-6 max-w-md space-y-4">
        <Field label="原密码"><input className="input-field" type="password" value={oldP} onChange={e => setOld(e.target.value)} required autoComplete="current-password" /></Field>
        <Field label="新密码"><input className="input-field" type="password" value={n} onChange={e => setN(e.target.value)} required autoComplete="new-password" /></Field>
        <button className="btn-primary" disabled={busy}>保存</button>
      </form>
    </div>
  )
}
