import { Empty, SkeletonRows } from './ui'
import { asArray } from '../lib/safe'

export function NodePreviewList({ data, error, loading }) {
  if (loading) {
    return <div className="card overflow-hidden"><SkeletonRows /></div>
  }
  if (error) {
    return <div className="alert-row is-warn">{error}</div>
  }
  const nodes = asArray(data?.nodes)
  const hidden = asArray(data?.hidden)
  if (!nodes.length && !hidden.length) {
    return <Empty title="没有节点" hint="套餐还没勾选节点，或节点都不在订阅里。" />
  }
  return (
    <div>
      {nodes.length ? (
        <div className="table-wrap">
          <table className="data">
            <thead>
              <tr>
                <th>名称</th>
                <th>实例</th>
                <th>地址</th>
                <th>协议</th>
              </tr>
            </thead>
            <tbody>
              {nodes.map(n => (
                <tr key={n.id}>
                  <td className="font-medium">
                    {n.name}
                    {n.line_kind === 'chain' ? <span className="line-tag">中转</span> : null}
                  </td>
                  <td>{n.server_name || '—'}</td>
                  <td className="copy-text">{n.host ? `${n.host}:${n.port}` : '—'}</td>
                  <td className="text-[12px] text-ink-mut">{n.profile}</td>
                </tr>
              ))}
            </tbody>
          </table>
        </div>
      ) : (
        <Empty title="没有可用节点" hint="下面是未进订阅的原因。" />
      )}
      {hidden.length ? (
        <div className="text-[12px] text-ink-mut mt-3 leading-relaxed">
          未进订阅 {hidden.length} 个：{hidden.map(h => `${h.name}（${h.reason}）`).join('、')}
        </div>
      ) : null}
    </div>
  )
}
