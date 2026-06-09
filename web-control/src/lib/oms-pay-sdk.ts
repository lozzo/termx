/**
 * OMS-Pay TypeScript Client SDK
 *
 * 零依赖的支付网关客户端，仅使用原生 fetch 和 crypto。
 * 适用于 Node.js >= 18 / 现代浏览器 / Edge Runtime / Cloudflare Workers。
 *
 * ## 安装 & 引入
 *
 * 将本文件复制到项目中直接引入即可，无需安装额外依赖。
 *
 * ```ts
 * import { OmsPayClient } from './oms-pay-client'
 * // 或
 * import OmsPayClient from './oms-pay-client'
 * ```
 *
 * ## 快速开始
 *
 * ```ts
 * import { OmsPayClient } from './oms-pay-client'
 *
 * const client = new OmsPayClient({
 *   baseUrl: 'https://pay.example.com',
 *   apiKey: 'opay_sk_xxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxx',
 * })
 *
 * // ── 1. 创建支付订单 ──────────────────────────────────
 * const payment = await client.payments.create({
 *   merchantOrderId: 'order_001',
 *   amount: 100,              // 金额：100分 = ¥1.00
 *   description: '测试商品',
 *   paymentMethod: 'wechat_native',
 * })
 * console.log(payment.codeUrl)     // 微信原始二维码链接，可自行渲染
 * console.log(payment.checkoutUrl) // 托管支付页面地址，可直接跳转
 *
 * // ── 2. 查询支付详情 ──────────────────────────────────
 * const detail = await client.payments.get(payment.paymentId)
 * console.log(detail.status)  // 'pending' | 'paid' | 'expired' | 'closed' | 'refunded'
 *
 * // ── 3. 按商户订单号查询 ──────────────────────────────
 * const order = await client.payments.getByMerchantOrderId('order_001')
 *
 * // ── 4. 轮询等待支付完成 ──────────────────────────────
 * const paid = await client.payments.poll(payment.paymentId, {
 *   interval: 3000,            // 每 3 秒查询一次
 *   timeout: 300_000,          // 最多等 5 分钟
 *   onStatusChange: (status) => console.log('状态变更:', status),
 * })
 *
 * // ── 5. 刷新二维码 ────────────────────────────────────
 * // 微信 code_url 与订单绑定，无法单独刷新。
 * // refresh 会先关闭旧订单，再创建新订单。
 * const newPayment = await client.payments.refresh(payment.paymentId, {
 *   merchantOrderId: 'order_001_v2',
 *   amount: 100,
 *   description: '测试商品',
 *   paymentMethod: 'wechat_native',
 * })
 *
 * // ── 6. 关闭未支付的订单 ──────────────────────────────
 * const closed = await client.payments.close(payment.paymentId)
 *
 * // ── 7. 发起退款 ──────────────────────────────────────
 * const refund = await client.refunds.create(payment.paymentId, {
 *   amount: 50,
 *   reason: '用户申请退款',
 * })
 *
 * // ── 8. 查询退款状态 ──────────────────────────────────
 * const refundDetail = await client.refunds.get(refund.refundId)
 * console.log(refundDetail.status)  // 'pending' | 'processing' | 'success' | 'failed'
 *
 * // ── 9. Webhook 验签 ──────────────────────────────────
 * // Node.js 环境（同步）
 * const isValid = OmsPayClient.verifyWebhook(rawBody, signature, timestamp, webhookSecret)
 *
 * // Edge Runtime / 浏览器（异步）
 * const isValidAsync = await OmsPayClient.verifyWebhookAsync(rawBody, signature, timestamp, webhookSecret)
 * ```
 *
 * ## 错误处理
 *
 * 所有 API 调用在出错时会抛出 `OmsPayError`，包含以下属性：
 * - `code`：错误码，如 `'VALIDATION_ERROR'`、`'NOT_FOUND'`、`'TIMEOUT'`、`'NETWORK_ERROR'`
 * - `status`：HTTP 状态码（网络错误/超时为 0）
 * - `message`：错误描述
 *
 * ```ts
 * import { OmsPayError } from './oms-pay-client'
 *
 * try {
 *   await client.payments.get('non-existent-id')
 * } catch (err) {
 *   if (err instanceof OmsPayError) {
 *     console.log(err.code)    // 'NOT_FOUND'
 *     console.log(err.status)  // 404
 *     console.log(err.message) // 'Payment not found'
 *   }
 * }
 * ```
 *
 * ## 支付状态流转
 *
 * ```
 *                 ┌──────────┐
 *                 │ pending  │
 *                 └────┬─────┘
 *            ┌─────────┼──────────┐
 *            ▼         ▼          ▼
 *       ┌────────┐ ┌────────┐ ┌────────┐
 *       │  paid  │ │expired │ │ closed │
 *       └───┬────┘ └────────┘ └────────┘
 *           ▼
 *      ┌──────────┐
 *      │ refunded │  (全额退款后)
 *      └──────────┘
 * ```
 *
 * ## Webhook 事件类型
 *
 * | 事件                | 触发时机           |
 * |--------------------|--------------------|
 * | `payment.success`  | 支付成功           |
 * | `payment.closed`   | 订单关闭           |
 * | `refund.success`   | 退款成功           |
 * | `refund.failed`    | 退款失败           |
 */

// ============================================================
// Types — 配置
// ============================================================

/**
 * 客户端初始化选项
 *
 * @example
 * ```ts
 * // 基础用法
 * const client = new OmsPayClient({
 *   baseUrl: 'https://pay.example.com',
 *   apiKey: 'opay_sk_xxx',
 * })
 *
 * // 自定义超时和 fetch 实现（如使用 undici）
 * import { fetch as undiciFetch } from 'undici'
 * const client = new OmsPayClient({
 *   baseUrl: 'https://pay.example.com',
 *   apiKey: 'opay_sk_xxx',
 *   fetch: undiciFetch as unknown as typeof globalThis.fetch,
 *   timeout: 10_000, // 10 秒超时
 * })
 * ```
 */
export interface OmsPayClientOptions {
    /**
     * API 基础地址（不带尾部斜杠）
     *
     * @example 'https://pay.example.com'
     * @example 'http://localhost:3000'
     */
    baseUrl: string

    /**
     * 商户 API Key
     *
     * 在创建商户时由管理后台颁发，格式: `opay_sk_...`
     * 请妥善保管，不要暴露在客户端代码中。
     *
     * @example 'opay_sk_a1b2c3d4e5f6...'
     */
    apiKey: string

    /**
     * 自定义 fetch 实现
     *
     * 默认使用全局 `globalThis.fetch`。
     * 可传入自定义实现，如 `undici`、`node-fetch` 等。
     */
    fetch?: typeof globalThis.fetch

    /**
     * 请求超时毫秒数
     *
     * 单次 HTTP 请求的超时时间，超时后抛出 `OmsPayError`（code='TIMEOUT'）。
     *
     * @default 30000
     */
    timeout?: number
}

// ============================================================
// Types — 请求参数
// ============================================================

/**
 * 创建支付订单的请求参数
 *
 * @example
 * ```ts
 * // 最简用法（仅必填参数）
 * await client.payments.create({
 *   merchantOrderId: 'order_001',
 *   amount: 100,
 *   description: '测试商品',
 *   paymentMethod: 'wechat_native',
 * })
 *
 * // 完整参数
 * await client.payments.create({
 *   merchantOrderId: 'order_001',
 *   amount: 9900,               // ¥99.00
 *   description: 'iPhone 手机壳',
 *   paymentMethod: 'wechat_native',
 *   currency: 'CNY',
 *   expireMinutes: 15,          // 15 分钟后过期
 *   clientIp: '203.0.113.1',    // 用户 IP，用于风控
 *   returnUrl: 'https://shop.example.com/order/success',  // 支付成功后跳转
 *   productName: 'iPhone 手机壳',    // 展示在托管支付页面
 *   merchantLogo: 'https://shop.example.com/logo.png',   // 展示在托管支付页面
 *   metadata: {                  // 自定义数据，回调时原样返回
 *     userId: 'u_123',
 *     sku: 'SKU-001',
 *   },
 * })
 * ```
 */
