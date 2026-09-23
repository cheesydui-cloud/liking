import { useEffect, useState } from 'react'
import { api } from '../lib/api'
import { useToast } from '../components/Layout'
import { PageHead } from '../components/ui'
import { catsForPreset, SubRulePicker } from '../components/SubRules'

export function SubscribeRulesPanel() {
  const toast = useToast()
  const [preset, setPreset] = useState('balanced')
  const [cats, setCats] = useState([])
  const [catalog, setCatalog] = useState([])
  const [busy, setBusy] = useState(false)

  const load = () => api.get('/settings').then(d => {
    const p = d.sub_rule_preset || 'balanced'
    const list = Array.isArray(d.sub_rule_catalog) ? d.sub_rule_catalog : []
    setPreset(p)
    setCatalog(list)
    setCats(Array.isArray(d.sub_rule_categories) ? d.sub_rule_categories : catsForPreset(p, list))
  }).catch(e => toast(e.message, 'error'))

  useEffect(() => { load() }, [])

  const pickPreset = (id) => {
    setPreset(id)
    if (id !== 'custom') setCats(catsForPreset(id, catalog))
  }

  const toggle = (name) => {
    setPreset('custom')
    setCats(prev => prev.includes(name) ? prev.filter(x => x !== name) : [...prev, name])
  }

  const save = async (e) => {
    e.preventDefault()
    setBusy(true)
    try {
      await api.put('/settings', { sub_rule_preset: preset, sub_rule_categories: cats })
      toast('已保存')
      load()
    } catch (err) { toast(err.message, 'error') }
    finally { setBusy(false) }
  }

  return (
    <form onSubmit={save} className="card p-5 max-w-3xl space-y-4">
      <div>
        <div className="text-[15px] font-medium">分流规则</div>
        <p className="text-[12.5px] text-ink-mut mt-1 leading-relaxed">
          默认写入所有用户的 Clash Meta 与 sing-box 订阅。用户页可单独改。节点仍由套餐决定，通用 URI 不受影响。规则集来自 MetaCubeX，客户端第一次更新会下载。
        </p>
      </div>
      <SubRulePicker preset={preset} cats={cats} catalog={catalog} onPreset={pickPreset} onToggle={toggle} />
      <button className="btn-primary" disabled={busy}>{busy ? '保存中…' : '保存'}</button>
    </form>
  )
}

export default function Subscribe() {
  return (
    <div>
      <PageHead title="分流" />
      <SubscribeRulesPanel />
    </div>
  )
}
