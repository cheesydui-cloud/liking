const BASE = '/api'

function httpErrorMessage(status) {
  if (status === 502 || status === 503 || status === 504) return `服务暂时不可用（${status}）`
  if (status === 500) return '服务器内部错误'
  if (status === 403) return '没有权限'
  if (status === 404) return '不存在'
  if (status === 429) return '操作过于频繁'
  return `请求失败（${status}）`
}

function readSignal(x) {
  if (!x) return undefined
  if (typeof AbortSignal !== 'undefined' && x instanceof AbortSignal) return x
  if (x.signal) return x.signal
  return undefined
}

function fail(status, message, extra) {
  const err = new Error(message)
  err.status = status
  if (extra && extra.need_totp) err.need_totp = true
  if (extra && extra.code) err.code = extra.code
  return err
}

function isAbort(e) {
  return e?.name === 'AbortError' || e?.code === 20
}

async function request(method, path, body, signal) {
  const opts = { method, headers: {}, credentials: 'same-origin', signal }
  if (body) {
    opts.headers['Content-Type'] = 'application/json'
    opts.body = JSON.stringify(body)
  }
  let res
  try {
    res = await fetch(BASE + path, opts)
  } catch (e) {
    if (isAbort(e)) throw e
    throw new Error('网络错误')
  }
  const ct = res.headers.get('content-type') || ''
  let data = null
  if (ct.includes('application/json')) {
    try { data = await res.json() } catch { data = null }
  }
  if (res.status === 401) {
    const isLogin = path === '/login'
    if (!isLogin) window.dispatchEvent(new CustomEvent('lk-unauthorized'))
    throw fail(401, (data && data.error) || (isLogin ? '用户名或密码错误' : '登录已过期'), data)
  }
  if (res.status === 204) return null
  if (!res.ok) {
    throw fail(res.status, (data && data.error) || httpErrorMessage(res.status), data)
  }
  return data
}

async function parseError(res) {
  const ct = res.headers.get('content-type') || ''
  if (ct.includes('application/json')) {
    try {
      const data = await res.json()
      return (data && data.error) || httpErrorMessage(res.status)
    } catch {
      return httpErrorMessage(res.status)
    }
  }
  return httpErrorMessage(res.status)
}

async function fetchOrThrow(path, opts) {
  let res
  try {
    res = await fetch(BASE + path, opts)
  } catch (e) {
    if (isAbort(e)) throw e
    throw new Error('网络错误')
  }
  return res
}

export const api = {
  get: (path, opt) => request('GET', path, null, readSignal(opt)),
  post: (path, body, opt) => request('POST', path, body, readSignal(opt)),
  put: (path, body, opt) => request('PUT', path, body, readSignal(opt)),
  del: (path, opt) => request('DELETE', path, null, readSignal(opt)),
  postForm: async (path, formData) => {
    const res = await fetchOrThrow(path, { method: 'POST', body: formData, credentials: 'same-origin' })
    if (res.status === 401) {
      window.dispatchEvent(new CustomEvent('lk-unauthorized'))
      throw fail(401, '登录已过期')
    }
    const ct = res.headers.get('content-type') || ''
    let data = null
    if (ct.includes('application/json')) {
      try { data = await res.json() } catch { data = null }
    }
    if (!res.ok) throw fail(res.status, (data && data.error) || httpErrorMessage(res.status), data)
    return data
  },
  download: async (path, fallbackName) => {
    const res = await fetchOrThrow(path, { credentials: 'same-origin' })
    if (res.status === 401) {
      window.dispatchEvent(new CustomEvent('lk-unauthorized'))
      throw fail(401, '登录已过期')
    }
    if (!res.ok) throw fail(res.status, await parseError(res))
    await saveBlob(res, fallbackName)
  },
  downloadPost: async (path, body, fallbackName) => {
    const res = await fetchOrThrow(path, {
      method: 'POST',
      headers: { 'Content-Type': 'application/json' },
      body: JSON.stringify(body || {}),
      credentials: 'same-origin',
    })
    if (res.status === 401) {
      window.dispatchEvent(new CustomEvent('lk-unauthorized'))
      throw fail(401, '登录已过期')
    }
    if (!res.ok) throw fail(res.status, await parseError(res))
    await saveBlob(res, fallbackName)
  },
}

async function saveBlob(res, fallbackName) {
  const blob = await res.blob()
  let name = fallbackName || 'download'
  const cd = res.headers.get('Content-Disposition') || ''
  const m = cd.match(/filename="([^"]+)"/)
  if (m) name = m[1]
  const url = URL.createObjectURL(blob)
  const a = document.createElement('a')
  a.href = url
  a.download = name
  document.body.appendChild(a)
  a.click()
  a.remove()
  URL.revokeObjectURL(url)
}
