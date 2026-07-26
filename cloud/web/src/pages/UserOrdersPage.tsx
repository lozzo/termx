import { attemptState, compactID, dateTime, money, orderState } from '../format'
import { GetAccountCommerceResponseSchema, OrderStatus } from '../generated/cloud/v1/commerce_pb'
import { useProtoQuery } from '../query'
import { Empty, ErrorState, PageHeader, Skeleton, Status, TableFrame } from '../ui'

export function UserOrdersPage() {
  const query = useProtoQuery(['user', 'commerce'], '/api/commerce/me', GetAccountCommerceResponseSchema, 15_000)
  if (query.isPending) return <><PageHeader title="我的订单" /><Skeleton /></>
  if (query.error) return <ErrorState error={query.error} />
  const attempts = new Map(query.data?.paymentAttempts.map((value) => [value.orderId, value]))
  return <>
    <PageHeader title="我的订单" meta="订单和支付状态来自 Controller 持久交易记录" />
    {!query.data?.orders.length ? <Empty>还没有订单。</Empty> : <div className="user-data-table"><TableFrame><table><thead><tr><th>订单</th><th>套餐</th><th>状态</th><th>金额</th><th>支付</th><th>创建时间</th></tr></thead><tbody>{query.data.orders.map((value) => { const attempt = attempts.get(value.orderId); return <tr key={value.orderId}><td data-label="订单"><strong className="mono">{compactID(value.orderId)}</strong><small className="mono">{value.idempotencyKey}</small></td><td data-label="套餐">{value.planId}<small>v{value.planVersion.toString()}</small></td><td data-label="状态"><Status active={value.status === OrderStatus.PAID}>{orderState(value.status)}</Status></td><td data-label="金额">{money(value.amount?.currency ?? 'CNY', value.amount?.minorUnits ?? 0n)}</td><td data-label="支付">{attempt ? attemptState(attempt.status) : '-'}<small>{value.provider}</small></td><td data-label="创建时间">{dateTime(value.createdAt)}</td></tr> })}</tbody></table></TableFrame></div>}
  </>
}
