#!/usr/bin/env bash
# liking 固定一键安装入口（URL 不随版本变化）
#   bash <(curl -fsSL https://raw.githubusercontent.com/cheesydui-cloud/liking/main/install.sh)
# 需 root。默认安装 GitHub Releases 的 latest 面板二进制。
set -euo pipefail

REPO="cheesydui-cloud/liking"
RELEASE="${LIKING_RELEASE:-latest}"
INSTALL_DIR="/usr/local/sbin"
SYSTEMD_DIR="/etc/systemd/system"
ETC_DIR="/etc/liking"
DATA_DIR="/var/lib/liking"
SCRIPT_PATH="$INSTALL_DIR/liking-upgrade"
GH_PROXY_FILE="$ETC_DIR/gh-proxy"
GH_PROXY="${LIKING_GH_PROXY:-}"
PANEL_ADDR="${LIKING_ADDR:-:8899}"
BOOTSTRAP_PW="${LIKING_ADMIN_PASSWORD:-}"

die() { echo "错误: $*" >&2; exit 1; }
note() { printf '\033[36m%s\033[0m\n' "$*"; }
ok()   { printf '\033[32m%s\033[0m\n' "$*"; }
warn() { printf '\033[33m警告: %s\033[0m\n' "$*" >&2; }
require_val() {
  if [[ -z "${2:-}" || "${2:-}" == --* ]]; then
    die "$1 需要值"
  fi
}

[[ ${EUID:-$(id -u)} -eq 0 ]] || die "请以 root 运行（精简系统没有 sudo 时直接用 root）"
command -v systemctl >/dev/null 2>&1 || die "需要 systemd"

usage() {
  cat <<USAGE
liking 一键安装 / 升级 / 卸载（面板 liking-server）

  bash <(curl -fsSL https://raw.githubusercontent.com/cheesydui-cloud/liking/main/install.sh)

用法:
  $0 [server|update|update-script|uninstall|reset-password] [选项]

模式:
  server           安装控制面板（默认）
  update           拉 latest 二进制升级（可用 --release 钉版本）
  update-script    只更新本升级脚本
  uninstall        停服务、删二进制与 unit；默认保留数据库
  reset-password   重置面板 admin 密码

选项:
  --addr ADDR                 监听地址，默认 :8899
  --release VER               GitHub release tag，默认 latest
  --from-dir DIR              从本地目录安装（需 SHA256SUMS 与二进制）
  --gh-proxy PFX              GitHub 镜像前缀，如 https://gh-proxy.com/
  --bootstrap-admin-password  首次安装管理员密码（否则随机打印）
  --password PW               reset-password 的新密码
  --purge                     uninstall 时连 /var/lib/liking 与 /etc/liking 一起删
  -h, --help

安装完成后:
  liking-upgrade              升级到 latest
  liking-uninstall            卸载（加 --purge 清数据）

节点 Agent 不走 GitHub，在面板「服务器」页复制一键命令。
USAGE
}

MODE="server"
PURGE=0
RESET_PW=""
FROM_DIR=""

while [[ $# -gt 0 ]]; do
  case "$1" in
    server|update|update-script|uninstall|reset-password) MODE="$1"; shift ;;
    --addr) require_val --addr "${2:-}"; PANEL_ADDR="$2"; shift 2 ;;
    --addr=*) PANEL_ADDR="${1#*=}"; shift ;;
    --release) require_val --release "${2:-}"; RELEASE="$2"; shift 2 ;;
    --release=*) RELEASE="${1#*=}"; shift ;;
    --from-dir) require_val --from-dir "${2:-}"; FROM_DIR="$2"; shift 2 ;;
    --from-dir=*) FROM_DIR="${1#*=}"; shift ;;
    --gh-proxy) require_val --gh-proxy "${2:-}"; GH_PROXY="$2"; shift 2 ;;
    --gh-proxy=*) GH_PROXY="${1#*=}"; shift ;;
    --bootstrap-admin-password) require_val --bootstrap-admin-password "${2:-}"; BOOTSTRAP_PW="$2"; shift 2 ;;
    --bootstrap-admin-password=*) BOOTSTRAP_PW="${1#*=}"; shift ;;
    --password) require_val --password "${2:-}"; RESET_PW="$2"; shift 2 ;;
    --password=*) RESET_PW="${1#*=}"; shift ;;
    --purge) PURGE=1; shift ;;
    -h|--help) usage; exit 0 ;;
    *) die "未知参数: $1" ;;
  esac
