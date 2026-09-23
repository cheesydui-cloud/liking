import { Field } from './ui'
import { KB_PER_MBPS, SPEED_MAX_KBPS, switchSpeedUnit } from '../lib/speed'

export function SpeedField({ value, unit, onChange, hint }) {
  const mbps = unit === 'mbps'
  const setUnit = (next) => onChange(switchSpeedUnit(value, unit, next))
  return (
    <Field as="div" label="限速" hint={hint}>
      <div className="speed-field">
        <input
          className="input-field font-mono"
          type="number"
          min="0"
          max={mbps ? 10000 : SPEED_MAX_KBPS}
          step={mbps ? 'any' : '1'}
          placeholder="不限"
          value={value}
          onChange={e => onChange({ value: e.target.value, unit })}
        />
        <div className="unit-switch" role="group" aria-label="限速单位">
          <button type="button" aria-pressed={!mbps} className={!mbps ? 'is-on' : ''} onClick={() => setUnit('kbps')}>KB/s</button>
          <button type="button" aria-pressed={mbps} className={mbps ? 'is-on' : ''} onClick={() => setUnit('mbps')}>Mbps</button>
        </div>
      </div>
      <span className="block text-[11.5px] text-ink-mut mt-1">1 Mbps = {KB_PER_MBPS} KB/s。KB/s 按下载速度，可填 100。</span>
    </Field>
  )
}
