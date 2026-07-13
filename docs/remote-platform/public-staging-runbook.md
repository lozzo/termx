# 单区域公网 Cloud staging 运维手册

## 定位

本手册记录 CLOUD006/CLOUD007 在 `114.66.58.243` 的 staging 装配。它用于从开发机验证真实 Hub signaling、WebRTC direct 和公网 UDP TURN，不是生产部署模板。

owning Control Plane、Hub、Web Controller BFF 与 Next.js 用户站仍只监听服务器 loopback。Nginx 在 `41100-41102/tcp` 提供无隧道公网 HTTP staging；Relay 使用 `41003/udp`。terminal protocol、CapabilityGrant 和文件内容仍位于端到端 DTLS DataChannel，Web Controller/Hub/Relay 不接收这些内容。

公网 HTTP 只允许 staging 测试账号和数据。Web 账号、bcrypt 密码摘要、浏览器 session、订单、AFF 归因、奖励与审计保存在 SQLite；这些内容仍会经过明文 HTTP，禁止真实用户凭据、真实 terminal 数据或生产使用。上线前必须切换域名 HTTPS/TLS profile。

## 已部署服务

| systemd unit | listener | 责任 |
| --- | --- | --- |
| `termx-staging-cloud.service` | `127.0.0.1:41001/tcp`、`127.0.0.1:41002/tcp`、`0.0.0.0:41003/udp` | 内存 Control Plane、Hub 与 lease-bound TURN |
| `termx-staging-web-controller.service` | `127.0.0.1:41000/tcp` | Next.js Landing 与浏览器 BFF surface |
| `termx-staging-web-controller-bff.service` | `127.0.0.1:41004/tcp` | Control Plane/Hub status 与套餐目录 BFF |
| `termx-staging-daemon-companion.service` | `/run/termx-staging/daemon-companion.sock` | daemon device session、presence 与 signaling |
| `termx-staging-daemon.service` | `/run/termx-staging/daemon.sock` | 公开 core-v2/termx protocol daemon |

二进制位于 `/opt/termx-staging/bin`，Cloud 非秘密运行状态位于 `/var/lib/termx-staging`，Web 账号库位于 `/var/lib/termx-web-controller/accounts.db`。systemd 使用无登录权限的 `termx-staging` 用户。Companion session 写入 GNOME Keyring；keyring 解锁材料由 systemd `LoadCredential` 从服务器 root-only 文件加载，不进入仓库、进程参数或日志。

Next.js 16 使用 TermX 私有的 Node 24 LTS runtime `/opt/termx-staging/node/bin/node`，不替换服务器 `/usr/bin/node`，避免影响其他系统服务。

Next.js 不直连 Control Plane；它只访问 loopback Go BFF。套餐价格来自部署的 `plans.json`，未发布价格不会由页面推导：

```bash
ssh root@114.66.58.243 'curl -fsS http://127.0.0.1:41000/api/status'
ssh root@114.66.58.243 'curl -fsS http://127.0.0.1:41004/v1/catalog'
```

它只代理 Web 账号/订阅 BFF，不代理 signaling 或 terminal protocol，也不解释 terminal capability。

WEB002/WEB003 staging profile 在 `/login` 提供固定开发账号以及邮箱密码注册登录，在 `/account` 提供 Managed Free/Pro、测试 Checkout、密码修改和 AFF 推荐奖励。该 provider 不扣款；confirm 仍经 HMAC webhook transaction，只有首次有效事件才调用 Control Plane internal entitlement endpoint。Control Plane 更新 edge revision 并重新发布 Hub snapshot 后，订单、payment event 与邀请人 +15 天/被邀请人 +7 天奖励才在同一 SQLite 事务提交。浏览器 session 使用 HttpOnly、SameSite=Strict Cookie，登录/注册校验精确 Origin，所有已登录写请求同时校验 Origin 与 CSRF token。生产 OAuth、价格和支付 provider 未配置时保持禁用。

公网入口：

| URL | 用途 |
| --- | --- |
| `http://114.66.58.243:41100/` | Next.js 用户订阅 Landing Page |
| `http://114.66.58.243:41100/api/status` | Web Controller readiness/status |
| `http://114.66.58.243:41100/api/catalog` | 公开套餐目录投影 |
| `http://114.66.58.243:41100/runtime.json` | 不含有效 enrollment code 的 development client manifest |
| `http://114.66.58.243:41101` | Control Plane contract origin |
| `http://114.66.58.243:41102` | Hub signaling contract origin |

## 状态检查

```bash
ssh root@114.66.58.243 \
  'systemctl is-active termx-staging-cloud termx-staging-web-controller-bff termx-staging-web-controller termx-staging-daemon-companion termx-staging-daemon'

ssh root@114.66.58.243 \
  'curl -fsS -o /dev/null -w "control=%{http_code} hub=%{http_code}\n" \
    http://127.0.0.1:41001/healthz http://127.0.0.1:41002/healthz'

ssh root@114.66.58.243 'ss -lnup | grep 41003'
```

预期五个 unit 都是 `active`，两个 health response 是 `204`，TURN listener 为 `0.0.0.0:41003/udp`。

`ss` 看到 listener 只证明进程已绑定端口，不证明云厂商入站允许 UDP。上线或真机 Relay 验收前，必须在云安全组放行 `41003/udp`，并从服务器外发送探测，同时在实际公网网卡观察入站包：

```bash
ssh root@114.66.58.243 'tcpdump -nn -U -i ens18 udp port 41003'
printf test | nc -u -w1 114.66.58.243 41003
```

