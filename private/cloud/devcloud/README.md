# Dev Local Cloud

`devcloud` 是 CLOUD002-CLOUD003 的单区域开发装配，不是生产部署模板。它复用现有 Control Plane 与 Hub 领域 service，并通过两个独立 loopback HTTP listener 强制请求经过序列化、认证、admission 和有界 stream。

```bash
make cloud-dev
```

入口以前台方式运行，响应 `SIGINT`/`SIGTERM`，并把当前地址、profile、固定开发账号标签和一次性 enrollment code 写入 `.artifacts/cloud-dev/runtime.json`。manifest 权限为 `0600`，不包含账号 session token、Hub signing private key、CapabilityGrant、DeviceIdentity private key 或 terminal payload。

Companion 必须显式传入该 manifest：

```bash
go run ./private/cloud/companion/cmd/termx-cloud serve \
  --socket /tmp/termx-cloud-client.sock \
  --profile client-dev \
  --dev-manifest .artifacts/cloud-dev/runtime.json
```

daemon Companion 使用另一个 socket/profile。默认无配置路径继续 fail closed；single Relay 和 Android 接线分别属于 CLOUD004-CLOUD005。

## Desktop Managed Direct

先构建 development `termx`，再在四个终端中启动 dev cloud、client Companion、daemon Companion 和远端 daemon。示例路径只服务本机开发测试：

```bash
make build
make cloud-dev
```

```bash
go run ./private/cloud/companion/cmd/termx-cloud serve \
  --socket /tmp/termx-cloud-client.sock \
  --profile client-dev \
  --dev-manifest .artifacts/cloud-dev/runtime.json
```

```bash
go run ./private/cloud/companion/cmd/termx-cloud serve \
  --socket /tmp/termx-cloud-daemon.sock \
  --profile daemon-dev \
  --dev-manifest .artifacts/cloud-dev/runtime.json
```

client 与 daemon 在同机测试时必须使用不同公开进程状态目录；这模拟两台机器，不是额外源码隔离：

```bash
export CLIENT_STATE=/tmp/termx-managed-client-state
export CLIENT_CONFIG=/tmp/termx-managed-client-config
export DAEMON_STATE=/tmp/termx-managed-daemon-state

TERMX_CLOUD_COMPANION_SOCKET=/tmp/termx-cloud-client.sock \
  XDG_STATE_HOME="$CLIENT_STATE" .artifacts/bin/termx cloud login --device-code

ENROLLMENT_CODE="$(sed -n 's/.*"enrollment_code": "\([^"]*\)".*/\1/p' .artifacts/cloud-dev/runtime.json)"
TERMX_CLOUD_COMPANION_SOCKET=/tmp/termx-cloud-daemon.sock \
  XDG_STATE_HOME="$DAEMON_STATE" .artifacts/bin/termx cloud enroll "$ENROLLMENT_CODE"

XDG_STATE_HOME="$DAEMON_STATE" .artifacts/bin/termx pair create \
  --label "Dev remote" --out /tmp/termx-managed-pairing.json

XDG_STATE_HOME="$CLIENT_STATE" XDG_CONFIG_HOME="$CLIENT_CONFIG" \
  .artifacts/bin/termx pair import --id dev-remote --relay direct \
  --registry "$CLIENT_CONFIG/termx/connections.yaml" /tmp/termx-managed-pairing.json
```

启动远端 daemon；`--cloud` 只增加 managed presence，本地 core listener 和 terminal lifecycle 仍独立存在：

```bash
TERMX_CLOUD_COMPANION_SOCKET=/tmp/termx-cloud-daemon.sock \
  XDG_STATE_HOME="$DAEMON_STATE" \
  .artifacts/bin/termx --socket /tmp/termx-managed-remote.sock daemon --cloud
```

最后启动 client TUI。terminal picker 会显示 `dev-remote`，连接阶段依次投影为 `resolving`、`signaling`、`connecting`、`authorizing`，成功后显示 `connected` 与 `direct`：

```bash
TERMX_CLOUD_COMPANION_SOCKET=/tmp/termx-cloud-client.sock \
  XDG_STATE_HOME="$CLIENT_STATE" XDG_CONFIG_HOME="$CLIENT_CONFIG" \
  .artifacts/bin/termx
```

`pairing.json` 包含短期 bearer capability，导入完成后应删除或通过安全通道转移。raw grant 只写入 client credential store；`connections.yaml` 不包含 grant、Hub URL 或账号 token。

定向准入：

```bash
cd private/cloud/devcloud
GOWORK=off go test ./... -count=1
```
