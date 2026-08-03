import { Activity } from 'lucide-react'
import { bytes, dateTime } from '../format'
import { GetAccountCommerceResponseSchema } from '../generated/cloud/v1/commerce_pb'
import { useProtoQuery } from '../query'
import { ErrorState, Notice, PageHeader, Skeleton } from '../ui'

export function UserUsagePage() {
  const query = useProtoQuery(['user', 'commerce'], '/api/commerce/me', GetAccountCommerceResponseSchema, 15_000)
  if (query.isPending) return <><PageHeader title="用量" /><Skeleton rows={5} /></>
  if (query.error || !query.data?.usage) return <ErrorState error={query.error} onRetry={() => void query.refetch()} />
  const value = query.data.usage
  const committedAndHeld = value.relayTotalBytes + value.relayHeldBytes
  const percentage = value.quotaBytes > 0n ? Math.min(100, Number(committedAndHeld * 100n / value.quotaBytes)) : 0
  const threshold = percentage >= 100 ? 'error' : percentage >= 80 ? 'warning' : percentage >= 50 ? 'info' : undefined
  return <>
    <PageHeader title="Relay 用量" meta={`本计费周期：${dateTime(value.periodStart)} 至 ${dateTime(value.periodEnd)}`} />
    {threshold && <Notice tone={threshold}>{percentage >= 100 ? '本周期 Relay 流量已用完。P2P、Local、SSH 和 Direct 仍可使用。' : percentage >= 80 ? '本周期 Relay 流量即将用完，请检查套餐或减少大文件中转。' : '本周期 Relay 流量已使用一半。'}</Notice>}
    <section className="usage-hero"><Activity size={24} /><span>本周期 Relay 用量（当前按入口 + 出口统计）</span><strong>{bytes(value.relayTotalBytes)}</strong><div className="usage-progress" role="progressbar" aria-label="Relay 配额使用比例" aria-valuenow={percentage} aria-valuemin={0} aria-valuemax={100}><i style={{ width: `${percentage}%` }} /></div><small>{percentage}% · 剩余 {bytes(value.remainingBytes)}</small></section>
    <section className="usage-breakdown"><div><span>入口流量</span><strong>{bytes(value.relayIngressBytes)}</strong></div><div><span>出口流量</span><strong>{bytes(value.relayEgressBytes)}</strong></div><div><span>Relay 预留</span><strong>{bytes(value.relayHeldBytes)}</strong></div><div><span>周期配额</span><strong>{bytes(value.quotaBytes)}</strong></div></section>
  </>
}
