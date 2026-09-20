import { FilterTabs } from './ui'

export const rulePresets = [
  ['minimal', '极简'],
  ['balanced', '均衡'],
  ['comprehensive', '完整'],
  ['custom', '自定义'],
]

export const userRulePresets = [['', '跟随全局'], ...rulePresets]

export function catsForPreset(preset, catalog) {
  if (!Array.isArray(catalog)) return []
  if (!preset || preset === 'custom') return []
  return catalog.filter(c => (c.presets || []).includes(preset)).map(c => c.name)
}

export function presetLabel(preset) {
  const hit = rulePresets.find(([id]) => id === preset)
  return hit ? hit[1] : '均衡'
}

export function SubRuleCatalog({ catalog, cats, onToggle }) {
  return (
    <div className="grid grid-cols-2 sm:grid-cols-3 gap-2">
      {(catalog || []).map(c => {
        const on = (cats || []).includes(c.name)
        return (
          <button
            key={c.name}
            type="button"
            className={`node-pick ${on ? 'is-on' : ''}`}
            onClick={() => onToggle(c.name)}
          >
            <span className={`node-check ${on ? 'is-on' : ''}`}>{on ? '✓' : ''}</span>
            <span className="text-[13px] leading-snug">{c.label}</span>
          </button>
        )
      })}
    </div>
  )
}

export function SubRulePicker({ preset, cats, catalog, onPreset, onToggle, items }) {
  return (
    <>
      <div>
        <div className="kicker mb-2">规则模式</div>
        <FilterTabs value={preset} onChange={onPreset} items={items || rulePresets} />
      </div>
      <div>
        <div className="kicker mb-2">规则选择</div>
        <SubRuleCatalog catalog={catalog} cats={cats} onToggle={onToggle} />
        <p className="text-[12px] text-ink-mut mt-2">{(cats || []).length} 类。广告拦截默认丢弃；私有网络和国内默认直连；其余走节点选择。</p>
      </div>
    </>
  )
}
