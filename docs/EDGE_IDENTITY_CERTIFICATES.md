# Edge 身份证书

本文说明 Edge 与 Controller 之间的 mTLS 身份，以及它和公网域名证书、客户端授权的区别。

## 三类凭据

| 凭据 | 用途 | 签发或配置方 | 过期后的影响 |
| --- | --- | --- | --- |
| Edge 公网域名证书 | 手机、daemon 访问 Edge 公网入口时证明域名 | 运营人员上传 certificate profile | 新建到该域名的 TLS 连接失败；已建立连接通常不立即断开 |
| EdgeIdentity mTLS 证书 | Edge 连接 Controller 时证明自己的 Edge ID | Controller 内部 Edge CA | Edge 不能新建 EdgeControl；现有控制流在断开前不重新验这张证书 |
| 客户端 CapabilityGrant | 限制客户端对 daemon 的权限和授权期限 | daemon 在配对流程中签发 | 客户端连接时提示授权已结束，需要重新生成或扫码 |

公网域名证书更新不会改变客户端授权，也不会改变 EdgeIdentity。`grant TTL` 已经负责客户端授权期限，本次改动不修改配对链接、二维码或 grant 的语义。

## 正常签发与续期

首次安装时，Edge 在本机生成 P-256 私钥和只包含 `spiffe://anytty.com/edge/<edge-id>` URI SAN 的 CSR。Controller 校验安装 claim 和 CSR 后，使用内部 Edge CA 签发 90 天、仅用于 `clientAuth` 的 EdgeIdentity 证书。私钥从不离开 Edge。

Edge 每次启动都会读取当前证书的 `NotAfter`。进入到期前 30 天的窗口后：

1. Edge 在现有、已经通过 mTLS 的 EdgeControl v8 中生成新的 P-256 私钥和 CSR。
2. 请求携带当前 TLS 证书的 SHA-256 指纹；Controller 要求它与这条控制流实际使用的证书一致。
3. Controller 校验 Edge 仍启用、CSR 只有准确的 Edge URI 身份，再签发新的 90 天证书并记录签发审计。
4. Edge 校验 CA 链、`clientAuth`、唯一 URI SAN、证书和新私钥匹配、响应指纹和有效期一致。
5. Edge 把证书链和新私钥作为一个 `0600` 文件原子写入 `managed-identity.pem`，然后切换内存中的 TLS credential 并回报 applied 审计。

切换只影响未来的 TLS 握手。当前 EdgeControl、AgentGateway、ClientGateway、WebRTC 和手机会话不会因为正常轮换而主动断开。EdgeControl 下次重连时才使用新证书。

若网络或 Controller 暂时不可用，Edge 从 1 分钟开始重试，最长退避到 1 小时；旧证书在有效期内继续使用。错误、错签或无法持久化的新证书不会替换当前身份。

## 已过期恢复

已经过期的 EdgeIdentity 无法通过 Controller 的 mTLS 准入，所以不能走正常自动续期。恢复使用独立的高熵 token，而不是放宽 mTLS 或复用安装 token：

- 只有最近重新认证的 admin 可以为离线、启用中的指定 Edge 创建 token。
- token 只显示一次、10 分钟有效；数据库只保存 SHA-256 摘要。
- token 同时绑定 Edge ID 和首次提交的 CSR，只能原子消费一次。
- Edge 仍在本机生成全新的私钥；Controller 只签 CSR。
- 签发结果必须通过与正常续期相同的 CA、身份、用途、指纹和有效期校验，才会写入 managed state。
- create、consume、recovery issuance 都写运营审计，但不记录 token、证书正文或私钥。

恢复 token 放入 Edge 配置的顶层字段：

```yaml
identity_recovery_token: REPLACE_ONE_TIME_TOKEN
```

启动成功后 Edge 会自动删除该字段。若恢复请求失败或 token 已过期，保留旧 identity 文件，重新创建 token 后再试。不得从另一台 Edge 复制 `managed-identity.pem`。

## 发布约束

EdgeControl v8 不兼容 v7。升级需要先完成 migration 12，再在同一维护窗口升级 Controller 和 Edge。升级后的旧 30 天身份若已经进入 30 天续期窗口，Edge 一连上 v8 Controller 就会立即换成新的 90 天证书。