export interface CreatePaymentParams {
    /**
     * 商户侧订单号
     *
     * 商户系统内部的唯一订单号，用于关联商户自身的业务订单。
     * 同一商户下不可重复。
     *
     * @maxLength 64
     * @example 'order_20260301_001'
     */
    merchantOrderId: string

    /**
     * 支付金额，单位：分
     *
     * 注意是 **分** 不是元。例如 ¥1.00 对应 `amount: 100`。
     *
     * @minimum 1
     * @example 100    // ¥1.00
     * @example 9900   // ¥99.00
     * @example 100000 // ¥1000.00
     */
    amount: number

    /**
     * 商品描述
     *
     * 会传递给微信支付，展示在用户的支付凭证上。
     *
     * @maxLength 128
     * @example '测试商品'
     * @example 'iPhone 16 手机壳 × 1'
     */
    description: string

    /**
     * 支付方式
     *
     * 目前仅支持 `wechat_native`（微信扫码支付）。
     */
    paymentMethod: 'wechat_native'

    /**
     * 货币类型
     *
     * 目前仅支持 `CNY`（人民币）。
     *
     * @default 'CNY'
     */
    currency?: 'CNY'

    /**
     * 订单过期时间（分钟）
     *
     * 从创建时刻算起，到期后订单自动变为 `expired` 状态。
     *
     * @minimum 5
     * @maximum 120
     * @default 30
     */
    expireMinutes?: number

    /**
     * 自定义元数据（透传数据）
     *
     * 支付成功后，会在 Webhook 回调中原样返回。
     * 可存放业务侧需要的关联信息（如用户 ID、商品 SKU 等）。
     *
     * @example { userId: 'u_123', sku: 'SKU-001' }
     */
    metadata?: Record<string, unknown>

    /**
     * 客户端 IP（IPv4）
     *
     * 用于微信支付的风控校验。建议从请求头中获取真实用户 IP 传入。
     *
     * @example '203.0.113.1'
     */
    clientIp?: string

    /**
     * 支付完成后跳转地址
     *
     * 用户在托管支付页面完成支付后，页面会展示"返回商户"按钮跳转到此 URL。
     * 如果不设置，支付完成后仅展示"支付成功"。
     *
     * @example 'https://shop.example.com/order/success?orderId=order_001'
     */
    returnUrl?: string

    /**
     * 商品名称
     *
     * 展示在托管支付页面上，让用户确认购买的商品。
     * 如果不设置，页面不展示商品名称。
     *
     * @maxLength 64
     * @example 'iPhone 16 手机壳'
     */
    productName?: string

    /**
     * 商户 Logo URL
     *
     * 展示在托管支付页面顶部，提升品牌辨识度。
     * 建议使用正方形图片，推荐尺寸 128x128。
     *
     * @example 'https://shop.example.com/logo.png'
     */
    merchantLogo?: string
}

/**
 * 创建退款的请求参数
 *
 * @example
 * ```ts
 * // 全额退款
 * await client.refunds.create(paymentId, {
 *   amount: 9900,   // 退全部 ¥99.00
 *   reason: '用户申请退款',
 * })
 *
 * // 部分退款（带商户退款单号）
 * await client.refunds.create(paymentId, {
 *   amount: 5000,                        // 退 ¥50.00
 *   merchantRefundId: 'refund_001',      // 商户侧退款单号
 *   reason: '部分商品退货',
 * })
 * ```
 */
export interface CreateRefundParams {
    /**
     * 退款金额，单位：分
     *
     * 不能超过订单剩余可退金额（订单金额 - 已退金额）。
     * 支持多次部分退款，累计退款金额不超过订单金额即可。
     *
     * @minimum 1
     * @example 100   // 退 ¥1.00
     * @example 9900  // 退 ¥99.00
     */
    amount: number

    /**
     * 商户侧退款单号
     *
     * 商户系统内部的退款单号，可选。
     * 如果不传，系统会自动生成一个（REF-XXXXXXXX 格式）。
     *
     * @maxLength 64
     * @example 'refund_20260301_001'
     */
    merchantRefundId?: string

    /**
     * 退款原因
     *
     * 可选，用于记录退款原因，会传递给微信支付。
     *
     * @maxLength 128
     * @example '用户申请退款'
     * @example '商品缺货'
     */
    reason?: string
}

/**
 * 轮询支付状态的选项
 *
 * @example
 * ```ts
 * // 使用默认参数（3秒间隔，5分钟超时）
 * const payment = await client.payments.poll(paymentId)
 *
 * // 自定义间隔和超时
 * const payment = await client.payments.poll(paymentId, {
 *   interval: 2000,    // 每 2 秒查一次
 *   timeout: 600_000,  // 最多等 10 分钟
 * })
 *
 * // 监听状态变化
 * const payment = await client.payments.poll(paymentId, {
 *   onStatusChange: (status, payment) => {
 *     console.log(`状态变更: ${status}`)
 *     if (status === 'paid') {
 *       console.log(`支付时间: ${payment.paidAt}`)
 *     }
 *   },
 * })
 *
 * // 使用 AbortController 手动取消
 * const controller = new AbortController()
 * setTimeout(() => controller.abort(), 60_000) // 60秒后取消
 *
 * try {
 *   const payment = await client.payments.poll(paymentId, {
 *     signal: controller.signal,
 *   })
 * } catch (err) {
 *   if (err instanceof OmsPayError && err.code === 'CANCELLED') {
 *     console.log('轮询被取消')
 *   }
 * }
 * ```
 */
export interface PollOptions {
    /**
     * 轮询间隔毫秒数
     *
     * 两次查询之间的等待时间。建议不低于 2000ms，避免过于频繁。
     *
     * @default 3000
     */
    interval?: number

    /**
     * 最大等待毫秒数
     *
     * 超过此时间后抛出 `OmsPayError`（code='TIMEOUT'）。
     *
     * @default 300000 (5 分钟)
     */
    timeout?: number

    /**
     * 状态变化时回调
     *
     * 每次轮询查到状态发生变化时调用。
     * 首次查询的状态也会触发回调。
     *
     * @param status  - 当前支付状态
     * @param payment - 完整的支付详情对象
     */
    onStatusChange?: (status: PaymentStatus, payment: PaymentDetail) => void

    /**
     * 外部取消信号
     *
     * 传入 `AbortSignal` 可从外部取消轮询。
     * 取消后抛出 `OmsPayError`（code='CANCELLED'）。
     */
    signal?: AbortSignal
}

// ============================================================
// Types — 响应数据
// ============================================================

/**
 * 支付订单状态
 *
 * | 状态       | 说明                                      |
 * |-----------|-------------------------------------------|
 * | `pending` | 待支付，等待用户扫码                         |
 * | `paid`    | 已支付                                     |
 * | `expired` | 已过期，超过 expireMinutes 未支付自动过期      |
 * | `closed`  | 已关闭，商户主动关闭或微信侧关闭               |
 * | `refunded`| 已全额退款                                  |
 */
export type PaymentStatus = 'pending' | 'paid' | 'expired' | 'closed' | 'refunded'

