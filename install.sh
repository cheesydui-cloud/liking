#!/usr/bin/env bash
# liking 固定一键安装入口（URL 不随版本变化）
#   bash <(curl -fsSL https://raw.githubusercontent.com/cheesydui-cloud/liking/main/install.sh)
#   bash <(curl -fsSL https://raw.githubusercontent.com/cheesydui-cloud/liking/main/install.sh) docker
# 需 root。默认安装 GitHub Releases 的 latest 面板二进制。docker 模式不需要 systemd。
set -euo pipefail
umask 077

REPO="cheesydui-cloud/liking"
RELEASE="${LIKING_RELEASE:-latest}"
INSTALL_DIR="/usr/local/sbin"
SYSTEMD_DIR="/etc/systemd/system"
ETC_DIR="/etc/liking"
DATA_DIR="/var/lib/liking"
SCRIPT_PATH="$INSTALL_DIR/liking-upgrade"
GH_PROXY_FILE="$ETC_DIR/gh-proxy"
GH_PROXY="${LIKING_GH_PROXY:-}"
PANEL_ADDR="${LIKING_ADDR:-127.0.0.1:8899}"
BOOTSTRAP_PW="${LIKING_ADMIN_PASSWORD:-}"
OPT_DIR="/opt/liking-docker"
CONTAINER_NAME="liking"
DOCKER_SOCK=1
DOCKER_ACTION="install"
DOCKER_BOOTSTRAP=0
HOST_BIND_IP=""
HOST_PORT=""
MAX_RELEASE_BYTES=134217728
MAX_METADATA_BYTES=1048576

die() { echo "错误: $*" >&2; exit 1; }
note() { printf '\033[36m%s\033[0m\n' "$*"; }
ok()   { printf '\033[32m%s\033[0m\n' "$*"; }
warn() { printf '\033[33m警告: %s\033[0m\n' "$*" >&2; }
require_val() {
  if [[ -z "${2:-}" || "${2:-}" == --* ]]; then
    die "$1 需要值"
  fi
}

if [[ "${LIKING_INSTALL_TEST:-}" != 1 ]]; then
  [[ ${EUID:-$(id -u)} -eq 0 ]] || die "请以 root 运行（精简系统没有 sudo 时直接用 root）"
fi

