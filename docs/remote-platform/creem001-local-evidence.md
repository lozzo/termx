# CREEM001 Local Evidence

## 已完成链路

- Creem Product、Discount、Checkout、Transaction 与 Subscription 均通过受限官方 API origin 的 Go client 读取；success redirect 不改变服务权限。
- Checkout、raw-body HMAC Webhook、后台轮询和运营立即对账最终进入同一个 normalized payment journal。
- PaymentAttempt 持久保存 checkout、transaction、discount、subscription reference、provider status、轮询次数、下次轮询时间和 revision。
- 运营立即对账要求 admin、CSRF、五分钟近期认证、原因和 request ID，并持久记录 attempt revision 边界。
- 已存在 Creem attempt 的订单不能由运营端手工标记支付；Creem 支付也不能由本地人工退款或撤销，反向状态必须来自 provider 核验。
- Operator UI 展示 Creem 引用、状态与轮询进度，并只对可核对的 Creem attempt 提供“Reconcile now”。

## 本地证据

2026-07-25 已通过：

```text
go test ./private/cloud/control-plane/commerce ./private/cloud/control-plane/creem ./private/cloud/web-controller ./private/cloud/controller
make test-private
make test
scripts/check-generated-code.sh
npm run typecheck && npm run build
MUXVIA_CREEM_E2E_BASE_URL=http://127.0.0.1:5197 npx playwright test e2e/creem001.spec.ts
```

Playwright 结果为 `1 passed`。证据截图：

- `.artifacts/creem001/operator-reconciliation.png`
- `.artifacts/creem001/operator-reconciliation-mobile.png`

桌面截图 SHA-256 为 `a3349e980c6bf11645751eb299ccb0f22e0a0a9b626efee92e52e3b63e51ee9e`，手机宽度截图 SHA-256 为 `f8a711fdcb1f1314b0548587350f7c06c9a4c134e2acf38cadbd78ae4e18c371`。390px viewport 的 document overflow 为 `0`。

## 未完成证据

真实 Creem Sandbox 的 Product ID、Webhook signing secret、Dashboard event subscription、test payment/refund/dispute 和线上 `/pay/creem` 投递仍是外部阻塞，详见 `creem001-sandbox-runbook.md`。因此本文件只证明本地纵向闭环，`CREEM001` 继续保持“进行中”。
