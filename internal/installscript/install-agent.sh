#!/usr/bin/env bash
# liking 节点 agent 安装脚本（由面板 /v1/install-agent 下发）
# 用法:
#   curl -fsSL http://面板:8899/v1/install-agent | bash -s -- --token <hex>
# 连不上面板时，勾选「国内机器」后的命令会从 GitHub 镜像拉本脚本和 Agent。
set -euo pipefail
umask 077

INSTALL_DIR="/usr/local/sbin"
SYSTEMD_DIR="/etc/systemd/system"
ETC_DIR="/etc/liking"
DATA_DIR="/var/lib/liking/agent"
LIKING_PANEL_URL_BAKED=__LIKING_PANEL_URL__
LIKING_VERSION_BAKED=__LIKING_VERSION__
REPO="cheesydui-cloud/liking"
CURL_TLS=()

die() { echo "错误: $*" >&2; exit 1; }
note() { printf '\033[36m%s\033[0m\n' "$*"; }
ok()   { printf '\033[32m%s\033[0m\n' "$*"; }
warn() { printf '\033[33m警告: %s\033[0m\n' "$*" >&2; }

unit_quote() {
  local s=$1
  s=${s//\\/\\\\}
  s=${s//\"/\\\"}
  s=${s//$'\n'/\\n}
  s=${s//$'\r'/\\r}
  s=${s//$'\t'/\\t}
  s=${s//%/%%}
  printf '"%s"' "$s"
}

# header_field reads the last matching header. Go sends X-Sha256, HTTP/2
# sends x-sha256, and mawk ignores awk IGNORECASE, so match in bash.
header_field() {
  local name="$1" file="$2" line key val found=""
  name="${name,,}"
  while IFS= read -r line || [[ -n "$line" ]]; do
    line="${line%$'\r'}"
    key="${line%%:*}"
    if [[ "${key,,}" == "$name" ]]; then
      val="${line#*:}"
      val="${val#"${val%%[![:space:]]*}"}"
      val="${val%"${val##*[![:space:]]}"}"
      found="$val"
    fi
  done < "$file"
  printf '%s' "$found"
}

if [[ "${LIKING_HEADER_TEST:-}" == 1 ]]; then
  header_field "${1:-}" "${2:-}"
  exit 0
fi

fetch_pause() {
  local n="$1"
  if [[ "${LIKING_FETCH_DELAY:-1}" == 0 ]]; then
    return 0
  fi
  sleep "$n"
}

curl_once() {
  local dest="$1" url="$2" hdr="${3:-}"
  rm -f "$dest"
  if [[ -n "$hdr" ]]; then
    rm -f "$hdr"
    curl -fL --http1.1 --connect-timeout 20 --max-time 180 --max-filesize 83886080 \
      ${CURL_TLS[@]+"${CURL_TLS[@]}"} -D "$hdr" -o "$dest" "$url"
  else
    curl -fL --http1.1 --connect-timeout 20 --max-time 180 --max-filesize 83886080 \
      ${CURL_TLS[@]+"${CURL_TLS[@]}"} -o "$dest" "$url"
  fi
}

fetch_url() {
  local dest="$1" url="$2" hdr="${3:-}" attempt
  for attempt in 1 2 3 4; do
    if curl_once "$dest" "$url" "$hdr"; then
      return 0
    fi
    rm -f "$dest"
    if [[ "$attempt" -lt 4 ]]; then
      fetch_pause $((attempt * 2))
    fi
  done
  return 1
}

is_agent_file() {
  local f="$1" sig size
  [[ -f "$f" ]] || return 1
  sig="$(od -An -tx1 -N4 "$f" | tr -d ' \r\n')"
  [[ "$sig" == "7f454c46" ]] || return 1
  size="$(wc -c < "$f" | tr -d ' ')"
  [[ "$size" -gt 1048576 && "$size" -le 83886080 ]]
}

verify_panel_agent() {
  local f="$1" hdr="$2" expected actual
  is_agent_file "$f" || return 1
  expected="$(header_field x-sha256 "$hdr")"
  [[ "$expected" =~ ^[0-9A-Fa-f]{64}$ ]] || return 1
  actual="$(sha256sum "$f" | awk '{print $1}' | tr -d '\r')"
  [[ "${actual,,}" == "${expected,,}" ]]
}

verify_sums_agent() {
  local f="$1" sums="$2" asset="$3" expected actual
  is_agent_file "$f" || return 1
  expected="$(awk -v a="$asset" '$2 == a { print $1; exit }' "$sums" | tr -d '\r')"
  [[ "$expected" =~ ^[0-9A-Fa-f]{64}$ ]] || return 1
  actual="$(sha256sum "$f" | awk '{print $1}' | tr -d '\r')"
  [[ "${actual,,}" == "${expected,,}" ]]
}

resolve_agent_version() {
  local v="${RELEASE:-}"
  if [[ -z "$v" ]]; then
    v="${LIKING_VERSION_BAKED:-}"
  fi
  v="${v#v}"
  if [[ -z "$v" || "$v" == "__LIKING_VERSION__" || "$v" == *" "* || "$v" == *"/"* ]]; then
    return 1
  fi
  printf '%s' "$v"
}

download_agent_to() {
  local dest="$1"
  local tmpbin hdr sums panel_ok ver base asset
  tmpbin="$(mktemp)"
  hdr="$(mktemp)"
  sums="$(mktemp)"
  cleanup_dl() { rm -f "$tmpbin" "$hdr" "$sums"; }
  panel_ok=0
  BIN_URL="${PANEL_URL%/}/v1/agent-bin?os=linux&arch=${GOARCH}"
  if fetch_url "$tmpbin" "$BIN_URL" "$hdr" && verify_panel_agent "$tmpbin" "$hdr"; then
    panel_ok=1
  else
    warn "从面板下载 agent 失败：$BIN_URL"
    rm -f "$tmpbin" "$hdr"
  fi
  if [[ "$panel_ok" -ne 1 ]]; then
    ver="$(resolve_agent_version)" || {
      cleanup_dl
      die "从面板下载失败，且没有版本号，不能改走 GitHub。请加 --release vX.Y.Z，或在面板升级后再复制安装命令"
    }
    if [[ -z "${GH_PROXY:-}" ]]; then
      cleanup_dl
      die "这台机器连不上面板，下不到 agent。请加上 --gh-proxy https://gh-proxy.com/ 后重试"
    fi
    note "改从 GitHub 镜像下载 agent ${ver} …"
    base="${GH_PROXY}https://github.com/${REPO}/releases/download/v${ver}"
    asset="liking-agent-linux-${GOARCH}"
    if ! fetch_url "$sums" "$base/SHA256SUMS"; then
      cleanup_dl
      die "镜像上下不到 SHA256SUMS。请检查 --gh-proxy 是否可访问"
    fi
    if ! fetch_url "$tmpbin" "$base/$asset" || ! verify_sums_agent "$tmpbin" "$sums" "$asset"; then
      if [[ "$GOARCH" == "amd64" ]] && fetch_url "$tmpbin" "$base/liking-agent" && verify_sums_agent "$tmpbin" "$sums" "liking-agent"; then
        note "已用 liking-agent 代替 $asset"
      else
        cleanup_dl
        die "从镜像下载 agent 失败：$base/$asset"
      fi
    fi
  fi
  install -m 0755 "$tmpbin" "$dest"
  cleanup_dl
}

if [[ "${LIKING_AGENT_FETCH_TEST:-}" == 1 ]]; then
  PANEL_URL="${LIKING_PANEL_URL:-http://panel.test}"
  GH_PROXY="${LIKING_GITHUB_PROXY:-}"
  if [[ -n "$GH_PROXY" && "$GH_PROXY" != */ ]]; then
    GH_PROXY="${GH_PROXY}/"
  fi
  GOARCH="${LIKING_TEST_ARCH:-amd64}"
  RELEASE="${LIKING_TEST_RELEASE:-}"
  if [[ -n "${LIKING_TEST_VERSION:-}" ]]; then
    LIKING_VERSION_BAKED="$LIKING_TEST_VERSION"
  fi
  download_agent_to "${LIKING_TEST_DEST:?}"
  exit 0
fi

require_val() {
  if [[ -z "${2:-}" || "${2:-}" == --* ]]; then
    die "$1 需要值"
  fi
}

[[ ${EUID:-$(id -u)} -eq 0 ]] || die "请以 root 运行"

PANEL_URL="${LIKING_PANEL_URL:-}"
TOKEN="${AGENT_TOKEN:-}"
ALLOW_INSECURE="${LIKING_ALLOW_INSECURE:-}"
GH_PROXY="${LIKING_GITHUB_PROXY:-}"
RELEASE=""

while [[ $# -gt 0 ]]; do
  case "$1" in
    --panel-url) require_val --panel-url "${2:-}"; PANEL_URL="$2"; shift 2 ;;
    --panel-url=*) PANEL_URL="${1#*=}"; shift ;;
    --token) require_val --token "${2:-}"; TOKEN="$2"; shift 2 ;;
    --token=*) TOKEN="${1#*=}"; shift ;;
    --insecure) ALLOW_INSECURE=1; shift ;;
    --gh-proxy) require_val --gh-proxy "${2:-}"; GH_PROXY="$2"; shift 2 ;;
    --gh-proxy=*) GH_PROXY="${1#*=}"; shift ;;
    --release) require_val --release "${2:-}"; RELEASE="$2"; shift 2 ;;
    --release=*) RELEASE="${1#*=}"; shift ;;
    -k|--insecure-tls) CURL_TLS=(-k); shift ;;
    -h|--help)
      cat <<'H'
