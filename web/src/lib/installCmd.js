export function shellQuote(s) {
  return `'${String(s ?? '').replace(/'/g, `'\\''`)}'`
}

export function normalizeGhProxy(url) {
  const p = String(url || '').trim()
  if (!/^https:\/\/[^ \t;|&`$<>\\]+$/i.test(p)) return ''
  return p.replace(/\/+$/, '') + '/'
}

const CURL = 'curl -fsSL --http1.1 --retry 5 --retry-delay 2 --connect-timeout 20'

// cnAgentInstallCommand fetches the installer and the agent through a GitHub
// mirror. The machine does not need to reach the panel to download them.
export function cnAgentInstallCommand({ panelURL, token, version, proxy, insecure }) {
  const p = normalizeGhProxy(proxy)
  const ver = String(version || '').replace(/^v/, '')
  if (!p || !panelURL || !token || !/^\d+\.\d+\.\d+$/.test(ver)) return ''
  const script = `${p}https://raw.githubusercontent.com/cheesydui-cloud/liking/v${ver}/internal/installscript/install-agent.sh`
  let cmd = `${CURL} ${shellQuote(script)} | bash -s -- --panel-url ${shellQuote(panelURL)} --token ${shellQuote(token)} --release ${shellQuote('v' + ver)} --gh-proxy ${shellQuote(p)}`
  if (insecure) cmd += ' --insecure'
  return cmd
}
