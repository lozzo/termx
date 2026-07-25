# OPSREL001 版本管理验收证据

## 完成范围

本切片完成 CLI/daemon 与 Android 的签名版本目录、stable/beta channel、兼容/强制/灰度/暂停/回滚和 Operator 页面。不建设 CDN、对象上传、Play Store 自动发布或通用制品平台。

## 真值与安全边界

- `release_artifacts` 是不可变签名 metadata；制品内容位于配置 allowlist 中的官方 HTTPS origin。
- `release_channel_heads` 是 active release、revision 与 paused 的唯一可变真值。普通激活只允许 version code 前进；低版本必须显式 rollback。
- SHA-256、download URL、target、版本、兼容下限、force deadline、rollout 与 changelog 都进入 deterministic Ed25519 payload。hash/URL/策略任一字段变化都会使签名失效。
- Controller 只读取 Ed25519 public key 和可信 origin；private key 只由 `muxvia-release-metadata` 从外部 `0600` PKCS#8 文件读取，输出和日志不包含 private key。
- 客户端 stable ID 只用于 `SHA-256(stable_id + release_id) mod 10000` 分桶，不写数据库。暂停 channel 不分发；兼容下限或强制截止时间覆盖 rollout。

## 真实测试矩阵

| 场景 | 证据 | 结果 |
| --- | --- | --- |
| Proto signed metadata 且无 private/token/artifact bytes | `TestReleaseArtifactContractCarriesSignedMetadataWithoutPrivateMaterial` | PASS |
| 真实文件摘要、PKCS#8 key、签名可验证且输出无私钥 | `TestRunSignsRealArtifactDigestWithoutPrivateKeyInOutput` | PASS |
| hash/signature 篡改拒绝 | `TestReleaseCatalogVerifiesSignatureMonotonicActivationRolloutAndRollback` | PASS |
| 非 allowlist HTTPS origin 拒绝 | 同上 | PASS |
| 隐式版本回退拒绝、channel revision CAS | 同上 | PASS |
| Android 稳定 rollout bucket、兼容下限强更 | 同上 | PASS |
| CLI force deadline 强更 | 同上 | PASS |
| pause/resume、显式 rollback、持久 audit | 同上 | PASS |
| admin publish/activate、readonly 拒绝、public resolve | `TestOperatorAPIEnforcesRoleCSRFRecentAuthAndPersistsSubscriptionAudit` | PASS |
| Operator publish/activate/pause/resume/rollback/audit | `e2e/opsrel001.spec.ts` | PASS |

`releasecatalog` 与 Operator API 测试使用临时真实 PostgreSQL。浏览器使用 Proto JSON fixture，只证明真实页面操作、状态投影和布局，不替代签名与事务测试。

## UI 证据

- `.artifacts/opsrel001/releases-desktop.png`
- `.artifacts/opsrel001/releases-mobile.png`

桌面 1366px 与移动 390px 均无横向溢出；签名输入、artifact history、active/paused 状态、操作按钮和 audit 在同一模块内可见。

## 发布工具示例

```sh
go run ./private/cloud/controller/cmd/muxvia-release-metadata \
  --file .artifacts/muxvia-linux-amd64 \
  --signing-key /secure/release-ed25519.pem \
  --key-id release-2026-01 \
  --release-id cli-linux-amd64-v1.0.0 \
  --product cli-daemon --channel stable \
  --version v1.0.0 --version-code 100 \
  --os linux --arch amd64 \
  --download-url https://releases.muxvia.com/cli/v1.0.0/muxvia-linux-amd64
```

命令输出是可提交给 Operator 的 signed Proto JSON。私钥内容不得写入 shell history、仓库、日志或 Controller 配置；实际发布环境应由 CI secret mount 提供临时 key 文件。

## 准入命令

```sh
./scripts/check-generated-code.sh
scripts/with-test-postgres.sh go test ./private/cloud/control-plane/releasecatalog ./private/cloud/control-plane/postgres ./private/cloud/web-controller ./private/cloud/controller/cmd/muxvia-release-metadata -count=1
scripts/with-test-postgres.sh go test -race ./private/cloud/control-plane/releasecatalog ./private/cloud/control-plane/postgres ./private/cloud/web-controller -count=1
npm run typecheck --workspace @muxvia/web-controller
npm run build --workspace @muxvia/web-controller
MUXVIA_OPSREL_E2E_BASE_URL=http://127.0.0.1:5174 npx playwright test e2e/opsrel001.spec.ts
GOFLAGS='-p=1' make test
make test-private
git diff --check
```

整仓默认并行公共测试仍可能触发 `docs/remote-platform/opsuser001-e2e.md` 已记录的既有 IPC 时序 flake；本切片继续使用不降低覆盖率的串行公共门禁，不跨范围修改 IPC。
