# Private Cloud Companion

`muxvia-cloud` 是桌面/headless managed cloud 的固定闭源 sidecar，不是通用插件。公开 `muxvia` 进程继续拥有 DeviceIdentity private key、CapabilityGrant、WebRTC/DTLS、DataChannel 和 terminal protocol。

## Build

Companion 自报版本和渠道必须与签名 manifest 完全一致：

```bash
cd private/cloud/companion
go build \
  -ldflags '-X main.companionVersion=v1.2.3 -X main.buildChannel=stable' \
  -o ./dist/muxvia-cloud ./cmd/muxvia-cloud
```

正式 `muxvia` CLI 只嵌入 release public key，不嵌入 private key：

```bash
(cd ../../.. && go build \
  -ldflags '-X main.muxviaBuildVersion=v1.2.3 -X main.cloudReleaseRootKeyID=release-2026 -X main.cloudReleaseRootPublicKey=BASE64_ED25519_PUBLIC_KEY' \
  -o ./dist/muxvia ./cmd/muxvia)
```

## Release Artifact

release private key 必须是仓库外的 Ed25519 PKCS#8 PEM：

```bash
cd private/cloud/companion
go run ./cmd/muxvia-cloud-release \
  --binary ./dist/muxvia-cloud \
  --signing-key /secure/release-ed25519.pk8.pem \
  --key-id release-2026 \
  --channel stable \
  --version v1.2.3 \
  --os darwin \
  --arch arm64 \
  --download-url https://releases.muxvia.dev/cloud-companion/artifacts/muxvia-cloud_1.2.3_darwin_arm64.tar.gz \
  --out ./dist/release
```

发布服务需要把签名 manifest 暴露到 installer 的固定 channel/platform/version 路径，并维护 `latest.json`。正式 key custody、artifact origin、notice/license 审查进入 LIC001 与发布流水线。

当前 `cloudservice.NewUnconfiguredAdapter` 是无显式 dev/production adapter 时的默认 fail-closed 边界，也用于 installer smoke；它不访问归档 Hub 或 session-token API。

## Dev Local

仓库根目录执行 `make cloud-dev` 会启动两个独立 loopback listener，并写入 `.artifacts/cloud-dev/runtime.json`。development build 只有收到显式 manifest 才启用该 adapter：

```bash
go run ./private/cloud/companion/cmd/muxvia-cloud serve \
  --profile client-dev \
  --dev-manifest .artifacts/cloud-dev/runtime.json
```

client 与 daemon 必须使用不同 `--profile` 和 IPC socket。无 `--dev-manifest`、stable build 或 installer smoke 始终装配 `UnconfiguredAdapter`；dev-local 明文 HTTP、固定账号和一次性 enrollment code 不是生产配置。