usage() {
  cat <<USAGE
liking 一键安装 / 升级 / 卸载（面板 liking-server）

  bash <(curl -fsSL https://raw.githubusercontent.com/cheesydui-cloud/liking/main/install.sh)

用法:
  $0 [server|update|update-script|uninstall|reset-password|docker] [选项]

模式:
  server           安装控制面板（默认，需要 systemd）
  update           拉 latest 二进制升级（可用 --release 钉版本）
  update-script    只更新本升级脚本
  uninstall        停服务、删二进制与 unit；默认保留数据库
  reset-password   重置面板 admin 密码
  docker           用 Docker 安装或升级面板。已有 /opt/liking-docker 时，update 也会走这里

选项:
  --addr ADDR                 监听地址，默认 127.0.0.1:8899。docker 模式这是宿主机发布地址，容器内仍听 0.0.0.0:8899
  --release VER               GitHub release tag，默认 latest
  --from-dir DIR              从本地目录安装（需 SHA256SUMS 与二进制）
  --gh-proxy PFX              GitHub 镜像前缀，如 https://gh-proxy.com/
  --bootstrap-admin-password  首次安装管理员密码（否则随机打印）
  --password PW               reset-password 的新密码
  --name NAME                 docker 容器名，默认 liking
  --no-sock                   docker 不挂载 docker.sock，页面里就不能升级
  --purge                     uninstall 时连数据和配置一起删
  -h, --help

Docker 示例:
  $0 docker --gh-proxy https://gh-proxy.com/ --bootstrap-admin-password '改成你的密码'
  $0 docker --addr 0.0.0.0:8899
  $0 docker reset-password --password '新密码'

安装完成后:
  liking-upgrade              升级到 latest
  liking-uninstall            卸载（加 --purge 清数据）

节点 Agent 在面板「服务器」页复制一键命令。连不上面板时勾选「国内机器」，脚本和 Agent 改从 GitHub 镜像下载。
USAGE
}

MODE="server"
PURGE=0
RESET_PW=""
FROM_DIR=""

while [[ $# -gt 0 ]]; do
  case "$1" in
    server|update|update-script|uninstall|reset-password) MODE="$1"; shift ;;
    docker)
      MODE="docker"
      shift
      case "${1:-}" in
        install|update|uninstall|reset-password)
          DOCKER_ACTION="$1"
          shift
          ;;
      esac
      ;;
    --name) require_val --name "${2:-}"; CONTAINER_NAME="$2"; shift 2 ;;
    --name=*) CONTAINER_NAME="${1#*=}"; shift ;;
    --no-sock) DOCKER_SOCK=0; shift ;;
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
if [[ -n "$GH_PROXY" && "$GH_PROXY" != https://* ]]; then
  die "--gh-proxy 必须使用 HTTPS"
fi

script_url() {
  echo "https://raw.githubusercontent.com/$REPO/main/install.sh"
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
  if curl -fsSL --max-filesize "$MAX_METADATA_BYTES" "$(script_url)" -o "$stmp/upgrade.sh" 2>/dev/null && looks_like_installer "$stmp/upgrade.sh"; then
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
  s=${s//$'\n'/\\n}
  s=${s//$'\r'/\\r}
  s=${s//$'\t'/\\t}
  s=${s//%/%%}
  printf '"%s"' "$s"
}

remove_bootstrap_dropin() {
  local dir="$SYSTEMD_DIR/liking-server.service.d"
  rm -f "$ETC_DIR/bootstrap-password"
  if [[ -f "$dir/bootstrap.conf" ]]; then
    rm -f "$dir/bootstrap.conf"
    rmdir "$dir" 2>/dev/null || true
    systemctl daemon-reload
  fi
}

rand_admin_pw() {
  local pw=""
  if command -v openssl >/dev/null 2>&1; then
    pw="$(openssl rand -hex 6 2>/dev/null || true)"
  fi
  if [[ -z "$pw" ]]; then
    pw="$(head -c 6 /dev/urandom | od -An -tx1 | tr -d ' \n')"
  fi
  [[ -n "$pw" ]] || die "无法生成管理员密码"
  printf '%s' "$pw"
}

guess_panel_url() {
  local port host h
  port="${PANEL_ADDR##*:}"
  if [[ -z "$port" || "$port" == "$PANEL_ADDR" ]]; then
    port="8899"
  fi
  host=""
  if [[ "$PANEL_ADDR" == *:* ]]; then
    h="${PANEL_ADDR%:*}"
    h="${h#[}"
    h="${h%]}"
    if [[ -n "$h" && "$h" != "0.0.0.0" && "$h" != "::" && "$h" != "*" ]]; then
      host="$h"
    fi
  fi
  if [[ -z "$host" ]] && command -v ip >/dev/null 2>&1; then
    host="$(ip -4 route get 1.1.1.1 2>/dev/null | awk '{for (i = 1; i <= NF; i++) if ($i == "src") { print $(i + 1); exit }}')"
  fi
  if [[ -z "$host" ]]; then
    host="$(hostname -I 2>/dev/null | awk '{for (i = 1; i <= NF; i++) if ($i !~ /^127\./ && $i !~ /:/) { print $i; exit }}')"
  fi
  [[ -n "$host" ]] || host="服务器IP"
  printf 'http://%s:%s' "$host" "$port"
}

scrape_boot_pw() {
  journalctl -u liking-server -n 120 --no-pager -o cat 2>/dev/null \
    | awk '/密[[:space:]]*码:/{ print $NF; found=1 } END { if (!found) exit 1 }'
}

print_install_done() {
  local url pw
  url="$(guess_panel_url)"
  pw="${1:-}"
  echo
  echo "================================================"
  if [[ -n "$pw" ]]; then
    ok "面板安装成功"
    printf '  地址:   %s\n' "$url"
    printf '  用户名: %s\n' "admin"
    printf '  密码:   %s\n' "$pw"
    echo "================================================"
    note "请妥善保存。忘记密码: liking-upgrade reset-password"
  else
    ok "liking-server 已启动（$PANEL_ADDR）"
    printf '  地址:   %s\n' "$url"
    echo "================================================"
    note "已有数据库，账号未改。忘记密码: liking-upgrade reset-password"
  fi
  note "升级: liking-upgrade"
  note "卸载: liking-uninstall"
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
  local ddir expected actual official_base
  ddir="$(dirname "$dest")"
  [[ -f "$dest" ]] || die "下载结果不存在: $dest"
  official_base="https://github.com${base#*https://github.com}"
  if curl -fLs --retry 5 --retry-delay 2 --connect-timeout 20 --max-filesize "$MAX_METADATA_BYTES" \
      "$official_base/SHA256SUMS" -o "$ddir/SHA256SUMS" 2>/dev/null; then
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

# fetch_agent_asset downloads one release asset into dest and checks ELF, size, and sha256.
# Failure removes dest and returns 1. Curl's stderr is kept so a bad download is visible.
fetch_agent_asset() {
  local base="$1" asset="$2" dest="$3" sums="$4"
  local errf
  errf="$(mktemp)"
  rm -f "$dest"
  if curl -fL --retry 5 --retry-delay 2 --connect-timeout 20 --max-time 180 --max-filesize "$MAX_RELEASE_BYTES" \
       -o "$dest" "$base/$asset" 2>"$errf" \
     && is_elf "$dest" \
     && [[ $(wc -c < "$dest" | tr -d ' ') -gt 1048576 ]] \
     && [[ $(wc -c < "$dest" | tr -d ' ') -le "$MAX_RELEASE_BYTES" ]] \
     && match_sha "$dest" "$asset" "$sums"; then
    rm -f "$errf"
    note "    sha256: OK ($asset)"
    return 0
  fi
  if [[ -s "$errf" ]]; then
    warn "下载 $asset 失败: $(tr '\n' ' ' < "$errf" | head -c 240)"
  else
    warn "下载或校验失败: $asset"
  fi
  rm -f "$dest" "$errf"
  return 1
}

# fetch_release_agents always installs the host arch. The other arch is best-effort.
# amd64 releases also publish unsuffixed liking-agent with the same bytes; use it
# when the arch-specific URL fails.
fetch_release_agents() {
  local base="$1" dir="$2"
  local host_asset other_asset sums
  host_asset="liking-agent-linux-${HOST_GOARCH}"
  if [[ "$HOST_GOARCH" == "amd64" ]]; then
    other_asset="liking-agent-linux-arm64"
  else
    other_asset="liking-agent-linux-amd64"
  fi
  sums="$dir/SHA256SUMS"
  [[ -f "$sums" ]] || die "未取到 SHA256SUMS，无法校验 agent"

  if ! fetch_agent_asset "$base" "$host_asset" "$dir/$host_asset" "$sums"; then
    if [[ "$HOST_GOARCH" == "amd64" ]] && fetch_agent_asset "$base" "liking-agent" "$dir/$host_asset" "$sums"; then
      note "    已用 liking-agent 代替 $host_asset"
      note "    sha256: OK ($host_asset)"
    else
      die "没有下到 $host_asset。升级已中止，正在运行的面板没有被替换。请检查到 GitHub 的网络，或加上 --gh-proxy https://gh-proxy.com/"
    fi
  fi
  if ! fetch_agent_asset "$base" "$other_asset" "$dir/$other_asset" "$sums"; then
    warn "没下到 $other_asset。这个架构的节点暂时不能一键升级。"
  fi
}

fetch_and_verify() {
  local base="$1" asset="$2" dest="$3"
  curl -fL --retry 5 --retry-delay 2 --connect-timeout 20 --max-filesize "$MAX_RELEASE_BYTES" --progress-bar \
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
    if curl -fL --retry 4 --retry-delay 2 --connect-timeout 20 --max-filesize "$MAX_RELEASE_BYTES" --progress-bar \
         "$base/$name" -o "$dest" 2>/dev/null; then
      size="$(wc -c < "$dest" | tr -d ' ')"
      if [[ -n "$size" && "$size" -gt 1048576 && "$size" -le "$MAX_RELEASE_BYTES" ]]; then
        verify_asset "$base" "$name" "$dest" >/dev/null
        return 0
      fi
    fi
  done
  die "下载 $kind 失败（$base）"
}

write_server_unit() {
  local addr="${1:-127.0.0.1:8899}"
  mkdir -p "$SYSTEMD_DIR"
  cat >"$SYSTEMD_DIR/liking-server.service" <<EOF
[Unit]
Description=liking web panel
After=network-online.target
Wants=network-online.target

[Service]
Type=simple
ExecStart=$(unit_quote "$INSTALL_DIR/liking-server") --addr $(unit_quote "$addr") --db $(unit_quote "$DATA_DIR/panel.db")
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

backup_db_before_upgrade() {
  [[ -f "$DATA_DIR/panel.db" ]] || return 0
  install -d -m 0700 "$DATA_DIR/backups"
  if systemctl is-active --quiet liking-server.service; then
    systemctl stop liking-server.service || true
  fi
  local stamp
  stamp="$(date +%Y%m%d-%H%M%S)"
  cp -a "$DATA_DIR/panel.db" "$DATA_DIR/backups/pre-upgrade-$stamp.db"
  [[ -f "$DATA_DIR/panel.db-wal" ]] && cp -a "$DATA_DIR/panel.db-wal" "$DATA_DIR/backups/pre-upgrade-$stamp.db-wal" || true
  [[ -f "$DATA_DIR/panel.db-shm" ]] && cp -a "$DATA_DIR/panel.db-shm" "$DATA_DIR/backups/pre-upgrade-$stamp.db-shm" || true
  local i=0 f
  while IFS= read -r f; do
    i=$((i + 1))
    if [[ $i -gt 7 ]]; then
      rm -f "$f" "${f}-wal" "${f}-shm"
    fi
  done < <(ls -1t "$DATA_DIR/backups"/pre-upgrade-*.db 2>/dev/null || true)
  note "升级前已备份数据库: $DATA_DIR/backups/pre-upgrade-$stamp.db"
}

warn_stale_agent() {
  local server_ver agent_bin agent_ver f
  for f in liking-agent-linux-amd64 liking-agent-linux-arm64; do
    if [[ ! -f "$INSTALL_DIR/$f" ]]; then
      warn "缺少 $f。这个架构的一键升级会失败。"
    fi
  done
  server_ver="$("$INSTALL_DIR/liking-server" --version 2>/dev/null || true)"
  agent_bin="$INSTALL_DIR/liking-agent-linux-${HOST_GOARCH}"
  if [[ ! -x "$agent_bin" ]]; then
    agent_bin="$INSTALL_DIR/liking-agent"
  fi
  if [[ -x "$agent_bin" && -n "$server_ver" ]]; then
    agent_ver="$("$agent_bin" --version 2>/dev/null || true)"
    if [[ -n "$agent_ver" && "$agent_ver" != "$server_ver" ]]; then
      warn "agent 二进制是 $agent_ver，面板是 $server_ver。一键升级会下发旧版本。请确认上面有 sha256: OK (liking-agent-linux-${HOST_GOARCH})"
    fi
  fi
}

restart_local_agent() {
  if ! systemctl cat liking-agent.service >/dev/null 2>&1; then
    return 0
  fi
  local src=""
  if [[ -f "$INSTALL_DIR/liking-agent-linux-${HOST_GOARCH}" ]]; then
    src="$INSTALL_DIR/liking-agent-linux-${HOST_GOARCH}"
  elif [[ -f "$INSTALL_DIR/liking-agent" ]]; then
    src="$INSTALL_DIR/liking-agent"
  fi
  if [[ -z "$src" ]]; then
    warn "本机有 Agent 服务，但没有对应架构的 liking-agent 二进制"
    return 0
  fi
  install -m 0755 "$src" "$INSTALL_DIR/liking-agent"
  if systemctl is-enabled --quiet liking-agent.service 2>/dev/null || systemctl is-active --quiet liking-agent.service; then
    systemctl restart liking-agent.service || warn "重启 liking-agent 失败"
    note "已更新并重启本机 Agent"
  fi
}

install_or_update() {
  detect_arch
  ensure_curl
  persist_gh_proxy
  install -d -m 0755 "$INSTALL_DIR" "$SYSTEMD_DIR"
  install -d -m 0700 "$ETC_DIR" "$DATA_DIR"
  chmod 0700 "$ETC_DIR" "$DATA_DIR"
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
    [[ -f "$tmp/SHA256SUMS" ]] || die "未取到 SHA256SUMS"
    fetch_release_agents "$base" "$tmp"
  fi

  if [[ "$HOST_GOARCH" == "amd64" && ! -f "$tmp/liking-agent-linux-amd64" && -f "$tmp/liking-agent" ]]; then
    cp -f "$tmp/liking-agent" "$tmp/liking-agent-linux-amd64"
    note "    已用 liking-agent 代替 liking-agent-linux-amd64"
    note "    sha256: OK (liking-agent-linux-amd64)"
  fi
  if [[ ! -f "$tmp/liking-agent-linux-${HOST_GOARCH}" ]]; then
    die "没有本机架构的 agent（liking-agent-linux-${HOST_GOARCH}）。升级已中止，正在运行的面板没有被替换。"
  fi

  if [[ -f "$INSTALL_DIR/liking-server" ]]; then
    cp -a "$INSTALL_DIR/liking-server" "$tmp/liking-server.bak" || true
  fi
  backup_db_before_upgrade
  install -m 0755 "$tmp/liking-server" "$INSTALL_DIR/liking-server"
  for f in liking-agent-linux-amd64 liking-agent-linux-arm64 liking-agent; do
    if [[ -f "$tmp/$f" && $(wc -c < "$tmp/$f" | tr -d ' ') -gt 1048576 ]]; then
      install -m 0755 "$tmp/$f" "$INSTALL_DIR/$f"
    fi
  done
  warn_stale_agent

  local first_install=0
  if [[ ! -f "$DATA_DIR/panel.db" ]]; then
    first_install=1
    if [[ -z "$BOOTSTRAP_PW" ]]; then
      BOOTSTRAP_PW="$(rand_admin_pw)"
    fi
  fi
  write_server_unit "$PANEL_ADDR"
  remove_bootstrap_dropin
  # 一次性 drop-in：启动时传入密码，起来后再删，避免密码长期留在 unit
  if [[ "$first_install" -eq 1 && -n "$BOOTSTRAP_PW" ]]; then
    mkdir -p "$SYSTEMD_DIR/liking-server.service.d"
    printf '%s' "$BOOTSTRAP_PW" >"$ETC_DIR/bootstrap-password"
    chmod 0600 "$ETC_DIR/bootstrap-password"
    cat >"$SYSTEMD_DIR/liking-server.service.d/bootstrap.conf" <<EOF
[Service]
ExecStart=
ExecStart=$(unit_quote "$INSTALL_DIR/liking-server") --addr $(unit_quote "$PANEL_ADDR") --db $(unit_quote "$DATA_DIR/panel.db") --bootstrap-admin-password-file $(unit_quote "$ETC_DIR/bootstrap-password")
EOF
  fi
  systemctl daemon-reload
  systemctl enable liking-server.service
  # enable --now 不会重启已在跑的进程，升级必须 restart 才能载入新二进制
  if ! systemctl restart liking-server.service; then
    remove_bootstrap_dropin
    if [[ -f "$tmp/liking-server.bak" ]]; then
      install -m 0755 "$tmp/liking-server.bak" "$INSTALL_DIR/liking-server"
      systemctl restart liking-server.service || true
    fi
    die "服务未能启动，请看: journalctl -u liking-server -n 80 --no-pager"
  fi
  printf '%s\n' "$PANEL_ADDR" >"$ETC_DIR/addr"
  if command -v "$INSTALL_DIR/liking-server" >/dev/null 2>&1; then
    note "版本 $($INSTALL_DIR/liking-server --version 2>/dev/null || echo unknown)"
  fi
  sleep 1
  if systemctl is-active --quiet liking-server.service; then
    remove_bootstrap_dropin
    ok "liking-server 已启动（$PANEL_ADDR）"
  else
    remove_bootstrap_dropin
    if [[ -f "$tmp/liking-server.bak" ]]; then
      install -m 0755 "$tmp/liking-server.bak" "$INSTALL_DIR/liking-server"
      systemctl restart liking-server.service || true
    fi
    die "服务未能启动，请看: journalctl -u liking-server -n 80 --no-pager"
  fi
  persist_script
  systemctl --no-pager --full status liking-server.service | head -20 || true
  restart_local_agent
  local shown_pw=""
  if [[ "$first_install" -eq 1 ]]; then
    shown_pw="$BOOTSTRAP_PW"
    if [[ -z "$shown_pw" ]]; then
      shown_pw="$(scrape_boot_pw || true)"
    fi
  fi
  print_install_done "$shown_pw"
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
  echo
  echo "================================================"
  ok "已重置 admin 密码"
  printf '  用户名: %s\n' "admin"
  printf '  密码:   %s\n' "$pw"
  echo "================================================"
}

do_update_script() {
  ensure_curl
  local stmp surl
  surl="$(script_url)"
  stmp="$(mktemp -d)"
  curl -fsSL --max-filesize "$MAX_METADATA_BYTES" "$surl" -o "$stmp/upgrade.sh" || { rm -rf "$stmp"; die "下载失败: $surl"; }
  looks_like_installer "$stmp/upgrade.sh" || { rm -rf "$stmp"; die "内容异常，拒绝覆盖"; }
  install -m 0755 "$stmp/upgrade.sh" "$SCRIPT_PATH"
  ln -sfn "$SCRIPT_PATH" "$INSTALL_DIR/liking-uninstall"
  rm -rf "$stmp"
  ok "升级脚本已更新: $SCRIPT_PATH"
}

valid_container_name() {
  [[ "$1" =~ ^[A-Za-z0-9][A-Za-z0-9_.-]{0,63}$ ]]
}

parse_panel_addr() {
  local addr="$PANEL_ADDR"
  if [[ "$addr" == \[*\]:* ]]; then
    HOST_PORT="${addr##*:}"
    HOST_BIND_IP="${addr%:*}"
    HOST_BIND_IP="${HOST_BIND_IP#[}"
    HOST_BIND_IP="${HOST_BIND_IP%]}"
  else
    HOST_PORT="${addr##*:}"
    HOST_BIND_IP="${addr%:*}"
  fi
  if [[ -z "$HOST_BIND_IP" || "$HOST_BIND_IP" == "$addr" || ! "$HOST_PORT" =~ ^[0-9]+$ ]]; then
    die "--addr 需要写成 IP:端口，例如 127.0.0.1:8899"
  fi
  if [[ "$HOST_PORT" -lt 1 || "$HOST_PORT" -gt 65535 ]]; then
    die "端口无效"
  fi
}

managed_dockerfile() {
  cat <<'EOF'
# liking-managed-dockerfile
FROM scratch
COPY ca-certificates.crt /etc/ssl/certs/ca-certificates.crt
COPY liking-server /usr/local/bin/liking-server
ENV SSL_CERT_FILE=/etc/ssl/certs/ca-certificates.crt
EXPOSE 8899
STOPSIGNAL SIGTERM
ENTRYPOINT ["/usr/local/bin/liking-server"]
CMD ["--addr", "0.0.0.0:8899", "--db", "/var/lib/liking/panel.db"]
HEALTHCHECK --interval=30s --timeout=3s --start-period=20s --retries=3 CMD ["/usr/local/bin/liking-server", "--health", "http://127.0.0.1:8899/healthz"]
EOF
}

write_dockerignore() {
  cat >"$OPT_DIR/.dockerignore" <<'EOF'
*
!Dockerfile
!liking-server
!ca-certificates.crt
EOF
}

write_docker_compose() {
  local dest="$1" publish project trust=0
  parse_panel_addr
  publish="$HOST_BIND_IP:$HOST_PORT:8899"
  if [[ "$HOST_BIND_IP" == *:* ]]; then
    publish="[$HOST_BIND_IP]:$HOST_PORT:8899"
  fi
  project="$CONTAINER_NAME"
  if [[ "$HOST_BIND_IP" == "127.0.0.1" || "$HOST_BIND_IP" == "::1" ]]; then
    trust=1
  fi
  {
    printf 'name: %s\n\n' "$project"
    printf 'services:\n'
    printf '  liking:\n'
    printf '    image: liking:panel\n'
    printf '    container_name: %s\n' "$CONTAINER_NAME"
    printf '    restart: unless-stopped\n'
    printf '    stop_grace_period: 15s\n'
    printf '    ports:\n'
    printf '      - "%s"\n' "$publish"
    printf '    volumes:\n'
    printf '      - %s:/var/lib/liking\n' "$DATA_DIR"
    if [[ "$DOCKER_SOCK" -eq 1 ]]; then
      printf '      - /var/run/docker.sock:/var/run/docker.sock\n'
      printf '      - %s:/opt/liking-docker\n' "$OPT_DIR"
    fi
    if [[ "$trust" -eq 1 || -n "$GH_PROXY" ]]; then
      printf '    environment:\n'
      if [[ "$trust" -eq 1 ]]; then
        printf '      LIKING_TRUST_DOCKER_GATEWAY: "1"\n'
      fi
      if [[ -n "$GH_PROXY" ]]; then
        printf '      LIKING_GH_PROXY: "%s"\n' "$GH_PROXY"
      fi
    fi
    if [[ "${DOCKER_BOOTSTRAP:-0}" -eq 1 ]]; then
      printf '    command:\n'
      printf '      - --addr\n'
      printf '      - 0.0.0.0:8899\n'
      printf '      - --db\n'
      printf '      - /var/lib/liking/panel.db\n'
      printf '      - --bootstrap-admin-password-file\n'
      printf '      - /var/lib/liking/bootstrap-password\n'
    fi
    printf '    security_opt:\n'
    printf '      - no-new-privileges:true\n'
    printf '    cap_drop:\n'
    printf '      - ALL\n'
    printf '    pids_limit: 512\n'
    printf '    logging:\n'
    printf '      driver: json-file\n'
    printf '      options:\n'
    printf '        max-size: "10m"\n'
    printf '        max-file: "3"\n'
    printf '    labels:\n'
    printf '      liking.panel: "1"\n'
  } >"$dest"
}

copy_ca_bundle() {
  local dest="$1" src size
  for src in /etc/ssl/certs/ca-certificates.crt /etc/ssl/cert.pem /etc/pki/tls/certs/ca-bundle.crt /etc/pki/ca-trust/extracted/pem/tls-ca-bundle.pem; do
    [[ -f "$src" ]] || continue
    size="$(wc -c <"$src" | tr -d '[:space:]')"
    if [[ "$size" -gt 1000 ]]; then
      install -m 0644 "$src" "$dest"
      return 0
    fi
  done
  return 1
}

run_compose() {
  if docker compose version >/dev/null 2>&1; then
    docker compose -f "$OPT_DIR/docker-compose.yml" "$@"
  elif command -v docker-compose >/dev/null 2>&1; then
    docker-compose -f "$OPT_DIR/docker-compose.yml" "$@"
  else
    die "需要 docker compose"
  fi
}

health_url() {
  local ip="$HOST_BIND_IP"
  if [[ "$ip" == "0.0.0.0" || "$ip" == "::" || "$ip" == "*" ]]; then
    ip="127.0.0.1"
  fi
  if [[ "$ip" == *:* ]]; then
    printf 'http://[%s]:%s/healthz' "$ip" "$HOST_PORT"
  else
    printf 'http://%s:%s/healthz' "$ip" "$HOST_PORT"
  fi
}

rewrite_container_listen() {
  local file="$1" tmp
  [[ -f "$file" ]] || return 0
  tmp="$(mktemp)"
  awk '
    /^[[:space:]]*command:/ { incmd=1; print; next }
    incmd && /^[[:space:]]*[A-Za-z0-9_]+:/ { incmd=0 }
    incmd {
      gsub(/127\.0\.0\.1:/, "0.0.0.0:")
      gsub(/localhost:/, "0.0.0.0:")
      gsub(/\[::1\]:/, "0.0.0.0:")
    }
    { print }
  ' "$file" >"$tmp"
  mv "$tmp" "$file"
}

wait_panel_health() {
  local url i inside=0
  url="$(health_url)"
  for i in 1 2 3 4 5 6 7 8 9 10 11 12 13 14 15 16 17 18 19 20 21 22 23 24 25 26 27 28 29 30 31 32 33 34 35 36 37 38 39 40; do
    if curl -fsS --max-time 2 "$url" >/dev/null 2>&1; then
      return 0
    fi
    if docker exec "$CONTAINER_NAME" /usr/local/bin/liking-server --health http://127.0.0.1:8899/healthz >/dev/null 2>&1; then
      inside=1
    fi
    sleep 1
  done
  docker logs --tail 40 "$CONTAINER_NAME" >&2 || true
  if [[ "$inside" -eq 1 ]]; then
    die "容器里面是好的，但宿主机 $url 没通。看 compose 端口，以及容器里的 --addr 是不是 0.0.0.0"
  fi
  die "容器已启动，但健康检查没有通过。看上面的日志"
}

ensure_compose_mount() {
  local file="$1" mount="$2" tmp indent
  grep -qF "$mount" "$file" && return 0
  indent="$(awk '/\/var\/lib\/liking:\/var\/lib\/liking/ { match($0, /^ */); print substr($0, 1, RLENGTH); exit }' "$file")"
  [[ -n "$indent" ]] || return 1
  tmp="$(mktemp)"
  awk -v ins="${indent}- ${mount}" '
    { print }
    !done && index($0, "/var/lib/liking:/var/lib/liking") { print ins; done=1 }
  ' "$file" >"$tmp"
  mv "$tmp" "$file"
}

panel_container_ok() {
  local mounts
  docker inspect "$CONTAINER_NAME" >/dev/null 2>&1 || return 1
  mounts="$(docker inspect -f '{{range .Mounts}}{{.Destination}} {{end}}' "$CONTAINER_NAME" 2>/dev/null || true)"
  [[ "$mounts" == *"/var/lib/liking"* ]]
}

stage_docker_binaries() {
  local tag base tmp="$1"
  if [[ -n "$FROM_DIR" ]]; then
    [[ -d "$FROM_DIR" ]] || die "--from-dir 不是目录: $FROM_DIR"
    [[ -f "$FROM_DIR/SHA256SUMS" ]] || die "--from-dir 缺少 SHA256SUMS"
    local server_asset=""
    for server_asset in "liking-server-linux-${HOST_GOARCH}" liking-server; do
      if copy_from_dir "$FROM_DIR" "$tmp/liking-server" "$server_asset"; then
        note "    sha256: OK ($server_asset)"
        break
      fi
      server_asset=""
    done
    [[ -n "$server_asset" ]] || die "本地目录没有可用的 liking-server"
    local agent_asset
    for agent_asset in liking-agent-linux-amd64 liking-agent-linux-arm64 liking-agent; do
      if copy_from_dir "$FROM_DIR" "$tmp/$agent_asset" "$agent_asset"; then
        note "    sha256: OK ($agent_asset)"
      else
        rm -f "$tmp/$agent_asset"
      fi
    done
    return 0
  fi
  tag="$(resolve_release_tag)"
  base="$(release_download_base "$tag")"
  note "版本 $tag  架构 linux/$HOST_GOARCH"
  note "下载 $base"
  download_named "$base" "liking-server" "$tmp/liking-server"
  is_elf "$tmp/liking-server" || die "下载的 liking-server 不是 ELF"
  [[ -f "$tmp/SHA256SUMS" ]] || die "未取到 SHA256SUMS"
  fetch_release_agents "$base" "$tmp"
}

backup_docker_db() {
  [[ -f "$DATA_DIR/panel.db" ]] || return 0
  if panel_container_ok; then
    docker stop "$CONTAINER_NAME" >/dev/null || true
  fi
  install -d -m 0700 "$DATA_DIR/backups"
  local stamp i f
  stamp="$(date +%Y%m%d-%H%M%S)"
  cp -a "$DATA_DIR/panel.db" "$DATA_DIR/backups/pre-upgrade-$stamp.db"
  [[ -f "$DATA_DIR/panel.db-wal" ]] && cp -a "$DATA_DIR/panel.db-wal" "$DATA_DIR/backups/pre-upgrade-$stamp.db-wal" || true
  [[ -f "$DATA_DIR/panel.db-shm" ]] && cp -a "$DATA_DIR/panel.db-shm" "$DATA_DIR/backups/pre-upgrade-$stamp.db-shm" || true
  i=0
  while IFS= read -r f; do
    i=$((i + 1))
    if [[ $i -gt 7 ]]; then
      rm -f "$f" "${f}-wal" "${f}-shm"
    fi
  done < <(ls -1t "$DATA_DIR/backups"/pre-upgrade-*.db 2>/dev/null || true)
  note "升级前已备份数据库: $DATA_DIR/backups/pre-upgrade-$stamp.db"
}

do_docker_uninstall() {
  command -v docker >/dev/null 2>&1 || die "需要 Docker"
  if docker inspect "$CONTAINER_NAME" >/dev/null 2>&1 && ! panel_container_ok; then
    die "容器 $CONTAINER_NAME 不是 liking 面板，没有删除"
  fi
  if [[ -f "$OPT_DIR/docker-compose.yml" ]]; then
    run_compose down || true
  fi
  docker rm -f "$CONTAINER_NAME" >/dev/null 2>&1 || true
  if [[ "$PURGE" -eq 1 ]]; then
    rm -rf "$DATA_DIR" "$OPT_DIR" "$ETC_DIR"
    docker rmi liking:panel >/dev/null 2>&1 || true
    rm -f "$SCRIPT_PATH" "$INSTALL_DIR/liking-uninstall"
    ok "已卸载并清除数据"
  else
    ok "已卸载容器（数据库保留在 $DATA_DIR ）"
  fi
}

do_docker_reset() {
  command -v docker >/dev/null 2>&1 || die "需要 Docker"
  docker image inspect liking:panel >/dev/null 2>&1 || die "没有 liking:panel 镜像"
  panel_container_ok || die "没有找到面板容器 $CONTAINER_NAME"
  local pw="$RESET_PW"
  if [[ -z "$pw" ]]; then
    if [[ -t 0 ]]; then
      read -r -s -p "新密码: " pw
      echo
    else
      pw="$(rand_admin_pw)"
      note "未提供密码，已随机生成"
    fi
  fi
  [[ -n "$pw" ]] || die "密码为空"
  local pwfile rc
  pwfile="$(mktemp)"
  chmod 0600 "$pwfile"
  printf '%s' "$pw" >"$pwfile"
  docker stop "$CONTAINER_NAME" >/dev/null
  set +e
  docker run --rm --network none --cap-drop ALL --security-opt no-new-privileges --pids-limit 64 \
    -v "$DATA_DIR:/var/lib/liking" \
    -v "$pwfile:/run/liking-reset-password:ro" \
    --entrypoint /usr/local/bin/liking-server \
    liking:panel \
    --db /var/lib/liking/panel.db --reset-admin-password-file /run/liking-reset-password
  rc=$?
  set -e
  rm -f "$pwfile"
  if [[ "$rc" -ne 0 ]]; then
    docker start "$CONTAINER_NAME" >/dev/null 2>&1 || true
    die "重置失败，已尝试拉回原来的容器"
  fi
  docker start "$CONTAINER_NAME" >/dev/null
  echo
  echo "================================================"
  ok "已重置 admin 密码"
  printf '  用户名: %s\n' "admin"
  printf '  密码:   %s\n' "$pw"
  echo "================================================"
}

do_docker() {
  case "$DOCKER_ACTION" in
    uninstall) do_docker_uninstall; return ;;
    reset-password) do_docker_reset; return ;;
  esac
  valid_container_name "$CONTAINER_NAME" || die "容器名无效: $CONTAINER_NAME"
  command -v docker >/dev/null 2>&1 || die "需要 Docker"
  docker info >/dev/null 2>&1 || die "Docker 没有在运行，或当前用户不能访问"
  detect_arch
  ensure_curl
  persist_gh_proxy
  parse_panel_addr
  if [[ -n "$GH_PROXY" && "$GH_PROXY" == *[!A-Za-z0-9:/._~-] ]]; then
    die "--gh-proxy 含有不能写入配置的字符"
  fi
  if docker inspect "$CONTAINER_NAME" >/dev/null 2>&1 && ! panel_container_ok; then
    die "已有容器 $CONTAINER_NAME，但不是 liking 面板。换一个名字：install.sh docker --name liking-panel"
  fi
  install -d -m 0755 "$OPT_DIR" "$INSTALL_DIR"
  install -d -m 0700 "$ETC_DIR" "$DATA_DIR"
  chmod 0700 "$ETC_DIR" "$DATA_DIR"

  local tmp first_install=0
  tmp="$(mktemp -d)"
  trap 'rm -rf "$tmp"' EXIT
  stage_docker_binaries "$tmp"
  if [[ "$HOST_GOARCH" == "amd64" && ! -f "$tmp/liking-agent-linux-amd64" && -f "$tmp/liking-agent" ]]; then
    cp -f "$tmp/liking-agent" "$tmp/liking-agent-linux-amd64"
  fi
  if [[ ! -f "$tmp/liking-agent-linux-${HOST_GOARCH}" ]]; then
    die "没有本机架构的 agent（liking-agent-linux-${HOST_GOARCH}）。升级已中止，正在运行的容器没有被替换。"
  fi

  if [[ ! -f "$DATA_DIR/panel.db" ]]; then
    first_install=1
    if [[ -z "$BOOTSTRAP_PW" ]]; then
      BOOTSTRAP_PW="$(rand_admin_pw)"
    fi
    printf '%s' "$BOOTSTRAP_PW" >"$DATA_DIR/bootstrap-password"
    chmod 0600 "$DATA_DIR/bootstrap-password"
    DOCKER_BOOTSTRAP=1
  else
    backup_docker_db
  fi

  install -m 0755 "$tmp/liking-server" "$OPT_DIR/liking-server"
  local f
  for f in liking-agent-linux-amd64 liking-agent-linux-arm64 liking-agent; do
    if [[ -f "$tmp/$f" ]]; then
      install -m 0755 "$tmp/$f" "$OPT_DIR/$f"
    fi
  done
  if [[ -f "$OPT_DIR/Dockerfile" ]] && ! grep -q 'liking-managed-dockerfile' "$OPT_DIR/Dockerfile"; then
    cp -a "$OPT_DIR/Dockerfile" "$OPT_DIR/Dockerfile.bak.$(date +%Y%m%d%H%M%S)"
    note "原来的 Dockerfile 已备份"
  fi
  managed_dockerfile >"$OPT_DIR/Dockerfile"
  copy_ca_bundle "$OPT_DIR/ca-certificates.crt" || die "找不到 CA 证书包。先安装 ca-certificates"
  write_dockerignore

  if [[ "$first_install" -eq 1 || ! -f "$OPT_DIR/docker-compose.yml" ]]; then
    write_docker_compose "$OPT_DIR/docker-compose.yml"
    if [[ "$HOST_BIND_IP" == "0.0.0.0" || "$HOST_BIND_IP" == "::" ]]; then
      warn "面板端口对所有地址开放。这种发布方式不信任转发头，避免伪造来源绕过 IP 白名单"
    fi
  else
    ensure_compose_mount "$OPT_DIR/docker-compose.yml" "/var/lib/liking:/var/lib/liking" || warn "compose 里没有数据目录那一行，请确认 /var/lib/liking 已挂载"
    if [[ "$DOCKER_SOCK" -eq 1 ]]; then
      ensure_compose_mount "$OPT_DIR/docker-compose.yml" "/var/run/docker.sock:/var/run/docker.sock" || warn "没能写上 docker.sock 挂载"
      ensure_compose_mount "$OPT_DIR/docker-compose.yml" "$OPT_DIR:/opt/liking-docker" || warn "没能写上构建目录挂载"
    fi
    rewrite_container_listen "$OPT_DIR/docker-compose.yml"
    note "沿用已有 docker-compose.yml"
  fi

  if [[ "$first_install" -eq 1 ]]; then
    note "管理员密码已写入 $DATA_DIR/bootstrap-password。启动成功后会删掉"
  fi
  note "构建镜像 liking:panel"
  if ! docker build -t liking:panel "$OPT_DIR"; then
    docker start "$CONTAINER_NAME" >/dev/null 2>&1 || true
    die "构建失败，已尝试拉回原来的容器"
  fi
  if ! run_compose up -d --force-recreate --no-build; then
    if panel_container_ok || docker inspect "$CONTAINER_NAME" >/dev/null 2>&1; then
      note "旧容器不在这个 compose 项目里，换掉容器，数据目录不动"
      docker rm -f "$CONTAINER_NAME" >/dev/null
      run_compose up -d --force-recreate --no-build
    else
      die "启动容器失败"
    fi
  fi
  wait_panel_health
  if [[ "$first_install" -eq 1 ]]; then
    rm -f "$DATA_DIR/bootstrap-password"
    DOCKER_BOOTSTRAP=0
    write_docker_compose "$OPT_DIR/docker-compose.yml"
    run_compose up -d --force-recreate --no-build
    wait_panel_health
  fi
  persist_script
  local ver
  ver="$(docker exec "$CONTAINER_NAME" /usr/local/bin/liking-server --version 2>/dev/null || true)"
  note "版本 ${ver:-unknown}"
  if [[ "$DOCKER_SOCK" -eq 1 ]]; then
    note "已挂载 docker.sock。这个容器可以操作宿主机上的 Docker，设置页才能自己升级"
  else
    note "没有挂载 docker.sock。以后在宿主机执行 liking-upgrade docker"
  fi
  if [[ "$first_install" -eq 1 ]]; then
    print_install_done "$BOOTSTRAP_PW"
  else
    print_install_done ""
  fi
  note "以后升级: liking-upgrade docker"
}

# 已有容器安装、又没有 systemd 服务时，升级和卸载走 docker。
prefer_docker_mode() {
  local has_compose="$1" has_unit="$2"
  if [[ "$MODE" == "docker" ]]; then
    return 0
  fi
  if [[ "$has_compose" == 1 && "$has_unit" != 1 ]]; then
    case "$MODE" in
      server|update) MODE="docker"; DOCKER_ACTION="update" ;;
      reset-password) MODE="docker"; DOCKER_ACTION="reset-password" ;;
      uninstall) MODE="docker"; DOCKER_ACTION="uninstall" ;;
    esac
  fi
}

# 若通过 liking-uninstall 调用，切到 uninstall
self="$(basename "${0:-}" 2>/dev/null || true)"
if [[ "$self" == "liking-uninstall" && "$MODE" == "server" ]]; then
  MODE="uninstall"
fi

if [[ "${LIKING_INSTALL_TEST:-}" != 1 ]]; then
  has_compose=0
  has_unit=0
  [[ -f "$OPT_DIR/docker-compose.yml" ]] && has_compose=1
  if systemctl cat liking-server.service >/dev/null 2>&1; then
    has_unit=1
  fi
  prefer_docker_mode "$has_compose" "$has_unit"
  if [[ "$MODE" != "docker" ]]; then
    command -v systemctl >/dev/null 2>&1 || die "需要 systemd。容器安装请用: $0 docker"
  fi
  case "$MODE" in
    server|update) install_or_update ;;
    update-script) do_update_script ;;
    uninstall) do_uninstall ;;
    reset-password) do_reset_password ;;
    docker) do_docker ;;
    *) die "未知模式 $MODE" ;;
  esac
fi
