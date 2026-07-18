# 单区域公网 Cloud staging 运维手册

## 定位

本手册记录 CLOUD006/CLOUD007 在 `114.66.58.243` 的 staging 装配。它用于从开发机验证真实 Hub signaling、WebRTC direct 和公网 UDP TURN，不是生产部署模板。

owning Control Plane 与 Hub 仍只监听服务器 loopback。React Web Controller 由 Nginx 直接托管，`41100/api/*` 同源转发到 Control Plane；Nginx 在 `41100-41102/tcp` 提供无隧道公网 HTTP staging，Relay 使用 `41003/udp`。terminal protocol、CapabilityGrant 和文件内容仍位于端到端 DTLS DataChannel，Control Plane/Hub/Relay 不接收这些内容。

公网 HTTP 只允许 staging 测试账号和数据。Web 账号、bcrypt 密码摘要、浏览器 session、订单、AFF 归因、奖励与审计保存在 SQLite；这些内容仍会经过明文 HTTP，禁止真实用户凭据、真实 terminal 数据或生产使用。上线前必须切换域名 HTTPS/TLS profile。

## 已部署服务

| systemd unit | listener | 责任 |
| --- | --- | --- |
| `termx-staging-cloud.service` | `127.0.0.1:41001/tcp`、`127.0.0.1:41002/tcp`、`0.0.0.0:41003/udp` | Control Plane 浏览器/edge API、Hub 与 lease-bound TURN |
| `termx-staging-daemon-companion.service` | `/run/termx-staging/daemon-companion.sock` | daemon device session、presence 与 signaling |
| `termx-staging-daemon.service` | `/run/termx-staging/daemon.sock` | 当前账号名下的 managed core-v2/termx protocol daemon |

二进制位于 `/opt/termx-staging/bin`，React 静态文件位于 `/opt/termx-staging/web/dist`，Cloud 非秘密运行状态与 Web 账号库位于 `/var/lib/termx-staging`。systemd 使用无登录权限的 `termx-staging` 用户。Companion session 写入 GNOME Keyring；keyring 解锁材料由 systemd `LoadCredential` 从服务器 root-only 文件加载，不进入仓库、进程参数或日志。CLOUD013 后，development Companion 已内嵌 `staging-public-http` 非秘密 manifest，unit 不再传入 `--dev-manifest` 或等待 `/var/lib/termx-staging/runtime.json`；该 runtime 文件只属于 Cloud supervisor 与公开投影生成，不得覆盖 Companion 构建期配置。

Linux `termx` 与相邻 `termx-cloud` 必须来自同一次 `build-cloud-test`，摘要在构建期绑定，且两个文件都归运行它们的 `termx-staging` 用户所有；`/opt/termx-staging/bin` 目录和 `termx-cloud-staging` supervisor 仍由 root 拥有。不得把独立 public `termx`、旧 Companion 或 root-owned development Companion 混入这对产物，也不得放宽当前用户 ownership/SHA 校验。

managed daemon 的 DeviceIdentity、history 和 runtime state 固定使用 `/var/lib/termx-staging/managed-daemon-state`。历史测试目录 `/var/lib/termx-staging/daemon-state` 保留为旧设备状态，不参与当前 enrollment；一个已经绑定其他账号的 DeviceIdentity 不能由新账号接管，也不得通过删除私钥或重启 Control Plane 绕过 ownership conflict。

React 只在构建期使用 Vite，服务器没有 Web Controller Node 进程。套餐价格由 Control Plane 从部署的 `plans.json` 读取，未发布价格不会由页面推导：

```bash
ssh root@114.66.58.243 'curl -fsS http://127.0.0.1:41001/api/status'
ssh root@114.66.58.243 'curl -fsS http://127.0.0.1:41001/api/catalog'
```

浏览器 Session Cookie、CSRF、账号、订阅和 AFF API 由 Control Plane 直接拥有；React 不能读取 HttpOnly bearer，也不解释 terminal capability。

### `cn-fast` SSH endpoint