liking 节点安装
  --panel-url URL   面板地址，缺省用脚本内置
  --token TOKEN     节点 token（必填）
  --insecure        允许明文 http / ws 控制信道
  --gh-proxy URL    GitHub 镜像。连不上面板时用它下载 Agent，内核也走这个镜像
  --release VER     Agent 版本，例如 v0.3.47。面板下发的脚本不用填
  -k                curl 跳过 TLS 校验
H
      exit 0 ;;
    *) die "未知参数: $1" ;;
  esac
done

if [[ -z "$PANEL_URL" || "$PANEL_URL" == "__LIKING_PANEL_URL__" ]]; then
  PANEL_URL="${LIKING_PANEL_URL_BAKED:-}"
fi
if [[ "$PANEL_URL" == "__LIKING_PANEL_URL__" ]]; then
  PANEL_URL=""
fi
case "$PANEL_URL" in
  http://*|https://*|ws://*|wss://*) ;;
  *.*) PANEL_URL="http://$PANEL_URL" ;;
  *) die "缺少面板地址：加 --panel-url http://面板IP:8899" ;;
esac
[[ -n "$TOKEN" ]] || die "缺少 --token"
[[ "$PANEL_URL" != *[[:space:]\'\"\\]* ]] || die "面板地址含无效字符"
[[ "$TOKEN" =~ ^[0-9A-Fa-f]{16,}$ ]] || die "token 格式无效"

ARCH="$(uname -m)"
case "$ARCH" in
  x86_64|amd64) GOARCH=amd64 ;;
  aarch64|arm64) GOARCH=arm64 ;;
  *) die "不支持的架构: $ARCH" ;;
