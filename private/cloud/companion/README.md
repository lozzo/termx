# Private Cloud Companion

`termx-cloud` 是桌面/headless managed cloud 的固定闭源 sidecar，不是通用插件。公开 `termx` 进程继续拥有 DeviceIdentity private key、CapabilityGrant、WebRTC/DTLS、DataChannel 和 terminal protocol。

## Build

Companion 自报版本和渠道必须与签名 manifest 完全一致：

```bash
cd private/cloud/companion
go build \
  -ldflags '-X main.companionVersion=v1.2.3 -X main.buildChannel=stable' \
  -o ./dist/termx-cloud ./cmd/termx-cloud
```

正式 `termx` CLI 只嵌入 release public key，不嵌入 private key：

```bash
(cd ../../.. && go build \
  -ldflags '-X main.termxBuildVersion=v1.2.3 -X main.cloudReleaseRootKeyID=release-2026 -X main.cloudReleaseRootPublicKey=BASE64_ED25519_PUBLIC_KEY' \
  -o ./dist/termx ./cmd/termx)
```

## Release Artifact

release private key 必须是仓库外的 Ed25519 PKCS#8 PEM：

```bash
cd private/cloud/companion
go run ./cmd/termx-cloud-release \
  --binary ./dist/termx-cloud \
  --signing-key /secure/release-ed25519.pk8.pem \
  --key-id release-2026 \
  --channel stable \
  --version v1.2.3 \
  --os darwin \
  --arch arm64 \
  --download-url https://releases.termx.dev/cloud-companion/artifacts/termx-cloud_1.2.3_darwin_arm64.tar.gz \
  --out ./dist/release
```

发布服务需要把签名 manifest 暴露到 installer 的固定 channel/platform/version 路径，并维护 `latest.json`。正式 key custody、artifact origin、notice/license 审查进入 LIC001 与发布流水线。

当前 `cloudservice.NewUnconfiguredAdapter` 只用于开发和 installer smoke；未注入生产 OAuth/TLS Control Plane/Hub adapter 时稳定 fail closed，不访问归档 Hub 或 session-token API。
