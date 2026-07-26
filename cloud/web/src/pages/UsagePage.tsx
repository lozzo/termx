import { Download } from 'lucide-react'
import { bytes, compactID, dateTime } from '../format'
import { ListOperatorUsageResponseSchema } from '../generated/cloud/v1/operator_pb'
import { CursorPagination, pageURL, useCursorPagination } from '../pagination'
import { useProtoQuery } from '../query'
import { Button, Empty, ErrorState, PageHeader, Skeleton, TableFrame } from '../ui'

export function UsagePage() {
  const pagination = useCursorPagination()
  const query = useProtoQuery(['usage', { cursor: pagination.cursor }], pageURL('/api/operator/usage', pagination.cursor), ListOperatorUsageResponseSchema, 15_000)
  const exportCSV = () => {
    const rows = [['类型', '对象', '入口字节', '出口字节', '总量', '周期开始'], ...(query.data?.accounts.map((value) => ['账号', value.accountId, value.relayIngressBytes.toString(), value.relayEgressBytes.toString(), value.relayTotalBytes.toString(), dateTime(value.periodStart)]) ?? []), ...(query.data?.edges.map((value) => ['Edge', value.edgeId, value.ingressBytes.toString(), value.egressBytes.toString(), (value.ingressBytes + value.egressBytes).toString(), dateTime(value.periodStart)]) ?? [])]
    const blob = new Blob([rows.map((row) => row.map((cell) => `"${cell.replaceAll('"', '""')}"`).join(',')).join('\n')], { type: 'text/csv;charset=utf-8' })
    const link = document.createElement('a'); link.href = URL.createObjectURL(blob); link.download = 'muxvia-cloud-usage.csv'; link.click(); URL.revokeObjectURL(link.href)
  }
  return <>
    <PageHeader title="用量与结算" meta="只展示已由 Controller 幂等结算的持久累计值" actions={<Button onClick={exportCSV} disabled={!query.data}><Download size={16} />导出 CSV</Button>} />
    {query.isPending ? <Skeleton /> : query.error ? <ErrorState error={query.error} /> : <><section className="plain-section"><header><h2>账号账期</h2></header>{!query.data?.accounts.length ? <Empty>当前账期暂无账号用量</Empty> : <TableFrame><table><thead><tr><th>账号</th><th>周期</th><th>入口</th><th>出口</th><th>累计</th><th>配额</th><th>剩余</th></tr></thead><tbody>{query.data.accounts.map((value) => <tr key={value.accountId}><td className="mono">{compactID(value.accountId)}</td><td>{dateTime(value.periodStart)}<small>至 {dateTime(value.periodEnd)}</small></td><td>{bytes(value.relayIngressBytes)}</td><td>{bytes(value.relayEgressBytes)}</td><td><strong>{bytes(value.relayTotalBytes)}</strong></td><td>{bytes(value.quotaBytes)}</td><td>{bytes(value.remainingBytes)}</td></tr>)}</tbody></table></TableFrame>}</section><CursorPagination page={pagination.page} nextCursor={query.data?.nextCursor ?? ''} onPrevious={pagination.previous} onNext={pagination.next} /><section className="plain-section"><header><h2>Edge 聚合</h2></header>{!query.data?.edges.length ? <Empty>当前账期暂无 Edge 用量</Empty> : <TableFrame><table><thead><tr><th>Edge</th><th>区域</th><th>入口</th><th>出口</th><th>事件数</th><th>周期开始</th></tr></thead><tbody>{query.data.edges.map((value) => <tr key={value.edgeId}><td><strong>{value.edgeName}</strong><small className="mono">{compactID(value.edgeId)}</small></td><td>{value.region}</td><td>{bytes(value.ingressBytes)}</td><td>{bytes(value.egressBytes)}</td><td>{value.eventCount.toString()}</td><td>{dateTime(value.periodStart)}</td></tr>)}</tbody></table></TableFrame>}</section></>}
  </>
}
