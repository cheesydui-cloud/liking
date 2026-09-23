import { useEffect, useMemo, useRef, useState } from 'react'
import { api } from '../lib/api'
import { cacheGen, peekList, putList } from '../lib/listCache'
import { startPoll } from '../lib/poll'
import { createOpLock } from '../lib/opLock'
import { asArray, isAbort } from '../lib/safe'
import { copyText } from '../lib/copy'
import { useToast, useDialog } from '../components/Layout'
import { Badge, DayBars, Empty, Field, FilterTabs, Icon, Meter, Modal, MoreMenu, PageHead, SearchInput, SkeletonRows, billedBytes, fmtBps, fmtBytes, fmtDateShort } from '../components/ui'
import { SubPanel } from '../components/SubPanel'
import { NodePreviewList } from '../components/NodePreview'
import { catsForPreset, presetLabel, SubRulePicker, userRulePresets } from '../components/SubRules'
import { SpeedField } from '../components/SpeedField'
import { formatSpeedLimit, kbpsFromForm, speedFormFromKbps } from '../lib/speed'

function randPassword() {
  const a = 'ABCDEFGHJKLMNPQRSTUVWXYZabcdefghijkmnopqrstuvwxyz23456789'
  const b = new Uint8Array(10)
  crypto.getRandomValues(b)
  return [...b].map(x => a[x % a.length]).join('')
}

function trafficCap(u, pkgs) {
  if (u.traffic_limit != null) return Number(u.traffic_limit)
  const p = pkgs.find(x => x.id === u.package_id)
  return p?.traffic_bytes || 0
}

function ymd(ts) {
  if (!ts) return ''
  const d = new Date(ts * 1000)
  if (Number.isNaN(d.getTime())) return ''
  return new Intl.DateTimeFormat('en-CA', {
    timeZone: 'Asia/Shanghai',
    year: 'numeric',
    month: '2-digit',
    day: '2-digit',
  }).format(d)
}

function ymdToUnix(s) {
  if (!s) return 0
  const d = new Date(`${s}T23:59:59+08:00`)
  const n = d.getTime()
  return Number.isNaN(n) ? 0 : Math.floor(n / 1000)
}

function gbFromBytes(n) {
  if (n == null || n === '') return ''
  const gb = Number(n) / (1024 ** 3)
  if (!Number.isFinite(gb) || gb < 0) return ''
  if (gb === 0) return '0'
  return String(Math.round(gb * 1000) / 1000)
}

function bytesFromGB(s) {
  if (s == null || String(s).trim() === '') return null
  const n = Number(s)
  if (!Number.isFinite(n) || n < 0) return null
  return Math.round(n * 1024 * 1024 * 1024)
}

const emptyForm = { username: '', password: '', remark: '', package_id: '', days: 30, expires: '', traffic_gb: '', enabled: true, traffic_reset_day: 0, speed_value: '', speed_unit: 'mbps' }

function speedLimitBps(kbps) {
  const n = Number(kbps) || 0
  if (n <= 0) return 0
  return n * 1000
}

function userExpiryTs(u) {
  const a = Number(u?.expires_at) || 0
  const b = Number(u?.package_expires_at) || 0
  if (!a) return b
  if (!b) return a
  return Math.min(a, b)
}

function UserFlags({ u }) {
  if (u.role === 'admin') return null
  const flags = []
  const exp = userExpiryTs(u)
  const now = Date.now()
  if (exp && exp * 1000 < now) flags.push(<Badge key="exp" tone="danger">到期</Badge>)
  else if (exp && exp * 1000 < now + 7 * 86400 * 1000) flags.push(<Badge key="soon" tone="warn">即将到期</Badge>)
  if (u.traffic_cap > 0 && billedBytes(u) >= u.traffic_cap) flags.push(<Badge key="cap" tone="danger">超量</Badge>)
  else if (u.quota_ratio >= 80) flags.push(<Badge key="q" tone="warn">{u.quota_ratio}%</Badge>)
  if (u.enabled === false) flags.push(<Badge key="off" tone="muted">停用</Badge>)
  if (u.speed_limit > 0) flags.push(<Badge key="spd" tone="muted">{formatSpeedLimit(u.speed_limit)}</Badge>)
  if (u.sub_rule_preset) flags.push(<Badge key="rule" tone="muted">独立规则</Badge>)
  const deny = denyBadgeText(u)
  if (deny) flags.push(<Badge key="deny" tone="muted">{deny}</Badge>)
  if (!flags.length) return null
  return flags
}

const siteDenyFallback = [
  { name: 'tiktok', label: 'TikTok', group: '社交' },
  { name: 'facebook', label: 'Facebook', group: '社交' },
  { name: 'instagram', label: 'Instagram', group: '社交' },
  { name: 'twitter', label: 'Twitter / X', group: '社交' },
  { name: 'telegram', label: 'Telegram', group: '社交' },
  { name: 'discord', label: 'Discord', group: '社交' },
  { name: 'whatsapp', label: 'WhatsApp', group: '社交' },
  { name: 'line', label: 'LINE', group: '社交' },
  { name: 'reddit', label: 'Reddit', group: '社交' },
  { name: 'linkedin', label: 'LinkedIn', group: '社交' },
  { name: 'pinterest', label: 'Pinterest', group: '社交' },
  { name: 'snapchat', label: 'Snapchat', group: '社交' },
  { name: 'threads', label: 'Threads', group: '社交' },
  { name: 'weibo', label: '微博', group: '社交' },
  { name: 'xiaohongshu', label: '小红书', group: '社交' },
  { name: 'douyin', label: '抖音', group: '社交' },
  { name: 'youtube', label: '油管', group: '视频' },
  { name: 'netflix', label: 'Netflix', group: '视频' },
  { name: 'twitch', label: 'Twitch', group: '视频' },
  { name: 'bilibili', label: '哔哩哔哩', group: '视频' },
  { name: 'openai', label: 'ChatGPT', group: 'AI' },
  { name: 'claude', label: 'Claude', group: 'AI' },
  { name: 'gemini', label: 'Gemini', group: 'AI' },
  { name: 'grok', label: 'Grok', group: 'AI' },
  { name: 'perplexity', label: 'Perplexity', group: 'AI' },
  { name: 'deepseek', label: 'DeepSeek', group: 'AI' },
  { name: 'huggingface', label: 'Hugging Face', group: 'AI' },
  { name: 'midjourney', label: 'Midjourney', group: 'AI' },
  { name: 'characterai', label: 'Character.AI', group: 'AI' },
  { name: 'copilot', label: 'Copilot', group: 'AI' },
  { name: 'kimi', label: 'Kimi', group: 'AI' },
  { name: 'tongyi', label: '通义千问', group: 'AI' },
  { name: 'doubao', label: '豆包', group: 'AI' },
  { name: 'poe', label: 'Poe', group: 'AI' },
  { name: 'google', label: '谷歌', group: '工具' },
  { name: 'speedtest', label: '测速', group: '工具' },
  { name: 'iplookup', label: 'IP 查询', group: '工具' },
]