/**
 * 退款状态
 *
 * | 状态          | 说明                              |
 * |--------------|-----------------------------------|
 * | `pending`    | 退款请求已提交，等待处理             |
 * | `processing` | 退款处理中（微信侧正在处理）          |
 * | `success`    | 退款成功，款项已原路退回              |
 * | `failed`     | 退款失败，请查看 errorCode/errorMessage |
 */
export type RefundStatus = 'pending' | 'processing' | 'success' | 'failed'

/**
 * 创建支付订单的返回结果（精简结构）
 *
 * 仅包含前端渲染支付页面所需的必要字段。
 * 如需完整订单信息，请调用 `client.payments.get(paymentId)` 获取 `PaymentDetail`。
 *
 * @example
 * ```ts
 * const result = await client.payments.create({
 *   merchantOrderId: 'order_001',
 *   amount: 100,
 *   description: '测试商品',
 *   paymentMethod: 'wechat_native',
 * })
 *
 * // 方式一：使用原始二维码链接，自行渲染二维码（推荐 APP/桌面端）
 * renderQrCode(result.codeUrl)
 *
 * // 方式二：跳转到托管支付页面（推荐 H5/Web）
 * window.location.href = result.checkoutUrl
 *
 * // 后续可用 paymentId 查询/轮询/关闭
 * const detail = await client.payments.get(result.paymentId)
 * ```
 */
export interface PaymentCreated {
    /**
     * 支付订单 ID（UUID 格式）
     *
     * 后续查询、关闭、退款等操作均使用此 ID。
     *
     * @example 'a1b2c3d4-e5f6-7890-abcd-ef1234567890'
     */
    paymentId: string

    /**
     * 系统订单号
     *
     * OMS-Pay 生成的唯一订单号，格式: `PAY-YYYYMMDD-XXXXXXXX`。
     *
     * @example 'PAY-20260301-A1B2C3D4'
     */
    orderNo: string

    /**
     * 订单状态
     *
     * 创建时始终为 `'pending'`。
     */
    status: 'pending'

    /**
     * 支付金额，单位：分
     *
     * @example 100   // ¥1.00
     * @example 9900  // ¥99.00
     */
    amount: number

    /**
     * 货币类型
     *
     * @example 'CNY'
     */
    currency: string

    /**
     * 微信支付原始二维码链接
     *
     * `weixin://wxpay/bizpayurl?...` 格式的链接，需要将此内容编码为二维码图片展示给用户扫码。
     * 可使用 `qrcode` 等库生成二维码图片。
     *
     * @example 'weixin://wxpay/bizpayurl?pr=xxxxx'
     */
    codeUrl: string

    /**
     * 托管支付页面地址
     *
     * OMS-Pay 提供的完整支付页面，包含二维码展示、倒计时、状态轮询等功能。
     * 可直接跳转或嵌入 iframe 使用。
     *
     * @example 'https://pay.example.com/checkout/a1b2c3d4-e5f6-7890-abcd-ef1234567890'
     */
    checkoutUrl: string

    /**
     * 订单过期时间（ISO 8601 格式）
     *
     * 超过此时间未支付，订单将自动变为 `expired` 状态。
     *
     * @example '2026-03-01T12:30:00.000Z'
     */
    expireAt: string
}

/**
 * 支付订单完整详情
 *
 * 通过 `get()`、`close()`、`getByMerchantOrderId()`、`poll()` 返回。
 * 包含订单的完整信息。
 *
 * @example
 * ```ts
 * const detail = await client.payments.get(paymentId)
 *
 * console.log(detail.status)            // 'pending' | 'paid' | 'expired' | 'closed' | 'refunded'
 * console.log(detail.amount)            // 金额（分）
 * console.log(detail.merchantOrderId)   // 商户侧订单号
 * console.log(detail.codeUrl)           // 二维码链接（已过期/已关闭时可能为 null）
 * console.log(detail.checkoutUrl)       // 托管支付页面地址
 * console.log(detail.paidAt)            // 支付时间（未支付时为 null）
 * console.log(detail.metadata)          // 商户透传的自定义数据
 *
 * // 判断订单状态
 * if (detail.status === 'paid') {
 *   console.log(`已支付，交易号: ${detail.providerTransactionId}`)
 * }
 * if (detail.refundedAmount > 0) {
 *   console.log(`已退款 ${detail.refundedAmount / 100} 元`)
 * }
 * ```
 */
export interface PaymentDetail {
    /**
     * 支付订单 ID（UUID 格式）
     *
     * @example 'a1b2c3d4-e5f6-7890-abcd-ef1234567890'
     */
    paymentId: string

    /**
     * 系统订单号
     *
     * OMS-Pay 生成的唯一订单号，格式: `PAY-YYYYMMDD-XXXXXXXX`。
     *
     * @example 'PAY-20260301-A1B2C3D4'
     */
    orderNo: string

    /**
     * 商户侧订单号
     *
     * 创建订单时传入的商户系统内部订单号。
     *
     * @example 'order_20260301_001'
     */
    merchantOrderId: string

    /**
     * 订单状态
     *
     * @see PaymentStatus
     */
    status: PaymentStatus

    /**
     * 支付金额，单位：分
     *
     * @example 9900 // ¥99.00
     */
    amount: number

    /**
     * 货币类型
     *
     * @example 'CNY'
     */
    currency: string

    /**
     * 商品描述
     *
     * 创建订单时传入的描述信息。
     */
    description: string

    /**
     * 支付方式
     *
     * @example 'wechat_native'
     */
    paymentMethod: string

    /**
     * 微信支付原始二维码链接
     *
     * 订单已过期或已关闭时可能为 `null`。
     */
    codeUrl: string | null

    /**
     * 托管支付页面地址
     *
     * @example 'https://pay.example.com/checkout/a1b2c3d4-...'
     */
    checkoutUrl: string

    /**
     * 微信支付交易号
     *
     * 支付成功后由微信返回，未支付时为 `null`。
     * 可用于对账和在微信商户后台查询。
     *
     * @example '4200001234202603010012345678'
     */
    providerTransactionId: string | null

    /**
     * 订单过期时间（ISO 8601 格式）
     *
     * @example '2026-03-01T12:30:00.000Z'
     */
    expireAt: string

    /**
     * 支付完成时间（ISO 8601 格式）
     *
     * 未支付时为 `null`。
     *
     * @example '2026-03-01T12:05:30.000Z'
     */
    paidAt: string | null

    /**
     * 订单关闭时间（ISO 8601 格式）
     *
     * 未关闭时为 `null`。
     *
     * @example '2026-03-01T12:10:00.000Z'
     */
    closedAt: string | null

    /**
     * 已退款金额，单位：分
     *
     * 累计已退款的总金额。未退款时为 `0`。
     *
     * @example 0     // 未退款
     * @example 5000  // 已退 ¥50.00
     */
    refundedAmount: number

    /**
     * 商户自定义元数据
     *
     * 创建订单时传入的 `metadata`，原样返回。
     * 未设置时为 `null`。
     *
     * @example { userId: 'u_123', sku: 'SKU-001' }
     */
    metadata: Record<string, unknown> | null

    /**
     * 订单创建时间（ISO 8601 格式）
     *
     * @example '2026-03-01T12:00:00.000Z'
     */
    createdAt: string

    /**
     * 订单最后更新时间（ISO 8601 格式）
     *
     * @example '2026-03-01T12:05:30.000Z'
     */
    updatedAt: string
}

