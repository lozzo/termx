import { Activity } from 'lucide-react'
import { bytes, dateTime } from '../format'
import { GetAccountCommerceResponseSchema } from '../generated/cloud/v1/commerce_pb'
import { useProtoQuery } from '../query'
import { ErrorState, PageHeader, Skeleton } from '../ui'

export function UserUsagePage() {
  const query = useProtoQuery(['user', 'commerce'], '/api/commerce/me', GetAccountCommerceResponseSchema, 15_000)
  if (query.isPending) return <><PageHeader title="用量" /><Skeleton rows={5} /></>
  if (query.error || !query.data?.usage) return <ErrorState error={query.error} onRetry={() => void query.refetch()} />
  const value = query.data.usage
  const percentage = value.quotaBytes > 0n ? Math.min(100, Number(value.relayTotalBytes * 100n / value.quotaBytes)) : 0
  return <>
    <PageHeader title="Relay 用量" meta={`本计费周期：${dateTime(value.periodStart)} 至 ${dateTime(value.periodEnd)}`} />
    <section className="usage-hero"><Activity size={24} /><span>本周期 Relay 用量</span><strong>{bytes(value.relayTotalBytes)}</strong><div className="usage-progress" role="progressbar" aria-label="Relay 配额使用比例" aria-valuenow={percentage} aria-valuemin={0} aria-valuemax={100}><i style={{ width: `${percentage}%` }} /></div><small>{percentage}% · 剩余 {bytes(value.remainingBytes)}</small></section>
    <section className="usage-breakdown"><div><span>入口流量</span><strong>{bytes(value.relayIngressBytes)}</strong></div><div><span>出口流量</span><strong>{bytes(value.relayEgressBytes)}</strong></div><div><span>周期配额</span><strong>{bytes(value.quotaBytes)}</strong></div></section>
  </>
}