若 `ens18` 没有任何包，需继续区分探测源出口、云安全组与上游网络；不得把本机 `iptables` 放行、TURN listener 或已签发 Relay lease 冒充 `single_relay` 可用。2026-07-12 真机 5G 验收确认手机与 daemon 的 TURN allocation 均双向成功，最终 selected pair 为 `relay / host`，因此该服务器不存在 `41003/udp` 入站阻断。

## 无隧道客户端验证

```bash
curl -fsS http://114.66.58.243:41100/runtime.json -o runtime.json
```

pairing bundle 是 bearer terminal capability，仍须从 daemon owner 的安全渠道取得，不能从 Web Controller 公开下载。然后直接启动 development Companion，无需 SSH tunnel：

```bash
termx-cloud serve \
  --socket /absolute/owner-only-dir/companion.sock \
  --profile client-cloud007 \
  --dev-manifest /absolute/path/runtime.json
```

完成 `termx cloud login --device-code`、`termx pair import` 后启动 `termx`。2026-07-12 本机实测无需 tunnel 完成 `resolving`、`signaling`、`connecting`、`authorizing`、`connected`。

## SSH-only 客户端验证

先在本机或 `ssh al` 建立 Control Plane/Hub 隧道。示例使用本地 `42001/42002`，避免与本机 devcloud 冲突：

```bash
ssh -N \
  -L 42001:127.0.0.1:41001 \
  -L 42002:127.0.0.1:41002 \
  root@114.66.58.243
```

从服务器安全取得 `/var/lib/termx-staging/runtime.json` 和 `/var/lib/termx-staging/pairing.json`，权限保持 `0600`。在 runtime manifest 的副本中只把两个 URL 改为隧道地址：

```json
{
  "control_plane_url": "http://127.0.0.1:42001",
  "hub_url": "http://127.0.0.1:42002"
}
```

不得修改 `profile=staging-ssh` 或公网 Relay URL。启动 development Companion：

```bash
termx-cloud serve \
  --socket /absolute/owner-only-dir/companion.sock \
  --profile client-cloud006 \
  --dev-manifest /absolute/path/runtime.json
```

使用隔离的 `XDG_STATE_HOME`/`XDG_CONFIG_HOME` 执行：

```bash
TERMX_CLOUD_COMPANION_SOCKET=/absolute/owner-only-dir/companion.sock \
  termx cloud login --device-code

termx pair import --id public-staging --relay direct \
  --registry "$XDG_CONFIG_HOME/termx/connections.yaml" pairing.json

TERMX_CLOUD_COMPANION_SOCKET=/absolute/owner-only-dir/companion.sock termx
```

direct 验收必须经过 `resolving`、`signaling`、`connecting`、`authorizing`、`connected`。Relay 验收重新导入同一 bundle 并显式使用 `--relay relay_only`；该策略失败时不得改走 direct。测试完成后删除客户端 pairing bundle 副本。

2026-07-12 本机验收结果：direct 与显式 `relay_only` 均到达 `connected`；Control Plane/Hub 请求经 SSH tunnel，TURN 发布地址为 `114.66.58.243:41003/udp`。当前 terminal picker 只显示 `hub-p2p` transport，没有单独展示观测 path 文本，因此本次没有把 UI 文本当作 `single_relay` 路径证据；后续应补 packet/usage 或 path projection 自动化证据。

## 重启与 bootstrap

daemon 与 daemon Companion 共享 `/run/termx-staging`。替换 daemon 二进制时不要在 Companion 运行期间单独执行 daemon 的完整 `stop`/`start`，否则 systemd 可能删除仍在监听的 Companion socket 路径。需要完整停止时使用以下顺序：停止 daemon，重启 Companion并等待 `test -S /run/termx-staging/daemon-companion.sock` 成功，再启动 daemon；daemon 日志必须出现 `managed cloud presence starting`。

当前 staging cloud 使用内存 store，每次重启 `termx-staging-cloud.service` 都会轮换签名 key、enrollment code 并清除 account/device/presence。它不会自动复用旧 session。重启后按以下顺序操作：

1. 重启 cloud 和 daemon Companion。
2. 从 owner-only runtime manifest 读取新的一次性 enrollment code。
3. 以 `termx-staging` 用户执行 `termx cloud enroll`。
4. 重新生成 `/var/lib/termx-staging/pairing.json`。
5. 执行 `/opt/termx-staging/bin/render-public-http-manifest`，更新 `/var/www/termx-staging/runtime.json`。
6. 启动 daemon，并在客户端重新登录和导入新 bundle。

Web Controller BFF 重启不会清除 `/var/lib/termx-web-controller/accounts.db` 中的账号、session、订单或奖励；清库必须先停止 Next/BFF 并同时删除 SQLite 主文件、`-wal` 和 `-shm`。不要把有效 enrollment code、pairing bundle、keyring password 或 cloud session 写入本文、unit、shell history 和日志。生产 OAuth/TLS、托管数据库、高可用备份、真实支付 provider 与多区域部署仍是后续切片。

## 停止与清理

```bash
ssh root@114.66.58.243 \
  'systemctl disable --now termx-staging-daemon termx-staging-daemon-companion termx-staging-web-controller termx-staging-web-controller-bff termx-staging-cloud'

ssh root@114.66.58.243 \
  'rm -f /etc/nginx/conf.d/termx-public-http.conf && nginx -t && systemctl reload nginx'
```

删除 `/opt/termx-staging`、`/var/lib/termx-staging`、`/etc/termx-staging` 和对应 unit 会永久删除 pairing、daemon identity 与 keyring；只有明确废弃该 staging 环境时执行。