/**
 * 退款详情
 *
 * 通过 `client.refunds.create()` 或 `client.refunds.get()` 返回。
 *
 * @example
 * ```ts
 * const refund = await client.refunds.create(paymentId, {
 *   amount: 5000,
 *   reason: '用户申请退款',
 * })
 *
 * console.log(refund.refundId)          // 退款单 ID
 * console.log(refund.status)            // 'processing' — 微信正在处理
 * console.log(refund.providerRefundId)  // 微信退款单号
 *
 * // 稍后查询退款结果
 * const detail = await client.refunds.get(refund.refundId)
 * if (detail.status === 'success') {
 *   console.log(`退款成功，完成时间: ${detail.completedAt}`)
 * } else if (detail.status === 'failed') {
 *   console.log(`退款失败: [${detail.errorCode}] ${detail.errorMessage}`)
 * }
 * ```
 */
export interface Refund {
    /**
     * 退款单 ID（UUID 格式）
     *
     * 后续查询退款状态使用此 ID。
     *
     * @example 'f1e2d3c4-b5a6-7890-abcd-ef1234567890'
     */
    refundId: string

    /**
     * 关联的支付订单 ID
     *
     * @example 'a1b2c3d4-e5f6-7890-abcd-ef1234567890'
     */
    paymentId: string

    /**
     * 退款金额，单位：分
     *
     * @example 5000 // ¥50.00
     */
    amount: number

    /**
     * 货币类型
     *
     * @example 'CNY'
     */
    currency: string

    /**
     * 退款状态
     *
     * @see RefundStatus
     */
    status: RefundStatus

    /**
     * 商户侧退款单号
     *
     * 创建退款时传入的商户退款单号。未设置时为 `null`（系统自动生成）。
     */
    merchantRefundId: string | null

    /**
     * 退款原因
     *
     * 创建退款时传入的原因。未设置时为 `null`。
     */
    reason: string | null

    /**
     * 微信退款单号
     *
     * 微信支付返回的退款交易号，可用于对账。
     */
    providerRefundId: string | null

    /**
     * 退款失败错误码
     *
     * 仅在 `status === 'failed'` 时有值。
     *
     * @example 'NOTENOUGH'           // 余额不足
     * @example 'USER_ACCOUNT_ABNORMAL' // 用户账户异常
     */
    errorCode?: string | null

    /**
     * 退款失败错误描述
     *
     * 仅在 `status === 'failed'` 时有值。
     *
     * @example '基本账户余额不足，请充值后重新发起退款'
     */
    errorMessage?: string | null

    /**
     * 退款完成时间（ISO 8601 格式）
     *
     * 仅在 `status === 'success'` 时有值。
     *
     * @example '2026-03-01T13:00:00.000Z'
     */
    completedAt: string | null

    /**
     * 退款创建时间（ISO 8601 格式）
     *
     * @example '2026-03-01T12:30:00.000Z'
     */
    createdAt: string
}

// ============================================================
// Types — API 响应包装
// ============================================================

/**
 * API 成功响应
 *
 * 所有接口成功时返回此格式，`data` 中包含实际业务数据。
 *
 * ```json
 * { "success": true, "data": { ... } }
 * ```
 */
export interface ApiSuccessResponse<T> {
    success: true
    data: T
}

/**
 * API 错误详情
 *
 * ```json
 * { "code": "NOT_FOUND", "message": "Payment not found" }
 * ```
 */
export interface ApiErrorDetail {
    /** 错误码，如 `'VALIDATION_ERROR'`、`'NOT_FOUND'`、`'UNAUTHORIZED'` */
    code: string
    /** 错误描述（人类可读） */
    message: string
}

/**
 * API 错误响应
 *
 * ```json
 * { "success": false, "error": { "code": "NOT_FOUND", "message": "Payment not found" } }
 * ```
 */
export interface ApiErrorResponse {
    success: false
    error: ApiErrorDetail
}

/**
 * API 响应联合类型
 *
 * SDK 内部会自动判断 `success` 字段：
 * - `true`：返回 `data`
 * - `false`：抛出 `OmsPayError`
 *
 * 使用 SDK 时无需手动处理此类型。
 */
export type ApiResponse<T> = ApiSuccessResponse<T> | ApiErrorResponse

// ============================================================
// Types — Webhook
// ============================================================

/**
 * Webhook 回调的请求体
 *
 * OMS-Pay 在支付成功、关闭、退款等事件发生时，会向商户配置的 `webhookUrl` 发送 POST 请求。
 *
 * @example
 * ```ts
 * // Express 接收 Webhook 示例
 * app.post('/webhook', express.raw({ type: 'application/json' }), (req, res) => {
 *   const rawBody = req.body.toString()
 *
 *   // 1. 验证签名
 *   const isValid = OmsPayClient.verifyWebhook(
 *     rawBody,
 *     req.headers['x-opay-signature'] as string,
 *     req.headers['x-opay-timestamp'] as string,
 *     process.env.WEBHOOK_SECRET!,
 *   )
 *   if (!isValid) return res.status(401).json({ error: 'Invalid signature' })
 *
 *   // 2. 解析事件
 *   const payload: WebhookPayload = JSON.parse(rawBody)
 *
 *   // 3. 按事件类型处理
 *   switch (payload.event) {
 *     case 'payment.success':
 *       // 支付成功，更新业务订单状态
 *       await markOrderPaid(payload.merchantOrderId, payload.providerTransactionId)
 *       break
 *     case 'payment.closed':
 *       // 订单关闭
 *       await markOrderClosed(payload.merchantOrderId)
 *       break
 *     case 'refund.success':
 *       // 退款成功
 *       await markRefundSuccess(payload.refundId!, payload.refundAmount!)
 *       break
 *     case 'refund.failed':
 *       // 退款失败
 *       await markRefundFailed(payload.refundId!)
 *       break
 *   }
 *
 *   // 4. 返回成功（必须返回 2xx，否则会触发重试）
 *   res.json({ received: true })
 * })
 * ```
 */
export interface WebhookPayload {
    /** 事件类型 */
    event: WebhookEvent
    /** 支付订单 ID */
    paymentId: string
    /** 系统订单号 */
    orderNo: string
    /** 商户侧订单号 */
    merchantOrderId: string
    /** 支付金额（分） */
    amount: number
    /** 货币类型 */
    currency: string
    /** 订单状态 */
    status: PaymentStatus
    /** 微信支付交易号（仅 payment.success 事件有值） */
    providerTransactionId: string | null
    /** 支付完成时间（仅 payment.success 事件有值） */
    paidAt: string | null
    /** 商户透传的自定义元数据 */
    metadata: Record<string, unknown> | null
    /** 退款单 ID（仅退款事件有值） */
    refundId: string | null
    /** 退款金额（仅退款事件有值，单位：分） */
    refundAmount: number | null
    /** 事件触发时间（ISO 8601 格式） */
    timestamp: string
}

/**
 * Webhook 事件类型
 *
 * | 事件                | 触发时机                          |
 * |--------------------|-----------------------------------|
 * | `payment.success`  | 用户扫码支付成功                    |
 * | `payment.closed`   | 订单被商户主动关闭或微信侧关闭        |
 * | `refund.success`   | 退款处理成功，款项已退回用户          |
 * | `refund.failed`    | 退款处理失败                        |
 */
export type WebhookEvent = 'payment.success' | 'payment.closed' | 'refund.success' | 'refund.failed'

/**
 * Webhook 请求头
 *
 * OMS-Pay 发送 Webhook 时会附带以下请求头，用于验签。
 *
 * | Header              | 说明                                  |
 * |--------------------|---------------------------------------|
 * | `x-opay-signature` | HMAC-SHA256 签名（hex 格式）            |
 * | `x-opay-timestamp` | 签名时的 Unix 时间戳（秒）               |
 * | `x-opay-event`     | 事件类型，如 `payment.success`          |
 */
