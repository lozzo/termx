# Dev Local Cloud

`devcloud` 是 CLOUD002 的单区域开发装配，不是生产部署模板。它复用现有 Control Plane 与 Hub 领域 service，并通过两个独立 loopback HTTP listener 强制请求经过序列化、认证、admission 和有界 stream。

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

daemon Companion 使用另一个 socket/profile。默认无配置路径继续 fail closed；Relay、Pion、TUI 和 Android 接线分别属于后续 CLOUD003-CLOUD005。

定向准入：

```bash
cd private/cloud/devcloud
GOWORK=off go test ./... -count=1
```
