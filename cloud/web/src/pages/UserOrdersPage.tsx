import { create } from '@bufbuild/protobuf'
import { useMutation, useQueryClient } from '@tanstack/react-query'
import { CreditCard } from 'lucide-react'
import { useState } from 'react'
import { protoSend } from '../api'
import { attemptState, compactID, dateTime, money, orderState } from '../format'
import { ApplyPaymentEventResponseSchema, CompleteDevelopmentPaymentRequestSchema, GetAccountCommerceResponseSchema, OrderStatus, PaymentAttemptStatus } from '../generated/cloud/v1/commerce_pb'
import { useProtoQuery } from '../query'
import { Button, Dialog, Empty, ErrorState, Notice, PageHeader, Skeleton, Status, TableFrame } from '../ui'

type PendingPayment = { orderId: string; paymentAttemptId: string; planId: string; amount: string }

export function UserOrdersPage() {
  const query = useProtoQuery(['user', 'commerce'], '/api/commerce/me', GetAccountCommerceResponseSchema, 15_000)
  const client = useQueryClient()
  const [pendingPayment, setPendingPayment] = useState<PendingPayment>()
  const pay = useMutation({
    mutationFn: () => protoSend('/api/commerce/payments/development', CompleteDevelopmentPaymentRequestSchema, create(CompleteDevelopmentPaymentRequestSchema, { orderId: pendingPayment?.orderId, paymentAttemptId: pendingPayment?.paymentAttemptId }), ApplyPaymentEventResponseSchema),
    onSuccess: () => { setPendingPayment(undefined); void client.invalidateQueries({ queryKey: ['user', 'commerce'] }) },
  })
  if (query.isPending) return <><PageHeader title="我的订单" /><Skeleton /></>
  if (query.error) return <ErrorState error={query.error} />
  const attempts = new Map(query.data?.paymentAttempts.map((value) => [value.orderId, value]))
  return <>
    <PageHeader title="我的订单" meta="查看套餐订单、金额和支付处理结果" />
    {!query.data?.orders.length ? <Empty>还没有订单。选择套餐后，订单会显示在这里。</Empty> : <div className="user-data-table"><TableFrame><table><thead><tr><th>订单</th><th>套餐</th><th>状态</th><th>金额</th><th>支付</th><th>创建时间</th><th /></tr></thead><tbody>{query.data.orders.map((value) => { const attempt = attempts.get(value.orderId); return <tr key={value.orderId}><td data-label="订单"><strong className="mono">{compactID(value.orderId)}</strong><small className="mono">{value.idempotencyKey}</small></td><td data-label="套餐">{value.planId}<small>v{value.planVersion.toString()}</small></td><td data-label="状态"><Status active={value.status === OrderStatus.PAID}>{orderState(value.status)}</Status></td><td data-label="金额">{money(value.amount?.currency ?? 'CNY', value.amount?.minorUnits ?? 0n)}</td><td data-label="支付">{attempt ? attemptState(attempt.status) : '-'}<small>{value.provider}</small></td><td data-label="创建时间">{dateTime(value.createdAt)}</td><td data-label="">{value.status === OrderStatus.PENDING && attempt?.status === PaymentAttemptStatus.PENDING && <Button onClick={() => setPendingPayment({ orderId: value.orderId, paymentAttemptId: attempt.paymentAttemptId, planId: value.planId, amount: money(value.amount?.currency ?? 'CNY', value.amount?.minorUnits ?? 0n) })}><CreditCard size={16} />继续支付</Button>}</td></tr> })}</tbody></table></TableFrame></div>}
    <Dialog title="继续完成订单" open={Boolean(pendingPayment)} onClose={() => { if (!pay.isPending) setPendingPayment(undefined) }} footer={<><Button tone="quiet" onClick={() => setPendingPayment(undefined)} disabled={pay.isPending}>稍后支付</Button><Button tone="primary" onClick={() => pay.mutate()} disabled={pay.isPending}>{pay.isPending ? '正在确认' : '确认测试支付'}</Button></>}><Notice tone="warning">当前为 Development 支付适配器，不会发起真实扣款。</Notice><dl className="detail-list"><div><dt>套餐</dt><dd>{pendingPayment?.planId}</dd></div><div><dt>金额</dt><dd>{pendingPayment?.amount}</dd></div><div><dt>订单 ID</dt><dd className="mono">{pendingPayment?.orderId}</dd></div></dl>{pay.error && <p className="form-error" role="alert">支付确认失败，订单仍保留为待支付。</p>}</Dialog>
  </>
}