export interface WebhookHeaders {
    'x-opay-signature': string
    'x-opay-timestamp': string
    'x-opay-event': string
}

// ============================================================
// Errors
// ============================================================

/**
 * OMS-Pay API 错误
 *
 * 所有 API 调用在出错时（HTTP 错误、网络错误、超时）均抛出此异常。
 *
 * @example
 * ```ts
 * import { OmsPayError } from './oms-pay-client'
 *
 * try {
 *   await client.payments.get('invalid-id')
 * } catch (err) {
 *   if (err instanceof OmsPayError) {
 *     switch (err.code) {
 *       case 'NOT_FOUND':
 *         console.log('订单不存在')
 *         break
 *       case 'UNAUTHORIZED':
 *         console.log('API Key 无效')
 *         break
 *       case 'VALIDATION_ERROR':
 *         console.log('参数错误:', err.message)
 *         break
 *       case 'TIMEOUT':
 *         console.log('请求超时')
 *         break
 *       case 'NETWORK_ERROR':
 *         console.log('网络错误:', err.message)
 *         break
 *       case 'CANCELLED':
 *         console.log('轮询被取消')
 *         break
 *       default:
 *         console.log(`未知错误 [${err.code}]: ${err.message}`)
 *     }
 *   }
 * }
 * ```
 */
export class OmsPayError extends Error {
    constructor(
        message: string,
        /**
         * 错误码
         *
         * 常见值：
         * - `'VALIDATION_ERROR'` — 请求参数校验失败
         * - `'NOT_FOUND'` — 订单/退款不存在
         * - `'UNAUTHORIZED'` — API Key 无效或缺失
         * - `'TIMEOUT'` — 请求超时或轮询超时
         * - `'NETWORK_ERROR'` — 网络连接错误
         * - `'CANCELLED'` — 轮询被 AbortSignal 取消
         */
        public readonly code: string,
        /**
         * HTTP 状态码
         *
         * 网络错误和超时时为 `0`。
         */
        public readonly status: number,
    ) {
        super(message)
        this.name = 'OmsPayError'
    }
}

// ============================================================
// Client
// ============================================================

/**
 * OMS-Pay 客户端
 *
 * 所有 API 操作的入口，通过 `payments` 和 `refunds` 两个子资源访问具体方法。
 *
 * @example
 * ```ts
 * // ── 初始化 ────────────────────────────────────────────
 * const client = new OmsPayClient({
 *   baseUrl: 'https://pay.example.com',
 *   apiKey: 'opay_sk_xxx',
 * })
 *
 * // ── 支付相关 ──────────────────────────────────────────
 * client.payments.create(...)           // 创建支付订单
 * client.payments.get(paymentId)        // 查询支付详情
 * client.payments.close(paymentId)      // 关闭支付订单
 * client.payments.getByMerchantOrderId(merchantOrderId)  // 按商户订单号查询
 * client.payments.poll(paymentId, options)    // 轮询等待终态
 * client.payments.refresh(paymentId, params) // 刷新二维码
 *
 * // ── 退款相关 ──────────────────────────────────────────
 * client.refunds.create(paymentId, params)   // 创建退款
 * client.refunds.get(refundId)               // 查询退款详情
 *
 * // ── 工具方法 ──────────────────────────────────────────
 * client.health()                       // 健康检查（无需认证）
 * OmsPayClient.verifyWebhook(...)       // Webhook 验签（Node.js，静态方法）
 * OmsPayClient.verifyWebhookAsync(...)  // Webhook 验签（Edge Runtime，静态方法）
 * ```
 */
export class OmsPayClient {
    private readonly baseUrl: string
    private readonly apiKey: string
    private readonly _fetch: typeof globalThis.fetch
    private readonly timeout: number

    /** 支付订单相关操作 */
    public readonly payments: PaymentResource
    /** 退款相关操作 */
    public readonly refunds: RefundResource

    constructor(options: OmsPayClientOptions) {
        this.baseUrl = options.baseUrl.replace(/\/+$/, '')
        this.apiKey = options.apiKey
        this._fetch = options.fetch ?? globalThis.fetch.bind(globalThis)
        this.timeout = options.timeout ?? 30_000

        this.payments = new PaymentResource(this)
        this.refunds = new RefundResource(this)
    }

    /**
     * 健康检查
     *
     * 检查 OMS-Pay 服务是否正常运行。此接口无需 API Key 认证。
     *
     * @returns `{ status: 'ok', timestamp: '2026-03-01T12:00:00.000Z' }`
     *
     * @example
     * ```ts
     * const health = await client.health()
     * console.log(health.status)    // 'ok'
     * console.log(health.timestamp) // '2026-03-01T12:00:00.000Z'
     * ```
     */
    async health(): Promise<{ status: string; timestamp: string }> {
        return this.request<{ status: string; timestamp: string }>('GET', '/api/health', undefined, false)
    }

    // ---- internal helpers ----

    /**
     * 发送 HTTP 请求（内部方法）
     *
     * SDK 内部使用，不建议外部直接调用。
     * 自动处理 JSON 序列化/反序列化、认证头、超时和错误转换。
     *
     * @internal
     * @param method - HTTP 方法（GET / POST / PATCH / DELETE）
     * @param path   - 请求路径（如 `/api/v1/payments`）
     * @param body   - 请求体（会自动 JSON.stringify）
     * @param auth   - 是否附加 Authorization 头（默认 true）
     */
    async request<T>(method: string, path: string, body?: unknown, auth = true): Promise<T> {
        const url = `${this.baseUrl}${path}`
        const headers: Record<string, string> = {
            'Content-Type': 'application/json',
        }
        if (auth) {
            headers['Authorization'] = `Bearer ${this.apiKey}`
        }

        const controller = new AbortController()
        const timer = setTimeout(() => controller.abort(), this.timeout)

        try {
            const response = await this._fetch(url, {
                method,
                headers,
                body: body !== undefined ? JSON.stringify(body) : undefined,
                signal: controller.signal,
            })

            const json = await response.json() as ApiResponse<T>

            if (!json.success) {
                const err = (json as ApiErrorResponse).error
                throw new OmsPayError(err.message, err.code, response.status)
            }

            return (json as ApiSuccessResponse<T>).data
        } catch (err) {
            if (err instanceof OmsPayError) throw err
            if (err instanceof DOMException && err.name === 'AbortError') {
                throw new OmsPayError('Request timeout', 'TIMEOUT', 0)
            }
            throw new OmsPayError(
                err instanceof Error ? err.message : String(err),
                'NETWORK_ERROR',
                0,
            )
        } finally {
            clearTimeout(timer)
        }
    }

    // ============================================================
    // Webhook 验签工具（静态方法，不需要实例化 client）
    // ============================================================

