import { Activity, Database, RadioTower } from 'lucide-react'
import { dateTime } from '../format'
import { GetOperatorOverviewResponseSchema } from '../generated/cloud/v1/operator_pb'
import { useProtoQuery } from '../query'
import { ErrorState, PageHeader, Skeleton, Status } from '../ui'

export function SystemPage() {
  const query = useProtoQuery(['overview'], '/api/operator/overview', GetOperatorOverviewResponseSchema, 5_000)
  if (query.isPending) return <Skeleton />
  if (query.error || !query.data?.overview) return <ErrorState error={query.error} onRetry={() => void query.refetch()} />
  const value = query.data.overview
  return <><PageHeader title="系统" meta="当前 Controller generation 与依赖状态" /><div className="system-status"><section><Activity size={18} /><div><span>Controller</span><strong><Status active>在线</Status></strong></div><small className="mono">{value.controllerInstanceId}</small></section><section><RadioTower size={18} /><div><span>Edge 控制流</span><strong>{value.edgeOnline.toString()} / {value.edgeTotal.toString()}</strong></div><small>最近投影：{dateTime(value.generatedAt)}</small></section><section><Database size={18} /><div><span>持久数据</span><strong><Status active>可查询</Status></strong></div><small>账号、交易、配置与审计</small></section></div></>
}
