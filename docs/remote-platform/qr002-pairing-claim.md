# QR002 daemon 短码配对验收

> 本文记录 QR002 完成时的单 Route seed 实现事实，不再作为当前多 Route 产品契约。当前决策和后续删除项以 `workflow.md` 与 `pairing-route-management-design.md` 为准。

## 结论

`muxvia pair create` 的默认二维码与 `--text` 已切换为 `MXP1-...` portable claim code。静态载荷是 deterministic `PairingClaimOfferV1`，只包含 128-bit claim、DeviceID、DeviceIdentity public key、十分钟有效期和一个首连 Route seed；不包含 PairingTicket、scope、terminal ID、CapabilityGrant 或客户端 key。

完整 `EndpointBootstrapBundleV2` 继续由 DeviceIdentity 签名并由 `AccessStore` 持久登记，但只保存在 owning daemon 的 claim 内存记录中。客户端建立 Direct 或 Cloud managed WebRTC DataChannel、验证 DeviceHello 并提交 ClientAccessIdentity proof 后，daemon 才复用原有 PairingTicket 原子兑换事务，并在 `PairingAccepted` 中端到端返回完整 bundle 与 client-bound grant。

## 真值与失败条件

- `AccessStore` 仍是 PairingTicket digest、scope ceiling、client key binding、grant、delivery receipt 和撤销的唯一持久真值。
- claim map 只存在于 owning daemon 进程内，以 claim SHA-256 digest 索引；daemon 重启后未兑换 claim 失效，不从持久 state 恢复。
- 未消费 claim 到期后拒绝；成功消费后只有同一 client key 可在 delivery grace 内恢复丢失响应，其他 key 返回 consumed。
- offer 的 DeviceID/public key 必须匹配当前 daemon；proof 绑定 offer canonical bytes、当前 auth session、server/client nonce 和实际 DTLS/local Unix channel binding。
- Direct claim 只携带一个 signaling/ICE-TCP locator；Cloud claim 只携带 target DeviceID。Cloud signaling 请求不携带 claim、bundle 或 grant。

## 客户端链路

```text
CLI/API create
  -> AccessStore IssuePairingClaim
  -> MXP1 claim code / QR
  -> Go Client Engine ImportPairing
  -> Direct or Cloud managed pairing peer
  -> DeviceHello + PairingOpen(claim, client proof)
  -> daemon ResolvePairingClaim + RedeemPairingBundle
  -> PairingAccepted(bundle, grant, receipt)
  -> platform secure credential bind
  -> full bundle -> Endpoint registry commit
```

Android/未来平台 binding 仍只有一个 `ImportPairingRequest.portable_payload`。相机扫描和无摄像头手工输入传递同一个 `MXP1-...` 字符串；Kotlin/TypeScript 不解析 claim、Route、proof、bundle 或 grant。

## 验收证据

- Proto descriptor 固定 claim offer、Route seed、`PairingOpen.pairing_claim_offer=6` 和 `PairingAccepted.pairing_bundle=5`，并拒绝 claim 中出现 ticket/scope/grant/terminal 字段。
- 安全 harness 覆盖 128-bit、十分钟、单次、错误 client、错误 daemon、过期、同 key 丢响应恢复和 daemon 重启失效。
- 真实 Pion ICE-TCP Direct connector 完成 claim 兑换并返回签名 bundle。
- Cloud managed connector 完成 signaling、DataChannel claim 兑换，并证明 Cloud 请求不含 claim/bundle。
- CLI `--text` 的实际二维码在 Medium 纠错级别不高于 QR Version 10；PNG 保持正方形和 owner-only 文件权限。
- `make test`、`make test-clients`、generated/descriptor 检查与 `make test-android` 通过。