    /**
     * 验证 Webhook 签名（Node.js 同步版）
     *
     * 使用 Node.js 的 `crypto` 模块进行 HMAC-SHA256 验签。
     * 适用于 Express、Koa、Fastify 等 Node.js 后端框架。
     *
     * 签名算法: `HMAC-SHA256(timestamp + '.' + rawBody, webhookSecret)`
     *
     * @param rawBody   - 请求原始 body 字符串（JSON string），**注意不要 parse 后再 stringify**
     * @param signature - 请求头 `X-OPay-Signature` 的值（hex 格式的 HMAC 签名）
     * @param timestamp - 请求头 `X-OPay-Timestamp` 的值（Unix 时间戳，秒）
     * @param secret    - 商户的 webhookSecret（创建商户时颁发，格式: `whsec_...`）
     * @param tolerance - 允许的时间偏差（秒），超过此范围视为过期，防止重放攻击
     * @returns `true` 表示签名合法且时间戳在有效范围内
     *
     * @example
     * ```ts
     * // ── Express 完整示例 ────────────────────────────────
     * import express from 'express'
     * import { OmsPayClient, WebhookPayload } from './oms-pay-client'
     *
     * const app = express()
     *
     * // 重要：必须使用 raw body，不能用 express.json() 中间件
     * app.post('/webhook', express.raw({ type: 'application/json' }), (req, res) => {
     *   const rawBody = req.body.toString()
     *   const signature = req.headers['x-opay-signature'] as string
     *   const timestamp = req.headers['x-opay-timestamp'] as string
     *
     *   // 验证签名
     *   const isValid = OmsPayClient.verifyWebhook(
     *     rawBody,
     *     signature,
     *     timestamp,
     *     process.env.WEBHOOK_SECRET!,
     *   )
     *   if (!isValid) {
     *     return res.status(401).json({ error: 'Invalid signature' })
     *   }
     *
     *   // 解析并处理事件
     *   const payload: WebhookPayload = JSON.parse(rawBody)
     *   console.log(`收到事件: ${payload.event}, 订单: ${payload.merchantOrderId}`)
     *
     *   // 返回 2xx 确认接收（否则 OMS-Pay 会重试，最多 5 次）
     *   res.json({ received: true })
     * })
     * ```
     *
     * @example
     * ```ts
     * // ── Next.js App Router 示例 ─────────────────────────
     * import { NextRequest, NextResponse } from 'next/server'
     * import { OmsPayClient, WebhookPayload } from './oms-pay-client'
     *
     * export async function POST(request: NextRequest) {
     *   const rawBody = await request.text()
     *   const signature = request.headers.get('x-opay-signature')!
     *   const timestamp = request.headers.get('x-opay-timestamp')!
     *
     *   const isValid = OmsPayClient.verifyWebhook(
     *     rawBody, signature, timestamp, process.env.WEBHOOK_SECRET!,
     *   )
     *   if (!isValid) {
     *     return NextResponse.json({ error: 'Invalid signature' }, { status: 401 })
     *   }
     *
     *   const payload: WebhookPayload = JSON.parse(rawBody)
     *   // 处理事件...
     *
     *   return NextResponse.json({ received: true })
     * }
     * ```
     */
    static verifyWebhook(
        rawBody: string,
        signature: string,
        timestamp: string,
        secret: string,
        tolerance = 300,
    ): boolean {
        // 防止重放攻击：检查时间戳偏差
        const ts = parseInt(timestamp, 10)
        if (isNaN(ts)) return false
        const diff = Math.abs(Math.floor(Date.now() / 1000) - ts)
        if (diff > tolerance) return false

        // 验签：HMAC-SHA256( timestamp + '.' + rawBody, secret )
        try {
            const crypto = require('crypto') as typeof import('crypto')
            const signContent = `${timestamp}.${rawBody}`
            const expected = crypto.createHmac('sha256', secret).update(signContent).digest('hex')
            const expectedBuf = Buffer.from(expected, 'hex')
            const signatureBuf = Buffer.from(signature, 'hex')
            if (expectedBuf.length !== signatureBuf.length) return false
            return crypto.timingSafeEqual(expectedBuf, signatureBuf)
        } catch {
            return false
        }
    }

    /**
     * 验证 Webhook 签名（Web Crypto API 异步版）
     *
     * 使用 Web Crypto API，适用于不支持 Node.js `crypto` 模块的环境：
     * - Cloudflare Workers
     * - Vercel Edge Functions
     * - Deno Deploy
     * - 浏览器（理论上不应在浏览器接收 Webhook，仅供特殊场景使用）
     *
     * @param rawBody   - 请求原始 body 字符串
     * @param signature - 请求头 `X-OPay-Signature` 的值
     * @param timestamp - 请求头 `X-OPay-Timestamp` 的值
     * @param secret    - 商户的 webhookSecret
     * @param tolerance - 允许的时间偏差（秒），默认 300
     * @returns `true` 表示签名合法
     *
     * @example
     * ```ts
     * // ── Cloudflare Workers 示例 ─────────────────────────
     * import { OmsPayClient, WebhookPayload } from './oms-pay-client'
     *
     * export default {
     *   async fetch(request: Request, env: Env): Promise<Response> {
     *     if (request.method !== 'POST') {
     *       return new Response('Method not allowed', { status: 405 })
     *     }
     *
     *     const rawBody = await request.text()
     *     const signature = request.headers.get('x-opay-signature')!
     *     const timestamp = request.headers.get('x-opay-timestamp')!
     *
     *     const isValid = await OmsPayClient.verifyWebhookAsync(
     *       rawBody, signature, timestamp, env.WEBHOOK_SECRET,
     *     )
     *     if (!isValid) {
     *       return Response.json({ error: 'Invalid signature' }, { status: 401 })
     *     }
     *
     *     const payload: WebhookPayload = JSON.parse(rawBody)
     *     // 处理事件...
     *
     *     return Response.json({ received: true })
     *   },
     * }
     * ```
     */
    static async verifyWebhookAsync(
        rawBody: string,
        signature: string,
        timestamp: string,
        secret: string,
        tolerance = 300,
    ): Promise<boolean> {
        const ts = parseInt(timestamp, 10)
        if (isNaN(ts)) return false
        const diff = Math.abs(Math.floor(Date.now() / 1000) - ts)
        if (diff > tolerance) return false

        try {
            const encoder = new TextEncoder()
            const key = await crypto.subtle.importKey(
                'raw',
                encoder.encode(secret),
                { name: 'HMAC', hash: 'SHA-256' },
                false,
                ['sign'],
            )
            const signContent = `${timestamp}.${rawBody}`
            const sig = await crypto.subtle.sign('HMAC', key, encoder.encode(signContent))
            const expected = Array.from(new Uint8Array(sig))
                .map((b) => b.toString(16).padStart(2, '0'))
                .join('')
            return expected === signature
        } catch {
            return false
        }
    }
}

// ============================================================
// Resources — 支付
// ============================================================

/**
 * 支付订单资源
 *
 * 通过 `client.payments` 访问，提供支付订单的创建、查询、关闭、轮询等操作。
 *
 * @example
 * ```ts
 * // 完整支付流程示例
 * async function processPayment(orderId: string, amount: number) {
 *   // 1. 创建支付
 *   const payment = await client.payments.create({
 *     merchantOrderId: orderId,
 *     amount,
 *     description: `订单 ${orderId}`,
 *     paymentMethod: 'wechat_native',
 *     returnUrl: `https://shop.example.com/orders/${orderId}`,
 *     metadata: { orderId },
 *   })
 *
 *   // 2. 返回二维码给前端展示
 *   // payment.codeUrl  — 用于自行渲染二维码
 *   // payment.checkoutUrl — 用于跳转到托管支付页面
 *
 *   // 3. 轮询等待支付结果（也可以通过 Webhook 接收通知）
 *   try {
 *     const result = await client.payments.poll(payment.paymentId, {
 *       timeout: 300_000,
 *       onStatusChange: (status) => {
 *         console.log(`订单 ${orderId} 状态: ${status}`)
 *       },
 *     })
 *
 *     if (result.status === 'paid') {
 *       console.log(`订单 ${orderId} 支付成功！`)
 *     }
 *   } catch (err) {
 *     if (err instanceof OmsPayError && err.code === 'TIMEOUT') {
 *       // 超时未支付，可选择关闭订单
 *       await client.payments.close(payment.paymentId)
 *     }
 *   }
 * }
 * ```
 */
class PaymentResource {
    constructor(private readonly client: OmsPayClient) { }

