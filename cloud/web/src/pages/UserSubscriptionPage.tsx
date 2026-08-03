import { create } from '@bufbuild/protobuf'
import { useMutation, useQueryClient } from '@tanstack/react-query'
import { Check, CreditCard, RotateCcw } from 'lucide-react'
import { useMemo, useState } from 'react'
import { protoSend } from '../api'
import { bytes, dateTime, money, subscriptionState } from '../format'
import { ApplyPaymentEventResponseSchema, ChangeMySubscriptionRequestSchema, CompleteDevelopmentPaymentRequestSchema, CreateMyOrderRequestSchema, CreateOrderResponseSchema, GetAccountCommerceResponseSchema, ListPlansResponseSchema, SubscriptionState, SubscriptionTransition, TransitionSubscriptionResponseSchema, type PlanDefinition } from '../generated/cloud/v1/commerce_pb'
import { useProtoQuery } from '../query'
import { Button, Dialog, ErrorState, Notice, PageHeader, Skeleton, Status } from '../ui'

type Checkout = { orderId: string; attemptId: string; planName: string; amount: string }

export function UserSubscriptionPage() {
  const [yearly, setYearly] = useState(false)
  const [checkout, setCheckout] = useState<Checkout>()
  const [cancelOpen, setCancelOpen] = useState(false)
  const plans = useProtoQuery(['user', 'plans'], '/api/commerce/plans', ListPlansResponseSchema, 60_000)
  const commerce = useProtoQuery(['user', 'commerce'], '/api/commerce/me', GetAccountCommerceResponseSchema, 15_000)
  const client = useQueryClient()
  const currentPlan = useMemo(() => plans.data?.plans.find((plan) => plan.planId === commerce.data?.subscription?.planId && plan.version === commerce.data?.subscription?.planVersion), [commerce.data, plans.data])
  const createOrder = useMutation({
    mutationFn: (plan: PlanDefinition) => {
      const transition = plan.planId === currentPlan?.planId ? SubscriptionTransition.RENEW : selectedPrice(plan, yearly) >= selectedPrice(currentPlan, yearly) ? SubscriptionTransition.UPGRADE : SubscriptionTransition.DOWNGRADE
      return protoSend('/api/commerce/orders', CreateMyOrderRequestSchema, create(CreateMyOrderRequestSchema, { planId: plan.planId, planVersion: plan.version, idempotencyKey: crypto.randomUUID(), requestedTransition: transition, yearly }), CreateOrderResponseSchema)
    },
    onSuccess: (value, plan) => setCheckout({ orderId: value.order?.orderId ?? '', attemptId: value.paymentAttempt?.paymentAttemptId ?? '', planName: plan.name, amount: money(value.order?.amount?.currency ?? 'CNY', value.order?.amount?.minorUnits ?? 0n) }),
  })
  const pay = useMutation({ mutationFn: () => protoSend('/api/commerce/payments/development', CompleteDevelopmentPaymentRequestSchema, create(CompleteDevelopmentPaymentRequestSchema, { orderId: checkout?.orderId, paymentAttemptId: checkout?.attemptId }), ApplyPaymentEventResponseSchema), onSuccess: () => { setCheckout(undefined); invalidateCommerce(client) } })
  const change = useMutation({ mutationFn: (transition: SubscriptionTransition) => protoSend('/api/commerce/subscription', ChangeMySubscriptionRequestSchema, create(ChangeMySubscriptionRequestSchema, { transition, expectedRevision: commerce.data?.subscription?.revision }), TransitionSubscriptionResponseSchema), onSuccess: () => { setCancelOpen(false); invalidateCommerce(client) } })
  if (plans.isPending || commerce.isPending) return <><PageHeader title="订阅套餐" /><Skeleton rows={8} /></>
  const failedQuery = plans.error ? plans : commerce.error ? commerce : undefined
  if (failedQuery) return <ErrorState error={failedQuery.error} onRetry={() => void failedQuery.refetch()} />
  const subscription = commerce.data?.subscription
  return <>
    <PageHeader title="选择适合你的套餐" meta="Direct 与 SSH 始终免费；订阅只增加托管设备、P2P 和 Relay 容量" actions={<div className="segmented" role="group" aria-label="计费周期"><button className={!yearly ? 'active' : ''} onClick={() => setYearly(false)}>月付</button><button className={yearly ? 'active' : ''} onClick={() => setYearly(true)}>年付</button></div>} />
    {subscription && <section className="current-subscription"><div><span>当前订阅</span><strong>{currentPlan?.name ?? subscription.planId}</strong><Status active={subscription.state === SubscriptionState.ACTIVE}>{subscriptionState(subscription.state)}</Status></div><dl><div><dt>当前周期</dt><dd>{dateTime(subscription.periodStart)} 至 {dateTime(subscription.periodEnd)}</dd></div><div><dt>Relay 剩余</dt><dd>{bytes(commerce.data?.entitlement?.relayRemainingBytes ?? 0n)}</dd></div></dl><div>{subscription.state === SubscriptionState.CANCEL_AT_PERIOD_END ? <Button onClick={() => change.mutate(SubscriptionTransition.RESUME)} disabled={change.isPending}><RotateCcw size={16} />恢复续订</Button> : <Button tone="quiet" onClick={() => setCancelOpen(true)} disabled={change.isPending}>到期后取消</Button>}</div></section>}
    <section className="plan-grid">{plans.data?.plans.map((plan) => {
      const selected = plan.planId === subscription?.planId && plan.version === subscription.planVersion
      const price = yearly ? plan.yearlyPrice : plan.monthlyPrice
      return <article key={`${plan.planId}:${plan.version}`} className={selected ? 'selected' : ''}><header><div><h2>{plan.name}</h2><p>{plan.description}</p></div>{selected && <span className="current-label">当前套餐</span>}</header><strong className="plan-price">{price?.minorUnits === 0n ? '免费' : money(price?.currency ?? 'CNY', price?.minorUnits ?? 0n)}<small>{price?.minorUnits === 0n ? '' : yearly ? '/ 年' : '/ 月'}</small></strong><ul><li><Check size={16} />{plan.capability?.cloudDaemonLimit ?? 0} 个 daemon 同时在线</li><li><Check size={16} />P2P 不限并发</li><li><Check size={16} />{plan.capability?.relayMaxConcurrency ?? 0} 路 Relay 并发</li><li><Check size={16} />{bytes(plan.capability?.relayMaxBytesPerPeriod ?? 0n)} Relay / 周期</li></ul><Button tone={selected ? 'quiet' : 'primary'} disabled={createOrder.isPending} onClick={() => createOrder.mutate(plan)}><CreditCard size={16} />{selected ? '续订当前套餐' : `选择${plan.name}`}</Button></article>
    })}</section>
    {createOrder.error && <Notice tone="error">无法创建订单，请检查订阅状态后重试。</Notice>}
    {change.error && <Notice tone="error">订阅状态已变化，请刷新后重试。</Notice>}
    <Dialog title="Development 支付确认" open={Boolean(checkout)} onClose={() => { if (!pay.isPending) setCheckout(undefined) }} footer={<><Button tone="quiet" onClick={() => setCheckout(undefined)} disabled={pay.isPending}>稍后支付</Button><Button tone="primary" onClick={() => pay.mutate()} disabled={pay.isPending}>{pay.isPending ? '正在确认' : '确认测试支付'}</Button></>}><Notice tone="warning">当前为 Development 支付适配器，不会发起真实扣款。</Notice><dl className="detail-list"><div><dt>套餐</dt><dd>{checkout?.planName}</dd></div><div><dt>金额</dt><dd>{checkout?.amount}</dd></div><div><dt>订单 ID</dt><dd className="mono">{checkout?.orderId}</dd></div></dl>{pay.error && <p className="form-error" role="alert">支付确认失败，订单仍保留为待支付。</p>}</Dialog>
    <Dialog title="取消自动续订" open={cancelOpen} onClose={() => { if (!change.isPending) setCancelOpen(false) }} footer={<><Button tone="quiet" onClick={() => setCancelOpen(false)} disabled={change.isPending}>保留订阅</Button><Button tone="danger" onClick={() => change.mutate(SubscriptionTransition.CANCEL_AT_PERIOD_END)} disabled={change.isPending}>{change.isPending ? '正在处理' : '确认到期后取消'}</Button></>}><Notice tone="warning">当前套餐会继续使用到本周期结束，此操作不会立即删除设备或中断现有连接。</Notice>{change.error && <p className="form-error" role="alert">订阅状态已经变化，请关闭窗口刷新后重试。</p>}</Dialog>
  </>
}

function selectedPrice(plan: PlanDefinition | undefined, yearly: boolean): bigint { return (yearly ? plan?.yearlyPrice : plan?.monthlyPrice)?.minorUnits ?? 0n }
function invalidateCommerce(client: ReturnType<typeof useQueryClient>) {
  void client.invalidateQueries({ queryKey: ['user', 'commerce'] })
  void client.invalidateQueries({ queryKey: ['user', 'plans'] })
}