`cn-fast` 是独立的 SSH endpoint，连接 `root@114.66.58.243` 后固定执行 `/usr/local/bin/termx daemon stdio-proxy`。它使用 root 当前用户 daemon 与默认 `/run/user/0/termx-v2-wire4.sock`，不启用 Cloud presence，也不依赖上表两个 managed daemon unit。`remote_socket: auto` 必须由服务器当前版本的 `stdio-proxy` 解析为远端默认 socket，不能创建名为 `auto` 的字面 socket。

2026-07-14 已把 `/usr/local/bin/termx` 更新为 CLI008 Linux/amd64 构建，清理旧 wire3 daemon 和 16 个异常 `stdio-proxy`，并通过带 runtime record 的 `termx daemon start` 启动 PID 可验证的 wire4 daemon。验证命令：

```bash
termx --timeout 10s endpoint test cn-fast --json
termx --timeout 10s file list cn-fast / --limit 20 --json
ssh root@114.66.58.243 '/usr/local/bin/termx --timeout 5s daemon status --json'
```

WEB002/WEB003 staging profile 在 `/login` 提供固定开发账号以及邮箱密码注册登录，在 `/account` 提供 Managed Free/Pro、测试 Checkout、密码修改和 AFF 推荐奖励。该 provider 不扣款；confirm 仍经 HMAC webhook transaction，只有首次有效事件才调用 Control Plane internal entitlement endpoint。Control Plane 更新 edge revision 并重新发布 Hub snapshot 后，订单、payment event 与邀请人 +15 天/被邀请人 +7 天奖励才在同一 SQLite 事务提交。浏览器 session 使用 HttpOnly、SameSite=Strict Cookie，登录/注册校验精确 Origin，所有已登录写请求同时校验 Origin 与 CSRF token。生产 OAuth、价格和支付 provider 未配置时保持禁用。

CLOUD012 起，TUI 不再自动兑换固定账号，而是请求短期设备码，由用户在 `/device?code=...` 使用已有 Web Session 审批。CLOUD015 起，Official Android 不打开系统浏览器：App 可以显示短码供用户在 Web 输入，也可以扫描账号中心生成的一次性二维码；扫码后 Web 必须展示手机 metadata 并再次批准。短码和二维码都只是活动 Flow locator，App 原生层还必须持有未进入 WebView 的高熵 flow credential 才能领取 edge session。密码、浏览器 Cookie 与 edge token 不进入 TUI/App UI；成功登录后的目录、resolve、signaling、direct 和 Relay lease 只访问 Hub。

## 注册、登录与添加节点

1. 打开 `http://114.66.58.243:41100/login`，使用测试邮箱注册并进入账号中心。公网 HTTP staging 禁止使用真实密码。
2. TUI/CLI 执行 `termx cloud login`，打开命令输出的 Web 地址并批准设备码。Official Android 在 Settings / Account 选择 Web 激活：可以把 App 短码输入 Web，也可以扫描账号中心生成的二维码；Web 显示手机名称和平台后仍需再次批准。
3. 在 Web 账号中心的 Nodes 页面选择 `Enroll daemon`，取得十分钟有效、仅使用一次的 enrollment code。页面直接提供完整命令、倒计时和公网 HTTP staging 可用的复制 fallback；同一账号生成新码会废弃尚未使用的旧码。
4. 在 daemon owner 机器执行 `termx cloud enroll CODE`，随后执行 `termx daemon restart --cloud`。daemon 与客户端必须属于同一账号，Hub 不允许跨账号枚举或连接节点。
5. 登录后的 App/TUI 执行节点刷新，从 Hub 内存目录取得同账号 daemon 与 active Presence。目录可见不表示具备 terminal 权限；未配对节点必须显示“需要配对”。
6. daemon owner 在交互式终端执行 `termx pair create`，默认显示可由 App 扫描的二维码。脚本环境必须显式使用 `--raw`，写文件必须显式使用 `--out OWNER_ONLY_PATH`。CapabilityGrant 仍只在 DTLS DataChannel 内由 owning daemon 验证。

已移除的 daemon 不需要删除本地 DeviceIdentity。持有原私钥的 daemon 可以使用新 enrollment code 重新注册；Control Plane 会先撤销旧账号 access/refresh session、Hub Presence 与 Web 投影，再恢复或迁移 ownership。不同 public key 不能占用原 DeviceID。`termx cloud status` 分别展示 account session 与 daemon enrollment，`termx cloud node status` 只检查当前 daemon 身份。

