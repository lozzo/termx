# 单区域公网 Cloud staging 运维手册

## 定位

本手册记录 CLOUD006 在 `114.66.58.243` 的 SSH-only staging 装配。它用于从开发机验证真实 Hub signaling、WebRTC direct 和公网 UDP TURN，不是生产部署模板。

Control Plane、Hub 与 Web Controller 只监听服务器 loopback，通过 SSH port forwarding 访问。Relay 是唯一公网 cloud listener；terminal protocol、CapabilityGrant 和文件内容仍位于端到端 DTLS DataChannel，Web Controller/Hub/Relay 不接收这些内容。

## 已部署服务

| systemd unit | listener | 责任 |
| --- | --- | --- |
| `termx-staging-cloud.service` | `127.0.0.1:41001/tcp`、`127.0.0.1:41002/tcp`、`0.0.0.0:41003/udp` | 内存 Control Plane、Hub 与 lease-bound TURN |
| `termx-staging-web-controller.service` | `127.0.0.1:41000/tcp` | SSH-only 运维 health/status API |
| `termx-staging-daemon-companion.service` | `/run/termx-staging/daemon-companion.sock` | daemon device session、presence 与 signaling |
| `termx-staging-daemon.service` | `/run/termx-staging/daemon.sock` | 公开 core-v2/termx protocol daemon |

二进制位于 `/opt/termx-staging/bin`，非秘密运行状态位于 `/var/lib/termx-staging`。systemd 使用无登录权限的 `termx-staging` 用户。Companion session 写入 GNOME Keyring；keyring 解锁材料由 systemd `LoadCredential` 从服务器 root-only 文件加载，不进入仓库、进程参数或日志。

Web Controller 当前只提供运维 surface：

```bash
ssh root@114.66.58.243 'curl -fsS http://127.0.0.1:41000/v1/status'
```

它不代理登录、signaling 或 terminal protocol，也不解释 terminal capability。

## 状态检查

```bash
ssh root@114.66.58.243 \
  'systemctl is-active termx-staging-cloud termx-staging-web-controller termx-staging-daemon-companion termx-staging-daemon'

ssh root@114.66.58.243 \
  'curl -fsS -o /dev/null -w "control=%{http_code} hub=%{http_code}\n" \
    http://127.0.0.1:41001/healthz http://127.0.0.1:41002/healthz'

ssh root@114.66.58.243 'ss -lnup | grep 41003'
```

预期四个 unit 都是 `active`，两个 health response 是 `204`，TURN listener 为 `0.0.0.0:41003/udp`。

## 客户端验证

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

当前 staging cloud 使用内存 store，每次重启 `termx-staging-cloud.service` 都会轮换签名 key、enrollment code 并清除 account/device/presence。它不会自动复用旧 session。重启后按以下顺序操作：

1. 重启 cloud 和 daemon Companion。
2. 从 owner-only runtime manifest 读取新的一次性 enrollment code。
3. 以 `termx-staging` 用户执行 `termx cloud enroll`。
4. 重新生成 `/var/lib/termx-staging/pairing.json`。
5. 启动 daemon，并在客户端重新登录和导入新 bundle。

不要把 enrollment code、pairing bundle、keyring password 或 cloud session 写入本文、unit、shell history和日志。生产 OAuth/TLS、持久数据库、域名证书、Web Controller 完整账号管理 UI 与多区域部署仍是后续切片。

## 停止与清理

```bash
ssh root@114.66.58.243 \
  'systemctl disable --now termx-staging-daemon termx-staging-daemon-companion termx-staging-web-controller termx-staging-cloud'
```

删除 `/opt/termx-staging`、`/var/lib/termx-staging`、`/etc/termx-staging` 和对应 unit 会永久删除 pairing、daemon identity 与 keyring；只有明确废弃该 staging 环境时执行。
