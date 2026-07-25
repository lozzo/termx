# Creem Sandbox Runbook

## 管理边界

Creem 是外部支付资源的 owner：Product、Discount、Checkout、Transaction、退款和 provider subscription 均在 Creem Dashboard 管理。Muxvia 不复制一套支付后台，只保存以下产品真值：

- 不可变套餐版本到 Creem 月付/年付 Product ID 的映射；
- Muxvia 优惠活动到已核对 Creem Discount Code/ID 的映射；
- 下单时的套餐、币种、原价、折扣和应付金额快照；
- normalized payment journal、Muxvia Subscription、Entitlement 和审计记录。

浏览器 success redirect、客户端自报状态和 `subscription.active` 都不能开通权限。只有服务端核验的 paid transaction 或验签后再经服务端核验的 `subscription.paid` 可以进入同一个 journal。

## Creem Dashboard

1. 在 Test Mode 分别创建月付和年付 recurring Product。Creem Product 的 billing period 固定，因此不能让两个计费周期共用一个 Product ID。
2. 需要优惠时，在 Creem 创建 Discount。Muxvia Operator 的“创建优惠”只登记并核对现有 Discount mapping，不创建第二个支付优惠对象。
3. 创建 Webhook，URL 固定为 `https://muxvia.com/pay/creem`，并保存独立 webhook signing secret。
4. 订阅至少开启 `checkout.completed`、`subscription.paid`、`subscription.active`、`subscription.scheduled_cancel`、`subscription.canceled`、`subscription.past_due`、`subscription.expired`、`subscription.paused`、`refund.created` 和 `dispute.created`。

## Controller 配置

bootstrap 可以生成非敏感 Creem 配置：

```bash
export MUXVIA_CONTROLLER_POSTGRES_DSN='postgresql://...'

muxvia-cloud-bootstrap \
  --output-dir /secure/generated-deployment \
  --creem-environment test \
  --creem-success-url 'https://muxvia.com/account?payment=return'
```

生成的 `controller-config.json` 只包含：

```json
{
  "creem_environment": "test",
  "creem_success_url": "https://muxvia.com/account?payment=return"
}
```

API key 和 Webhook secret 只能由 Controller 进程环境注入：

```text
MUXVIA_CREEM_API_KEY
MUXVIA_CREEM_WEBHOOK_SECRET
```

它们不得进入 Controller JSON、systemd unit 正文、manifest、日志、Web 构建变量、Android 构建或仓库。部署时使用权限为 `0600` 的 systemd `EnvironmentFile` 或等价 secret manager，随后重启 `muxvia-cloud-controller`。

## Catalog 与优惠映射

在 Operator 套餐管理中发布新的 configured 套餐版本，并分别填写 Creem 月付和年付 Product ID。Controller 发布前会核对 configured price 的每个已启用计费周期都有对应 Product mapping；Creem adapter 创建 Checkout 时还会回读 Product，核对状态、币种、价格、billing type 和 billing period。

在 Operator 优惠码管理中登记 Muxvia code 与 Creem Discount Code。Controller 会回读 Creem Discount，核对状态、固定金额或百分比、币种、有效期、总量和适用 Product。Muxvia 的每账号限额仍由本地 PostgreSQL 原子 reservation 管理。

## Sandbox 验收

1. 从 Muxvia 账号页点击升级，确认浏览器跳转到 Creem 托管 Checkout；此时 Muxvia 订单必须仍为 pending，Entitlement 不变。
2. 完成 test payment，确认返回账号页后不能仅靠 query string 开通套餐。
3. 确认 `POST /pay/creem` 返回 HTTP 200，journal 中只有一条对应 transaction/status 事件，PaymentAttempt、Order、Subscription 和 Entitlement 在同一提交后更新。
4. 重放相同 Webhook，确认 journal、Subscription revision 和周期结束时间不重复推进。
5. 暂时在 Creem Dashboard 禁用 Webhook 投递，再完成一笔 test payment；重启 Controller，确认 PostgreSQL 中的 pending attempt 被轮询恢复并结算。
6. 在 Creem Test Mode 执行 scheduled cancel、past due、pause、cancel、refund 和 dispute，确认 Webhook 或 subscription polling 都进入同一 journal，并按状态更新服务权限。
7. 在 Operator 订单详情核对 provider checkout、transaction、subscription reference、最近 provider status、attempt 次数、event timeline 和 audit；不得展示 API key、webhook secret 或原始敏感 payload。

本切片只有在真实 sandbox Product/Discount、真实 Webhook secret、test event、阻断 Webhook 后轮询补偿及 Controller/PostgreSQL 重启证据齐全后才能标记完成。

## 当前外部阻塞

截至 2026-07-25，仓库内实现和本地 PostgreSQL/HTTP/Playwright harness 不受阻塞；以下证据必须依赖 Creem Test Mode 与线上部署状态，不能由本地 fake 冒充：

- 尚未取得并核对月付、年付 recurring Product ID，因此不能发布真实 sandbox catalog mapping；
- Controller 运行环境尚未注入 `MUXVIA_CREEM_WEBHOOK_SECRET`，无法验证 Dashboard 发出的真实签名事件；
- `https://muxvia.com/pay/creem` 的线上 Controller route、Dashboard event subscription 和投递结果尚未共同验证；
- 尚未执行真实 test checkout、退款、dispute、scheduled cancel、past due、pause，以及禁用 Webhook 后的轮询补偿；
- 聊天中出现过的 test API key 不会写入仓库、命令记录、日志或文档。执行真实验收前，应由操作者通过受保护的进程环境或 secret manager 注入。

这些阻塞只阻止 `CREEM001` 标记完成，不阻止继续完成本地 adapter、运营对账、权限、防误操作和回归门禁。