### TUI 导入 daemon 访问凭据

`termx cloud login` 只把当前 TUI/CLI 注册成账号客户端，使它可以从 Hub 发现同账号 daemon；它不会自动获得任何 terminal 或文件权限。访问某个 daemon 必须再导入该 daemon 自己签发的 capability bundle。

daemon 与 TUI 不在同一台机器时，推荐通过已有 SSH 信任链直接传递，不让 bearer bundle 落盘：

```bash
ssh user@daemon-host 'termx pair create --raw --ttl 24h' \
  | termx pair import - --id build-daemon --relay auto
```

也可以由 daemon owner 显式写入 owner-only 文件，再通过可信渠道交给 TUI 用户导入：

```bash
# daemon owner
termx pair create --out pairing.json --ttl 24h

# TUI client
termx pair import pairing.json --id build-daemon --relay auto
termx endpoint show build-daemon
termx
```

`--id` 是当前客户端自己的 endpoint 别名，不会修改 daemon 身份；`--relay` 只选择到达该 daemon 的 transport 策略。bundle 内的 CapabilityGrant 由 daemon 在端到端 DataChannel 内验证，Web、Control Plane 和 Hub 都不能代发、存储或查看它。导入成功后应删除客户端侧临时 bundle 文件。

## 设备移除

账号中心的 Cloud 设备列表同时管理 client access 与 daemon node，两者不能混成同一种在线状态：

- 移除手机、TUI 或未来 GUI client 后，Hub 内存投影立即拒绝该 device ID 的目录、resolve 和 signaling；App 下一次刷新清除本机 edge session并回到未登录状态。该操作不删除 App 本地保存的 daemon CapabilityGrant。
- 移除 daemon 后，Hub 关闭它的 Presence 和尚未完成的 signaling session，并拒绝新的 managed connection；账号目录不再返回该 daemon。已经完成 DTLS/DataChannel 建连的 peer session 不经过 Hub，当前切片不伪造“服务端已强制切断”的状态。
- daemon-owned CapabilityGrant 与账号目录是两类真值。移除 daemon 不把 Cloud 服务变成 terminal capability owner；如果该设备仍可通过免费 local/SSH 路径到达，授权仍由 daemon 自己判断。

Control Plane 可以在 edge session 与 Hub 授权快照有效期内中断；已有客户端后续建立 direct 或 single Relay 不同步回源。新登录、订阅/能力变化、节点 enrollment 和下一次快照刷新仍由 Control Plane 负责。

公网入口：

| URL | 用途 |
| --- | --- |
| `http://114.66.58.243:41100/` | React 用户订阅 Landing Page |
| `http://114.66.58.243:41100/api/status` | Web Controller readiness/status |
| `http://114.66.58.243:41100/api/catalog` | 公开套餐目录投影 |
| `http://114.66.58.243:41100/runtime.json` | 不含有效 enrollment code 的 development client manifest |
| `http://114.66.58.243:41101` | Control Plane contract origin |
| `http://114.66.58.243:41102` | Hub signaling contract origin |

## 状态检查

```bash
ssh root@114.66.58.243 \
  'systemctl is-active termx-staging-cloud nginx'

ssh root@114.66.58.243 \
  '/usr/local/bin/termx --timeout 5s daemon status --json'

ssh root@114.66.58.243 \
  'curl -fsS -o /dev/null -w "control=%{http_code} hub=%{http_code}\n" \
    http://127.0.0.1:41001/healthz http://127.0.0.1:41002/healthz'

ssh root@114.66.58.243 'ss -lnup | grep 41003'
```

预期 Cloud、Nginx、managed Companion 与 managed daemon 均为 `active`，root 当前用户 SSH daemon 为 `running`，两个 health response 是 `204`，TURN listener 为 `0.0.0.0:41003/udp`。root SSH daemon 与 `termx-staging` managed daemon 使用独立用户、socket、state 和 DeviceIdentity，不能互相替代。

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
  --registry "$XDG_CONFIG_HOME/termx/endpoints.yaml" pairing.json