esac

install -d -m 0755 "$INSTALL_DIR" "$SYSTEMD_DIR"
install -d -m 0700 "$ETC_DIR"
install -d -m 0750 "$DATA_DIR"
chmod 0700 "$ETC_DIR"
chmod 0750 "$DATA_DIR"
# v0.1.0 内核日志写在面板目录，升到独立 agent 目录后清掉残留
if [[ "$DATA_DIR" != "/var/lib/liking" ]]; then
  rm -f /var/lib/liking/xray.log /var/lib/liking/singbox.log /var/lib/liking/mita.log
fi
printf '%s\n' "$TOKEN" > "$ETC_DIR/panel.token"
chmod 600 "$ETC_DIR/panel.token"
printf '%s\n' "$PANEL_URL" > "$ETC_DIR/panel.url"

PROXY_ENV=""
if [[ -n "$GH_PROXY" ]]; then
  case "$GH_PROXY" in
    https://*) ;;
    *) die "--gh-proxy 需要 HTTPS 地址，例如 https://gh-proxy.com/" ;;
  esac
  [[ "$GH_PROXY" != *[[:space:]\'\"\\]* ]] || die "--gh-proxy 含无效字符"
  if [[ "$GH_PROXY" != */ ]]; then
    GH_PROXY="$GH_PROXY/"
  fi
  printf '%s\n' "$GH_PROXY" > "$ETC_DIR/gh-proxy"
  chmod 644 "$ETC_DIR/gh-proxy"
  PROXY_ENV="Environment=$(unit_quote "LIKING_GITHUB_PROXY=$GH_PROXY")"
fi

note "下载 liking-agent ($GOARCH) …"
download_agent_to "$INSTALL_DIR/liking-agent"

CONNECT="$PANEL_URL"
CONNECT="${CONNECT/https:/wss:}"
CONNECT="${CONNECT/http:/ws:}"
CONNECT="${CONNECT%/}/v1/agents"

INSECURE_FLAG=""
if [[ "$CONNECT" == ws://* ]]; then
  [[ -n "$ALLOW_INSECURE" ]] || die "明文 http 控制信道需要 --insecure"
  INSECURE_FLAG="--insecure-connect"
fi

cat > "$SYSTEMD_DIR/liking-agent.service" <<UNIT
[Unit]
Description=liking agent
After=network-online.target
Wants=network-online.target

[Service]
Type=simple
Environment=PATH=/usr/local/sbin:/usr/local/bin:/usr/sbin:/usr/bin
$PROXY_ENV
ExecStart=$(unit_quote "$INSTALL_DIR/liking-agent") --connect $(unit_quote "$CONNECT") --token-file $(unit_quote "$ETC_DIR/panel.token") --dir $(unit_quote "$DATA_DIR") $INSECURE_FLAG
Restart=always
RestartSec=3
LimitNOFILE=1048576

[Install]
WantedBy=multi-user.target
UNIT

systemctl daemon-reload
systemctl enable liking-agent.service
systemctl restart liking-agent.service
ok "liking-agent 已启动"
if [[ -n "$GH_PROXY" ]]; then
  note "内核下载走镜像 $GH_PROXY"
else
  note "内核会在第一次下发对应入站时自动安装（Xray / sing-box / mita），节点需要能访问 GitHub"
fi
systemctl --no-pager --full status liking-agent.service || true
