export async function copyText(text) {
  const value = String(text || '')
  if (!value) throw new Error('空内容')
  if (navigator.clipboard && window.isSecureContext) {
    try {
      await navigator.clipboard.writeText(value)
      return
    } catch { /* fall through */ }
  }
  const ta = document.createElement('textarea')
  ta.value = value
  ta.setAttribute('readonly', '')
  ta.style.position = 'fixed'
  ta.style.top = '0'
  ta.style.left = '-9999px'
  document.body.appendChild(ta)
  ta.focus()
  ta.select()
  let ok = false
  try { ok = document.execCommand('copy') } catch { ok = false }
  document.body.removeChild(ta)
  if (!ok) throw new Error('浏览器不允许自动复制')
}