const siteDenyGroupOrder = ['社交', '视频', 'AI', '工具']

function catalogGroups(catalog) {
  const by = {}
  for (const c of catalog || []) {
    const g = c.group || '其它'
    if (!by[g]) by[g] = []
    by[g].push(c)
  }
  const order = [...siteDenyGroupOrder]
  for (const g of Object.keys(by)) {
    if (!order.includes(g)) order.push(g)
  }
  return order.filter(g => by[g]?.length).map(g => [g, by[g]])
}

const siteFilterTabs = [
  ['', '不限制'],
  ['deny', '禁止这些'],
  ['allow', '只允许这些'],
]

function denyCatLabel(name, catalog) {
  const hit = (catalog || siteDenyFallback).find(c => c.name === name)
  return hit ? hit.label : name
}

function siteFilterModeOf(u) {
  const m = String(u?.site_filter_mode || '')
  if (m === 'allow' || m === 'deny') return m
  if (m === 'off') return ''
  const cats = Array.isArray(u?.site_deny_categories) ? u.site_deny_categories : []
  const domains = Array.isArray(u?.site_deny_domains) ? u.site_deny_domains : []
  if (cats.length || domains.length) return 'deny'
  return ''
}

function denyBadgeText(u) {
  const mode = siteFilterModeOf(u)
  if (!mode) return ''
  const cats = Array.isArray(u?.site_deny_categories) ? u.site_deny_categories : []
  const domains = Array.isArray(u?.site_deny_domains) ? u.site_deny_domains : []
  if (!cats.length && !domains.length) return ''
  const parts = cats.map(n => denyCatLabel(n))
  if (domains.length) parts.push(domains.length === 1 ? domains[0] : `${domains.length} 个域名`)
  const joined = parts.join('、')
  return mode === 'allow' ? `只允许：${joined}` : `已禁止：${joined}`
}

function parseDenyLines(text) {
  return String(text || '').split(/[\s,;]+/).map(s => s.trim()).filter(Boolean)
}

function cardDate(ts) {
  if (!ts) return '不限期'
  const d = new Date(Number(ts) * 1000)
  if (Number.isNaN(d.getTime())) return '—'
  return new Intl.DateTimeFormat('en-CA', {
    timeZone: 'Asia/Shanghai',
    year: 'numeric',
    month: '2-digit',
    day: '2-digit',
  }).format(d)
}

function formatUserCard(u, pkgs = []) {
  const origin = typeof window !== 'undefined' ? window.location.origin : ''
  const lines = [`网址：${origin}`, `账号：${u.username || ''}`]
  if (u.password) lines.push(`密码：${u.password}`)
  lines.push(`到期：${cardDate(u.expires_at)}`)
  if (u.package_name) lines.push(`套餐：${u.package_name}`)
  else if (u.role !== 'admin') lines.push('套餐：未绑定')
  if (u.role !== 'admin') {
    const cap = u.traffic_cap || trafficCap(u, pkgs)
    if (cap > 0) lines.push(`流量：${fmtBytes(billedBytes(u))} / ${fmtBytes(cap)}`)
    else lines.push('流量：不限')
    if (u.speed_limit > 0) lines.push(`限速：${formatSpeedLimit(u.speed_limit)}`)
    else lines.push('限速：不限')
  }
  if (u.sub_token) lines.push(`订阅：${origin}/api/sub/${u.sub_token}`)
  if (u.remark) lines.push(`备注：${u.remark}`)
  if (u.enabled === false) lines.push('状态：停用')
  return lines.join('\n')
}

function userTone(u) {
  if (u.enabled === false) return 'is-off'
  const exp = userExpiryTs(u)
  if (exp && exp * 1000 < Date.now()) return 'is-fault'
  if (u.traffic_cap > 0 && billedBytes(u) >= u.traffic_cap) return 'is-fault'
  return 'is-pkg'
}

function Metric({ label, value, danger, plain, tone }) {
  return (
    <div className={`metric${tone ? ` is-${tone}` : ''}`}>
      <span className="metric-k">{label}</span>
      <span className={`metric-v${plain ? ' is-plain' : ''}${danger ? ' is-expired' : ''}`}>{value}</span>
    </div>
  )
}