    /**
     * 创建支付订单
     *
     * 调用微信支付创建 Native 支付订单，返回二维码链接和托管支付页面地址。
     *
     * API: `POST /api/v1/payments`
     *
     * @param params - 创建支付的参数
     * @returns 创建结果，包含 `paymentId`、`codeUrl`、`checkoutUrl` 等
     * @throws {OmsPayError} code='VALIDATION_ERROR' — 参数校验失败
     * @throws {OmsPayError} code='UNAUTHORIZED' — API Key 无效
     *
     * @example
     * ```ts
     * const payment = await client.payments.create({
     *   merchantOrderId: 'order_001',
     *   amount: 100,              // ¥1.00
     *   description: '测试商品',
     *   paymentMethod: 'wechat_native',
     * })
     *
     * console.log(payment.paymentId)   // 'a1b2c3d4-...'
     * console.log(payment.codeUrl)     // 'weixin://wxpay/bizpayurl?...'
     * console.log(payment.checkoutUrl) // 'https://pay.example.com/checkout/a1b2c3d4-...'
     * console.log(payment.expireAt)    // '2026-03-01T12:30:00.000Z'
     * ```
     */
    async create(params: CreatePaymentParams): Promise<PaymentCreated> {
        return this.client.request<PaymentCreated>('POST', '/api/v1/payments', params)
    }

    /**
     * 查询支付订单详情
     *
     * 通过支付订单 ID 查询完整的订单信息。
     *
     * API: `GET /api/v1/payments/{paymentId}`
     *
     * @param paymentId - 支付订单 ID（创建时返回的 `paymentId`）
     * @returns 支付订单完整详情
     * @throws {OmsPayError} code='NOT_FOUND' — 订单不存在（或不属于当前商户）
     * @throws {OmsPayError} code='UNAUTHORIZED' — API Key 无效
     *
     * @example
     * ```ts
     * const detail = await client.payments.get('a1b2c3d4-e5f6-7890-abcd-ef1234567890')
     *
     * console.log(detail.status)              // 'paid'
     * console.log(detail.amount)              // 9900
     * console.log(detail.paidAt)              // '2026-03-01T12:05:30.000Z'
     * console.log(detail.providerTransactionId)  // '4200001234...'
     * console.log(detail.merchantOrderId)     // 'order_001'
     * console.log(detail.metadata)            // { userId: 'u_123' }
     * ```
     */
    async get(paymentId: string): Promise<PaymentDetail> {
        return this.client.request<PaymentDetail>('GET', `/api/v1/payments/${paymentId}`)
    }

    /**
     * 关闭支付订单
     *
     * 主动关闭一个 `pending` 状态的订单。关闭后用户无法继续支付。
     * 同时会调用微信支付关单接口。
     *
     * **注意**: 只能关闭 `pending` 状态的订单。已支付/已过期/已关闭的订单会报错。
     *
     * API: `POST /api/v1/payments/{paymentId}/close`
     *
     * @param paymentId - 支付订单 ID
     * @returns 关闭后的订单详情（status 变为 'closed'）
     * @throws {OmsPayError} code='NOT_FOUND' — 订单不存在
     * @throws {OmsPayError} code='VALIDATION_ERROR' — 订单状态不允许关闭（如已支付）
     *
     * @example
     * ```ts
     * const closed = await client.payments.close('a1b2c3d4-...')
     * console.log(closed.status)   // 'closed'
     * console.log(closed.closedAt) // '2026-03-01T12:10:00.000Z'
     * ```
     */
    async close(paymentId: string): Promise<PaymentDetail> {
        return this.client.request<PaymentDetail>('POST', `/api/v1/payments/${paymentId}/close`)
    }

    /**
     * 按商户订单号查询支付订单
     *
     * 通过商户自身系统的订单号查询关联的支付订单详情。
     * 适用于商户侧只持有自身订单号的场景。
     *
     * API: `GET /api/v1/payments?merchantOrderId={merchantOrderId}`
     *
     * @param merchantOrderId - 商户侧订单号（创建时传入的 `merchantOrderId`）
     * @returns 支付订单完整详情
     * @throws {OmsPayError} code='NOT_FOUND' — 该商户订单号下没有关联的支付订单
     * @throws {OmsPayError} code='VALIDATION_ERROR' — 未提供 merchantOrderId
     *
     * @example
     * ```ts
     * const detail = await client.payments.getByMerchantOrderId('order_001')
     * console.log(detail.paymentId)  // 'a1b2c3d4-...'
     * console.log(detail.status)     // 'paid'
     * ```
     */
    async getByMerchantOrderId(merchantOrderId: string): Promise<PaymentDetail> {
        return this.client.request<PaymentDetail>(
            'GET',
            `/api/v1/payments?merchantOrderId=${encodeURIComponent(merchantOrderId)}`
        )
    }

    /**
     * 轮询支付状态直到终态
     *
     * 持续查询订单状态，直到进入终态（`paid` / `expired` / `closed` / `refunded`）或超时。
     *
     * 终态说明：
     * - `paid` — 支付成功
     * - `expired` — 超时未支付，自动过期
     * - `closed` — 被商户主动关闭
     * - `refunded` — 已全额退款
     *
     * API: 内部调用 `GET /api/v1/payments/{paymentId}`
     *
     * @param paymentId - 支付订单 ID
     * @param options   - 轮询选项（间隔、超时、回调、取消信号）
     * @returns 达到终态时的订单详情
     * @throws {OmsPayError} code='TIMEOUT' — 超过 `options.timeout` 仍未到终态
     * @throws {OmsPayError} code='CANCELLED' — 被 `options.signal` 取消
     *
     * @example
     * ```ts
     * // 简单用法：使用默认参数（3秒间隔，5分钟超时）
     * const payment = await client.payments.poll(paymentId)
     * if (payment.status === 'paid') {
     *   console.log('支付成功！')
     * }
     * ```
     *
     * @example
     * ```ts
     * // 进阶用法：自定义参数 + AbortController
     * const controller = new AbortController()
     *
     * // 用户点击取消按钮时调用
     * cancelButton.onclick = () => controller.abort()
     *
     * try {
     *   const payment = await client.payments.poll(paymentId, {
     *     interval: 2000,       // 每 2 秒查一次
     *     timeout: 600_000,     // 最多等 10 分钟
     *     signal: controller.signal,
     *     onStatusChange: (status, payment) => {
     *       updateUI(status) // 更新页面状态展示
     *     },
     *   })
     *   showSuccess(payment)
     * } catch (err) {
     *   if (err instanceof OmsPayError) {
     *     if (err.code === 'TIMEOUT') showExpired()
     *     if (err.code === 'CANCELLED') showCancelled()
     *   }
     * }
     * ```
     */
    async poll(paymentId: string, options?: PollOptions): Promise<PaymentDetail> {
        const interval = options?.interval ?? 3_000
        const timeout = options?.timeout ?? 300_000
        const onStatusChange = options?.onStatusChange
        const externalSignal = options?.signal

        const controller = new AbortController()
        const startTime = Date.now()
        let lastStatus: PaymentStatus | null = null

        // Wire external signal to our controller
        if (externalSignal) {
            if (externalSignal.aborted) {
                throw new OmsPayError('Polling cancelled', 'CANCELLED', 0)
            }
            externalSignal.addEventListener('abort', () => controller.abort(), { once: true })
        }

        // Overall timeout
        const timer = setTimeout(() => controller.abort(), timeout)

        try {
            while (!controller.signal.aborted) {
                const payment = await this.get(paymentId)

                // Notify on status change
                if (onStatusChange && payment.status !== lastStatus) {
                    lastStatus = payment.status
                    onStatusChange(payment.status, payment)
                } else if (!lastStatus) {
                    lastStatus = payment.status
                }

                // Terminal states: paid / expired / closed / refunded
                if (['paid', 'expired', 'closed', 'refunded'].includes(payment.status)) {
                    return payment
                }

                // Check timeout
                if (Date.now() - startTime >= timeout) {
                    throw new OmsPayError('Polling timeout', 'TIMEOUT', 0)
                }

                // Wait for next interval
                await new Promise<void>((resolve, reject) => {
                    const waitTimer = setTimeout(resolve, interval)
                    controller.signal.addEventListener(
                        'abort',
                        () => {
                            clearTimeout(waitTimer)
                            reject(new OmsPayError('Polling cancelled', 'CANCELLED', 0))
                        },
                        { once: true }
                    )
                })
            }

            throw new OmsPayError('Polling cancelled', 'CANCELLED', 0)
        } finally {
            clearTimeout(timer)
        }
    }

