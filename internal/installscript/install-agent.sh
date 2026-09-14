#!/usr/bin/env bash
# liking 节点 agent 安装脚本（由面板 /v1/install-agent 下发）
# 用法:
#   curl -fsSL http://面板:8899/v1/install-agent | bash -s -- --token <hex>
set -euo pipefail

INSTALL_DIR="/usr/local/sbin"
SYSTEMD_DIR="/etc/systemd/system"
ETC_DIR="/etc/liking"
DATA_DIR="/var/lib/liking/agent"
LIKING_PANEL_URL_BAKED='__LIKING_PANEL_URL__'

die() { echo "错误: $*" >&2; exit 1; }
note() { printf '\033[36m%s\033[0m\n' "$*"; }
ok()   { printf '\033[32m%s\033[0m\n' "$*"; }

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
CURL_TLS=()

while [[ $# -gt 0 ]]; do
  case "$1" in
    --panel-url) require_val --panel-url "${2:-}"; PANEL_URL="$2"; shift 2 ;;
    --panel-url=*) PANEL_URL="${1#*=}"; shift ;;
    --token) require_val --token "${2:-}"; TOKEN="$2"; shift 2 ;;
    --token=*) TOKEN="${1#*=}"; shift ;;
    --insecure) ALLOW_INSECURE=1; shift ;;
    --gh-proxy) require_val --gh-proxy "${2:-}"; GH_PROXY="$2"; shift 2 ;;
    --gh-proxy=*) GH_PROXY="${1#*=}"; shift ;;
    -k|--insecure-tls) CURL_TLS=(-k); shift ;;
    -h|--help)
      cat <<'H'
liking 节点安装
  --panel-url URL   面板地址，缺省用脚本内置
  --token TOKEN     节点 token（必填）
  --insecure        允许明文 http / ws 控制信道
  --gh-proxy URL    GitHub 镜像前缀，国内机器装内核用，如 https://gh-proxy.com/
  -k                curl 跳过 TLS 校验
H
      exit 0 ;;
    *) die "未知参数: $1" ;;
  esac
done

if [[ -z "$PANEL_URL" ]]; then
  PANEL_URL="${LIKING_PANEL_URL_BAKED:-}"
fi
case "$PANEL_URL" in
  http://*|https://*|ws://*|wss://*) ;;
  *.*) PANEL_URL="http://$PANEL_URL" ;;
  *) die "缺少面板地址：加 --panel-url http://面板IP:8899" ;;
esac
[[ -n "$TOKEN" ]] || die "缺少 --token"

ARCH="$(uname -m)"
case "$ARCH" in
  x86_64|amd64) GOARCH=amd64 ;;
  aarch64|arm64) GOARCH=arm64 ;;
  *) die "不支持的架构: $ARCH" ;;
esac

mkdir -p "$INSTALL_DIR" "$ETC_DIR" "$DATA_DIR" "$SYSTEMD_DIR"
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
    http://*|https://*) ;;
    *) die "--gh-proxy 需要 http(s) 地址，例如 https://gh-proxy.com/" ;;
  esac
  if [[ "$GH_PROXY" != */ ]]; then
    GH_PROXY="$GH_PROXY/"
  fi
  printf '%s\n' "$GH_PROXY" > "$ETC_DIR/gh-proxy"
  chmod 644 "$ETC_DIR/gh-proxy"
  PROXY_ENV="Environment=\"LIKING_GITHUB_PROXY=$GH_PROXY\""
fi

note "下载 liking-agent ($GOARCH) …"
BIN_URL="${PANEL_URL%/}/v1/agent-bin?os=linux&arch=${GOARCH}"
tmpbin="$(mktemp)"
hdr="$(mktemp)"
trap 'rm -f "$tmpbin" "$hdr"' EXIT
curl -fsSL "${CURL_TLS[@]}" -D "$hdr" -o "$tmpbin" "$BIN_URL" \
  || die "下载 agent 失败（面板旁边需要 liking-agent-linux-$GOARCH） $BIN_URL"
sig="$(od -An -tx1 -N4 "$tmpbin" | tr -d ' \n')"
[[ "$sig" == "7f454c46" ]] || die "下载的不是 Linux 可执行文件（面板是否放了对应架构的 agent？）"
size="$(wc -c < "$tmpbin" | tr -d ' ')"
[[ "$size" -gt 1048576 ]] || die "agent 文件过小 ($size)"
expected="$(awk 'BEGIN{IGNORECASE=1} /^X-SHA256:/{print $2; exit}' "$hdr" | tr -d '\r')"
if [[ -n "$expected" ]]; then
  actual="$(sha256sum "$tmpbin" | awk '{print $1}')"
  [[ "$actual" == "$expected" ]] || die "agent sha256 不匹配"
fi
install -m 0755 "$tmpbin" "$INSTALL_DIR/liking-agent"

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
ExecStart=$INSTALL_DIR/liking-agent --connect $CONNECT --token-file $ETC_DIR/panel.token --dir $DATA_DIR $INSECURE_FLAG
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
