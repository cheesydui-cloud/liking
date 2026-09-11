# Changelog

每个版本必须先写本章节，再打 tag / 发 GitHub Release。

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
