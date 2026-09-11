const BASE = '/api'

function httpErrorMessage(status) {
  if (status === 502 || status === 503 || status === 504) return `服务暂时不可用（${status}）`
  if (status === 500) return '服务器内部错误'
  if (status === 403) return '没有权限'
  if (status === 404) return '不存在'
  if (status === 429) return '操作过于频繁'
  return `请求失败（${status}）`
}

async function request(method, path, body) {
  const opts = { method, headers: {}, credentials: 'same-origin' }
  if (body) {
    opts.headers['Content-Type'] = 'application/json'
    opts.body = JSON.stringify(body)
  }
  let res
  try {
    res = await fetch(BASE + path, opts)
  } catch {
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
    throw new Error((data && data.error) || (isLogin ? '用户名或密码错误' : '登录已过期'))
  }
  if (res.status === 204) return null
  if (!res.ok) throw new Error((data && data.error) || httpErrorMessage(res.status))
  return data
}

export const api = {
  get: (path) => request('GET', path),
  post: (path, body) => request('POST', path, body),
  put: (path, body) => request('PUT', path, body),
  del: (path) => request('DELETE', path),
}