done

if [[ -n "$GH_PROXY" && "$GH_PROXY" != */ ]]; then
  GH_PROXY="$GH_PROXY/"
fi
if [[ -z "$GH_PROXY" && -f "$GH_PROXY_FILE" ]]; then
  GH_PROXY="$(cat "$GH_PROXY_FILE" || true)"
fi

script_url() {
  echo "${GH_PROXY}https://raw.githubusercontent.com/$REPO/main/install.sh"
}

persist_gh_proxy() {
  mkdir -p "$ETC_DIR"
  printf '%s' "$GH_PROXY" >"$GH_PROXY_FILE"
}

looks_like_installer() {
  local f="$1"
  [[ -s "$f" ]] || return 1
  head -1 "$f" | grep -q '^#!' || return 1
  grep -q 'cheesydui-cloud/liking' "$f" || return 1
  grep -q 'liking-server' "$f" || return 1
}

persist_script() {
  local stmp src
  stmp="$(mktemp -d)"
  if curl -fsSL "$(script_url)" -o "$stmp/upgrade.sh" 2>/dev/null && looks_like_installer "$stmp/upgrade.sh"; then
    install -m 0755 "$stmp/upgrade.sh" "$SCRIPT_PATH"
    ln -sfn "$SCRIPT_PATH" "$INSTALL_DIR/liking-uninstall"
    note "升级脚本: $SCRIPT_PATH （liking-upgrade）"
    note "卸载命令: liking-uninstall [--purge]"
    rm -rf "$stmp"
    return 0
  fi
  rm -rf "$stmp"
  src="${BASH_SOURCE[0]:-}"
  if [[ -n "$src" && -f "$src" ]] && looks_like_installer "$src"; then
    install -m 0755 "$src" "$SCRIPT_PATH"
    ln -sfn "$SCRIPT_PATH" "$INSTALL_DIR/liking-uninstall"
    note "升级脚本: $SCRIPT_PATH （来自本次安装脚本）"
    return 0
  fi
  warn "未能保存升级脚本（GitHub raw 不可达时，稍后可再跑 liking-upgrade update-script）"
}

unit_quote() {
  # systemd 单元里的 ExecStart 参数
  local s=$1
  s=${s//\\/\\\\}
  s=${s//\"/\\\"}
  s=${s//%/%%}
  printf '"%s"' "$s"
}

is_elf() {
  local sig
  [[ -s "$1" ]] || return 1
  sig="$(od -An -tx1 -N4 "$1" | tr -d ' \n')"
  [[ "$sig" == "7f454c46" ]]
}

detect_arch() {
  local m
  m="$(uname -m)"
  case "$m" in
    x86_64|amd64) HOST_GOARCH=amd64 ;;
    aarch64|arm64) HOST_GOARCH=arm64 ;;
    *) die "不支持的架构: $m（需要 linux/amd64 或 linux/arm64）" ;;
  esac
}

