export function noteApply(toast, data, ok) {
  if (data && data.apply_error) {
    toast(data.apply_error, 'error')
    return
  }
  if (data && data.apply === 'queued') {
    toast(ok ? `${ok}（正在下发）` : '已保存，正在下发')
    return
  }
  if (ok) toast(ok)
}
