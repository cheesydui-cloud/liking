import { Component } from 'react'

export class ErrorBoundary extends Component {
  state = { err: null }

  static getDerivedStateFromError(err) {
    return { err }
  }

  render() {
    if (!this.state.err) return this.props.children
    const msg = this.state.err.message || '未知错误'
    return (
      <div className="alert-row is-fault">
        页面出错：{msg}
        <button type="button" className="btn-ghost ml-2" onClick={() => this.setState({ err: null })}>
          重试
        </button>
      </div>
    )
  }
}