TERMX_CLOUD_COMPANION_SOCKET=/absolute/owner-only-dir/companion.sock termx
```

direct 验收必须经过 `resolving`、`signaling`、`connecting`、`authorizing`、`connected`。Relay 验收重新导入同一 bundle 并显式使用 `--relay relay_only`；该策略失败时不得改走 direct。测试完成后删除客户端 pairing bundle 副本。

2026-07-12 本机验收结果：direct 与显式 `relay_only` 均到达 `connected`；Control Plane/Hub 请求经 SSH tunnel，TURN 发布地址为 `114.66.58.243:41003/udp`。当前 terminal picker 只显示 `hub-p2p` transport，没有单独展示观测 path 文本，因此本次没有把 UI 文本当作 `single_relay` 路径证据；后续应补 packet/usage 或 path projection 自动化证据。

## 重启与 bootstrap

daemon 与 daemon Companion 共享 `/run/termx-staging`。替换 daemon 二进制时不要在 Companion 运行期间单独执行 daemon 的完整 `stop`/`start`，否则 systemd 可能删除仍在监听的 Companion socket 路径。需要完整停止时使用以下顺序：停止 daemon，重启 Companion并等待 `test -S /run/termx-staging/daemon-companion.sock` 成功，再启动 daemon；daemon 日志必须出现 `managed cloud presence starting`。

当前 staging cloud 持久化四类安全状态：`security-directory.json` 保存账号/设备 ownership 与 daemon public key，`control-plane-authority.json` 保存稳定 Ed25519 authority，`hub-policy.snapshot` 保存 Hub 已验签 policy，`refresh-sessions.json` 只保存 account/device refresh secret 的 SHA-256 和绑定 metadata。四个文件均为 `termx-staging:termx-staging`、`0600`。

常规重启不再重新 enrollment、登录或生成 pairing。按以下顺序操作：

1. 重启 `termx-staging-cloud.service`。
2. 重启 `termx-staging-daemon-companion.service`；unit 的 `ExecStartPre` 会删除 preserved runtime directory 中的 stale socket。
3. 同时满足 `test -S /run/termx-staging/daemon-companion.sock` 和 `termx cloud status --json` 成功后，再启动 `termx-staging-daemon.service`。
4. daemon 日志必须出现新的 `managed cloud presence starting`，且不得出现 `UNAUTHENTICATED` 或 `COMPANION_MISSING`。
5. 执行 `/opt/termx-staging/bin/render-public-http-manifest` 更新公开非秘密 runtime 地址；不要公开 owner-only enrollment code。

从旧内存 authority 版本首次迁移到 CLOUD018 时，旧签名 edge token 无法跨 authority 更换复用，需要对服务器 daemon 执行一次 enrollment，并让现有 App/TUI 重新登录一次。四个 durable 文件建立后，后续 supervisor/Hub 重启不得再执行该迁移步骤。

Control Plane 重启不会清除 `/var/lib/termx-staging/accounts.db` 中的账号、浏览器 session、订单或奖励，也不会清除上述四个安全文件。清库必须先停止 `termx-staging-cloud`，并明确决定是否同时删除 SQLite、security directory、authority、Hub snapshot 和 refresh hash；只删除其中一部分会造成 fail-closed 的身份不一致。不要把有效 enrollment code、pairing bundle、keyring password、edge token 或 refresh secret 写入本文、unit、shell history 和日志。生产 OAuth/TLS、托管数据库、高可用备份、authority 轮换、真实支付 provider 与多区域部署仍是后续切片。

## 停止与清理

```bash
ssh root@114.66.58.243 \
  'systemctl disable --now termx-staging-daemon termx-staging-daemon-companion termx-staging-cloud'

ssh root@114.66.58.243 \
  'rm -f /etc/nginx/conf.d/termx-public-http.conf && nginx -t && systemctl reload nginx'
```

删除 `/opt/termx-staging`、`/var/lib/termx-staging`、`/etc/termx-staging` 和对应 unit 会永久删除 pairing、daemon identity 与 keyring；只有明确废弃该 staging 环境时执行。
