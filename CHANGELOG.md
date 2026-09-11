# Changelog

每个版本必须先写本章节，再打 tag / 发 GitHub Release。

## v0.1.2 — 2026-09-12

真机验收：配置没变时不再每 30 秒重启 Xray。

### 修复
- 周期巡检下发若配置未变且内核仍在跑，则跳过重启（原先 VLESS 会每 30 秒闪断）
- 相同的下发失败只记一次日志，避免刷 journal
- 下发失败文案去掉多余的 `apply rejected:` 前缀
- Agent 安装时清理 v0.1.0 留在 `/var/lib/liking/xray.log` 的残留日志

### 升级注意
- 升到 v0.1.2 后请重新执行节点 Agent 安装命令
- 回滚：`liking-upgrade --release v0.1.1`（需对应 Release 资产）

## v0.1.1 — 2026-09-12

真机验收修复：配置下发失败可见、端口占用不再抢已有服务、Xray 日志不再刷盘。

### 修复
- 节点配置下发失败会写入服务器 `last_error`，总览和服务器页直接显示，不再只打日志
- 创建/修改入站会同步等待下发结果，失败时返回 `apply_error`
- Agent 启动前检测公网端口；若 443 等已被占用则拒绝下发并回滚到上一份可用配置（避免和本机其它服务抢端口）
- Xray 关闭 access 日志，避免 Stats API 把 `xray.log` 刷到数 MB
- 并发下发合并为每台机器一条队列，减少 Xray 反复重启和僵尸进程
- Agent 数据目录改为 `/var/lib/liking/agent`，不再和面板 `panel.db` 混在同一目录

### 改进
- 登录页与卡片质感加强；443 端口给出占用提示
- Agent systemd 带完整 PATH；重装安装脚本会 restart 而不是只 enable
- 面板 `install.sh` / `liking-upgrade` 替换二进制后会 `systemctl restart`（原先 `enable --now` 不会重启已在跑的进程）
- 某一内核缺失时仍下发其它内核（例如没有 mita 时 VLESS 照常监听）

### 升级注意
- 升到 v0.1.1 后请在节点上重新执行面板里的 Agent 安装命令，以更新二进制和数据目录
- 入站默认仍是 443；本机已有 Nginx / 其它面板占用 443 时请改用 8443
- 回滚：`liking-upgrade --release v0.1.0`（需对应 Release 资产）

## v0.1.0 — 2026-09-12

第一版面板：主控 + 节点 Agent，深色金质管理台，一键安装与升级。

### 新增
- 管理员登录、服务器列表、反向 WSS 纳管节点
- 七组入站：VLESS+REALITY、VLESS+REALITY+Vision、VLESS+XHTTP+TLS、Trojan+TLS、SS2022、AnyTLS+TLS、Mieru
- 用户 / 套餐（一人一套餐）；到期或超量从内核配置摘掉客户端
- 订阅：Clash Meta YAML、sing-box JSON、URI（base64）
- 链式转发：入口 → 另一台落地；Mieru / AnyTLS 不能当链式落地
- 面板一键安装、`liking-upgrade` 升级、`liking-uninstall` 卸载；支持 `--from-dir` 离线安装
- 节点从面板 `/v1/install-agent` 下载 Agent，不走 GitHub；`/v1/agent-bin` 带 `X-SHA256`，支持 HEAD

### 改进
- 默认深色电影金质 UI，浅色为暖纸色例外主题；字体打进二进制，不依赖 Google Fonts
- SQLite WAL；删除服务器会断开在线 Agent
- 列表接口不再返回节点 token

### 升级注意
- 默认监听 `:8899`
- 节点需自行安装内核到 PATH：`xray`、AnyTLS 用 `sing-box`（≥1.12）、Mieru 用 `mita`
- 首次安装会打印 admin 密码；也可用 `--bootstrap-admin-password`