function UserRulesModal({ user, onClose, onSaved }) {
  const toast = useToast()
  const [preset, setPreset] = useState(user.sub_rule_preset || '')
  const [cats, setCats] = useState(Array.isArray(user.sub_rule_categories) ? user.sub_rule_categories : [])
  const [catalog, setCatalog] = useState([])
  const [globalPreset, setGlobalPreset] = useState('balanced')
  const [globalCats, setGlobalCats] = useState([])
  const [busy, setBusy] = useState(false)
  const [ready, setReady] = useState(false)

  useEffect(() => {
    api.get('/settings').then(d => {
      const gp = d.sub_rule_preset || 'balanced'
      const list = Array.isArray(d.sub_rule_catalog) ? d.sub_rule_catalog : []
      const gc = Array.isArray(d.sub_rule_categories) ? d.sub_rule_categories : catsForPreset(gp, list)
      setCatalog(list)
      setGlobalPreset(gp)
      setGlobalCats(gc)
      const p = user.sub_rule_preset || ''
      setPreset(p)
      if (!p) setCats(gc)
      else if (p === 'custom') setCats(Array.isArray(user.sub_rule_categories) ? user.sub_rule_categories : [])
      else setCats(catsForPreset(p, list))
      setReady(true)
    }).catch(e => toast(e.message, 'error'))
  }, [user])

  const pickPreset = (id) => {
    setPreset(id)
    if (!id) setCats(globalCats)
    else if (id !== 'custom') setCats(catsForPreset(id, catalog))
  }

  const toggle = (name) => {
    setPreset('custom')
    setCats(prev => prev.includes(name) ? prev.filter(x => x !== name) : [...prev, name])
  }

  const save = async () => {
    setBusy(true)
    try {
      await api.put(`/users/${user.id}`, {
        sub_rule_preset: preset,
        sub_rule_categories: preset ? cats : [],
      })
      toast('已保存')
      onSaved()
      onClose()
    } catch (e) { toast(e.message, 'error') }
    finally { setBusy(false) }
  }

  return (
    <Modal open title={`${user.username} 的分流规则`} onClose={onClose} size="lg" footer={
      <>
        <button type="button" className="btn-ghost" onClick={onClose}>取消</button>
        <button type="button" className="btn-primary" disabled={busy || !ready} onClick={save}>{busy ? '保存中…' : '保存'}</button>
      </>
    }>
      <div className="space-y-4">
        <p className="text-[12.5px] text-ink-mut leading-relaxed">
          只改这个用户的 Clash Meta 与 sing-box 订阅。跟随全局则用「设置 · 分流」里的规则。节点仍由套餐决定，通用 URI 不受影响。
        </p>
        {!preset ? (
          <p className="text-[12px] text-ink-mut">当前全局：{presetLabel(globalPreset)}，{globalCats.length} 类。点规则会变成这个用户自己的自定义。</p>
        ) : null}
        <SubRulePicker
          preset={preset}
          cats={cats}
          catalog={catalog}
          onPreset={pickPreset}
          onToggle={toggle}
          items={userRulePresets}
        />
      </div>
    </Modal>
  )
}

function UserDenyModal({ user, onClose, onSaved }) {
  const toast = useToast()
  const [catalog, setCatalog] = useState(siteDenyFallback)
  const [mode, setMode] = useState(() => siteFilterModeOf(user))
  const [cats, setCats] = useState(Array.isArray(user.site_deny_categories) ? user.site_deny_categories : [])
  const [custom, setCustom] = useState(Array.isArray(user.site_deny_domains) ? user.site_deny_domains.join('\n') : '')
  const [busy, setBusy] = useState(false)

  useEffect(() => {
    api.get('/settings').then(d => {
      const list = Array.isArray(d.site_deny_catalog) && d.site_deny_catalog.length ? d.site_deny_catalog : siteDenyFallback
      setCatalog(list)
    }).catch(() => setCatalog(siteDenyFallback))
  }, [user])

  const toggle = (name) => {
    setCats(prev => prev.includes(name) ? prev.filter(x => x !== name) : [...prev, name])
  }

  const save = async () => {
    const domains = mode ? parseDenyLines(custom) : []
    if (mode && domains.length > 50) {
      toast('自定义域名最多 50 个', 'error')
      return
    }
    if (mode === 'allow' && !cats.length && !domains.length) {
      toast('只允许至少选一个网站', 'error')
      return
    }
    setBusy(true)
    try {
      await api.put(`/users/${user.id}`, {
        site_filter_mode: mode,
        site_deny_categories: mode ? cats : [],
        site_deny_domains: mode ? domains : [],
      })
      toast('已保存，节点立刻生效')
      onSaved()
      onClose()
    } catch (e) { toast(e.message, 'error') }
    finally { setBusy(false) }
  }

  const hint = mode === 'allow'
    ? '只放行勾选的网站。走节点的流量立刻拦截。只允许时会关掉 Clash / sing-box 的国内直连，否则百度这些根本不到节点，需要重新拉取订阅。v2rayN 请关掉绕过大陆。局域网仍直连。测活和常用 DNS 自动放行。已建立的连接可能要重连。Mieru 无效。'
    : '在节点上拦截，Clash、v2rayN、URI 都生效，不用更新订阅。已建立的连接可能要重连。访问限制对 Mieru 无效。'

  return (
    <Modal open title={`${user.username} 的访问限制`} onClose={onClose} size="lg" footer={
      <>
        <button type="button" className="btn-ghost" onClick={onClose}>取消</button>
        <button type="button" className="btn-primary" disabled={busy} onClick={save}>{busy ? '保存中…' : '保存'}</button>
      </>
    }>
      <div className="space-y-4">
        <p className="text-[12.5px] text-ink-mut leading-relaxed">{hint}</p>
        <FilterTabs value={mode} onChange={setMode} items={siteFilterTabs} />
        {mode ? (
          <>
            <div className="space-y-4">
              {catalogGroups(catalog).map(([g, items]) => (
                <div key={g}>
                  <div className="kicker mb-2">{g}</div>
                  <div className="grid grid-cols-2 sm:grid-cols-3 gap-2">
                    {items.map(c => {
                      const on = cats.includes(c.name)
                      return (
                        <button
                          key={c.name}
                          type="button"
                          className={`node-pick ${on ? 'is-on' : ''}`}
                          onClick={() => toggle(c.name)}
                        >
                          <span className={`node-check ${on ? 'is-on' : ''}`}>{on ? '✓' : ''}</span>
                          <span className="text-[13px] leading-snug">{c.label}</span>
                        </button>
                      )
                    })}
                  </div>
                </div>
              ))}
            </div>
            <Field label="自定义域名" hint="每行一个，域名或 IP。最多 50 个。">
              <textarea
                className="input-field"
                rows={5}
                placeholder={"instagram.com\n1.1.1.1"}
                value={custom}
                onChange={e => setCustom(e.target.value)}
              />
            </Field>
          </>
        ) : (
          <p className="text-[12px] text-ink-mut">这个用户走节点时不拦网站。</p>
        )}
      </div>
    </Modal>
  )
}

