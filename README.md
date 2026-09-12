# liking

简单、稳定的代理面板：主控 + 节点 Agent。默认 **Xray**，AnyTLS 走 **sing-box**，Mieru 走 **mita**。

一条线路 = 一个入站 = 一个出口。一人一套餐。默认端口 **8899**。

仓库：<https://github.com/cheesydui-cloud/liking>

## 能做什么

- 管理员登录、服务器管理（机器 + 节点，实时上下行和已用 / 剩余流量）、反向 WSS 纳管 Agent；公开地址可从 Cloudflare 拉取已托管域名
- 设置分 Tab：面板、证书、备份、账号。证书：Cloudflare DNS 申请 Let's Encrypt（支持泛域名，域名不必指向面板）、自签、上传 PEM；到期前自动续期。账号可改用户名和密码。备份下载整份快照（含证书私钥和 Agent 令牌），恢复时覆盖本机数据，用来迁到新机器
- 七组入站：VLESS+REALITY、VLESS+REALITY+Vision、VLESS+XHTTP+TLS、Trojan+TLS、SS2022、AnyTLS+TLS、Mieru；第一次下发时节点自动安装对应内核。增加节点按协议给 dest / SNI / 指纹 / TLS 1.3 等选项；REALITY dest 不能指向本机
- 用户 / 套餐（一人一套餐；套餐勾选节点，不选表示全部）；到期或超量从内核配置摘掉客户端
- 开户随机密码、用户编辑（用户名 / 套餐 / 到期 / 流量 / 登录密码）、流量进度、订阅二维码 / Clash·sing-box 导入
- 服务器管理「复制」给出协议链接（VLESS `vless://`，Mieru 为小火箭 `mierus://…?udp=&port=&profile=default#节点名`）。「参数」查看公钥 / short_id
- 链式转发：入口 → 另一台落地。**Mieru / AnyTLS 不能当链式落地**；Mieru 可以直出，也可以当链式入口
- 管理端：侧栏分组、列表优先、弹窗创建/编辑（对照妙妙屋的操作习惯，不搬偷自己 / Nginx / TG / 十二套模板）

## 一键安装面板

需 root、systemd、linux/amd64 或 arm64。

```bash
bash <(curl -fsSL https://raw.githubusercontent.com/cheesydui-cloud/liking/main/install.sh)
```

自定义端口与首装密码：

```bash
bash <(curl -fsSL https://raw.githubusercontent.com/cheesydui-cloud/liking/main/install.sh) \
  server --addr :8899 --bootstrap-admin-password '改成你的密码'
```

离线 / 内网把 Release 文件放到同一目录后：

```bash
bash install.sh server --from-dir ./dist --bootstrap-admin-password '改成你的密码'
```

国内拉 GitHub 失败时加镜像：

```bash
bash <(curl -fsSL https://gh-proxy.com/https://raw.githubusercontent.com/cheesydui-cloud/liking/main/install.sh) \
  --gh-proxy https://gh-proxy.com/
```

浏览器打开安装结束时打印的地址。首次安装会打印用户名 `admin` 和密码；未指定时随机生成。忘记密码：`liking-upgrade reset-password`。配置下发失败会显示在「服务器管理」页。

## 升级

```bash
liking-upgrade
```

指定版本：

```bash
liking-upgrade --release v0.1.30
```

只更新安装脚本本身：

```bash
liking-upgrade update-script
```

## 卸载

```bash
liking-uninstall
```

连数据库一起删：

```bash
liking-uninstall --purge
```

重置管理员密码：

```bash
liking-upgrade reset-password --password '新密码'
```

## 安装节点 Agent

在面板「服务器」里添加机器，复制一键命令，到节点上以 root 执行。Agent 从面板下载，不访问 GitHub。

明文 http 控制信道需要 `--insecure`（命令里会自动带上）。

节点按入站协议自行把内核放到 PATH：

| 协议 | 内核 |
|------|------|
| VLESS / Trojan / SS2022 | `xray` |
| AnyTLS | `sing-box` ≥ 1.12 |
| Mieru | `mita` |

Agent 只在有对应入站时才拉起该内核。数据目录：`/var/lib/liking/agent`。

升级面板后请重新执行节点安装命令。入站端口可自定义；443 已被占用时改用 8443 或其它空闲口。

## 本地开发

需要 Go 1.23+ 和 Node 20+。

```bash
make run
```

首次启动会打印 `admin` 密码。打开 `http://127.0.0.1:8899`。

```bash
./bin/liking-agent --connect ws://127.0.0.1:8899/v1/agents --token <服务器 token> --dir ./data/agent --insecure-connect
```

## 明确不做

Nginx 偷自己、联邦、Telegram 机器人、十二套客户端模板、fork Xray 做 AnyTLS、把 Mieru 当链式落地。