    /**
     * 刷新二维码（关闭旧订单 + 创建新订单）
     *
     * 微信支付的 `code_url` 与订单一对一绑定，无法单独刷新二维码。
     * 此方法封装了"关闭旧订单 → 创建新订单"的流程，返回新的支付信息。
     *
     * 流程：
     * 1. 尝试关闭旧订单（best-effort，失败会静默忽略，旧订单可能已过期/已关闭）
     * 2. 使用新参数创建新的支付订单
     * 3. 返回新订单信息（包含新的 `codeUrl` 和 `checkoutUrl`）
     *
     * **注意**: 必须提供新的 `merchantOrderId`，不能与旧订单重复。
     *
     * @param oldPaymentId - 旧支付订单 ID
     * @param createParams - 新订单参数（merchantOrderId 必须是新的）
     * @returns 新创建的支付订单信息
     *
     * @example
     * ```ts
     * // 二维码过期后刷新
     * const newPayment = await client.payments.refresh(oldPaymentId, {
     *   merchantOrderId: 'order_001_v2',  // 新的商户订单号
     *   amount: 100,
     *   description: '测试商品',
     *   paymentMethod: 'wechat_native',
     * })
     *
     * // 用新的二维码替换页面上的旧二维码
     * renderQrCode(newPayment.codeUrl)
     *
     * // 开始轮询新订单
     * await client.payments.poll(newPayment.paymentId)
     * ```
     */
    async refresh(oldPaymentId: string, createParams: CreatePaymentParams): Promise<PaymentCreated> {
        // Close the old order (best-effort)
        try {
            await this.close(oldPaymentId)
        } catch {
            // Old order may already be closed/expired — that's fine
        }

        // Create a new payment
        return this.create(createParams)
    }
}

// ============================================================
// Resources — 退款
// ============================================================

/**
 * 退款资源
 *
 * 通过 `client.refunds` 访问，提供退款的创建和查询操作。
 *
 * @example
 * ```ts
 * // 完整退款流程
 * async function processRefund(paymentId: string, amount: number, reason: string) {
 *   // 1. 创建退款
 *   const refund = await client.refunds.create(paymentId, { amount, reason })
 *   console.log(`退款单已创建: ${refund.refundId}, 状态: ${refund.status}`)
 *
 *   // 2. 退款可能是异步的，需要查询最终结果
 *   //    也可以通过 Webhook（refund.success / refund.failed）接收通知
 *   if (refund.status === 'processing') {
 *     // 等待几秒后查询
 *     await new Promise(resolve => setTimeout(resolve, 5000))
 *     const detail = await client.refunds.get(refund.refundId)
 *
 *     if (detail.status === 'success') {
 *       console.log(`退款成功，完成时间: ${detail.completedAt}`)
 *     } else if (detail.status === 'failed') {
 *       console.log(`退款失败: [${detail.errorCode}] ${detail.errorMessage}`)
 *     } else {
 *       console.log(`退款仍在处理中: ${detail.status}`)
 *     }
 *   }
 * }
 * ```
 */
class RefundResource {
    constructor(private readonly client: OmsPayClient) { }

    /**
     * 创建退款
     *
     * 对已支付的订单发起退款请求。支持全额退款和部分退款。
     * 同一订单可多次部分退款，累计退款金额不超过订单金额。
     *
     * 退款流程：
     * 1. 校验订单状态（必须是 `paid` 或 `refunded`）
     * 2. 校验退款金额（不超过剩余可退金额）
     * 3. 调用微信支付退款接口
     * 4. 记录退款流水
     * 5. 如果微信立即返回成功，触发 `refund.success` Webhook
     *
     * API: `POST /api/v1/payments/{paymentId}/refund`
     *
     * @param paymentId - 要退款的支付订单 ID
     * @param params    - 退款参数（金额、原因等）
     * @returns 退款详情，包含退款单 ID 和状态
     * @throws {OmsPayError} code='NOT_FOUND' — 订单不存在
     * @throws {OmsPayError} code='VALIDATION_ERROR' — 订单状态不允许退款，或金额超出可退范围
     *
     * @example
     * ```ts
     * // 全额退款
     * const refund = await client.refunds.create('a1b2c3d4-...', {
     *   amount: 9900,
     *   reason: '用户申请退款',
     * })
     * console.log(refund.refundId)  // 'f1e2d3c4-...'
     * console.log(refund.status)    // 'processing' 或 'success'
     * ```
     *
     * @example
     * ```ts
     * // 部分退款（带商户退款单号）
     * const refund = await client.refunds.create('a1b2c3d4-...', {
     *   amount: 3000,                    // 退 ¥30.00
     *   merchantRefundId: 'ref_001',     // 商户自己的退款单号
     *   reason: '部分商品退货',
     * })
     * ```
     *
     * @example
     * ```ts
     * // 多次部分退款
     * // 订单金额 ¥100.00
     * await client.refunds.create(paymentId, { amount: 3000 })  // 退 ¥30.00
     * await client.refunds.create(paymentId, { amount: 5000 })  // 退 ¥50.00
     * await client.refunds.create(paymentId, { amount: 2000 })  // 退 ¥20.00（累计 ¥100，全额退完）
     * // 此时订单状态变为 'refunded'
     * ```
     */
    async create(paymentId: string, params: CreateRefundParams): Promise<Refund> {
        return this.client.request<Refund>('POST', `/api/v1/payments/${paymentId}/refund`, params)
    }

    /**
     * 查询退款详情
     *
     * 通过退款单 ID 查询退款的最新状态。
     * 退款可能需要一定时间处理（通常几秒到几分钟），可定期查询或等待 Webhook 通知。
     *
     * API: `GET /api/v1/refunds/{refundId}`
     *
     * @param refundId - 退款单 ID（创建退款时返回的 `refundId`）
     * @returns 退款详情
     * @throws {OmsPayError} code='NOT_FOUND' — 退款单不存在（或不属于当前商户）
     *
     * @example
     * ```ts
     * const refund = await client.refunds.get('f1e2d3c4-...')
     *
     * switch (refund.status) {
     *   case 'pending':
     *     console.log('退款待处理')
     *     break
     *   case 'processing':
     *     console.log('退款处理中...')
     *     break
     *   case 'success':
     *     console.log(`退款成功！完成时间: ${refund.completedAt}`)
     *     break
     *   case 'failed':
     *     console.log(`退款失败: [${refund.errorCode}] ${refund.errorMessage}`)
     *     break
     * }
     * ```
     */
    async get(refundId: string): Promise<Refund> {
        return this.client.request<Refund>('GET', `/api/v1/refunds/${refundId}`)
    }
}

// ============================================================
// Default export
// ============================================================

export default OmsPayClient