export default function Users() {
  const toast = useToast()
  const dialog = useDialog()
  const [list, setList] = useState(() => asArray(peekList('users')))
  const [pkgs, setPkgs] = useState(() => asArray(peekList('packages')))
  const [ready, setReady] = useState(() => peekList('users') !== undefined)
  const [pollErr, setPollErr] = useState('')
  const ops = useRef(createOpLock())
  const trafficSeq = useRef(0)
  const [f, setF] = useState(emptyForm)
  const [busy, setBusy] = useState(false)
  const [formOpen, setFormOpen] = useState(false)
  const [editUser, setEditUser] = useState(null)
  const [subUser, setSubUser] = useState(null)
  const [previewUser, setPreviewUser] = useState(null)
  const [previewData, setPreviewData] = useState(null)
  const [previewErr, setPreviewErr] = useState('')
  const [previewLoading, setPreviewLoading] = useState(false)
  const [trafficUser, setTrafficUser] = useState(null)
  const [trafficDetail, setTrafficDetail] = useState(null)
  const [q, setQ] = useState('')
  const [pkgFilter, setPkgFilter] = useState('')
  const [statusFilter, setStatusFilter] = useState('')
  const [bulkOpen, setBulkOpen] = useState(false)
  const [bulkText, setBulkText] = useState('')
  const [bulkBusy, setBulkBusy] = useState(false)
  const [bulkPkg, setBulkPkg] = useState('')
  const [bulkDays, setBulkDays] = useState(30)
  const [ruleUser, setRuleUser] = useState(null)
  const [denyUser, setDenyUser] = useState(null)
  const [selected, setSelected] = useState(() => new Set())
  const [batchBusy, setBatchBusy] = useState(false)

  const load = async () => {
    try {
      const g = cacheGen()
      const [a, b] = await Promise.all([api.get('/users'), api.get('/packages')])
      setList(putList('users', asArray(a.users), g))
      setPkgs(putList('packages', asArray(b.packages), g))
    } catch (e) { toast(e.message, 'error') }
    finally { setReady(true) }
  }
  useEffect(() => {
    load()
    return startPoll(async (signal) => {
      const g = cacheGen()
      try {
        const a = await api.get('/users', signal)
        setList(putList('users', asArray(a.users), g))
        setPollErr('')
      } catch (e) {
        if (isAbort(e)) return
        setPollErr('实时刷新失败，显示的是上次成功数据')
        throw e
      }
    }, 5000, { immediate: false })
  }, [])

  const closeForm = () => {
    setFormOpen(false)
    setEditUser(null)
    setF({ ...emptyForm, package_id: f.package_id, days: 30 })
  }

  const openCreate = () => {
    setEditUser(null)
    setF({ ...emptyForm, package_id: f.package_id, days: 30 })
    setFormOpen(true)
  }

  const openEdit = (u) => {
    const speed = speedFormFromKbps(u.speed_limit)
    setEditUser(u)
    setF({
      username: u.username || '',
      password: '',
      remark: u.remark || '',
      package_id: u.package_id || '',
      days: 30,
      expires: ymd(u.expires_at),
      traffic_gb: gbFromBytes(u.traffic_limit),
      enabled: u.enabled !== false,
      traffic_reset_day: u.traffic_reset_day || 0,
      speed_value: speed.value,
      speed_unit: speed.unit,
    })
    setFormOpen(true)
  }

  const bumpExpiry = (days) => {
    const today = new Date()
    today.setHours(0, 0, 0, 0)
    let d = f.expires ? new Date(`${f.expires}T00:00:00`) : new Date(today)
    if (Number.isNaN(d.getTime()) || d < today) d = new Date(today)
    d.setDate(d.getDate() + days)
    const z = n => String(n).padStart(2, '0')
    setF({ ...f, expires: `${d.getFullYear()}-${z(d.getMonth() + 1)}-${z(d.getDate())}` })
  }

  const save = async (e) => {
    e.preventDefault()
    const username = f.username.trim()
    if (!username) {
      toast('用户名不能为空', 'error')
      return
    }
    if (editUser) {
      if (editUser.enabled !== false && f.enabled === false) {
        if (!(await dialog.confirm({
          title: '停用用户',
          message: `将停用 ${editUser.username}，该用户立刻无法登录，订阅节点会从客户端消失。`,
          danger: true,
          okText: '停用',
        }))) return
      }
      const prev = Number(editUser.package_id || 0)
      const next = Number(f.package_id || 0)
      if (prev && next && prev !== next) {
        const from = editUser.package_name || '当前套餐'
        const to = pkgs.find(p => Number(p.id) === next)?.name || '新套餐'
        if (!(await dialog.confirm({ title: '更换套餐', message: `将从「${from}」换到「${to}」，已用流量会清零。` }))) return
      }
      const speedKbps = kbpsFromForm(f.speed_value, f.speed_unit)
      if (!Number.isFinite(speedKbps)) {
        toast('限速无效', 'error')
        return
      }
    }
    setBusy(true)
    try {
      if (editUser) {
        const speedKbps = kbpsFromForm(f.speed_value, f.speed_unit)
        const body = {
          username,
          remark: f.remark,
          enabled: f.enabled !== false,
          expires_at: ymdToUnix(f.expires),
          traffic_limit: bytesFromGB(f.traffic_gb),
          traffic_reset_day: Number(f.traffic_reset_day) || 0,
          speed_limit: speedKbps,
        }
        if (!f.package_id) body.unbind_package = true
        else body.package_id = Number(f.package_id)
        if (f.password.trim()) body.password = f.password.trim()
        await api.put(`/users/${editUser.id}`, body)
        if (f.password.trim()) {
          const pkgName = pkgs.find(p => Number(p.id) === Number(f.package_id))?.name || ''
          const card = formatUserCard({
            ...editUser,
            username,
            remark: f.remark,
            expires_at: ymdToUnix(f.expires),
            package_name: pkgName,
            password: f.password.trim(),
            enabled: f.enabled !== false,
            speed_limit: kbpsFromForm(f.speed_value, f.speed_unit),
          }, pkgs)
          try {
            await copyText(card)
            try { await api.post(`/users/${editUser.id}/forget-password`) } catch { /* keep going */ }
            toast('已保存，名片已复制。密码只这一次。')
          } catch { toast('已保存') }
        } else toast('已保存')
      } else {
        const body = { username, remark: f.remark, days: Number(f.days) || 0 }
        if (f.password) body.password = f.password
        if (f.package_id) body.package_id = Number(f.package_id)
        const d = await api.post('/users', body)
        const pw = d.password || f.password
        const created = d.user ? { ...d.user, password: pw || d.user.password || '' } : null
        if (created) {
          try {
            await copyText(formatUserCard(created, pkgs))
            if (created.id) {
              try { await api.post(`/users/${created.id}/forget-password`) } catch { /* keep going */ }
            }
            toast('已创建，名片已复制。密码只这一次。')
          } catch { toast(pw ? `已创建，密码 ${pw}` : '已创建') }
        } else if (pw) {
          try { await copyText(pw); toast('已创建，密码已复制') } catch { toast(`已创建，密码 ${pw}`) }
        } else toast('已创建')
        if (!body.package_id) toast('未绑定套餐，订阅里不会有节点', 'error')
        else if (d.user) setSubUser(d.user)
      }
      closeForm()
      load()
    } catch (e) { toast(e.message, 'error') }
    finally { setBusy(false) }
  }

  const act = async (key, fn) => {
    await ops.current.run(key, async () => {
      try { await fn(); await load() } catch (e) { toast(e.message, 'error') }
    })
  }

  const toggleEnabled = async (u) => {
    const next = !u.enabled
    if (!next) {
      if (!(await dialog.confirm({
        title: '停用用户',
        message: `将停用 ${u.username}，该用户立刻无法登录，订阅节点会从客户端消失。`,
        danger: true,
        okText: '停用',
      }))) return
    }
    await act(`tog-${u.id}`, () => api.put(`/users/${u.id}`, { enabled: next }))
  }

  const resetTraffic = async () => {
    if (!editUser) return
    if (!(await dialog.confirm({ title: '清零流量', message: `将清空 ${editUser.username} 已用流量。` }))) return
    try {
      await api.post(`/users/${editUser.id}/reset-traffic`)
      toast('已清零流量')
      const fresh = (await api.get('/users')).users?.find(x => x.id === editUser.id)
      if (fresh) setEditUser(fresh)
      load()
    } catch (e) { toast(e.message, 'error') }
  }

  const remove = async (u) => {
    if (!(await dialog.confirm({ title: '删除用户', message: `将删除 ${u.username} 及其订阅。`, danger: true }))) return
    await act(`del-${u.id}`, () => api.del(`/users/${u.id}`))
  }

  const openTraffic = async (u) => {
    const seq = ++trafficSeq.current
    setTrafficUser(u)
    setTrafficDetail(null)
    try {
      const d = await api.get(`/users/${u.id}/traffic?days=14`)
      if (seq !== trafficSeq.current) return
      setTrafficDetail(d)
    } catch (e) {
      if (seq !== trafficSeq.current) return
      toast(e.message, 'error')
    }
  }

  const copyCard = async (u) => {
    try {
      await copyText(formatUserCard(u, pkgs))
      if (u.password) {
        try { await api.post(`/users/${u.id}/forget-password`) } catch { /* keep going */ }
        setList(cur => cur.map(x => x.id === u.id ? { ...x, password: '' } : x))
        toast('已复制名片。密码只这一次，下次请重置。')
      } else {
        toast('已复制名片。没有保存的密码，可用「重置密码」生成一次。')
      }
    } catch {
      toast('浏览器不允许自动复制', 'error')
    }
  }

  const resetPassword = async (u) => {
    if (!(await dialog.confirm({ title: '重置密码', message: '会生成新密码，旧密码和其它登录立刻失效。', danger: true }))) return
    try {
      const d = await api.post(`/users/${u.id}/password`, { password: '' })
      const next = { ...u, password: d.password || '' }
      try {
        await copyText(formatUserCard(next, pkgs))
        try { await api.post(`/users/${u.id}/forget-password`) } catch { /* keep going */ }
        toast('已重置并复制名片。密码只这一次。')
      } catch {
        toast('已重置。浏览器不允许自动复制，请再点复制名片。')
        setList(cur => cur.map(x => x.id === u.id ? next : x))
        return
      }
      load()
    } catch (e) { toast(e.message, 'error') }
  }

  const rotate = async (u) => {
    if (!(await dialog.confirm({ title: '重置订阅令牌', message: '旧订阅链接立刻失效。' }))) return
    try {
      const d = await api.post(`/users/${u.id}/rotate-sub`)
      load()
      const next = { ...u, sub_token: d.sub_token }
      setSubUser(next)
      toast('订阅令牌已更换')
    } catch (e) { toast(e.message, 'error') }
  }

  const kickUser = async (u) => {
    const nodes = asArray(u.live_nodes).map(n => n.inbound_name || n.server_name).filter(Boolean)
    const where = nodes.length ? `当前在 ${nodes.join('、')}。` : '断开这个用户现在的连接。'
    if (!(await dialog.confirm({
      title: '踢下线',
      message: `${where}用户可以立刻重连。旧 Agent 需先升级。`,
    }))) return
    await act(`kick-${u.id}`, async () => {
      const d = await api.post(`/users/${u.id}/kick`, {})
      const fail = asArray(d.results).filter(x => !x.ok)
      if (fail.length && !d.ok) toast(fail[0]?.error || '踢下线失败', 'error')
      else if (fail.length) toast(`部分实例失败：${fail.map(x => x.server_name || x.error).join('、')}`, 'error')
      else toast('已踢下线')
    })
  }

  const toggleSelect = (id) => {
    setSelected(prev => {
      const next = new Set(prev)
      if (next.has(id)) next.delete(id)
      else next.add(id)
      return next
    })
  }

  const batchAct = async (action, extra = {}) => {
    const ids = [...selected]
    if (!ids.length) return
    const titles = {
      extend: { title: '批量续期', message: `给已选 ${ids.length} 个用户续期 30 天。` },
      reset_traffic: { title: '批量清零流量', message: `清空已选 ${ids.length} 个用户的已用流量。` },
      enable: { title: '批量启用', message: `启用已选 ${ids.length} 个用户。` },
      disable: { title: '批量停用', message: `停用已选 ${ids.length} 个用户，他们立刻无法登录。`, danger: true, okText: '停用' },
    }
    const conf = titles[action]
    if (conf && !(await dialog.confirm(conf))) return
    setBatchBusy(true)
    try {
      const d = await api.post('/users/batch', { ids, action, ...extra })
      toast(`完成 ${d.ok}/${d.total}`)
      setSelected(new Set())
      load()
    } catch (e) { toast(e.message, 'error') }
    finally { setBatchBusy(false) }
  }

  const openPreview = async (u) => {
    setPreviewUser(u)
    setPreviewData(null)
    setPreviewErr('')
    setPreviewLoading(true)
    try {
      const d = await api.get(`/users/${u.id}/nodes`)
      setPreviewData(d)
    } catch (e) {
      setPreviewErr(e.message || '加载失败')
    } finally {
      setPreviewLoading(false)
    }
  }

  const rows = useMemo(() => {
    const needle = q.trim().toLowerCase()
    const now = Date.now()
    return list.filter(u => {
      if (u.role === 'admin') return false
      if (pkgFilter === 'none' && u.package_id) return false
      if (pkgFilter && pkgFilter !== 'none' && String(u.package_id) !== pkgFilter) return false
      if (statusFilter === 'warn') {
        if (!(u.quota_ratio >= 80 && u.quota_ratio < 100)) return false
      }
      if (statusFilter === 'expired') {
        const exp = userExpiryTs(u)
        if (!(exp && exp * 1000 < now)) return false
      }
      if (!needle) return true
      const hay = [u.username, u.remark, u.package_name]
      return hay.some(x => String(x || '').toLowerCase().includes(needle))
    })
  }, [list, q, pkgFilter, statusFilter])

  const hasCustomers = list.some(u => u.role !== 'admin')
  const editing = !!editUser
  const selectedPkg = pkgs.find(p => Number(p.id) === Number(f.package_id))
  const selectedN = selected.size
  const allVisibleSelected = rows.length > 0 && rows.every(u => selected.has(u.id))

  return (
    <div>
      <PageHead
        title="用户"
        actions={
          <div className="flex gap-2">
            <button type="button" className="btn-ghost" onClick={() => setBulkOpen(true)}>批量开户</button>
            <button type="button" className="btn-primary" onClick={openCreate}>
              <Icon name="plus" size={15} /> 新建用户
            </button>
          </div>
        }
      />
      {pollErr ? <div className="alert-row is-warn mb-4">{pollErr}</div> : null}
      <div className="flex flex-col sm:flex-row gap-2 mb-3">
        <SearchInput value={q} onChange={e => setQ(e.target.value)} placeholder="搜索用户名 / 备注 / 套餐" />
        <select className="input-field toolbar-select" value={pkgFilter} onChange={e => setPkgFilter(e.target.value)}>
          <option value="">全部套餐</option>
          <option value="none">未绑定</option>
          {pkgs.map(p => <option key={p.id} value={p.id}>{p.name}</option>)}
        </select>
        <FilterTabs
          value={statusFilter}
          onChange={setStatusFilter}
          items={[['','全部'],['warn','将满 80%'],['expired','已到期']]}
        />
      </div>
      {rows.length > 0 ? (
        <div className="flex flex-wrap items-center gap-2 mb-3">
          <button
            type="button"
            className="row-act"
            onClick={() => {
              if (allVisibleSelected) setSelected(new Set())
              else setSelected(new Set(rows.map(u => u.id)))
            }}
          >
            {allVisibleSelected ? '取消全选' : '全选当前'}
          </button>
          {selectedN > 0 ? (
            <>
              <span className="text-[12px] text-ink-mut">已选 {selectedN} 个</span>
              <button type="button" className="row-act" disabled={batchBusy} onClick={() => batchAct('extend', { days: 30 })}>续期 30 天</button>
              <button type="button" className="row-act" disabled={batchBusy} onClick={() => batchAct('reset_traffic')}>清零流量</button>
              <button type="button" className="row-act" disabled={batchBusy} onClick={() => batchAct('enable')}>启用</button>
              <button type="button" className="row-act" disabled={batchBusy} onClick={() => batchAct('disable')}>停用</button>
              <button type="button" className="row-act" disabled={batchBusy} onClick={() => setSelected(new Set())}>取消</button>
            </>
          ) : null}
        </div>
      ) : null}

      {!ready ? (
        <div className="card overflow-hidden"><SkeletonRows /></div>
      ) : !hasCustomers ? (
        <div className="card overflow-hidden">
          <Empty title="暂无用户" hint="先建套餐并勾选节点，再开账号。" action={
            <button type="button" className="btn-primary" onClick={openCreate}><Icon name="plus" size={15} /> 新建用户</button>
          } />
        </div>
      ) : rows.length === 0 ? (
        <div className="card overflow-hidden">
          <Empty title="没有匹配的用户" hint="换个关键词或套餐筛选。" />
        </div>
      ) : (
        <div className="machine-grid">
          {rows.map(u => {
            const cap = u.traffic_cap || trafficCap(u, pkgs)
            const used = billedBytes(u)
            const expTs = userExpiryTs(u)
            const expired = !!(expTs && expTs * 1000 < Date.now())
            const capBps = speedLimitBps(u.speed_limit)
            const upOver = capBps > 0 && (u.net_up_bps || 0) > capBps
            const downOver = capBps > 0 && (u.net_down_bps || 0) > capBps
            return (
              <div key={u.id} className={`machine ${userTone(u)}`}>
                <div className="machine-head">
                  <button
                    type="button"
                    className={`node-check shrink-0 mt-1 ${selected.has(u.id) ? 'is-on' : ''}`}
                    aria-pressed={selected.has(u.id)}
                    aria-label={selected.has(u.id) ? '取消选择' : '选择'}
                    onClick={() => toggleSelect(u.id)}
                  >
                    {selected.has(u.id) ? '✓' : ''}
                  </button>
                  <div className="min-w-0 flex-1">
                    <div className="machine-title">
                      <span className="machine-name truncate">{u.username}</span>
                      <UserFlags u={u} />
                    </div>
                    {u.remark ? (
                      <div className="machine-host">
                        <span className="machine-remark" title={u.remark}>{u.remark}</span>
                      </div>
                    ) : null}
                    {asArray(u.live_nodes).length ? (
                      <div className="machine-host">
                        <span className="text-[12px] text-ink-mut truncate" title={asArray(u.live_nodes).map(n => n.inbound_name || n.server_name).join('、')}>
                          在线 {asArray(u.live_nodes).map(n => n.inbound_name || n.server_name).filter(Boolean).join(' · ') || '连接中'}
                        </span>
                      </div>
                    ) : null}
                  </div>
                  <div className="machine-toolbar">
                    <button type="button" className="icon-btn" onClick={() => openEdit(u)} aria-label="编辑用户" title="编辑">
                      <Icon name="pencil" size={14} />
                    </button>
                    <MoreMenu iconOnly items={[
                      { label: '编辑', onSelect: () => openEdit(u) },
                      { label: '分流规则', onSelect: () => setRuleUser(u) },
                      { label: '访问限制', onSelect: () => setDenyUser(u) },
                      { label: '预览节点', onSelect: () => openPreview(u) },
                      { label: '重置密码', onSelect: () => resetPassword(u) },
                      { sep: true },
                      { label: '流量', onSelect: () => openTraffic(u) },
                      { label: u.enabled ? '停用' : '启用', onSelect: () => toggleEnabled(u) },
                      { sep: true },
                      { label: '删除', danger: true, onSelect: () => remove(u) },
                    ]} />
                  </div>
                </div>
                <div className="machine-metrics">
                  <Metric label="套餐" value={u.package_name || '未绑定'} plain />
                  <Metric label="到期" value={expTs ? fmtDateShort(expTs) : '不限期'} danger={expired} />
                  <Metric label="额度" value={cap > 0 ? fmtBytes(cap) : '不限'} />
                  <Metric label="计费" value={u.direction === 'twoway' ? '双向' : '单向'} plain />
                  <Metric label="上行" value={fmtBps(u.net_up_bps)} tone={upOver ? undefined : 'up'} danger={upOver} />
                  <Metric label="下行" value={fmtBps(u.net_down_bps)} tone={downOver ? undefined : 'down'} danger={downOver} />
                </div>
                <div className="machine-meter">
                  <Meter value={used} max={cap} />
                  {cap > 0 ? <div className="text-[12px] text-ink-mut mt-1">剩余 {fmtBytes(Math.max(0, cap - used))}</div> : null}
                </div>
                <div className="machine-foot">
                  <button type="button" className="machine-ports" onClick={() => setSubUser(u)}>订阅</button>
                  <button type="button" className="row-act" onClick={() => copyCard(u)}>复制名片</button>
                  {asArray(u.live_nodes).length ? (
                    <button type="button" className="row-act" onClick={() => kickUser(u)}>踢下线</button>
                  ) : null}
                </div>
              </div>
            )
          })}
        </div>
      )}
      <Modal open={formOpen} title={editing ? `编辑 ${editUser.username}` : '新建用户'} onClose={closeForm} size="lg" footer={
        <>
          <button type="button" className="btn-ghost" onClick={closeForm}>取消</button>
          <button type="submit" form="user-form" className="btn-primary" disabled={busy}>{busy ? '保存中…' : (editing ? '保存' : '创建')}</button>
        </>
      }>
        <form id="user-form" onSubmit={save} className="grid grid-cols-1 sm:grid-cols-2 gap-3">
          <Field label="用户名">
            <input className="input-field" placeholder="alice" value={f.username} onChange={e => setF({ ...f, username: e.target.value })} required autoFocus />
          </Field>
          <Field label="登录密码" hint={editing ? '留空不改；节点链接里的 UUID / 密钥不变' : '可留空随机'}>
            <div className="flex gap-2">
              <input className="input-field" placeholder={editing ? '不修改' : '随机'} value={f.password} onChange={e => setF({ ...f, password: e.target.value })} autoComplete="new-password" />
              <button type="button" className="btn-ghost shrink-0" onClick={() => setF({ ...f, password: randPassword() })}>随机</button>
            </div>
          </Field>
          <Field label="备注"><input className="input-field" placeholder="可选" value={f.remark} onChange={e => setF({ ...f, remark: e.target.value })} /></Field>
          <Field label="套餐" hint={editing && Number(f.package_id || 0) !== Number(editUser.package_id || 0) && f.package_id ? '更换套餐会清零已用流量' : undefined}>
            <select className="input-field" value={f.package_id} onChange={e => setF({ ...f, package_id: e.target.value })}>
              <option value="">不绑定</option>
              {pkgs.map(p => {
                const n = (p.inbound_ids || []).length
                const tag = n ? `${n} 个节点` : ((p.server_ids || []).length ? `${p.server_ids.length} 台实例` : '未选节点')
                return <option key={p.id} value={p.id}>{p.name} / {tag}</option>
              })}
            </select>
          </Field>
          {editing ? (
            <div>
              <Field label="到期" hint="留空表示不限期">
                <input className="input-field" type="date" value={f.expires} onChange={e => setF({ ...f, expires: e.target.value })} />
              </Field>
              <div className="flex gap-2 mt-1.5">
                <button type="button" className="row-act" onClick={() => bumpExpiry(30)}>+30 天</button>
                <button type="button" className="row-act" onClick={() => bumpExpiry(60)}>+60 天</button>
                <button type="button" className="row-act" onClick={() => bumpExpiry(90)}>+90 天</button>
              </div>
            </div>
          ) : (
            <Field label="天数" hint="从今天起算">
              <input className="input-field" type="number" min="0" value={f.days} onChange={e => setF({ ...f, days: e.target.value })} />
            </Field>
          )}
          {editing && (
            <Field label="流量上限 GB" hint={f.traffic_gb === '' ? (selectedPkg?.traffic_bytes ? `留空跟随套餐 ${fmtBytes(selectedPkg.traffic_bytes)}` : '留空跟随套餐，0 为不限') : (Number(f.traffic_gb) === 0 ? '0 = 不限流量' : '覆盖套餐额度')}>
              <input className="input-field" type="number" min="0" step="0.1" placeholder="跟随套餐" value={f.traffic_gb} onChange={e => setF({ ...f, traffic_gb: e.target.value })} />
            </Field>
          )}
          {editing && (
            <SpeedField
              value={f.speed_value}
              unit={f.speed_unit}
              onChange={next => setF({ ...f, speed_value: next.value, speed_unit: next.unit })}
              hint={f.speed_value === '' || Number(f.speed_value) === 0 ? '0 或不填 = 不限。直连 Xray / AnyTLS 上下行同一上限；中转、Mieru、SK5 不节流' : '直连 Xray / AnyTLS 上下行同一上限；中转、Mieru、SK5 不节流'}
            />
          )}
          {editing && (
            <Field label="流量重置日" hint="0 跟随套餐 / 每月周期。1–31 表示每月这一天清零。">
              <input className="input-field" type="number" min="0" max="31" value={f.traffic_reset_day} onChange={e => setF({ ...f, traffic_reset_day: e.target.value })} />
            </Field>
          )}
          {editing && (
            <Field label="状态">
              <select className="input-field" value={f.enabled ? '1' : '0'} onChange={e => setF({ ...f, enabled: e.target.value === '1' })}>
                <option value="1">启用</option>
                <option value="0">停用</option>
              </select>
            </Field>
          )}
          {editing && (
            <div className="sm:col-span-2 px-3 py-2.5" style={{ background: 'var(--color-fill)' }}>
              <div className="flex items-center justify-between gap-3">
                <div className="min-w-0 flex-1">
                  <div className="text-[12px] text-ink-mut mb-1">已用流量{editUser.direction === 'twoway' || selectedPkg?.direction === 'twoway' ? '（双向）' : ''}</div>
                  <Meter value={billedBytes(editUser)} max={trafficCap({ ...editUser, traffic_limit: bytesFromGB(f.traffic_gb) ?? editUser.traffic_limit, package_id: f.package_id || editUser.package_id }, pkgs)} />
                </div>
                <button type="button" className="btn-danger shrink-0 h-8" onClick={resetTraffic}>清零</button>
              </div>
            </div>
          )}
        </form>
      </Modal>
      {ruleUser ? (
        <UserRulesModal
          user={ruleUser}
          onClose={() => setRuleUser(null)}
          onSaved={load}
        />
      ) : null}
      {denyUser ? (
        <UserDenyModal
          user={denyUser}
          onClose={() => setDenyUser(null)}
          onSaved={load}
        />
      ) : null}
      <Modal open={!!subUser} title={subUser ? `${subUser.username} 的订阅` : '订阅'} onClose={() => setSubUser(null)} wide footer={
        <>
          <button type="button" className="btn-ghost" onClick={() => subUser && rotate(subUser)}>重置令牌</button>
          <button type="button" className="btn-ghost" onClick={() => setSubUser(null)}>关闭</button>
        </>
      }>
        {subUser && <SubPanel token={subUser.sub_token} onCopied={(msg, kind) => toast(msg, kind)} />}
      </Modal>
      <Modal open={!!previewUser} title={previewUser ? `${previewUser.username} 会看到的节点` : '预览节点'} onClose={() => setPreviewUser(null)} size="lg" footer={
        <button type="button" className="btn-ghost" onClick={() => setPreviewUser(null)}>关闭</button>
      }>
        <p className="text-[12px] text-ink-mut mb-3">只读预览，不是管理员自己的订阅。不含该用户的分享链接。</p>
        <NodePreviewList data={previewData} error={previewErr} loading={previewLoading} />
      </Modal>
      <Modal open={!!trafficUser} title={trafficUser ? `${trafficUser.username} 的流量` : '流量'} onClose={() => { trafficSeq.current += 1; setTrafficUser(null); setTrafficDetail(null) }} size="lg" footer={
        <button type="button" className="btn-ghost" onClick={() => { trafficSeq.current += 1; setTrafficUser(null); setTrafficDetail(null) }}>关闭</button>
      }>
        {trafficUser && (
          <div>
            <Meter className="mb-3" value={billedBytes(trafficUser)} max={trafficUser.traffic_cap || trafficCap(trafficUser, pkgs)} />
            {trafficUser.direction === 'twoway' ? (
              <div className="text-[12px] text-ink-mut mb-3">双向：上行和下行都计入额度。节点倍率在入账时已乘过。</div>
            ) : trafficUser.direction === 'oneway' ? (
              <div className="text-[12px] text-ink-mut mb-3">单向：只计下行。</div>
            ) : null}
            {trafficDetail ? (
              <>
                <div className="text-[13px] font-medium mb-2">近 14 日（原始）</div>
                <DayBars days={asArray(trafficDetail.days)} />
                {asArray(trafficDetail.inbounds).length > 0 && (
                  <div className="table-wrap mt-4">
                    <table className="data">
                      <thead><tr><th>节点</th><th>上行</th><th>下行</th></tr></thead>
                      <tbody>
                        {asArray(trafficDetail.inbounds).map(inb => (
                          <tr key={inb.id}>
                            <td>{inb.name}</td>
                            <td className="tabular-nums">{fmtBytes(inb.up)}</td>
                            <td className="tabular-nums">{fmtBytes(inb.down)}</td>
                          </tr>
                        ))}
                      </tbody>
                    </table>
                  </div>
                )}
              </>
            ) : <div className="text-[13px] text-ink-mut">加载中</div>}
          </div>
        )}
      </Modal>
      <Modal open={bulkOpen} title="批量开户" onClose={() => setBulkOpen(false)} size="lg" footer={
        <>
          <button type="button" className="btn-ghost" onClick={() => setBulkOpen(false)}>取消</button>
          <button type="button" className="btn-primary" disabled={bulkBusy} onClick={async () => {
            const lines = bulkText.split('\n').map(x => x.trim()).filter(Boolean)
            if (!lines.length) { toast('请填写用户名，一行一个', 'error'); return }
            setBulkBusy(true)
            try {
              const users = lines.map(line => {
                const [username, password] = line.split(/[\s,]+/).filter(Boolean)
                return { username, password: password || '', package_id: bulkPkg ? Number(bulkPkg) : undefined, days: Number(bulkDays) || 0 }
              })
              const d = await api.post('/users/bulk', { users })
              const pw = (d.users || []).filter(x => x.ok && x.password).map(x => `${x.username} ${x.password}`).join('\n')
              toast(`成功 ${d.ok}/${d.total}`)
              if (pw) {
                try { await copyText(pw); toast('随机密码已复制') } catch {}
              }
              setBulkOpen(false)
              setBulkText('')
              load()
            } catch (e) { toast(e.message, 'error') }
            finally { setBulkBusy(false) }
          }}>{bulkBusy ? '创建中…' : '创建'}</button>
        </>
      }>
        <div className="space-y-3">
          <Field label="用户名" hint="一行一个。可写成「用户名 密码」，密码留空则随机。">
            <textarea className="input-field font-mono text-[12px] min-h-40" value={bulkText} onChange={e => setBulkText(e.target.value)} placeholder={'alice\nbob secret12'} />
          </Field>
          <div className="grid grid-cols-1 sm:grid-cols-2 gap-3">
            <Field label="套餐">
              <select className="input-field" value={bulkPkg} onChange={e => setBulkPkg(e.target.value)}>
                <option value="">不绑定</option>
                {pkgs.map(p => <option key={p.id} value={p.id}>{p.name}</option>)}
              </select>
            </Field>
            <Field label="天数" hint="0 表示不限期">
              <input className="input-field" type="number" min="0" value={bulkDays} onChange={e => setBulkDays(e.target.value)} />
            </Field>
          </div>
        </div>
      </Modal>
    </div>
  )
}