resolve_release_tag() {
  if [[ "$RELEASE" != "latest" ]]; then
    echo "$RELEASE"
    return 0
  fi
  local resolved src
  local apis=("https://api.github.com/repos/$REPO/releases/latest")
  local pages=("https://github.com/$REPO/releases/latest")
  if [[ -n "$GH_PROXY" ]]; then
    apis+=("${GH_PROXY}https://api.github.com/repos/$REPO/releases/latest")
    pages+=("${GH_PROXY}https://github.com/$REPO/releases/latest")
  fi
  for src in "${apis[@]}"; do
    resolved="$(curl -fsSL --max-time 12 "$src" 2>/dev/null \
                | sed -n 's/.*"tag_name":[[:space:]]*"\([^"]*\)".*/\1/p' | head -1)"
    if [[ "$resolved" == v* ]]; then
      echo "$resolved"
      return 0
    fi
  done
  for src in "${pages[@]}"; do
    resolved="$(curl -fsSLI -o /dev/null -w '%{url_effective}' "$src" 2>/dev/null \
                | sed -n 's#.*/releases/tag/##p' | tr -d '\r\n')"
    if [[ "$resolved" == v* ]]; then
      echo "$resolved"
      return 0
    fi
  done
  echo "latest"
}

release_download_base() {
  local tag="${1:-}"
  if [[ -z "$tag" ]]; then
    tag="$(resolve_release_tag)"
  fi
  if [[ "$tag" == "latest" ]]; then
    echo "${GH_PROXY}https://github.com/$REPO/releases/latest/download"
  else
    echo "${GH_PROXY}https://github.com/$REPO/releases/download/$tag"
  fi
}

asset_candidates() {
  local kind="$1"
  echo "${kind}-linux-${HOST_GOARCH}"
  if [[ "$HOST_GOARCH" == "amd64" ]]; then
    echo "$kind"
  fi
}

verify_asset() {
  local base="$1" asset="$2" dest="$3"
  local ddir expected actual
  ddir="$(dirname "$dest")"
  [[ -f "$dest" ]] || die "下载结果不存在: $dest"
  if curl -fLs --retry 5 --retry-delay 2 --connect-timeout 20 \
      "$base/SHA256SUMS" -o "$ddir/SHA256SUMS" 2>/dev/null; then
    expected="$(awk -v a="$asset" '$2 == a { print $1; exit }' "$ddir/SHA256SUMS")"
    [[ -n "$expected" ]] || die "SHA256SUMS 中没有 $asset"
    actual="$(sha256sum "$dest" | awk '{print $1}')"
    [[ "$actual" == "$expected" ]] || die "sha256 校验失败: $asset"
    echo "    sha256: OK ($asset)"
  else
    die "未取到 SHA256SUMS：拒绝裸跑"
  fi
  sha256sum "$dest" | awk '{print $1}'
}

match_sha() {
  local dest="$1" asset="$2" sums="$3" expected actual
  [[ -f "$dest" && -f "$sums" ]] || return 1
  expected="$(awk -v a="$asset" '$2 == a { print $1; exit }' "$sums")"
  [[ -n "$expected" ]] || return 1
  actual="$(sha256sum "$dest" | awk '{print $1}')"
  [[ "$actual" == "$expected" ]]
}

fetch_and_verify() {
  local base="$1" asset="$2" dest="$3"
  curl -fL --retry 5 --retry-delay 2 --connect-timeout 20 --progress-bar \
    "$base/$asset" -o "$dest" \
    || die "下载失败: $base/$asset"
  verify_asset "$base" "$asset" "$dest"
}

download_named() {
  local base="$1" kind="$2" dest="$3"
  local name size
  for name in $(asset_candidates "$kind"); do
    note "    尝试 $name ..."
    rm -f "$dest"
    if curl -fL --retry 4 --retry-delay 2 --connect-timeout 20 --progress-bar \
         "$base/$name" -o "$dest" 2>/dev/null; then
      size="$(wc -c < "$dest" | tr -d ' ')"
      if [[ -n "$size" && "$size" -gt 1048576 ]]; then
        verify_asset "$base" "$name" "$dest" >/dev/null
        return 0
      fi
    fi
  done
  die "下载 $kind 失败（$base）"
}

write_server_unit() {
  local addr="${1:-:8899}"
  mkdir -p "$SYSTEMD_DIR"
  cat >"$SYSTEMD_DIR/liking-server.service" <<EOF
[Unit]
Description=liking web panel
After=network-online.target
Wants=network-online.target

[Service]
Type=simple
ExecStart=$INSTALL_DIR/liking-server --addr $addr --db $DATA_DIR/panel.db
WorkingDirectory=$DATA_DIR
Restart=always
RestartSec=3
LimitNOFILE=1048576

[Install]
WantedBy=multi-user.target
EOF
}

ensure_curl() {
  command -v curl >/dev/null 2>&1 && return 0
  if command -v apt-get >/dev/null 2>&1; then
    DEBIAN_FRONTEND=noninteractive apt-get update -qq
    DEBIAN_FRONTEND=noninteractive apt-get install -y --no-install-recommends curl ca-certificates
    return 0
  fi
  die "需要 curl"
}

copy_from_dir() {
  local src="$1" dest="$2" asset="$3"
  [[ -f "$src/$asset" ]] || return 1
  cp -f "$src/$asset" "$dest"
  is_elf "$dest" || return 1
  [[ $(wc -c < "$dest" | tr -d ' ') -gt 1048576 ]] || return 1
  match_sha "$dest" "$asset" "$src/SHA256SUMS"
}

install_or_update() {
  detect_arch
  ensure_curl
  persist_gh_proxy
  mkdir -p "$INSTALL_DIR" "$ETC_DIR" "$DATA_DIR" "$SYSTEMD_DIR"
  local tag base tmp
  tmp="$(mktemp -d)"
  trap 'rm -rf "$tmp"' RETURN

  if [[ -n "$FROM_DIR" ]]; then
    [[ -d "$FROM_DIR" ]] || die "--from-dir 不是目录: $FROM_DIR"
    [[ -f "$FROM_DIR/SHA256SUMS" ]] || die "--from-dir 缺少 SHA256SUMS"
    note "从本地目录安装 $FROM_DIR  架构 linux/$HOST_GOARCH"
    local server_asset=""
    for server_asset in "liking-server-linux-${HOST_GOARCH}" liking-server; do
      if copy_from_dir "$FROM_DIR" "$tmp/liking-server" "$server_asset"; then
        note "    sha256: OK ($server_asset)"
        break
      fi
      server_asset=""
    done
    [[ -n "$server_asset" ]] || die "本地目录没有可用的 liking-server"
    for agent_asset in liking-agent-linux-amd64 liking-agent-linux-arm64 liking-agent; do
      if copy_from_dir "$FROM_DIR" "$tmp/$agent_asset" "$agent_asset"; then
        note "    sha256: OK ($agent_asset)"
      else
        rm -f "$tmp/$agent_asset"
      fi
    done
  else
    tag="$(resolve_release_tag)"
    base="$(release_download_base "$tag")"
    note "版本 $tag  架构 linux/$HOST_GOARCH"
    note "下载 $base"
    download_named "$base" "liking-server" "$tmp/liking-server"
    is_elf "$tmp/liking-server" || die "下载的 liking-server 不是 ELF"
    # 面板要给节点发 agent，两种架构都放下（失败不致命，有校验才保留）
    for agent_asset in liking-agent-linux-amd64 liking-agent-linux-arm64 liking-agent; do
      if curl -fL --retry 3 --retry-delay 2 --connect-timeout 20 -o "$tmp/$agent_asset" \
           "$base/$agent_asset" 2>/dev/null \
         && is_elf "$tmp/$agent_asset" \
         && [[ $(wc -c < "$tmp/$agent_asset" | tr -d ' ') -gt 1048576 ]] \
         && match_sha "$tmp/$agent_asset" "$agent_asset" "$tmp/SHA256SUMS"; then
        note "    sha256: OK ($agent_asset)"
      else
        rm -f "$tmp/$agent_asset"
      fi
    done
  fi

  if [[ -f "$INSTALL_DIR/liking-server" ]]; then
    cp -a "$INSTALL_DIR/liking-server" "$tmp/liking-server.bak" || true
  fi
  install -m 0755 "$tmp/liking-server" "$INSTALL_DIR/liking-server"
  for f in liking-agent-linux-amd64 liking-agent-linux-arm64 liking-agent; do
    if [[ -f "$tmp/$f" && $(wc -c < "$tmp/$f" | tr -d ' ') -gt 1048576 ]]; then
      install -m 0755 "$tmp/$f" "$INSTALL_DIR/$f"
    fi
  done

  local extra=()
  if [[ ! -f "$DATA_DIR/panel.db" && -n "$BOOTSTRAP_PW" ]]; then
    extra+=(--bootstrap-admin-password "$BOOTSTRAP_PW")
  fi
  write_server_unit "$PANEL_ADDR"
  # 把 bootstrap 写进一次性 drop-in，避免密码进 unit 文件长期保存
  if [[ ${#extra[@]} -gt 0 ]]; then
    mkdir -p "$SYSTEMD_DIR/liking-server.service.d"
    cat >"$SYSTEMD_DIR/liking-server.service.d/bootstrap.conf" <<EOF
[Service]
ExecStart=
ExecStart=$INSTALL_DIR/liking-server --addr $(unit_quote "$PANEL_ADDR") --db $DATA_DIR/panel.db --bootstrap-admin-password $(unit_quote "$BOOTSTRAP_PW")
EOF
  fi
  systemctl daemon-reload
  systemctl enable --now liking-server.service
  if [[ -f "$SYSTEMD_DIR/liking-server.service.d/bootstrap.conf" ]]; then
    rm -f "$SYSTEMD_DIR/liking-server.service.d/bootstrap.conf"
    rmdir "$SYSTEMD_DIR/liking-server.service.d" 2>/dev/null || true
    write_server_unit "$PANEL_ADDR"
    systemctl daemon-reload
    systemctl restart liking-server.service
  fi
  persist_script
  printf '%s\n' "$PANEL_ADDR" >"$ETC_DIR/addr"
  if command -v "$INSTALL_DIR/liking-server" >/dev/null 2>&1; then
    note "版本 $($INSTALL_DIR/liking-server --version 2>/dev/null || echo unknown)"
  fi
  sleep 1
  if systemctl is-active --quiet liking-server.service; then
    ok "liking-server 已启动（$PANEL_ADDR）"
  else
    if [[ -f "$tmp/liking-server.bak" ]]; then
      install -m 0755 "$tmp/liking-server.bak" "$INSTALL_DIR/liking-server"
      systemctl restart liking-server.service || true
    fi
    die "服务未能启动，请看: journalctl -u liking-server -n 80 --no-pager"
  fi
  echo
  note "浏览器打开: http://服务器IP:8899"
  note "升级: liking-upgrade"
  note "卸载: liking-uninstall"
  systemctl --no-pager --full status liking-server.service | head -20 || true
}

do_uninstall() {
  systemctl disable --now liking-server.service 2>/dev/null || true
  rm -f "$SYSTEMD_DIR/liking-server.service"
  rm -rf "$SYSTEMD_DIR/liking-server.service.d"
  rm -f "$INSTALL_DIR/liking-server" \
        "$INSTALL_DIR/liking-agent" \
        "$INSTALL_DIR/liking-agent-linux-amd64" \
        "$INSTALL_DIR/liking-agent-linux-arm64"
  systemctl daemon-reload
  if [[ "$PURGE" -eq 1 ]]; then
    rm -rf "$DATA_DIR" "$ETC_DIR"
    rm -f "$SCRIPT_PATH" "$INSTALL_DIR/liking-uninstall"
    ok "已卸载并清除数据"
  else
    ok "已卸载面板（数据库保留在 $DATA_DIR ）"
    note "彻底清除请再跑: $0 uninstall --purge"
  fi
}

do_reset_password() {
  [[ -x "$INSTALL_DIR/liking-server" ]] || die "未安装 liking-server"
  local pw="$RESET_PW"
  if [[ -z "$pw" ]]; then
    if [[ -t 0 ]]; then
      read -r -s -p "新密码: " pw
      echo
    else
      pw="$(head -c 8 /dev/urandom | od -An -tx1 | tr -d ' \n')"
      note "未提供密码，已随机生成"
    fi
  fi
  [[ -n "$pw" ]] || die "密码为空"
  systemctl stop liking-server.service 2>/dev/null || true
  "$INSTALL_DIR/liking-server" --db "$DATA_DIR/panel.db" --reset-admin-password "$pw"
  systemctl start liking-server.service
  ok "已重置 admin 密码"
}

do_update_script() {
  ensure_curl
  local stmp surl
  surl="$(script_url)"
  stmp="$(mktemp -d)"
  curl -fsSL "$surl" -o "$stmp/upgrade.sh" || { rm -rf "$stmp"; die "下载失败: $surl"; }
  looks_like_installer "$stmp/upgrade.sh" || { rm -rf "$stmp"; die "内容异常，拒绝覆盖"; }
  install -m 0755 "$stmp/upgrade.sh" "$SCRIPT_PATH"
  ln -sfn "$SCRIPT_PATH" "$INSTALL_DIR/liking-uninstall"
  rm -rf "$stmp"
  ok "升级脚本已更新: $SCRIPT_PATH"
}

# 若通过 liking-uninstall 调用，切到 uninstall
self="$(basename "${0:-}" 2>/dev/null || true)"
if [[ "$self" == "liking-uninstall" && "$MODE" == "server" ]]; then
  MODE="uninstall"
fi

case "$MODE" in
  server|update) install_or_update ;;
  update-script) do_update_script ;;
  uninstall) do_uninstall ;;
  reset-password) do_reset_password ;;
  *) die "未知模式 $MODE" ;;
esac
