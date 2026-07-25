# OPSCOM001 订单、订阅与优惠码验收

日期：2026-07-25

## 完成范围

- 订单固化 billing cadence、原价、折扣、实付金额和优惠映射快照；价格历史不随套餐或优惠停用改变。
- 所有 provider、测试 provider 和 operator 人工收款/退款/撤销共用 `NormalizedPaymentEvent` journal；相同 event 精确重放返回原结果，冲突 payload fail closed。
- operator 赠送、延期和变更套餐使用独立 `SubscriptionAdjustment`，在同一 PostgreSQL 事务中 CAS Subscription、重算 Entitlement 并写 operator audit，不创建虚假已支付订单。
- 优惠支持固定金额或比例、套餐范围、有效期、总次数和每账号次数；checkout 在锁定 promotion 的事务内创建订单与 reservation，支付成功兑换，失败或有界超时释放。
- Creem 拥有正式 Product、Discount、Checkout、Transaction、退款和 provider subscription；Muxvia 只登记不可变 product/discount mapping，保存经济快照、normalized journal、Subscription/Entitlement 和审计。
- 订阅延期或升级改变当前账期时，Relay quota 在无活动 lease 时保留已结算 usage 并更新 period/limit；存在活动 lease 时继续 fail closed。

## 自动化证据

- `scripts/check-generated-code.sh`。
- `go test ./proto/cloudpb`。
- 在 `private/cloud/control-plane`、`private/cloud/controller`、`private/cloud/web-controller` 分别执行 `scripts/with-test-postgres.sh env GOWORK=off go test ./... -count=1`。
- `npm run typecheck` 与 `npm run build`。
- `npx playwright test e2e/opscom001.spec.ts`：真实 Controller、PostgreSQL 和双 Edge；从 Operator UI 发布 configured catalog、登记 Creem 优惠映射、执行订阅 adjustment，再从账号 UI 输入优惠码并由 development provider 支付，最后回到 Operator 查看折后金额和 event timeline。

## 事务证据

- 两个账号并发争抢总额度为 1 的优惠 reservation，只有一个 checkout 成功。
- 支付成功后 redemption 从 `RESERVED` 推进到 `REDEEMED` revision 2；相同 payment event 重放不重复延长订阅。
- operator refund 使用相同 journal，精确 `request_id` 重放幂等，修改 reason 的冲突重放被拒绝。
- 最终真实 E2E 数据包含 1 order、1 normalized event、1 promotion、1 redeemed redemption 和 1 subscription adjustment。
- adjustment 与支付提交后，两个 Hub 的 signed policy projection revision 均从 1 推进到 3。

## UI 证据

- 桌面：`.artifacts/opscom001/operator-desktop.png`。
- 移动端 390x844：`.artifacts/opscom001/operator-mobile.png`。
- 移动端 promotion 表单单列排列，无横向溢出；订单列表直接展示金额、状态、操作原因和事件时间线。

## 安全边界

- Operator mutation 继续要求独立 listener、admin、同源 CSRF 和五分钟 recent-auth。
- 测试 provider 仅在显式 development 配置启用；contact plan 不允许创建订单。
- Creem API key、Webhook secret、operator token、账号密码和 provider raw payload 未写入仓库、截图、日志或 Proto projection。
- `CREEM001` 才接入 Creem sandbox API、`POST /pay/creem` raw-body HMAC 和有界轮询；本切片没有用浏览器 success redirect 开通 Entitlement。
