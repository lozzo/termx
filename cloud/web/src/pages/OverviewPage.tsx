import { Activity, Cable, Network, Server } from 'lucide-react'
import { Link } from 'react-router'
import { GetOperatorOverviewResponseSchema } from '../generated/cloud/v1/operator_pb'
import { bytes, dateTime } from '../format'
import { useProtoQuery } from '../query'
import { ErrorState, PageHeader, Skeleton, Status } from '../ui'

export function OverviewPage() {
  const query = useProtoQuery(['overview'], '/api/operator/overview', GetOperatorOverviewResponseSchema, 5_000)
  if (query.isPending) return <><PageHeader title="总览" /><Skeleton rows={8} /></>
  if (query.error || !query.data?.overview) return <ErrorState error={query.error} onRetry={() => void query.refetch()} />
  const value = query.data.overview
  const metrics = [
    { label: '在线 Edge', value: `${value.edgeOnline}/${value.edgeTotal}`, icon: Server, to: '/app/admin/edges' },
    { label: '在线 daemon', value: `${value.daemonOnline}/${value.daemonTotal}`, icon: Network, to: '/app/admin/daemons' },
    { label: '实时连接', value: String(value.clientSessionOnline), icon: Cable, to: '/app/admin/connections' },
    { label: '当期 Relay', value: bytes(value.relayBytesCurrentPeriod), icon: Activity, to: '/app/admin/usage' },
  ]
  return <>
    <PageHeader title="总览" meta={`最近生成：${dateTime(value.generatedAt)}`} actions={<button className="sync-state" onClick={() => query.refetch()}><span />数据已同步</button>} />
    <section className="metric-strip">{metrics.map(({ label, value: metric, icon: Icon, to }) => <Link to={to} key={label}><Icon size={18} /><span>{label}</span><strong>{metric}</strong></Link>)}</section>
    <div className="overview-grid">
      <section className="plain-section"><header><div><h2>控制面状态</h2><p>只表示当前进程 generation</p></div></header><dl className="detail-list"><div><dt>Controller</dt><dd><Status active>在线</Status></dd></div><div><dt>实例 ID</dt><dd className="mono">{value.controllerInstanceId}</dd></div><div><dt>在线 Edge</dt><dd>{value.edgeOnline ? <Status active>{value.edgeOnline} 个已同步</Status> : <Status active={false}>暂无已同步节点</Status>}</dd></div></dl></section>
    </div>
  </>
}
